package main

import (
	"context"
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
	"strconv"
	"strings"
	"sync"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/cachepath"
	"j5.nz/cc/internal/fulltest"
)

type daemonState struct {
	Addr string `json:"addr"`
}

type ccAPI interface {
	DownloadKernelStream(client.DownloadRequest, func(client.ProgressEvent) error) error
	VMSupported() (client.VMSupportedResponse, error)
	ListImages() ([]client.ImageState, error)
	GetImage(string) (client.ImageState, error)
	PullImageStream(string, client.PullImageRequest, func(client.ProgressEvent) error) error
	CreateInstance(client.CreateInstanceRequest) (client.InstanceState, error)
	CreateInstanceStream(client.CreateInstanceRequest, func(client.BootEvent) error) (client.InstanceState, error)
	CreateInstanceStreamWithID(string, client.CreateInstanceRequest, func(client.BootEvent) error) (client.InstanceState, error)
	KernelStatus() (client.KernelState, error)
	InstanceStatus() (client.InstanceState, error)
	InstanceStatusOf(string) (client.InstanceState, error)
	InstanceStatuses() ([]client.InstanceState, error)
	ShutdownInstance() error
	ShutdownInstanceWithID(string) error
	AddPortForwardTo(string, client.PortForward) error
	RunIn(string, client.RunRequest) (client.ExecResponse, error)
	ExecStream(client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
	ExecStreamIn(string, client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
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
		return fmt.Errorf("usage: cc [flags] <command>\ncommands: doctor, images [name], pull <name> <source>, start <image>, stop, status, run <image> -- <cmd...>, fulltest [flags], vm <subcommand>")
	}

	rootCache, err := resolveCacheDir(*cacheDir)
	if err != nil {
		return err
	}
	statePath := filepath.Join(rootCache, "ccvm.json")

	if args[0] == "fulltest" && fulltestBackendFromArgs(args[1:]) == "docker" {
		return handleCommand(nil, args)
	}

	ccvmBinary, err := resolveCCVMPath(*ccvmPath)
	if err != nil {
		return err
	}
	api, err := connectBackend(ccvmBinary, rootCache, statePath)
	if err != nil {
		return err
	}
	return handleCommand(api, args)
}

func handleCommand(api ccAPI, args []string) error {
	switch args[0] {
	case "doctor":
		if len(args) != 1 {
			return fmt.Errorf("usage: cc doctor")
		}
		if err := api.DownloadKernelStream(client.DownloadRequest{}, progressEventReporter(os.Stderr, "kernel")); err != nil {
			return err
		}
		supported, err := api.VMSupported()
		if err != nil {
			return err
		}
		return printJSON(supported)
	case "images":
		switch len(args) {
		case 1:
			images, err := api.ListImages()
			if err != nil {
				return err
			}
			return printJSON(images)
		case 2:
			image, err := api.GetImage(args[1])
			if err != nil {
				return err
			}
			return printJSON(image)
		default:
			return fmt.Errorf("usage: cc images [name]")
		}
	case "pull":
		if len(args) != 3 {
			return fmt.Errorf("usage: cc pull <name> <source>")
		}
		return api.PullImageStream(args[1], client.PullImageRequest{Source: args[2]}, progressEventReporter(os.Stderr, args[1]))
	case "start":
		if len(args) != 2 {
			return fmt.Errorf("usage: cc start <image>")
		}
		state, err := api.CreateInstanceStream(client.CreateInstanceRequest{
			Image: args[1],
		}, bootEventReporter(os.Stderr))
		if err != nil {
			return err
		}
		return printJSON(state)
	case "stop":
		if len(args) != 1 {
			return fmt.Errorf("usage: cc stop")
		}
		return api.ShutdownInstance()
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("usage: cc status")
		}
		kernel, err := api.KernelStatus()
		if err != nil {
			return err
		}
		supported, err := api.VMSupported()
		if err != nil {
			return err
		}
		vm, err := api.InstanceStatus()
		if err != nil {
			return err
		}
		return printJSON(map[string]any{
			"kernel":       kernel,
			"vm_supported": supported,
			"vm":           vm,
		})
	case "run":
		if len(args) < 4 || args[2] != "--" {
			return fmt.Errorf("usage: cc run <image> -- <cmd...>")
		}
		return runViaWebsocket(api, args[1], args[3:])
	case "fulltest":
		return handleFullTestCommand(api, args[1:])
	case "vm":
		return handleVMCommand(api, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func handleFullTestCommand(api ccAPI, args []string) error {
	fs := flag.NewFlagSet("cc fulltest", flag.ExitOnError)
	recipe := fs.String("recipe", filepath.Join("suites", "shell", "fulltest.yaml"), "Fulltest YAML recipe")
	imageSource := fs.String("image-source", "", "Image source override")
	imageName := fs.String("image-name", "", "Image cache name")
	workDir := fs.String("work-dir", "", "Host work directory mounted at /work")
	filter := fs.String("filter", "", "Run tests whose names contain this text")
	keepVM := fs.Bool("keep-vm", false, "Leave the fulltest VM running")
	mirror := fs.String("mirror", fulltest.DefaultCVMFSMirror, "CVMFS mirror")
	repo := fs.String("repo", fulltest.DefaultCVMFSRepo, "CVMFS repository")
	cacheDir := fs.String("image-cache-dir", "", "CVMFS image cache directory")
	prefetch := fs.Bool("prefetch", false, "Prefetch CVMFS image contents")
	prefetchWorkers := fs.Int("prefetch-workers", 4, "CVMFS prefetch workers")
	memoryMB := fs.Uint64("memory-mb", fulltest.DefaultMemoryMB, "VM memory in MiB")
	cpus := fs.Int("cpus", fulltest.DefaultCPUs(), "VM CPUs")
	dmesg := fs.Bool("dmesg", false, "Include VM dmesg output")
	maxTimeouts := fs.Int("max-consecutive-timeouts", fulltest.DefaultMaxConsecutiveTimeout, "Abort after this many consecutive command timeouts; use 0 to disable")
	dockerfile := fs.String("dockerfile", "", "Build this Dockerfile and run the suite against it")
	dockerContext := fs.String("docker-context", "", "Docker build context; defaults to Dockerfile directory")
	dockerTag := fs.String("docker-tag", "", "Docker image tag to build/save")
	dockerBinary := fs.String("docker-binary", "docker", "Docker-compatible CLI")
	backend := fs.String("backend", "ccvm", "Execution backend: ccvm or docker")
	jsonReport := fs.String("json-report", "", "Write a JSON report with per-test timing and resource usage")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: cc fulltest [flags]")
	}
	fulltestAPI := fulltest.API(api)
	dockerDirect := false
	switch strings.ToLower(strings.TrimSpace(*backend)) {
	case "", "ccvm":
		if api == nil {
			return fmt.Errorf("ccvm backend is not configured")
		}
	case "docker":
		fulltestAPI = fulltest.NewDockerAPI(context.Background(), *dockerBinary)
		dockerDirect = true
	default:
		return fmt.Errorf("unsupported fulltest backend %q", *backend)
	}
	result, err := fulltest.Run(context.Background(), fulltestAPI, fulltest.Options{
		Recipe:                 *recipe,
		ImageSource:            *imageSource,
		ImageName:              *imageName,
		WorkDir:                *workDir,
		Filter:                 *filter,
		KeepVM:                 *keepVM,
		Mirror:                 *mirror,
		Repo:                   *repo,
		CacheDir:               *cacheDir,
		Prefetch:               *prefetch,
		PrefetchWorkers:        *prefetchWorkers,
		MemoryMB:               *memoryMB,
		CPUs:                   *cpus,
		Dmesg:                  *dmesg,
		MaxConsecutiveTimeouts: *maxTimeouts,
		Dockerfile:             *dockerfile,
		DockerContext:          *dockerContext,
		DockerTag:              *dockerTag,
		DockerBinary:           *dockerBinary,
		DockerDirect:           dockerDirect,
		ReportPath:             *jsonReport,
		Progress:               os.Stderr,
	})
	if err != nil {
		return err
	}
	if code := fulltest.Summary(result, os.Stdout); code != 0 {
		return fmt.Errorf("fulltest failed")
	}
	return nil
}

func fulltestBackendFromArgs(args []string) string {
	for i, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "-backend" || arg == "--backend" {
			if i+1 < len(args) {
				return strings.ToLower(strings.TrimSpace(args[i+1]))
			}
			return ""
		}
		if strings.HasPrefix(arg, "-backend=") {
			return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(arg, "-backend=")))
		}
		if strings.HasPrefix(arg, "--backend=") {
			return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(arg, "--backend=")))
		}
	}
	return "ccvm"
}

