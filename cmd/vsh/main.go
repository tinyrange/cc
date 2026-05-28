package main

import (
	"bufio"
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
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/chzyer/readline"
	"j5.nz/cc/client"
)

const guestHostMount = "/host"
const defaultGuestUser = "1000:1000"

const (
	colorReset   = "\x1b[0m"
	colorGreen   = "\x1b[32m"
	colorCyan    = "\x1b[36m"
	colorBlue    = "\x1b[34m"
	colorMagenta = "\x1b[35m"
	colorYellow  = "\x1b[33m"
)

type daemonState struct {
	Addr string `json:"addr"`
}

type shellMode string

const (
	modeHost shellMode = "host"
	modeVM   shellMode = "vm"
)

type shellState struct {
	api       vshAPI
	context   commandContext
	hostCWD   string
	lastCode  int
	promptOut io.Writer
	history   string
	statusSeq atomic.Uint64
}

type vshAPI interface {
	HealthCheck() error
	GetImage(string) (client.ImageState, error)
	PullImageStream(string, client.PullImageRequest, func(client.ProgressEvent) error) error
	StartInstanceStreamWithID(string, client.StartInstanceRequest, func(client.BootEvent) error) (client.InstanceState, error)
	ShutdownInstanceWithID(string) error
	InstanceStatusOf(string) (client.InstanceState, error)
	InstanceStatuses() ([]client.InstanceState, error)
	AddPortForwardTo(string, client.PortForward) error
	CreateWatchdogLease(client.WatchdogLeaseRequest) (client.WatchdogLeaseResponse, error)
	FeedWatchdogLease(string) error
	ReleaseWatchdogLease(string) error
	RunStreamIn(string, client.RunRequest, func(client.ExecEvent) error) error
	RunStreamInContext(context.Context, string, client.RunRequest, func(client.ExecEvent) error) error
	RunInteractiveStreamIn(string, client.RunRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
}

type commandContext struct {
	Mode     shellMode `json:"mode"`
	Image    string    `json:"image,omitempty"`
	VMID     string    `json:"vm,omitempty"`
	CWD      string    `json:"cwd,omitempty"`
	User     string    `json:"user,omitempty"`
	MemoryMB uint64    `json:"memory_mb,omitempty"`
	CPUs     int       `json:"cpus,omitempty"`
	Network  bool      `json:"network,omitempty"`
}

type atLine struct {
	Target  string
	Options commandOptions
	Command string
}

type commandOptions struct {
	VMID         string
	CWD          string
	User         string
	Sudo         bool
	MemoryMB     uint64
	CPUs         int
	Network      *bool
	OptionFields []string
}

type shellToken struct {
	Value string
	Start int
	End   int
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "vsh:", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	ccvmPath := fs.String("ccvm", "", "Path to ccvm binary")
	cacheDir := fs.String("cache-dir", "", "Cache directory")
	image := fs.String("image", "", "Initial image for VM commands")
	vmID := fs.String("vm", "default", "Initial VM id")
	startVM := fs.Bool("start", false, "Start the selected blank VM before entering the shell")
	script := fs.String("script", "", "Internal test hook: read vsh commands from this file")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: vsh [flags]")
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
	api, err := connectBackend(ccvmBinary, rootCache, statePath)
	if err != nil {
		return err
	}
	stopLease, err := startDaemonLease(api)
	if err != nil {
		return err
	}
	defer stopLease()
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	sh := &shellState{
		api:       api,
		context:   defaultContext(strings.TrimSpace(*vmID), strings.TrimSpace(*image)),
		hostCWD:   cwd,
		promptOut: os.Stdout,
		history:   filepath.Join(rootCache, "vsh_history"),
	}
	if *startVM {
		if err := sh.startVM(sh.context.VMID, sh.context); err != nil {
			return err
		}
	}
	if *script != "" {
		f, err := os.Open(*script)
		if err != nil {
			return err
		}
		defer f.Close()
		return sh.runScript(f, os.Stdout, os.Stderr)
	}
	return sh.loop(os.Stdin, os.Stdout, os.Stderr)
}

func defaultContext(vmID, image string) commandContext {
	return commandContext{
		Mode:    modeHost,
		VMID:    firstNonEmpty(vmID, "default"),
		Image:   image,
		Network: true,
	}
}

func (s *shellState) loop(in io.Reader, stdout, stderr io.Writer) error {
	if !readerIsTerminal(in) || !writerIsTerminal(stdout) {
		return fmt.Errorf("vsh requires an interactive terminal")
	}
	inCloser, ok := in.(io.ReadCloser)
	if !ok {
		return fmt.Errorf("vsh stdin does not support interactive readline")
	}
	return s.evalReadline(inCloser, stdout, stderr)
}

func (s *shellState) evalReadline(in io.ReadCloser, stdout, stderr io.Writer) error {
	rl, err := readline.NewEx(&readline.Config{
		Prompt:                 s.prompt(),
		HistoryFile:            s.history,
		HistoryLimit:           1000,
		HistorySearchFold:      true,
		DisableAutoSaveHistory: true,
		InterruptPrompt:        "^C",
		EOFPrompt:              "",
		Stdin:                  in,
		Stdout:                 stdout,
		Stderr:                 stderr,
	})
	if err != nil {
		return err
	}
	defer rl.Close()

	for {
		rl.SetPrompt(s.prompt())
		s.drawPromptStatus(stdout)
		line, err := rl.Readline()
		s.statusSeq.Add(1)
		switch {
		case errors.Is(err, readline.ErrInterrupt):
			continue
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return err
		}
		if shouldSaveHistory(line) {
			_ = rl.SaveHistory(line)
		}
		if err := s.eval(line, stdout, stderr); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			s.lastCode = 1
			fmt.Fprintln(stderr, "vsh:", err)
		}
	}
}

