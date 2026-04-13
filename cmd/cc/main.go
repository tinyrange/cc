package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"

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
		return fmt.Errorf("usage: cc [flags] <command>\ncommands: daemon-stop, kernel-status, kernel-download, image-list, image-get <name>, pull <name> <source>, vm-supported, vm-status, vm-start <image> [cmd...], vm-stop, run <image> <cmd...>")
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
		state, err := api.VMStatus()
		if err != nil {
			return err
		}
		return printJSON(state)
	case "vm-start":
		if len(args) < 2 {
			return fmt.Errorf("usage: cc vm-start <image> [cmd...]")
		}
		state, err := api.StartVM(client.StartVMRequest{
			Image:   args[1],
			Command: append([]string(nil), args[2:]...),
		})
		if err != nil {
			return err
		}
		return printJSON(state)
	case "vm-stop":
		return api.ShutdownVM()
	case "run":
		if len(args) < 3 {
			return fmt.Errorf("usage: cc run <image> <cmd...>")
		}
		resp, err := api.RunVM(client.StartVMRequest{
			Image:   args[1],
			Command: append([]string(nil), args[2:]...),
		})
		if err != nil {
			return err
		}
		if resp.Output != "" {
			fmt.Fprintln(os.Stdout, resp.Output)
		}
		if resp.ExitCode != 0 {
			return fmt.Errorf("guest command exited with status %d", resp.ExitCode)
		}
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
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