func handleVMCommand(api ccAPI, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cc vm <list|start|stop|status|run|forward> ...")
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: cc vm list")
		}
		statuses, err := api.InstanceStatuses()
		if err != nil {
			return err
		}
		return printJSON(statuses)
	case "start":
		fs := flag.NewFlagSet("cc vm start", flag.ContinueOnError)
		vnc := fs.Bool("vnc", false, "Enable the loopback VNC server")
		vncListen := fs.String("vnc-listen", "127.0.0.1:0", "Loopback VNC listen address")
		displaySize := fs.String("display", "1280x720", "Display size WIDTHxHEIGHT")
		initSystem := fs.String("init", "", "Guest init system")
		timeout := fs.Duration("timeout", 0, "VM boot timeout")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 2 {
			return fmt.Errorf("usage: cc vm start [--vnc] [--vnc-listen ADDRESS] [--display WIDTHxHEIGHT] [--init SYSTEM] <name> <image>")
		}
		var display *client.DisplayConfig
		if *vnc {
			width, height, err := parseDisplaySize(*displaySize)
			if err != nil {
				return err
			}
			display = &client.DisplayConfig{Width: width, Height: height, VNCListen: *vncListen}
		}
		state, err := api.CreateInstanceStreamWithID(fs.Arg(0), client.CreateInstanceRequest{
			Image:          fs.Arg(1),
			InitSystem:     *initSystem,
			Display:        display,
			TimeoutSeconds: timeout.Seconds(),
		}, bootEventReporter(os.Stderr))
		if err != nil {
			return err
		}
		return printJSON(state)
	case "stop":
		if len(args) != 2 {
			return fmt.Errorf("usage: cc vm stop <name>")
		}
		return api.ShutdownInstanceWithID(args[1])
	case "status":
		if len(args) != 2 {
			return fmt.Errorf("usage: cc vm status <name>")
		}
		state, err := api.InstanceStatusOf(args[1])
		if err != nil {
			return err
		}
		return printJSON(state)
	case "run":
		if len(args) < 4 || args[2] != "--" {
			return fmt.Errorf("usage: cc vm run <name> -- <cmd...>")
		}
		return runInVMViaWebsocket(api, args[1], args[3:])
	case "forward":
		if len(args) != 3 {
			return fmt.Errorf("usage: cc vm forward <name> <HOST_PORT:GUEST_PORT>")
		}
		forward, err := parsePortForwardSpec(args[2])
		if err != nil {
			return err
		}
		return api.AddPortForwardTo(args[1], forward)
	default:
		return fmt.Errorf("unknown vm command %q", args[0])
	}
}