func (s *shellState) runScript(in io.Reader, stdout, stderr io.Writer) error {
	return s.evalScriptLines(in, stdout, stderr)
}

func (s *shellState) evalScriptLines(in io.Reader, stdout, stderr io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for {
		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if err := s.eval(line, stdout, stderr); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			s.lastCode = 1
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func shouldSaveHistory(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed != "" && !strings.HasPrefix(trimmed, "#")
}

func (s *shellState) eval(line string, stdout, stderr io.Writer) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	if strings.HasPrefix(line, "@") {
		return s.evalAt(line, stdout, stderr)
	}
	if isExitCommand(line) {
		return io.EOF
	}
	if cd, ok, err := parseCD(line); ok || err != nil {
		if err != nil {
			return err
		}
		return s.chdir(cd)
	}
	return s.runInContext(s.context, line, stdout, stderr)
}

func (s *shellState) runInContext(ctx commandContext, line string, stdout, stderr io.Writer) error {
	switch ctx.Mode {
	case modeHost:
		return s.runHost(line, stdout, stderr)
	case modeVM:
		return s.runGuest(ctx, line, stdout, stderr)
	default:
		return fmt.Errorf("unknown shell mode %q", ctx.Mode)
	}
}

func (s *shellState) evalAt(line string, stdout, stderr io.Writer) error {
	at, err := parseAtLine(line)
	if err != nil {
		return err
	}
	if at.Target == "" && len(at.Options.OptionFields) == 0 && at.Command == "" {
		return s.help(stdout)
	}
	if at.Target == "" {
		ctx := s.context.withOptions(at.Options)
		if at.Options.Sudo {
			ctx.Mode = modeVM
			ctx.User = "root"
		}
		if at.Command == "" {
			if at.Options.Sudo {
				return fmt.Errorf("usage: @ --sudo <cmd>")
			}
			s.context = ctx
			return nil
		}
		return s.runInContext(ctx, at.Command, stdout, stderr)
	}

	switch at.Target {
	case "help", "?":
		if at.Command != "" || len(at.Options.OptionFields) != 0 {
			return fmt.Errorf("usage: @help")
		}
		return s.help(stdout)
	case "host":
		ctx := s.context.withOptions(at.Options)
		ctx.Mode = modeHost
		if at.Options.Sudo {
			return fmt.Errorf("usage: @host [cmd]")
		}
		if at.Command == "" {
			s.context = ctx
			return nil
		}
		return s.runInContext(ctx, at.Command, stdout, stderr)
	case "ps":
		if at.Command != "" || len(at.Options.OptionFields) != 0 {
			return fmt.Errorf("usage: @ps")
		}
		return s.printVMs(stdout)
	case "status", "where":
		if at.Command != "" || len(at.Options.OptionFields) != 0 {
			return fmt.Errorf("usage: @%s", at.Target)
		}
		return s.printStatus(stdout)
	case "sudo":
		ctx := s.context.withOptions(at.Options)
		ctx.Mode = modeVM
		ctx.User = "root"
		if at.Command == "" {
			return fmt.Errorf("usage: @sudo <cmd>")
		}
		return s.runInContext(ctx, at.Command, stdout, stderr)
	case "start":
		if at.Command != "" {
			return fmt.Errorf("usage: @start [--vm id]")
		}
		ctx := s.context.withOptions(at.Options)
		id := firstNonEmpty(ctx.VMID, s.context.VMID)
		return s.startVM(id, ctx)
	case "stop":
		if at.Command != "" {
			return fmt.Errorf("usage: @stop [--vm id]")
		}
		id := firstNonEmpty(at.Options.VMID, s.context.VMID)
		return s.stopVM(id)
	case "forward":
		if at.Command == "" {
			return fmt.Errorf("usage: @forward <host-port:guest-port>")
		}
		fields, err := splitShellFields(at.Command)
		if err != nil {
			return err
		}
		if len(fields) != 1 {
			return fmt.Errorf("usage: @forward <host-port:guest-port>")
		}
		forward, err := parsePortForwardSpec(fields[0])
		if err != nil {
			return err
		}
		id := firstNonEmpty(at.Options.VMID, s.context.VMID)
		return s.api.AddPortForwardTo(id, forward)
	default:
		ctx := s.context.withOptions(at.Options)
		ctx.Mode = modeVM
		ctx.Image = at.Target
		if at.Options.Sudo {
			ctx.User = "root"
		}
		if at.Command == "" {
			if at.Options.Sudo {
				return fmt.Errorf("usage: @%s --sudo <cmd>", at.Target)
			}
			s.context = ctx
			return nil
		}
		return s.runInContext(ctx, at.Command, stdout, stderr)
	}
}

func (s *shellState) runHost(line string, stdout, stderr io.Writer) error {
	tty, cols, rows := terminalRequestSize(stdout)
	args := hostShellCommand(line, tty)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = s.hostCWD
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if tty {
		cmd.Env = mergedEnv(os.Environ(), terminalEnv(cols, rows))
	}
	err := cmd.Run()
	s.lastCode = exitCode(err)
	if err != nil && s.lastCode < 0 {
		return err
	}
	return nil
}

func hostShellCommand(line string, tty bool) []string {
	command := line
	if tty {
		command = hostShellPrelude() + line
	}
	return []string{hostShell(), "-lc", command}
}

func hostShellPrelude() string {
	switch filepath.Base(hostShell()) {
	case "bash":
		return colorPrelude("ls -G", "ls --color=auto", true)
	case "zsh":
		return colorPrelude("ls -G", "ls --color=auto", false)
	default:
		return colorPrelude("ls --color=auto", "ls -G", false)
	}
}

func colorPrelude(primaryLS, fallbackLS string, bash bool) string {
	var b strings.Builder
	if bash {
		b.WriteString("shopt -s expand_aliases 2>/dev/null || true\n")
	}
	b.WriteString("alias ls=")
	b.WriteString(shellQuote(primaryLS))
	b.WriteString(" 2>/dev/null || alias ls=")
	b.WriteString(shellQuote(fallbackLS))
	b.WriteString(" 2>/dev/null || true\n")
	return b.String()
}

func mergedEnv(base, overrides []string) []string {
	out := append([]string(nil), base...)
	index := make(map[string]int, len(out))
	for i, entry := range out {
		if key, _, ok := strings.Cut(entry, "="); ok {
			index[key] = i
		}
	}
	for _, entry := range overrides {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if i, exists := index[key]; exists {
			out[i] = entry
			continue
		}
		index[key] = len(out)
		out = append(out, entry)
	}
	return out
}

func (s *shellState) runGuest(ctx commandContext, line string, stdout, stderr io.Writer) error {
	if ctx.Image == "" {
		return fmt.Errorf("no guest image selected; run @<oci-tag> or set one with @<oci-tag>")
	}
	if err := s.ensureImageAvailable(ctx.Image, stderr); err != nil {
		return err
	}
	if err := s.ensureVMRunning(ctx); err != nil {
		return err
	}
	hostRoot, hostGuestCWD, err := guestHostPaths(s.hostCWD)
	if err != nil {
		return err
	}
	workDir := firstNonEmpty(ctx.CWD, hostGuestCWD)
	tty, cols, rows := terminalRequestSize(stdout)
	req := client.RunRequest{
		Image:   ctx.Image,
		Command: guestCommand(line, tty),
		Shares: []client.ShareMount{{
			Source:   hostRoot,
			Mount:    guestHostMount,
			Writable: true,
			MapOwner: true,
			OwnerUID: defaultGuestUID,
			OwnerGID: defaultGuestGID,
		}},
		WorkDir:  workDir,
		User:     guestRunUser(ctx),
		MemoryMB: ctx.MemoryMB,
		CPUs:     ctx.CPUs,
	}
	if tty {
		req.TTY = true
		req.Cols = cols
		req.Rows = rows
	}
	if tty {
		req.Env = terminalEnv(cols, rows)
	}
	if ctx.Network {
		req.Network = defaultNetworkConfig()
	}
	return s.streamGuestRun(ctx.VMID, req, stdout, stderr)
}

func (s *shellState) streamGuestRun(id string, req client.RunRequest, stdout, stderr io.Writer) error {
	if !req.TTY {
		exitCode := 0
		if err := s.api.RunStreamInContext(context.Background(), id, req, func(event client.ExecEvent) error {
			switch event.Kind {
			case "stdout", "output":
				writeExecEventOutput(stdout, event)
			case "stderr":
				writeExecEventOutput(stderr, event)
			case "exit":
				exitCode = event.ExitCode
			case "error":
				if event.Error != "" {
					return fmt.Errorf("%s", event.Error)
				}
				return fmt.Errorf("guest command failed")
			}
			return nil
		}); err != nil {
			s.lastCode = 1
			return err
		}
		s.lastCode = exitCode
		return nil
	}

	inputs := make(chan client.ExecInput, 8)
	done := make(chan struct{})
	var producers sync.WaitGroup
	restoreTerminal, err := s.startGuestInputForwarding(req.TTY, inputs, done, stdout, stderr, &producers)
	if err != nil {
		return err
	}
	defer restoreTerminal()
	defer func() {
		close(done)
		producers.Wait()
		close(inputs)
	}()

	exitCode := 0
	if err := s.api.RunInteractiveStreamIn(id, req, inputs, func(event client.ExecEvent) error {
		switch event.Kind {
		case "stdout", "output":
			writeExecEventOutput(stdout, event)
		case "stderr":
			writeExecEventOutput(stderr, event)
		case "exit":
			exitCode = event.ExitCode
		case "error":
			if event.Error != "" {
				return fmt.Errorf("%s", event.Error)
			}
			return fmt.Errorf("guest command failed")
		}
		return nil
	}); err != nil {
		s.lastCode = 1
		return err
	}
	s.lastCode = exitCode
	return nil
}

func (s *shellState) startGuestInputForwarding(tty bool, inputs chan<- client.ExecInput, done <-chan struct{}, stdout, stderr io.Writer, producers *sync.WaitGroup) (func(), error) {
	restore := func() {}
	if tty {
		file, ok := stdout.(*os.File)
		if ok && isTerminalFD(int(file.Fd())) && isTerminalFD(int(os.Stdin.Fd())) {
			terminalRestore, err := makeRawTerminal(os.Stdin)
			if err != nil {
				return nil, err
			}
			restore = terminalRestore
			producers.Add(1)
			go func() {
				defer producers.Done()
				streamGuestStdin(os.Stdin, inputs, done)
			}()
		}
	}

	producers.Add(1)
	go func() {
		defer producers.Done()
		forwardGuestSignals(inputs, done, tty, stdout, stderr)
	}()
	return restore, nil
}

func streamGuestStdin(file *os.File, out chan<- client.ExecInput, done <-chan struct{}) {
	var buf [4096]byte
	for {
		select {
		case <-done:
			return
		default:
		}
		n, err := file.Read(buf[:])
		if n > 0 {
			sendGuestInputBytes(out, done, buf[:n])
		}
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				sleepOrDone(done, 10*time.Millisecond)
				continue
			}
			if errors.Is(err, io.EOF) {
				sendGuestInput(out, done, client.ExecInput{Kind: "stdin_close"})
			}
			return
		}
	}
}

