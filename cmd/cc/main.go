package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
	"j5.nz/cc/client"
)

type daemonState struct {
	Addr string `json:"addr"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cc:", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	ccvmPath := fs.String("ccvm", "", "Path to ccvm binary")
	cacheDir := fs.String("cache-dir", "", "Cache directory")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	args := fs.Args()
	if len(args) == 0 {
		return fmt.Errorf("usage: cc [flags] <command>\ncommands: daemon-stop, kernel-status, kernel-download, image-list, image-get <name>, pull <name> <source>, vm-supported, vm-status, vm-start <image>, vm-stop, run <image> <cmd...>")
	}

	rootCache, err := resolveCacheDir(*cacheDir)
	if err != nil {
		return err
	}
	statePath := filepath.Join(rootCache, "ccvm.json")

	ccvmBinary, err := resolveCCVMPath(*ccvmPath)
	if err != nil {
		return err
	}

	if args[0] == "daemon-stop" {
		return stopDaemon(statePath)
	}

	api, err := connectBackend(ccvmBinary, rootCache, statePath)
	if err != nil {
		return err
	}

	switch args[0] {
	case "kernel-status":
		state, err := api.KernelStatus()
		if err != nil {
			return err
		}
		return printJSON(state)
	case "kernel-download":
		return api.DownloadKernel(client.DownloadRequest{})
	case "image-list":
		images, err := api.ListImages()
		if err != nil {
			return err
		}
		return printJSON(images)
	case "image-get":
		if len(args) != 2 {
			return fmt.Errorf("usage: cc image-get <name>")
		}
		image, err := api.GetImage(args[1])
		if err != nil {
			return err
		}
		return printJSON(image)
	case "pull":
		if len(args) != 3 {
			return fmt.Errorf("usage: cc pull <name> <source>")
		}
		return api.PullImage(args[1], client.PullImageRequest{Source: args[2]})
	case "vm-supported":
		supported, err := api.VMSupported()
		if err != nil {
			return err
		}
		return printJSON(supported)
	case "vm-status":
		state, err := api.InstanceStatus()
		if err != nil {
			return err
		}
		return printJSON(state)
	case "vm-start":
		if len(args) != 2 {
			return fmt.Errorf("usage: cc vm-start <image>")
		}
		state, err := api.CreateInstance(client.CreateInstanceRequest{
			Image: args[1],
		})
		if err != nil {
			return err
		}
		return printJSON(state)
	case "vm-stop":
		return api.ShutdownInstance()
	case "run":
		if len(args) < 3 {
			return fmt.Errorf("usage: cc run <image> <cmd...>")
		}
		return runViaWebsocket(api, args[1], args[2:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runViaWebsocket(api *client.Client, image string, command []string) error {
	state, err := api.InstanceStatus()
	if err != nil {
		return err
	}

	startedHere := false
	switch state.Status {
	case "running":
		if state.Image != "" && state.Image != image {
			return fmt.Errorf("a VM for image %q is already running", state.Image)
		}
	case "stopped":
		if _, err := api.CreateInstance(client.CreateInstanceRequest{Image: image}); err != nil {
			return err
		}
		startedHere = true
	default:
		return fmt.Errorf("VM status %q is not ready for exec", state.Status)
	}

	if startedHere {
		defer api.ShutdownInstance()
	}

	ttyMode := isTerminal(os.Stdin) && isTerminal(os.Stdout)
	req := client.ExecRequest{
		Command: append([]string(nil), command...),
		TTY:     ttyMode,
	}
	if ttyMode {
		if cols, rows, err := terminalSize(os.Stdout); err == nil {
			req.Cols = cols
			req.Rows = rows
		}
	}

	exitCode := 0
	inputs := make(chan client.ExecInput, 16)
	done := make(chan struct{})
	var producers sync.WaitGroup
	producers.Add(1)
	go func() {
		defer producers.Done()
		_ = streamHostStdin(os.Stdin, inputs)
	}()
	producers.Add(1)
	go func() {
		defer producers.Done()
		forwardHostControl(inputs, done, ttyMode, os.Stdout)
	}()
	go func() {
		producers.Wait()
		close(inputs)
	}()

	if err := api.ExecStream(req, inputs, func(event client.ExecEvent) error {
		switch event.Kind {
		case "stdout":
			if len(event.Data) > 0 {
				_, _ = os.Stdout.Write(event.Data)
			} else if event.Output != "" {
				_, _ = fmt.Fprint(os.Stdout, event.Output)
			}
		case "stderr":
			if len(event.Data) > 0 {
				_, _ = os.Stderr.Write(event.Data)
			} else if event.Output != "" {
				_, _ = fmt.Fprint(os.Stderr, event.Output)
			}
		case "error":
			return errors.New(event.Error)
		case "exit":
			exitCode = event.ExitCode
		}
		return nil
	}); err != nil {
		close(done)
		return err
	}
	close(done)
	if exitCode != 0 {
		return fmt.Errorf("guest command exited with status %d", exitCode)
	}
	return nil
}

func streamHostStdin(file *os.File, out chan<- client.ExecInput) error {
	if file == nil {
		return nil
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	var buf [4096]byte
	if info.Mode()&os.ModeCharDevice != 0 {
		for {
			n, err := file.Read(buf[:])
			if n > 0 {
				out <- client.ExecInput{Kind: "stdin", Data: append([]byte(nil), buf[:n]...)}
			}
			if err != nil {
				if err == io.EOF {
					out <- client.ExecInput{Kind: "stdin_close"}
					return nil
				}
				return err
			}
		}
	}
	for {
		n, err := file.Read(buf[:])
		if n > 0 {
			out <- client.ExecInput{Kind: "stdin", Data: append([]byte(nil), buf[:n]...)}
		}
		if err != nil {
			if err == io.EOF {
				out <- client.ExecInput{Kind: "stdin_close"}
				return nil
			}
			return err
		}
	}
}

func forwardHostControl(out chan<- client.ExecInput, done <-chan struct{}, tty bool, ttyFile *os.File) {
	signals := []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP}
	if tty {
		signals = append(signals, syscall.SIGWINCH)
	}
	sigCh := make(chan os.Signal, 8)
	signal.Notify(sigCh, signals...)
	defer signal.Stop(sigCh)

	for {
		select {
		case <-done:
			return
		case sig := <-sigCh:
			if sig == nil {
				continue
			}
			if sig == syscall.SIGWINCH {
				if ttyFile == nil {
					continue
				}
				cols, rows, err := terminalSize(ttyFile)
				if err != nil {
					continue
				}
				select {
				case <-done:
					return
				case out <- client.ExecInput{Kind: "resize", Cols: cols, Rows: rows}:
				}
				continue
			}
			name, ok := signalName(sig)
			if !ok {
				continue
			}
			select {
			case <-done:
				return
			case out <- client.ExecInput{Kind: "signal", Signal: name}:
			}
		}
	}
}

func isTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	return isTerminalFD(int(file.Fd()))
}

func terminalSize(file *os.File) (int, int, error) {
	ws, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, err
	}
	return int(ws.Col), int(ws.Row), nil
}

func signalName(sig os.Signal) (string, bool) {
	switch sig {
	case os.Interrupt:
		return "INT", true
	case syscall.SIGHUP:
		return "HUP", true
	case syscall.SIGQUIT:
		return "QUIT", true
	case syscall.SIGTERM:
		return "TERM", true
	default:
		return "", false
	}
}

func resolveCCVMPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}

	path = exePath + "vm"
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("ccvm binary not found at %s", path)
	}
	return path, nil
}

func connectBackend(ccvmPath, cacheDir, statePath string) (*client.Client, error) {
	if state, err := readDaemonState(statePath); err == nil {
		api := newClient(state.Addr)
		if err := api.HealthCheck(); err == nil {
			return api, nil
		}
		_ = os.Remove(statePath)
	}

	args := []string{"-cache-dir", cacheDir}
	proc := exec.Command(ccvmPath, args...)
	proc.Stderr = os.Stderr

	stdout, err := proc.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := proc.Start(); err != nil {
		return nil, err
	}

	var hello client.ServerHello
	if err := json.NewDecoder(stdout).Decode(&hello); err != nil {
		_ = proc.Wait()
		return nil, err
	}
	if err := writeDaemonState(statePath, daemonState{Addr: hello.Addr}); err != nil {
		_ = proc.Process.Kill()
		_ = proc.Wait()
		return nil, err
	}

	api := newClient(hello.Addr)
	if err := api.HealthCheck(); err != nil {
		_ = os.Remove(statePath)
		_ = proc.Process.Kill()
		_ = proc.Wait()
		return nil, err
	}
	return api, nil
}

func stopDaemon(statePath string) error {
	state, err := readDaemonState(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	api := newClient(state.Addr)
	if err := api.Shutdown(); err != nil {
		_ = os.Remove(statePath)
		return err
	}
	return os.Remove(statePath)
}

func newClient(addr string) *client.Client {
	return client.NewClient("http://"+addr, func() (net.Conn, error) {
		return net.Dial("tcp", addr)
	})
}

func readDaemonState(path string) (daemonState, error) {
	var state daemonState
	buf, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(buf, &state); err != nil {
		return state, err
	}
	if state.Addr == "" {
		return state, fmt.Errorf("daemon state missing address")
	}
	return state, nil
}

func writeDaemonState(path string, state daemonState) error {
	buf, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
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

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
