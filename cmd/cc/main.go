package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"

	"j5.nz/cc/client"
)

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
		return fmt.Errorf("usage: cc [flags] <command>\ncommands: kernel-status, kernel-download, image-list, image-get <name>, pull <name> <source>, vm-supported, vm-status, vm-start <image>, vm-stop")
	}

	ccvmBinary, err := resolveCCVMPath(*ccvmPath)
	if err != nil {
		return err
	}

	api, proc, err := startBackend(ccvmBinary, *cacheDir)
	if err != nil {
		return err
	}
	defer func() {
		_ = api.Shutdown()
		_ = proc.Wait()
	}()

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
		if len(args) != 2 {
			return fmt.Errorf("usage: cc vm-start <image>")
		}
		state, err := api.StartVM(client.StartVMRequest{Image: args[1]})
		if err != nil {
			return err
		}
		return printJSON(state)
	case "vm-stop":
		return api.ShutdownVM()
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

func startBackend(ccvmPath, cacheDir string) (*client.Client, *exec.Cmd, error) {
	args := []string{}
	if cacheDir != "" {
		args = append(args, "-cache-dir", cacheDir)
	}

	proc := exec.Command(ccvmPath, args...)
	proc.Stderr = os.Stderr

	stdout, err := proc.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}

	if err := proc.Start(); err != nil {
		return nil, nil, err
	}

	var hello client.ServerHello
	if err := json.NewDecoder(stdout).Decode(&hello); err != nil {
		_ = proc.Wait()
		return nil, nil, err
	}

	api := client.NewClient("http://"+hello.Addr, func() (net.Conn, error) {
		return net.Dial("tcp", hello.Addr)
	})
	if err := api.HealthCheck(); err != nil {
		_ = proc.Wait()
		return nil, nil, err
	}

	return api, proc, nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