func sendGuestInputBytes(out chan<- client.ExecInput, done <-chan struct{}, data []byte) {
	start := 0
	for i, b := range data {
		if b != 3 {
			continue
		}
		if i > start {
			sendGuestInput(out, done, client.ExecInput{Kind: "stdin", Data: append([]byte(nil), data[start:i]...)})
		}
		sendGuestInput(out, done, client.ExecInput{Kind: "signal", Signal: "INT"})
		start = i + 1
	}
	if start < len(data) {
		sendGuestInput(out, done, client.ExecInput{Kind: "stdin", Data: append([]byte(nil), data[start:]...)})
	}
}

func forwardGuestSignals(out chan<- client.ExecInput, done <-chan struct{}, tty bool, stdout, stderr io.Writer) {
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
				file, ok := stdout.(*os.File)
				if !ok {
					continue
				}
				cols, rows, err := terminalSize(file)
				if err != nil {
					continue
				}
				sendGuestInput(out, done, client.ExecInput{Kind: "resize", Cols: cols, Rows: rows})
				continue
			}
			name, ok := signalName(sig)
			if !ok {
				continue
			}
			if name == "INT" {
				fmt.Fprintln(stderr)
			}
			sendGuestInput(out, done, client.ExecInput{Kind: "signal", Signal: name})
		}
	}
}

