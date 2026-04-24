package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/websocket"
	"j5.nz/cc/client"
	intcvmfs "j5.nz/cc/internal/cvmfs"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/macos"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/vm"
)

var debugTiming = strings.TrimSpace(os.Getenv("CCX3_DEBUG_TIMING")) != ""

const vmBootTimeout = 30 * time.Second

func timingLog(format string, args ...any) {
	if !debugTiming {
		return
	}
	fmt.Fprintf(os.Stderr, "ccx3 timing: "+format+"\n", args...)
}

type server struct {
	kernel *alpine.Manager
	images *oci.Store
	vms    *vm.Manager
}

func main() {
	if err := macos.EnsureExecutableIsSigned(); err != nil {
		panic(err)
	}

	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	addr := fs.String("addr", "localhost:0", "Address to listen on")
	cacheDir := fs.String("cache-dir", "", "Cache directory")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	rootCache, err := resolveCacheDir(*cacheDir)
	if err != nil {
		panic(err)
	}

	srvState := &server{
		kernel: alpine.NewManager(filepath.Join(sharedRuntimeRoot(), "kernel")),
		images: oci.NewStore(filepath.Join(rootCache, "images")),
	}
	srvState.vms = vm.NewManagerWithBackend(vm.NewRuntimeBackend(srvState.kernel, srvState.images, filepath.Join(sharedRuntimeRoot(), "guestinit")))

	l, err := net.Listen("tcp", *addr)
	if err != nil {
		panic(err)
	}

	if err := json.NewEncoder(os.Stdout).Encode(client.ServerHello{
		Addr: l.Addr().String(),
	}); err != nil {
		panic(err)
	}

	var httpServer http.Server
	mux := newMux(srvState, &httpServer)

	httpServer = http.Server{Handler: mux}
	if err := httpServer.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}