func runViaWebsocket(api ccAPI, image string, command []string) error {
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

	return execViaWebsocket(api, "", command)
}

func runInVMViaWebsocket(api ccAPI, id string, command []string) error {
	state, err := api.InstanceStatusOf(id)
	if err != nil {
		return err
	}
	if state.Status != "running" {
		return fmt.Errorf("VM %q is not running", id)
	}
	return execViaWebsocket(api, id, command)
}

func execViaWebsocket(api ccAPI, id string, command []string) error {
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

	stream := api.ExecStream
	if strings.TrimSpace(id) != "" {
		stream = func(req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
			return api.ExecStreamIn(id, req, inputs, onEvent)
		}
	}
	if err := stream(req, inputs, func(event client.ExecEvent) error {
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

func parsePortForwardSpec(spec string) (client.PortForward, error) {
	parts := strings.Split(spec, ":")
	if len(parts) != 2 {
		return client.PortForward{}, fmt.Errorf("port forward must be HOST_PORT:GUEST_PORT")
	}
	hostPort, err := parseTCPPort(parts[0], "host")
	if err != nil {
		return client.PortForward{}, err
	}
	guestPort, err := parseTCPPort(parts[1], "guest")
	if err != nil {
		return client.PortForward{}, err
	}
	return client.PortForward{
		Protocol:  "tcp",
		HostAddr:  "127.0.0.1",
		HostPort:  hostPort,
		GuestPort: guestPort,
	}, nil
}

func parseDisplaySize(value string) (uint32, uint32, error) {
	widthText, heightText, ok := strings.Cut(strings.ToLower(strings.TrimSpace(value)), "x")
	if !ok {
		return 0, 0, fmt.Errorf("display size %q must be WIDTHxHEIGHT", value)
	}
	width, err := strconv.ParseUint(widthText, 10, 32)
	if err != nil || width == 0 {
		return 0, 0, fmt.Errorf("invalid display width %q", widthText)
	}
	height, err := strconv.ParseUint(heightText, 10, 32)
	if err != nil || height == 0 {
		return 0, 0, fmt.Errorf("invalid display height %q", heightText)
	}
	return uint32(width), uint32(height), nil
}

func parseTCPPort(value, label string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("%s port is required", label)
	}
	port, err := net.LookupPort("tcp", value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s port %q", label, value)
	}
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("%s port %d out of range", label, port)
	}
	return port, nil
}

func progressEventReporter(stream *os.File, fallbackArtifact string) func(client.ProgressEvent) error {
	if stream == nil || !isTerminal(stream) {
		return nil
	}
	return func(event client.ProgressEvent) error {
		message := formatProgressEvent(event, fallbackArtifact)
		if message == "" {
			return nil
		}
		_, _ = fmt.Fprintln(stream, message)
		return nil
	}
}

func bootEventReporter(stream *os.File) func(client.BootEvent) error {
	if stream == nil || !isTerminal(stream) {
		return nil
	}
	return func(event client.BootEvent) error {
		message := formatBootEvent(event)
		if message == "" {
			return nil
		}
		_, _ = fmt.Fprintln(stream, message)
		return nil
	}
}

func formatProgressEvent(event client.ProgressEvent, fallbackArtifact string) string {
	artifact := firstNonEmpty(event.Artifact, fallbackArtifact)
	var parts []string
	if artifact != "" {
		parts = append(parts, artifact)
	}
	if event.Blob != "" && event.Blob != artifact {
		parts = append(parts, event.Blob)
	}
	if event.BytesDownloaded > 0 || event.BytesTotal > 0 {
		if event.BytesTotal > 0 {
			parts = append(parts, fmt.Sprintf("%s/%s", formatByteSize(event.BytesDownloaded), formatByteSize(event.BytesTotal)))
		} else {
			parts = append(parts, formatByteSize(event.BytesDownloaded))
		}
	}
	if event.RateBytesPerSecond > 0 {
		parts = append(parts, formatByteSize(int64(event.RateBytesPerSecond))+"/s")
	}
	if event.ETASeconds > 0 {
		parts = append(parts, "ETA "+formatDurationSeconds(event.ETASeconds))
	}
	if len(parts) == 0 {
		return ""
	}

	prefix := "Preparing"
	switch event.Status {
	case "downloading", "prefetching":
		prefix = "Downloading"
	case "downloaded", "available", "restored":
		prefix = "Ready"
	case "resolving":
		prefix = "Resolving"
	case "error":
		prefix = "Error"
		if event.Error != "" {
			parts = append(parts, event.Error)
		}
	}
	return prefix + " " + joinProgressParts(parts)
}

func formatBootEvent(event client.BootEvent) string {
	switch event.Kind {
	case "status":
		if event.Message != "" {
			return "Boot: " + event.Message
		}
	case "ready":
		if event.State.Image != "" {
			return "Boot: ready " + event.State.Image
		}
		return "Boot: ready"
	case "error":
		if event.Error != "" {
			return "Boot error: " + event.Error
		}
		return "Boot error"
	case "serial":
		if event.Data != "" {
			return event.Data
		}
	}
	return ""
}

func joinProgressParts(parts []string) string {
	ret := ""
	for i, part := range parts {
		if i > 0 {
			ret += " | "
		}
		ret += part
	}
	return ret
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func formatByteSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value := float64(bytes)
	for _, suffix := range []string{"KB", "MB", "GB", "TB", "PB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f EB", value/unit)
}

func formatDurationSeconds(seconds float64) string {
	if seconds <= 0 {
		return "0s"
	}
	duration := time.Duration(seconds * float64(time.Second)).Round(time.Second)
	return duration.String()
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
	signals := hostSignals(tty)
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
			if isResizeSignal(sig) {
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
		return nil, fmt.Errorf("prepare ccvm stdout pipe for %s: %w", ccvmPath, err)
	}
	if err := proc.Start(); err != nil {
		return nil, fmt.Errorf("start ccvm daemon %s with cache %s: %w", ccvmPath, cacheDir, err)
	}

	var hello client.ServerHello
	if err := json.NewDecoder(stdout).Decode(&hello); err != nil {
		_ = proc.Wait()
		return nil, fmt.Errorf("ccvm daemon did not send a startup banner from %s: %w", ccvmPath, err)
	}
	if err := validateServerHello(hello, cacheDir); err != nil {
		_ = proc.Process.Kill()
		_ = proc.Wait()
		return nil, err
	}
	if err := writeDaemonState(statePath, daemonState{Addr: hello.Addr}); err != nil {
		_ = proc.Process.Kill()
		_ = proc.Wait()
		return nil, fmt.Errorf("write daemon state %s for %s: %w", statePath, hello.Addr, err)
	}

	api := newClient(hello.Addr)
	if err := api.HealthCheck(); err != nil {
		_ = os.Remove(statePath)
		_ = proc.Process.Kill()
		_ = proc.Wait()
		return nil, fmt.Errorf("ccvm daemon started at %s but health check failed: %w", hello.Addr, err)
	}
	return api, nil
}

func validateServerHello(hello client.ServerHello, cacheDir string) error {
	if hello.Error != "" || hello.Kind == "error" {
		detail := firstNonEmpty(hello.Detail, hello.Error, "unknown startup error")
		return fmt.Errorf("ccvm daemon failed to start using cache %s: %s", cacheDir, detail)
	}
	if strings.TrimSpace(hello.Addr) == "" {
		return fmt.Errorf("ccvm daemon sent a startup banner without an address: %+v", hello)
	}
	return nil
}

func newClient(addr string) *client.Client {
	return client.NewClientContext("http://"+addr, func(ctx context.Context) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", addr)
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
		return arg, cachepath.EnsurePrivateRoot(arg)
	}
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	dir := filepath.Join(userCacheDir, "ccx3")
	return dir, cachepath.EnsurePrivateRoot(dir)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