func sendGuestInput(out chan<- client.ExecInput, done <-chan struct{}, input client.ExecInput) {
	select {
	case <-done:
	case out <- input:
	}
}

func sleepOrDone(done <-chan struct{}, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

func writeExecEventOutput(w io.Writer, event client.ExecEvent) {
	if len(event.Data) > 0 {
		_, _ = w.Write(event.Data)
		return
	}
	if event.Output != "" {
		_, _ = fmt.Fprint(w, event.Output)
	}
}

func guestCommand(line string, tty bool) []string {
	if !tty {
		return []string{"sh", "-lc", line}
	}
	return []string{"sh", "-lc", colorPrelude("ls --color=always -C --width=${COLUMNS:-80}", "ls -G -C", false) + line}
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func (s *shellState) ensureImageAvailable(image string, stderr io.Writer) error {
	if _, err := s.api.GetImage(image); err == nil {
		return nil
	}
	report := func(event client.ProgressEvent) error {
		message := formatProgressEvent(event, image)
		if message != "" {
			fmt.Fprintln(stderr, message)
		}
		return nil
	}
	return s.api.PullImageStream(image, client.PullImageRequest{Source: image}, report)
}

func guestHostPaths(hostCWD string) (hostRoot, guestCWD string, err error) {
	abs, err := filepath.Abs(hostCWD)
	if err != nil {
		return "", "", err
	}
	volume := filepath.VolumeName(abs)
	if volume != "" {
		hostRoot = volume + string(filepath.Separator)
		rel, err := filepath.Rel(hostRoot, abs)
		if err != nil {
			return "", "", err
		}
		guestCWD = path.Join(guestHostMount, filepath.ToSlash(rel))
		return hostRoot, guestCWD, nil
	}
	hostRoot = string(filepath.Separator)
	rel := strings.TrimPrefix(filepath.ToSlash(abs), "/")
	guestCWD = path.Join(guestHostMount, rel)
	return hostRoot, guestCWD, nil
}

func (s *shellState) ensureVMRunning(ctx commandContext) error {
	id := ctx.VMID
	state, err := s.api.InstanceStatusOf(id)
	if err != nil {
		return err
	}
	if state.Status == "running" {
		return nil
	}
	return s.startVM(id, ctx)
}

func (s *shellState) startVM(id string, ctx commandContext) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("vm id is required")
	}
	req := client.StartInstanceRequest{
		MemoryMB: ctx.MemoryMB,
		CPUs:     ctx.CPUs,
	}
	if ctx.Network {
		req.Network = defaultNetworkConfig()
	}
	boot := newBootStatus(os.Stderr)
	defer boot.Close()
	state, err := s.api.StartInstanceStreamWithID(id, req, func(event client.BootEvent) error {
		boot.Update(event)
		return nil
	})
	if err != nil {
		return err
	}
	s.context.VMID = firstNonEmpty(state.ID, id)
	return nil
}