func newMux(srvState *server, httpServer *http.Server) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /capabilities", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, srvState.vms.Capabilities())
	})

	mux.HandleFunc("POST /shutdown", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		go func() {
			time.Sleep(10 * time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if httpServer != nil {
				_ = httpServer.Shutdown(ctx)
			}
		}()
	})

	mux.HandleFunc("GET /kernel", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, srvState.kernel.Status())
	})

	mux.HandleFunc("POST /kernel/download", func(w http.ResponseWriter, r *http.Request) {
		var req client.DownloadRequest
		if err := decodeOptionalJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if wantsProgressStream(r) {
			report := func(event client.ProgressEvent) {
				_ = writeProgressEvent(w, event)
			}
			if err := srvState.kernel.EnsureWithProgress(r.Context(), report); err != nil {
				_ = writeProgressEvent(w, client.ProgressEvent{Status: "error", Error: err.Error()})
			}
			return
		}
		if err := srvState.kernel.Ensure(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, srvState.kernel.Status())
	})

	mux.HandleFunc("GET /image", func(w http.ResponseWriter, r *http.Request) {
		images, err := srvState.images.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, images)
	})

	mux.HandleFunc("GET /image/{image}", func(w http.ResponseWriter, r *http.Request) {
		imageName := r.PathValue("image")
		state, err := srvState.images.Get(imageName)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, state)
	})

	mux.HandleFunc("POST /image/{image}/metadata", func(w http.ResponseWriter, r *http.Request) {
		imageName := r.PathValue("image")
		image, err := srvState.images.Open(imageName)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, client.ImageMetadataState{
			Name:         image.Name,
			Status:       "prepared",
			SourceKind:   image.SourceKind,
			Architecture: image.Architecture,
		})
	})

	mux.HandleFunc("POST /image/{image}/qemu/download", func(w http.ResponseWriter, r *http.Request) {
		imageName := r.PathValue("image")
		image, err := srvState.images.Open(imageName)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if !vm.NeedsAMD64Emulation(image) {
			writeJSON(w, http.StatusOK, client.EmulatorState{Status: "skipped", Required: false})
			return
		}
		if wantsProgressStream(r) {
			report := func(event client.ProgressEvent) {
				_ = writeProgressEvent(w, event)
			}
			path, err := srvState.kernel.ExtractPackageFileWithProgress(
				r.Context(),
				"community",
				"qemu-x86_64",
				"usr/bin/qemu-x86_64",
				report,
			)
			if err != nil {
				_ = writeProgressEvent(w, client.ProgressEvent{Status: "error", Error: err.Error()})
				return
			}
			_ = writeProgressEvent(w, client.ProgressEvent{Status: "downloaded", Artifact: filepath.Base(path)})
			return
		}
		path, err := vm.PrepareAMD64Emulator(r.Context(), image, srvState.kernel.ExtractPackageFile)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, client.EmulatorState{
			Status:   "downloaded",
			Path:     path,
			Required: true,
		})
	})

	mux.HandleFunc("POST /image/{image}", func(w http.ResponseWriter, r *http.Request) {
		imageName := r.PathValue("image")
		var req client.PullImageRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		source, err := req.SourceString()
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		state, err := srvState.images.Pull(r.Context(), imageName, source)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, state)
	})

	mux.HandleFunc("POST /cvmfs/list", func(w http.ResponseWriter, r *http.Request) {
		var req client.CVMFSListRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		cvmfsClient := intcvmfs.NewClient()
		cvmfsClient.CacheDir = strings.TrimSpace(req.CacheDir)
		target := cvmfsTarget(req.Mirror, req.Repo, req.Path)
		entries, err := cvmfsClient.ReadDir(target)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		resp := client.CVMFSListResponse{Entries: make([]client.CVMFSDirectoryEntry, 0, len(entries))}
		basePath := ensureAbsolutePath(req.Path)
		for _, entry := range entries {
			kind := "file"
			if entry.Mode.IsDir() {
				kind = "directory"
			} else if entry.Mode&fs.ModeSymlink != 0 {
				kind = "symlink"
			}
			resp.Entries = append(resp.Entries, client.CVMFSDirectoryEntry{
				Name: entry.Name,
				Path: pathJoin(basePath, entry.Name),
				Kind: kind,
				Size: entry.Size,
			})
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("POST /cvmfs/read", func(w http.ResponseWriter, r *http.Request) {
		var req client.CVMFSReadRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		cvmfsClient := intcvmfs.NewClient()
		cvmfsClient.CacheDir = strings.TrimSpace(req.CacheDir)
		target := cvmfsTarget(req.Mirror, req.Repo, req.Path)
		data, eof, err := cvmfsClient.ReadFileRange(target, req.Offset, req.Length)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, client.CVMFSReadResponse{
			Path:   ensureAbsolutePath(req.Path),
			Offset: req.Offset,
			Data:   data,
			EOF:    eof,
		})
	})

	mux.HandleFunc("GET /vm/supported", func(w http.ResponseWriter, r *http.Request) {
		err := vm.Supports()
		resp := client.VMSupportedResponse{Supported: err == nil}
		if err != nil {
			resp.Error = err.Error()
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("GET /vm/status", func(w http.ResponseWriter, r *http.Request) {
		if id := strings.TrimSpace(r.URL.Query().Get("id")); id != "" {
			writeJSON(w, http.StatusOK, srvState.vms.StatusOf(id))
			return
		}
		writeJSON(w, http.StatusOK, srvState.vms.Status())
	})
	mux.HandleFunc("GET /vm", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, srvState.vms.Statuses())
	})
	mux.HandleFunc("POST /vm/start", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		bootCtx, cancel := context.WithTimeout(r.Context(), vmBootTimeout)
		defer cancel()
		var req client.StartInstanceRequest
		if err := decodeOptionalJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		timingLog("POST /vm/start decode took=%s", time.Since(start))
		if srvState.kernel.Status().Status != "downloaded" {
			if wantsBootEventStream(r) {
				writeBootEvent(w, client.BootEvent{Kind: "status", Message: "ensuring kernel is available"})
			}
			if err := srvState.kernel.Ensure(bootCtx); err != nil {
				if wantsBootEventStream(r) {
					if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
						writeBootEvent(w, client.BootEvent{Kind: "error", Error: fmt.Sprintf("vm boot timed out after %s", vmBootTimeout)})
						return
					}
					writeBootEvent(w, client.BootEvent{Kind: "error", Error: err.Error()})
					return
				}
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
					writeError(w, http.StatusGatewayTimeout, fmt.Errorf("vm boot timed out after %s", vmBootTimeout))
					return
				}
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		timingLog("POST /vm/start kernel ensure/status took=%s", time.Since(start))
		if wantsBootEventStream(r) {
			writeBootEvent(w, client.BootEvent{Kind: "status", Message: "starting VM"})
			state, err := srvState.vms.StartBlankStream(bootCtx, req, func(event client.BootEvent) error {
				return writeBootEvent(w, event)
			})
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
					_ = writeBootEvent(w, client.BootEvent{Kind: "error", Error: fmt.Sprintf("vm boot timed out after %s", vmBootTimeout)})
					return
				}
				_ = writeBootEvent(w, client.BootEvent{Kind: "error", Error: err.Error()})
				return
			}
			timingLog("POST /vm/start vms.StartBlankStream took=%s", time.Since(start))
			_ = writeBootEvent(w, client.BootEvent{Kind: "ready", State: state})
			timingLog("POST /vm/start total=%s", time.Since(start))
			return
		}
		state, err := srvState.vms.StartBlank(bootCtx, req)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
				writeError(w, http.StatusGatewayTimeout, fmt.Errorf("vm boot timed out after %s", vmBootTimeout))
				return
			}
			writeError(w, http.StatusBadRequest, err)
			return
		}
		timingLog("POST /vm/start vms.StartBlank took=%s", time.Since(start))
		writeJSON(w, http.StatusOK, state)
		timingLog("POST /vm/start total=%s", time.Since(start))
	})
	mux.HandleFunc("POST /vm", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		bootCtx, cancel := context.WithTimeout(r.Context(), vmBootTimeout)
		defer cancel()
		var req client.CreateInstanceRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		timingLog("POST /vm decode took=%s image=%q", time.Since(start), req.Image)
		if _, err := srvState.images.Get(req.Image); err != nil {
			if wantsBootEventStream(r) {
				writeBootEvent(w, client.BootEvent{Kind: "error", Error: fmt.Sprintf("image %q is not available", req.Image)})
				return
			}
			writeError(w, http.StatusBadRequest, fmt.Errorf("image %q is not available", req.Image))
			return
		}
		timingLog("POST /vm image lookup took=%s", time.Since(start))
		if wantsBootEventStream(r) {
			writeBootEvent(w, client.BootEvent{Kind: "status", Message: fmt.Sprintf("validated image %s", req.Image)})
		}
		if srvState.kernel.Status().Status != "downloaded" {
			if wantsBootEventStream(r) {
				writeBootEvent(w, client.BootEvent{Kind: "status", Message: "ensuring kernel is available"})
			}
			if err := srvState.kernel.Ensure(bootCtx); err != nil {
				if wantsBootEventStream(r) {
					if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
						writeBootEvent(w, client.BootEvent{Kind: "error", Error: fmt.Sprintf("vm boot timed out after %s", vmBootTimeout)})
						return
					}
					writeBootEvent(w, client.BootEvent{Kind: "error", Error: err.Error()})
					return
				}
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
					writeError(w, http.StatusGatewayTimeout, fmt.Errorf("vm boot timed out after %s", vmBootTimeout))
					return
				}
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		timingLog("POST /vm kernel ensure/status took=%s", time.Since(start))
		if wantsBootEventStream(r) {
			writeBootEvent(w, client.BootEvent{Kind: "status", Message: fmt.Sprintf("starting VM for %s", req.Image)})
			state, err := srvState.vms.StartStream(bootCtx, req, func(event client.BootEvent) error {
				return writeBootEvent(w, event)
			})
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
					_ = writeBootEvent(w, client.BootEvent{Kind: "error", Error: fmt.Sprintf("vm boot timed out after %s", vmBootTimeout)})
					return
				}
				_ = writeBootEvent(w, client.BootEvent{Kind: "error", Error: err.Error()})
				return
			}
			timingLog("POST /vm vms.StartStream took=%s", time.Since(start))
			_ = writeBootEvent(w, client.BootEvent{Kind: "ready", State: state})
			timingLog("POST /vm total=%s", time.Since(start))
			return
		}
		state, err := srvState.vms.Start(bootCtx, req)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
				writeError(w, http.StatusGatewayTimeout, fmt.Errorf("vm boot timed out after %s", vmBootTimeout))
				return
			}
			writeError(w, http.StatusBadRequest, err)
			return
		}
		timingLog("POST /vm vms.Start took=%s", time.Since(start))
		writeJSON(w, http.StatusOK, state)
		timingLog("POST /vm total=%s", time.Since(start))
	})
	mux.HandleFunc("POST /vm/shutdown", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if err := srvState.vms.ShutdownInstance(r.Context(), id); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, srvState.vms.StatusOf(id))
	})
	mux.HandleFunc("POST /vm/run", func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeRunRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.Image != "" {
			if _, err := srvState.images.Open(req.Image); err != nil {
				writeError(w, http.StatusBadRequest, fmt.Errorf("image %q is not available", req.Image))
				return
			}
		}
		if srvState.kernel.Status().Status != "downloaded" && (req.Image != "" || srvState.vms.Status().Status == "running") {
			if err := srvState.kernel.Ensure(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		resp, err := srvState.vms.Run(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if wantsExecEventStream(r) {
			writeExecEventStream(w, resp)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})
	mux.Handle("/vm/run", websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			serveRunWebSocket(ws, func(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
				return srvState.vms.Stream(ctx, req, inputs, onEvent)
			})
		},
	})
	return mux
}

