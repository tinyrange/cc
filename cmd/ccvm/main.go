package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/net/websocket"
	"j5.nz/cc/client"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/macos"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/vm"
)

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
		kernel: alpine.NewManager(filepath.Join(rootCache, "kernel")),
		images: oci.NewStore(filepath.Join(rootCache, "images")),
	}
	srvState.vms = vm.NewManagerWithBackend(vm.NewRuntimeBackend(srvState.kernel, srvState.images))

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

	mux.HandleFunc("POST /image/{image}", func(w http.ResponseWriter, r *http.Request) {
		imageName := r.PathValue("image")
		var req client.PullImageRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		state, err := srvState.images.Pull(r.Context(), imageName, req.Source)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, state)
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
		writeJSON(w, http.StatusOK, srvState.vms.Status())
	})
	mux.HandleFunc("POST /vm", func(w http.ResponseWriter, r *http.Request) {
		var req client.CreateInstanceRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if _, err := srvState.images.Get(req.Image); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("image %q is not available", req.Image))
			return
		}
		if srvState.kernel.Status().Status != "downloaded" {
			if err := srvState.kernel.Ensure(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		state, err := srvState.vms.Start(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, state)
	})
	mux.HandleFunc("POST /vm/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if err := srvState.vms.Shutdown(r.Context()); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, srvState.vms.Status())
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

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, client.ErrorResponse{Error: err.Error()})
}