type bootStatus struct {
	w        io.Writer
	tty      bool
	done     chan struct{}
	finished chan struct{}
	mu       sync.Mutex
	message  string
	active   bool
}

func newBootStatus(w io.Writer) *bootStatus {
	b := &bootStatus{
		w:        w,
		done:     make(chan struct{}),
		finished: make(chan struct{}),
	}
	if file, ok := w.(*os.File); ok && isTerminalFD(int(file.Fd())) {
		b.tty = true
		b.active = true
		go b.spin()
		return b
	}
	close(b.finished)
	return b
}

func (b *bootStatus) Update(event client.BootEvent) {
	msg := formatBootEvent(event)
	if msg == "" {
		return
	}
	if !b.tty {
		fmt.Fprintln(b.w, msg)
		return
	}
	switch event.Kind {
	case "ready":
		b.Close()
	case "error":
		b.finishWith(msg)
	default:
		b.mu.Lock()
		b.message = msg
		b.mu.Unlock()
	}
}

func (b *bootStatus) Close() {
	if b == nil || !b.tty {
		return
	}
	b.mu.Lock()
	if !b.active {
		b.mu.Unlock()
		return
	}
	b.active = false
	close(b.done)
	b.mu.Unlock()
	<-b.finished
	fmt.Fprint(b.w, "\r\033[2K")
}

func (b *bootStatus) finishWith(message string) {
	b.Close()
	fmt.Fprintln(b.w, message)
}

func (b *bootStatus) spin() {
	defer close(b.finished)
	frames := []string{"-", "\\", "|", "/"}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	i := 0
	for {
		select {
		case <-b.done:
			return
		case <-ticker.C:
			b.mu.Lock()
			msg := b.message
			b.mu.Unlock()
			if msg == "" {
				msg = "Boot: starting VM"
			}
			fmt.Fprintf(b.w, "\r\033[2K%s %s", frames[i%len(frames)], msg)
			i++
		}
	}
}