func resolveCacheDir(arg string) (string, error) {
	if arg != "" {
		return arg, os.MkdirAll(arg, 0o755)
	}
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	dir := filepath.Join(userCacheDir, "ccx3")
	return dir, os.MkdirAll(dir, 0o755)
}

func sharedRuntimeRoot() string {
	if root := strings.TrimSpace(os.Getenv("CCX3_RUNTIME_SHARED_CACHE_DIR")); root != "" {
		return root
	}
	userCacheDir, err := os.UserCacheDir()
	if err != nil || userCacheDir == "" {
		return filepath.Join(os.TempDir(), "ccx3-runtime")
	}
	return filepath.Join(userCacheDir, "ccx3", "runtime")
}

func decodeRequiredJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return fmt.Errorf("request body is required")
	}
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func decodeOptionalJSON(r *http.Request, dst any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func decodeRunRequest(r *http.Request) (client.RunRequest, error) {
	var req client.RunRequest
	if err := decodeRequiredJSON(r, &req); err != nil {
		return req, err
	}
	return req, nil
}

func serveRunWebSocket(ws *websocket.Conn, runner func(context.Context, client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error) {
	defer ws.Close()

	var req client.ExecRequest
	if err := websocket.JSON.Receive(ws, &req); err != nil {
		_ = websocket.JSON.Send(ws, client.ExecEvent{Kind: "error", Error: fmt.Sprintf("decode exec request: %v", err)})
		return
	}

	inputs := make(chan client.ExecInput, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		defer close(inputs)
		for {
			var input client.ExecInput
			if err := websocket.JSON.Receive(ws, &input); err != nil {
				return
			}
			inputs <- input
		}
	}()

	err := runner(ctx, req, inputs, func(event client.ExecEvent) error {
		return websocket.JSON.Send(ws, event)
	})
	if err != nil {
		_ = websocket.JSON.Send(ws, client.ExecEvent{Kind: "error", Error: err.Error()})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func wantsExecEventStream(r *http.Request) bool {
	if r.URL.Query().Get("stream") == "1" {
		return true
	}
	return r.Header.Get("Accept") == "application/x-ndjson"
}

func wantsBootEventStream(r *http.Request) bool {
	if r.URL.Query().Get("stream") == "1" {
		return true
	}
	return r.Header.Get("Accept") == "application/x-ndjson"
}

func wantsProgressStream(r *http.Request) bool {
	if r.URL.Query().Get("stream") == "1" {
		return true
	}
	return r.Header.Get("Accept") == "application/x-ndjson"
}

func writeExecEventStream(w http.ResponseWriter, resp client.ExecResponse) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	if resp.Output != "" {
		_ = enc.Encode(client.ExecEvent{Kind: "output", Output: resp.Output})
	}
	_ = enc.Encode(client.ExecEvent{Kind: "exit", ExitCode: resp.ExitCode})
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeBootEvent(w http.ResponseWriter, event client.BootEvent) error {
	w.Header().Set("Content-Type", "application/x-ndjson")
	if event.Kind == "" {
		event.Kind = "status"
	}
	if _, ok := w.(http.Flusher); ok {
		// WriteHeader is safe to call repeatedly before the first write.
		w.WriteHeader(http.StatusOK)
	}
	if err := json.NewEncoder(w).Encode(event); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func writeProgressEvent(w http.ResponseWriter, event client.ProgressEvent) error {
	w.Header().Set("Content-Type", "application/x-ndjson")
	if _, ok := w.(http.Flusher); ok {
		w.WriteHeader(http.StatusOK)
	}
	if err := json.NewEncoder(w).Encode(event); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, client.ErrorResponse{Error: err.Error()})
}

func cvmfsTarget(mirror, repo, innerPath string) string {
	repo = strings.TrimSpace(repo)
	pathValue := ensureAbsolutePath(innerPath)
	mirror = strings.TrimRight(strings.TrimSpace(mirror), "/")
	if mirror == "" {
		return fmt.Sprintf("cvmfs://%s%s", repo, pathValue)
	}
	mirror = ensureCVMFSMirrorPath(mirror)
	return fmt.Sprintf("%s/%s%s", mirror, repo, pathValue)
}

func pathJoin(base, name string) string {
	base = ensureAbsolutePath(base)
	if base == "/" {
		return "/" + strings.TrimPrefix(name, "/")
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimPrefix(name, "/")
}

func ensureAbsolutePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if strings.HasPrefix(value, "/") {
		return value
	}
	return "/" + value
}

func ensureCVMFSMirrorPath(mirror string) string {
	mirror = strings.TrimRight(strings.TrimSpace(mirror), "/")
	u, err := url.Parse(mirror)
	if err != nil {
		if !strings.HasSuffix(mirror, "/cvmfs") {
			return mirror + "/cvmfs"
		}
		return mirror
	}
	if !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/cvmfs") {
		u.Path = strings.TrimRight(u.Path, "/") + "/cvmfs"
	}
	return strings.TrimRight(u.String(), "/")
}
