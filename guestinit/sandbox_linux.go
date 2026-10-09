//go:build linux

package guestinit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const sandboxProxyAddress = ":10780"
const sandboxProxyHeaderLimit = 4096
const sandboxProxyConnectionLimit = 128
const sandboxFileLimit int64 = 1 << 30

var sandboxInit bool

// RunSandbox is the SA2 init entrypoint. Its config must specify protocol:1.
// Helper modes are dispatched before config loading, just as in Run.
func RunSandbox() { sandboxInit = true; Run() }

func validateSandboxConfig(cfg config) error {
	if cfg.Protocol != 0 && cfg.Protocol != SandboxProtocol {
		return fmt.Errorf("unsupported guest init protocol %d", cfg.Protocol)
	}
	if sandboxInit && cfg.Protocol != SandboxProtocol {
		return errors.New("SA2 init requires protocol 1")
	}
	return nil
}

func validateSandboxRequest(req execRequest) error {
	if req.SandboxProtocol != 0 && req.SandboxProtocol != SandboxProtocol {
		return fmt.Errorf("unsupported sandbox protocol %d", req.SandboxProtocol)
	}
	switch req.ProcessMode {
	case "":
	case "single", "group":
		if req.SandboxProtocol != SandboxProtocol {
			return errors.New("process_mode requires sandbox_protocol 1")
		}
	default:
		return fmt.Errorf("unsupported process mode %q", req.ProcessMode)
	}
	return nil
}

func writeSandboxEvent(cfg config, control io.Writer, id, kind, message string) {
	data, _ := json.Marshal(struct {
		Version int    `json:"version"`
		Kind    string `json:"kind"`
		Error   string `json:"error,omitempty"`
	}{SandboxProtocol, kind, message})
	writeExecControlBytes(cfg, control, id, data)
}

func runSandboxHelper(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "--sa2-protocol":
		if len(args) != 1 {
			return true, errors.New("usage: --sa2-protocol")
		}
		_, err := fmt.Fprintln(os.Stdout, SandboxProtocol)
		return true, err
	case "--sa2-file-get":
		if len(args) != 2 {
			return true, errors.New("usage: --sa2-file-get PATH")
		}
		if !filepath.IsAbs(args[1]) {
			return true, errors.New("file-get requires an absolute path")
		}
		return true, sandboxFileGet(args[1], os.Stdout)
	case "--sa2-file-put":
		if len(args) != 3 || (args[2] != "true" && args[2] != "false") {
			return true, errors.New("usage: --sa2-file-put PATH true|false")
		}
		if !filepath.IsAbs(args[1]) {
			return true, errors.New("file-put requires an absolute path")
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		defer stop()
		// Inherited os.Stdin may use a blocking syscall that Close cannot wake.
		// Register a nonblocking duplicate with Go's poller so signal cancellation
		// reliably interrupts a pending read without waiting for host EOF.
		fd, err := syscall.Dup(int(os.Stdin.Fd()))
		if err != nil {
			return true, err
		}
		if err := syscall.SetNonblock(fd, true); err != nil {
			_ = syscall.Close(fd)
			return true, err
		}
		input := os.NewFile(uintptr(fd), "sa2-file-put-stdin")
		defer input.Close()
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				_ = input.Close()
			case <-done:
			}
		}()
		err = sandboxFilePut(ctx, args[1], args[2] == "true", input)
		close(done)
		return true, err
	}
	return false, nil
}

func sandboxFileGet(path string, output io.Writer) error {
	// O_NONBLOCK prevents opening a FIFO from hanging before the regular-file check.
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("file-get requires a regular file")
	}
	_, err = io.Copy(output, file)
	return err
}

func sandboxFilePut(ctx context.Context, path string, overwrite bool, input io.Reader) error {
	path = filepath.Clean(path)
	if info, err := os.Lstat(path); err == nil {
		if !overwrite {
			return os.ErrExist
		}
		if !info.Mode().IsRegular() {
			return errors.New("file-put destination is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	stage, err := os.CreateTemp(filepath.Dir(path), ".sa2-file-put-*")
	if err != nil {
		return err
	}
	defer os.Remove(stage.Name())
	defer stage.Close()
	n, err := io.Copy(stage, io.LimitReader(input, sandboxFileLimit+1))
	if err != nil {
		return err
	}
	if n > sandboxFileLimit {
		return errors.New("file-put exceeds 1 GiB")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := stage.Sync(); err != nil {
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if overwrite {
		return os.Rename(stage.Name(), path)
	}
	// Atomic no-clobber installation also works on guest filesystems without
	// hardlinks (such as SCS mounts). Do not fall back to a check-then-rename or
	// link: unsupported RENAME_NOREPLACE must fail closed and remove the stage.
	return unix.Renameat2(unix.AT_FDCWD, stage.Name(), unix.AT_FDCWD, path, unix.RENAME_NOREPLACE)
}

type sandboxProxyRequest struct {
	Version int    `json:"version"`
	Host    string `json:"host"`
	Port    string `json:"port"`
}

func startSandboxProxy() (func(), error) {
	listener, err := net.Listen("tcp", sandboxProxyAddress)
	if err != nil {
		return nil, fmt.Errorf("listen SA2 proxy: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stop := serveSandboxProxy(ctx, listener)
	return func() { cancel(); stop() }, nil
}

// serveSandboxProxy limits concurrent handlers, including slow header senders.
// Closing either endpoint on cancellation unblocks both relay goroutines.
func serveSandboxProxy(ctx context.Context, listener net.Listener) func() {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	slots := make(chan struct{}, sandboxProxyConnectionLimit)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			select {
			case slots <- struct{}{}:
				wg.Add(1)
				go func() { defer wg.Done(); defer func() { <-slots }(); handleSandboxProxy(ctx, conn) }()
			default:
				_ = conn.Close()
			}
		}
	}()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-done:
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { cancel(); _ = listener.Close(); wg.Wait(); close(done) }) }
}

func handleSandboxProxy(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-finished:
		}
	}()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReaderSize(conn, sandboxProxyHeaderLimit)
	line, err := reader.ReadSlice('\n')
	reply := func(err error) bool {
		response := struct {
			Version int    `json:"version"`
			Error   string `json:"error,omitempty"`
		}{Version: SandboxProtocol}
		if err != nil {
			response.Error = err.Error()
		}
		return json.NewEncoder(conn).Encode(response) == nil
	}
	if err != nil {
		reply(errors.New("invalid or oversized proxy header"))
		return
	}
	var request sandboxProxyRequest
	if err := json.Unmarshal(line, &request); err != nil {
		reply(err)
		return
	}
	port, err := strconv.Atoi(request.Port)
	if request.Version != SandboxProtocol || request.Host == "" || err != nil || port < 1 || port > 65535 {
		reply(errors.New("proxy requires version 1, nonempty host and port 1..65535"))
		return
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	remote, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(request.Host, request.Port))
	if err != nil {
		reply(err)
		return
	}
	defer remote.Close()
	if !reply(nil) {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	remoteDone := make(chan struct{})
	defer close(remoteDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = remote.Close()
		case <-remoteDone:
		}
	}()
	copied := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(remote, reader)
		if tcp, ok := remote.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		copied <- struct{}{}
	}()
	_, _ = io.Copy(conn, remote)
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}
	// Preserve TCP half-closes: the peer may still be sending after EOF.
	<-copied
}