func defaultNetworkConfig() *client.NetworkConfig {
	return &client.NetworkConfig{Enabled: true, AllowInternet: true}
}

func (s *shellState) stopVM(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("vm id is required")
	}
	return s.api.ShutdownInstanceWithID(id)
}

func (s *shellState) chdir(target string) error {
	if target == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		target = home
	}
	if strings.HasPrefix(target, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		switch {
		case target == "~":
			target = home
		case strings.HasPrefix(target, "~/"):
			target = filepath.Join(home, target[2:])
		default:
			return fmt.Errorf("user home expansion is only supported for ~ and ~/ paths")
		}
	}
	target = os.ExpandEnv(target)
	if !filepath.IsAbs(target) {
		target = filepath.Join(s.hostCWD, target)
	}
	target = filepath.Clean(target)
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", target)
	}
	s.hostCWD = target
	return os.Chdir(target)
}

func (s *shellState) prompt() string {
	leaf := filepath.Base(s.hostCWD)
	if leaf == "." || leaf == string(filepath.Separator) {
		leaf = s.hostCWD
	}
	base := colorGreen + "➜" + colorReset + "  " + colorCyan + leaf + colorReset
	if s.context.Mode == modeVM {
		target := "(" + s.context.Image
		if s.context.VMID != "" && s.context.VMID != "default" {
			target += ":" + s.context.VMID
		}
		target += ")"
		return base + " " + colorMagenta + "vm:" + colorReset + colorYellow + target + colorReset + " "
	}
	return base + " " + colorBlue + "host" + colorReset + " "
}

func terminalRequestSize(stdout io.Writer) (bool, int, int) {
	file, ok := stdout.(*os.File)
	if !ok || !isTerminalFD(int(file.Fd())) {
		return false, 0, 0
	}
	cols, rows, err := terminalSize(file)
	if err != nil {
		return true, 0, 0
	}
	return true, cols, rows
}

func terminalEnv(cols, rows int) []string {
	keys := []string{
		"TERM",
		"COLORTERM",
		"LS_COLORS",
		"NO_COLOR",
		"CLICOLOR",
		"CLICOLOR_FORCE",
		"FORCE_COLOR",
	}
	env := make([]string, 0, len(keys)+2)
	termSeen := false
	for _, key := range keys {
		value, ok := os.LookupEnv(key)
		if !ok || value == "" {
			continue
		}
		if key == "TERM" {
			termSeen = true
		}
		env = append(env, key+"="+value)
	}
	if !termSeen {
		env = append(env, "TERM=xterm-256color")
	}
	if _, ok := os.LookupEnv("CLICOLOR"); !ok {
		env = append(env, "CLICOLOR=1")
	}
	if cols > 0 {
		env = append(env, "COLUMNS="+strconv.Itoa(cols))
	}
	if rows > 0 {
		env = append(env, "LINES="+strconv.Itoa(rows))
	}
	return env
}

func (s *shellState) drawPromptStatus(stdout io.Writer) {
	seq := s.statusSeq.Add(1)
	code := s.lastCode
	if code == 0 {
		return
	}
	file, ok := stdout.(*os.File)
	if !ok || !isTerminalFD(int(file.Fd())) {
		return
	}
	cols, _, err := terminalSize(file)
	if err != nil || cols <= 0 {
		return
	}
	status := colorYellow + "exit " + strconv.Itoa(code) + colorReset
	visible := len("exit ") + len(strconv.Itoa(code))
	col := cols - visible + 1
	if col < 1 {
		col = 1
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		if s.statusSeq.Load() != seq {
			return
		}
		fmt.Fprintf(file, "\x1b7\x1b[%dG%s\x1b8", col, status)
	}()
}

func (s *shellState) printStatus(w io.Writer) error {
	state, err := s.api.InstanceStatusOf(s.context.VMID)
	if err != nil {
		return err
	}
	return printJSON(w, map[string]any{
		"context":  s.context,
		"host_cwd": s.hostCWD,
		"state":    state,
	})
}

func (s *shellState) printVMs(w io.Writer) error {
	states, err := s.api.InstanceStatuses()
	if err != nil {
		return err
	}
	return printJSON(w, states)
}

func (s *shellState) help(w io.Writer) error {
	_, err := fmt.Fprintln(w, strings.TrimSpace(`
@<oci-tag> [opts] [cmd]  run cmd in an OCI image, or make it current if cmd is omitted
@host [cmd]              run cmd on the host, or make host current if cmd is omitted
@ [opts] [cmd]           update or use the current context
@sudo <cmd>              run cmd as root in the current VM
@ps                      list VMs
@status                  show vsh and selected VM state
@start [--vm id]         start a blank VM
@stop [--vm id]          stop a VM
@forward H:G             forward host port H to guest port G
opts: --vm id --cwd path --user user --sudo --memory-mb n --memory n[m|g] --cpus n --network --no-network
cd <dir>                 change host directory; VM commands run under the mirrored /host path
exit                     leave vsh
`))
	return err
}

func parseAtLine(line string) (atLine, error) {
	body := strings.TrimSpace(strings.TrimPrefix(line, "@"))
	if body == "" {
		return atLine{}, nil
	}
	tokens, err := lexShellTokens(body)
	if err != nil {
		return atLine{}, err
	}
	if len(tokens) == 0 {
		return atLine{}, nil
	}
	var at atLine
	i := 0
	if !strings.HasPrefix(tokens[0].Value, "--") {
		at.Target = tokens[0].Value
		i = 1
	}
	opts, next, err := parseCommandOptions(tokens, i)
	if err != nil {
		return atLine{}, err
	}
	at.Options = opts
	if next < len(tokens) {
		at.Command = strings.TrimSpace(body[tokens[next].Start:])
	}
	return at, nil
}

func parseCommandOptions(tokens []shellToken, start int) (commandOptions, int, error) {
	var opts commandOptions
	i := start
	for i < len(tokens) {
		field := tokens[i].Value
		if field == "--" {
			return opts, i + 1, nil
		}
		if !strings.HasPrefix(field, "--") {
			return opts, i, nil
		}
		name, value, hasInlineValue := strings.Cut(field, "=")
		readValue := func() (string, error) {
			if hasInlineValue {
				return value, nil
			}
			if i+1 >= len(tokens) {
				return "", fmt.Errorf("%s requires a value", name)
			}
			i++
			return tokens[i].Value, nil
		}
		opts.OptionFields = append(opts.OptionFields, field)
		switch name {
		case "--vm":
			v, err := readValue()
			if err != nil {
				return opts, i, err
			}
			opts.VMID = v
		case "--cwd":
			v, err := readValue()
			if err != nil {
				return opts, i, err
			}
			opts.CWD = v
		case "--user":
			v, err := readValue()
			if err != nil {
				return opts, i, err
			}
			opts.User = v
		case "--sudo":
			if hasInlineValue {
				return opts, i, fmt.Errorf("--sudo does not take a value")
			}
			opts.Sudo = true
		case "--memory-mb":
			v, err := readValue()
			if err != nil {
				return opts, i, err
			}
			memory, err := parseMemoryMB(v)
			if err != nil {
				return opts, i, err
			}
			opts.MemoryMB = memory
		case "--memory":
			v, err := readValue()
			if err != nil {
				return opts, i, err
			}
			memory, err := parseMemoryMB(v)
			if err != nil {
				return opts, i, err
			}
			opts.MemoryMB = memory
		case "--cpus":
			v, err := readValue()
			if err != nil {
				return opts, i, err
			}
			cpus, err := strconv.Atoi(v)
			if err != nil || cpus <= 0 {
				return opts, i, fmt.Errorf("invalid --cpus value %q", v)
			}
			opts.CPUs = cpus
		case "--network":
			if hasInlineValue {
				return opts, i, fmt.Errorf("--network does not take a value")
			}
			enabled := true
			opts.Network = &enabled
		case "--no-network":
			if hasInlineValue {
				return opts, i, fmt.Errorf("--no-network does not take a value")
			}
			enabled := false
			opts.Network = &enabled
		default:
			return opts, i, fmt.Errorf("unknown vsh option %q", name)
		}
		i++
	}
	return opts, i, nil
}

func (c commandContext) withOptions(opts commandOptions) commandContext {
	if opts.VMID != "" {
		c.VMID = opts.VMID
	}
	if opts.CWD != "" {
		c.CWD = opts.CWD
	}
	if opts.User != "" {
		c.User = opts.User
	}
	if opts.Sudo {
		c.User = "root"
	}
	if opts.MemoryMB != 0 {
		c.MemoryMB = opts.MemoryMB
	}
	if opts.CPUs != 0 {
		c.CPUs = opts.CPUs
	}
	if opts.Network != nil {
		c.Network = *opts.Network
	}
	return c
}

const (
	defaultGuestUID = 1000
	defaultGuestGID = 1000
)

func guestRunUser(ctx commandContext) string {
	if strings.TrimSpace(ctx.User) != "" {
		return ctx.User
	}
	return defaultGuestUser
}

func parseMemoryMB(value string) (uint64, error) {
	raw := strings.TrimSpace(strings.ToLower(value))
	if raw == "" {
		return 0, fmt.Errorf("memory value is required")
	}
	multiplier := uint64(1)
	switch {
	case strings.HasSuffix(raw, "gb"):
		multiplier = 1024
		raw = strings.TrimSuffix(raw, "gb")
	case strings.HasSuffix(raw, "g"):
		multiplier = 1024
		raw = strings.TrimSuffix(raw, "g")
	case strings.HasSuffix(raw, "mb"):
		raw = strings.TrimSuffix(raw, "mb")
	case strings.HasSuffix(raw, "m"):
		raw = strings.TrimSuffix(raw, "m")
	}
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("invalid memory value %q", value)
	}
	return n * multiplier, nil
}

func parseCD(line string) (string, bool, error) {
	fields, err := splitShellFields(line)
	if err != nil {
		return "", false, err
	}
	if len(fields) == 0 || fields[0] != "cd" {
		return "", false, nil
	}
	if len(fields) > 2 {
		return "", true, fmt.Errorf("usage: cd [dir]")
	}
	if len(fields) == 1 {
		return "", true, nil
	}
	return fields[1], true, nil
}

func isExitCommand(line string) bool {
	fields, err := splitShellFields(line)
	return err == nil && len(fields) == 1 && fields[0] == "exit"
}

func splitShellFields(input string) ([]string, error) {
	tokens, err := lexShellTokens(input)
	if err != nil {
		return nil, err
	}
	fields := make([]string, 0, len(tokens))
	for _, token := range tokens {
		fields = append(fields, token.Value)
	}
	return fields, nil
}

func lexShellTokens(input string) ([]shellToken, error) {
	var b strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	haveField := false
	fieldStart := 0
	var tokens []shellToken
	for i, r := range input {
		switch {
		case escaped:
			b.WriteRune(r)
			haveField = true
			escaped = false
		case r == '\\' && !inSingle:
			if !haveField {
				fieldStart = i
			}
			escaped = true
			haveField = true
		case r == '\'' && !inDouble:
			if !haveField {
				fieldStart = i
			}
			inSingle = !inSingle
			haveField = true
		case r == '"' && !inSingle:
			if !haveField {
				fieldStart = i
			}
			inDouble = !inDouble
			haveField = true
		case (r == ' ' || r == '\t' || r == '\n') && !inSingle && !inDouble:
			if haveField {
				tokens = append(tokens, shellToken{Value: b.String(), Start: fieldStart, End: i})
				b.Reset()
				haveField = false
			}
		default:
			if !haveField {
				fieldStart = i
			}
			b.WriteRune(r)
			haveField = true
		}
	}
	if escaped {
		return nil, fmt.Errorf("unfinished escape")
	}
	if inSingle || inDouble {
		return nil, fmt.Errorf("unterminated quote")
	}
	if haveField {
		tokens = append(tokens, shellToken{Value: b.String(), Start: fieldStart, End: len(input)})
	}
	return tokens, nil
}

func hostShell() string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return shell
	}
	return "/bin/sh"
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
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
	return prefix + " " + strings.Join(parts, " | ")
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

func resolveCCVMPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	candidates := []string{
		filepath.Join(filepath.Dir(exePath), "ccvm"),
		exePath + "vm",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if found, err := exec.LookPath("ccvm"); err == nil {
		return found, nil
	}
	return "", fmt.Errorf("ccvm binary not found next to %s or on PATH; pass -ccvm", exePath)
}

func connectBackend(ccvmPath, cacheDir, statePath string) (*client.Client, error) {
	if state, err := readDaemonState(statePath); err == nil {
		api := newClient(state.Addr)
		if err := api.HealthCheck(); err == nil {
			return api, nil
		}
		_ = os.Remove(statePath)
	}

	proc := exec.Command(ccvmPath, "-cache-dir", cacheDir)
	proc.Stderr = os.Stderr
	detachDaemonCommand(proc)
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
	return client.NewClient("http://"+addr, func() (net.Conn, error) {
		return net.Dial("tcp", addr)
	})
}

func startDaemonLease(api *client.Client) (func(), error) {
	const timeout = 10 * time.Second
	lease, err := api.CreateWatchdogLease(client.WatchdogLeaseRequest{TimeoutSeconds: timeout.Seconds()})
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(timeout / 3)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_ = api.FeedWatchdogLease(lease.LeaseID)
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
		_ = api.ReleaseWatchdogLease(lease.LeaseID)
	}, nil
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

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func readerIsTerminal(r io.Reader) bool {
	file, ok := r.(*os.File)
	return ok && isTerminalFD(int(file.Fd()))
}

func writerIsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && isTerminalFD(int(file.Fd()))
}
