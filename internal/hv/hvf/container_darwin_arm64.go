//go:build darwin && arm64

package hvf

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/fdt"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/serial"
	"j5.nz/cc/internal/timing"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

var debugTiming = strings.TrimSpace(os.Getenv("CCX3_DEBUG_TIMING")) != ""

func timingLog(format string, args ...any) {
	if !debugTiming {
		return
	}
	fmt.Fprintf(os.Stderr, "ccx3 timing: "+format+"\n", args...)
}

const (
	instanceReadyMarker   = vmruntime.InstanceReadyMarker
	initDurationMarker    = vmruntime.InitDurationMarker
	execTimingMarker      = vmruntime.ExecTimingMarker
	commandBeginMarker    = vmruntime.CommandBeginMarker
	commandOutputMarker   = vmruntime.CommandOutputMarker
	commandErrorMarker    = vmruntime.CommandErrorMarker
	commandExitMarkerPref = vmruntime.CommandExitMarkerPref
	arm64VirtualTimerPPI  = 27
)

type serialTranscript = arm64vm.SerialTranscript
type bootEventWriter = arm64vm.BootEventWriter

func newSerialTranscript() *serialTranscript { return arm64vm.NewSerialTranscript() }
func newBootEventWriter(callback func(client.BootEvent) error) *bootEventWriter {
	return arm64vm.NewBootEventWriter(callback)
}
func hasFatalBootText(text string) bool { return arm64vm.HasFatalBootText(text) }
func parseInitDurationMarker(text string) (int, bool) {
	return arm64vm.ParseInitDurationMarker(text)
}

type ContainerRunRequest = vmruntime.RunRequest
type DirectoryShare = vmruntime.DirectoryShare
type ContainerRunResult = vmruntime.RunResult

type ContainerSession struct {
	cancel      context.CancelFunc
	doneCh      chan sessionRunResult
	closeDone   <-chan struct{}
	image       *oci.Image
	baseEnv     []string
	workDir     string
	dmesg       bool
	uart        *serial.UART8250
	control     virtio.VsockConn
	transcript  *arm64vm.SerialTranscript
	listener    virtio.VsockListener
	vsock       *virtio.Vsock
	rootFS      virtio.ShareMounter
	sendMu      sync.Mutex
	shareMu     sync.Mutex
	shares      map[string]client.ShareMount
	imageMounts map[string]string
	nextID      atomic.Uint64
	activeExecs *atomic.Int32
}

type readyResult struct {
	conn virtio.VsockConn
	err  error
}

type sessionRunResult struct {
	result ContainerRunResult
	err    error
}

func parseExecTimingMarkers(text, id string) map[string]int {
	out := map[string]int{}
	if text == "" || id == "" {
		return out
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, execTimingMarker+id+":") {
			continue
		}
		rest := strings.TrimPrefix(line, execTimingMarker+id+":")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			continue
		}
		ms, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			continue
		}
		out[strings.TrimSpace(parts[0])] = ms
	}
	return out
}

func hasManagedExecBegin(text, id string) bool {
	return strings.Contains(text, commandBeginMarker+id)
}

func hasManagedExecFirstByte(text, id string) bool {
	return strings.Contains(text, commandOutputMarker+id+":") ||
		strings.Contains(text, commandErrorMarker+id+":") ||
		strings.Contains(text, commandExitMarkerPref+id+":")
}

type guestExecRequest struct {
	ID          string   `json:"id"`
	Command     []string `json:"command"`
	Env         []string `json:"env,omitempty"`
	RootDir     string   `json:"root_dir,omitempty"`
	ReplaceEnv  bool     `json:"replace_env,omitempty"`
	SkipResolve bool     `json:"skip_resolve,omitempty"`
	WorkDir     string   `json:"workdir,omitempty"`
	Stdin       []byte   `json:"stdin,omitempty"`
	TTY         bool     `json:"tty,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Signal      string   `json:"signal,omitempty"`
	Cols        int      `json:"cols,omitempty"`
	Rows        int      `json:"rows,omitempty"`
}

func StartContainer(ctx context.Context, req ContainerRunRequest) (*ContainerSession, error) {
	return StartContainerStream(ctx, req, nil)
}

func StartContainerStream(ctx context.Context, req ContainerRunRequest, onEvent func(client.BootEvent) error) (*ContainerSession, error) {
	if req.Persistent {
		return startPersistentContainer(ctx, req, onEvent)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	readyCh := make(chan error, 1)
	doneCh := make(chan sessionRunResult, 1)

	go func() {
		result, err := runContainer(runCtx, req, readyCh)
		doneCh <- sessionRunResult{result: result, err: err}
	}()

	select {
	case err := <-readyCh:
		if err != nil {
			cancel()
			res := <-doneCh
			if res.err != nil {
				return nil, res.err
			}
			return nil, err
		}
		return &ContainerSession{cancel: cancel, doneCh: doneCh}, nil
	case <-ctx.Done():
		cancel()
		res := <-doneCh
		if res.err != nil {
			return nil, res.err
		}
		return nil, ctx.Err()
	}
}

func (s *ContainerSession) Wait() error {
	res := <-s.doneCh
	if s.closeDone != nil {
		<-s.closeDone
	}
	return res.err
}

func (s *ContainerSession) AddShare(ctx context.Context, share client.ShareMount) error {
	_ = ctx
	if s.rootFS == nil {
		return fmt.Errorf("root filesystem does not support runtime shares")
	}
	key := strings.TrimSpace(share.Mount)
	if key == "" {
		return fmt.Errorf("share mount path is required")
	}
	s.shareMu.Lock()
	if existing, ok := s.shares[key]; ok {
		s.shareMu.Unlock()
		if existing.Source == share.Source && existing.Writable == share.Writable {
			return nil
		}
		return fmt.Errorf("share mount %q already exists", key)
	}
	s.shareMu.Unlock()
	mount, err := arm64vm.BuildShareMount(0, DirectoryShare{
		Source:   share.Source,
		Mount:    share.Mount,
		Writable: share.Writable,
	})
	if err != nil {
		return err
	}
	if err := s.rootFS.AddShare(mount); err != nil {
		return err
	}
	s.shareMu.Lock()
	if s.shares == nil {
		s.shares = make(map[string]client.ShareMount)
	}
	s.shares[key] = share
	s.shareMu.Unlock()
	return nil
}

func (s *ContainerSession) AddPortForward(ctx context.Context, forward client.PortForward) error {
	_, _ = ctx, forward
	return fmt.Errorf("instance network port forwarding is not supported on darwin/arm64")
}

func (s *ContainerSession) AddImage(ctx context.Context, mount string, image *oci.Image) error {
	_ = ctx
	if s.rootFS == nil {
		return fmt.Errorf("root filesystem does not support runtime image mounts")
	}
	key := strings.TrimSpace(mount)
	if key == "" {
		return fmt.Errorf("image mount path is required")
	}
	if image == nil || image.RootFS == nil {
		return fmt.Errorf("image root filesystem is not available")
	}
	s.shareMu.Lock()
	if existing, ok := s.imageMounts[key]; ok {
		s.shareMu.Unlock()
		if existing == image.Name {
			return nil
		}
		return fmt.Errorf("image mount %q already exists", key)
	}
	if _, ok := s.shares[key]; ok {
		s.shareMu.Unlock()
		return fmt.Errorf("mount path %q is already in use", key)
	}
	s.shareMu.Unlock()
	if err := s.rootFS.AddShare(virtio.ShareMount{
		GuestPath: key,
		Backend:   virtio.NewImageFS(image.RootFS, image.RootFSDir),
		Writable:  false,
	}); err != nil {
		return err
	}
	s.shareMu.Lock()
	if s.imageMounts == nil {
		s.imageMounts = make(map[string]string)
	}
	s.imageMounts[key] = image.Name
	s.shareMu.Unlock()
	return nil
}

func (s *ContainerSession) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	startTime := time.Now()
	if len(req.Command) == 0 {
		return client.ExecResponse{}, fmt.Errorf("exec command is required")
	}
	s.markExecActive()
	defer s.markExecDone()
	user := strings.TrimSpace(req.User)
	if user != "" && user != "root" && user != "0" && user != "0:0" {
		return client.ExecResponse{}, fmt.Errorf("only root user is supported")
	}

	env := effectiveExecEnv(s.baseEnv, req.Env, req.ReplaceEnv)
	command := append([]string(nil), req.Command...)
	if !req.SkipResolve {
		if s.image == nil || s.image.RootFS == nil {
			return client.ExecResponse{}, fmt.Errorf("running instance does not have a default image root filesystem")
		}
		var err error
		command, err = imagefs.ResolveCommand(s.image.RootFS, req.Command, env)
		if err != nil {
			return client.ExecResponse{}, err
		}
	}
	timingLog("session.Exec ResolveCommand took=%s argv=%q", time.Since(startTime), req.Command)
	workDir := req.WorkDir
	if workDir == "" {
		workDir = s.workDir
	}
	if workDir == "" {
		workDir = "/"
	}
	if !strings.HasPrefix(workDir, "/") {
		return client.ExecResponse{}, fmt.Errorf("workdir must be absolute")
	}
	id := strconv.FormatUint(s.nextID.Add(1), 10)

	payload, err := json.Marshal(guestExecRequest{
		Kind:        "exec",
		ID:          id,
		Command:     command,
		Env:         env,
		RootDir:     req.RootDir,
		ReplaceEnv:  req.ReplaceEnv,
		SkipResolve: req.SkipResolve,
		WorkDir:     workDir,
		Stdin:       append([]byte(nil), req.Stdin...),
		TTY:         req.TTY,
		Cols:        req.Cols,
		Rows:        req.Rows,
	})
	if err != nil {
		return client.ExecResponse{}, fmt.Errorf("marshal exec request: %w", err)
	}

	start := s.transcript.Len()
	s.sendMu.Lock()
	err = s.writeControlPayload(append(payload, '\n'))
	s.sendMu.Unlock()
	if err != nil {
		return client.ExecResponse{}, err
	}
	timingLog("session.Exec writeControlPayload took=%s argv=%q id=%s", time.Since(startTime), req.Command, id)
	if err := s.sendStdinClose(id); err != nil {
		return client.ExecResponse{}, err
	}
	timingLog("session.Exec sendStdinClose took=%s argv=%q id=%s", time.Since(startTime), req.Command, id)

	beginSegment, err := s.transcript.WaitFor(ctx, start, func(text string) bool {
		return hasManagedExecBegin(text, id)
	})
	if err != nil {
		return client.ExecResponse{}, err
	}
	timingLog("session.Exec waitForBegin took=%s argv=%q id=%s segment_bytes=%d", time.Since(startTime), req.Command, id, len(beginSegment))
	firstByteSegment, err := s.transcript.WaitFor(ctx, start, func(text string) bool {
		return hasManagedExecFirstByte(text, id)
	})
	if err != nil {
		return client.ExecResponse{}, err
	}
	timingLog("session.Exec waitForFirstByte took=%s argv=%q id=%s segment_bytes=%d", time.Since(startTime), req.Command, id, len(firstByteSegment))
	segment, err := s.transcript.WaitFor(ctx, start, func(text string) bool {
		_, _, ok := extractManagedExecResult(text, id, s.dmesg)
		return ok
	})
	if err != nil {
		return client.ExecResponse{}, err
	}
	timingLog("session.Exec waitForResult took=%s argv=%q id=%s segment_bytes=%d", time.Since(startTime), req.Command, id, len(segment))
	if phases := parseExecTimingMarkers(segment, id); len(phases) > 0 {
		order := []string{"recv", "start_begin", "started", "wait_done", "streams_done", "exit_sent"}
		parts := make([]string, 0, len(order))
		for _, name := range order {
			if ms, ok := phases[name]; ok {
				parts = append(parts, fmt.Sprintf("%s=%dms", name, ms))
			}
		}
		if len(parts) > 0 {
			timingLog("session.Exec guest phases argv=%q id=%s %s", req.Command, id, strings.Join(parts, " "))
		}
	}
	exitCode, output, ok := extractManagedExecResult(segment, id, s.dmesg)
	if !ok {
		return client.ExecResponse{}, fmt.Errorf("exec did not produce a complete result")
	}
	timingLog("session.Exec total=%s argv=%q id=%s exit=%d output_bytes=%d", time.Since(startTime), req.Command, id, exitCode, len(output))
	return client.ExecResponse{ExitCode: exitCode, Output: output}, nil
}

func (s *ContainerSession) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	execStart := time.Now()
	if len(req.Command) == 0 {
		return fmt.Errorf("exec command is required")
	}
	s.markExecActive()
	defer s.markExecDone()
	user := strings.TrimSpace(req.User)
	if user != "" && user != "root" && user != "0" && user != "0:0" {
		return fmt.Errorf("only root user is supported")
	}

	env := effectiveExecEnv(s.baseEnv, req.Env, req.ReplaceEnv)
	command := append([]string(nil), req.Command...)
	start := time.Now()
	if !req.SkipResolve {
		var err error
		command, err = imagefs.ResolveCommand(s.image.RootFS, req.Command, env)
		if err != nil {
			return err
		}
	}
	timing.Since(ctx, "exec.resolve_command", start)
	workDir := req.WorkDir
	if workDir == "" {
		workDir = s.workDir
	}
	if workDir == "" {
		workDir = "/"
	}
	if !strings.HasPrefix(workDir, "/") {
		return fmt.Errorf("workdir must be absolute")
	}

	id := strconv.FormatUint(s.nextID.Add(1), 10)
	start = time.Now()
	payload, err := json.Marshal(guestExecRequest{
		Kind:        "exec",
		ID:          id,
		Command:     command,
		Env:         env,
		RootDir:     req.RootDir,
		ReplaceEnv:  req.ReplaceEnv,
		SkipResolve: req.SkipResolve,
		WorkDir:     workDir,
		Stdin:       append([]byte(nil), req.Stdin...),
		TTY:         req.TTY,
		Cols:        req.Cols,
		Rows:        req.Rows,
	})
	if err != nil {
		return fmt.Errorf("marshal exec request: %w", err)
	}
	timing.Since(ctx, "exec.marshal_request", start)

	transcriptStart := s.transcript.Len()
	writeStart := time.Now()
	s.sendMu.Lock()
	err = s.writeControlPayload(append(payload, '\n'))
	s.sendMu.Unlock()
	if err != nil {
		return err
	}
	timing.Since(ctx, "exec.write_control_payload", writeStart)

	if inputs != nil {
		go s.forwardExecInputs(ctx, id, inputs)
	} else {
		stdinStart := time.Now()
		if err := s.sendStdinClose(id); err != nil {
			return err
		}
		timing.Since(ctx, "exec.send_stdin_close", stdinStart)
	}

	streamStart := time.Now()
	err = s.streamExecEvents(ctx, transcriptStart, id, execStart, onEvent)
	timing.Since(ctx, "exec.stream_events", streamStart)
	timing.Since(ctx, "exec.total", execStart)
	return err
}

func (s *ContainerSession) forwardExecInputs(ctx context.Context, id string, inputs <-chan client.ExecInput) {
	for {
		select {
		case <-ctx.Done():
			return
		case input, ok := <-inputs:
			if !ok {
				_ = s.sendStdinClose(id)
				return
			}
			msg := guestExecRequest{ID: id, Kind: input.Kind}
			switch input.Kind {
			case "stdin":
				if len(input.Data) > 0 {
					msg.Stdin = append([]byte(nil), input.Data...)
				} else if input.Input != "" {
					msg.Stdin = []byte(input.Input)
				}
			case "signal":
				msg.Signal = input.Signal
			case "resize":
				msg.Cols = input.Cols
				msg.Rows = input.Rows
			}
			payload, err := json.Marshal(msg)
			if err != nil {
				return
			}
			s.sendMu.Lock()
			_ = s.writeControlPayload(append(payload, '\n'))
			s.sendMu.Unlock()
		}
	}
}

func (s *ContainerSession) sendStdinClose(id string) error {
	payload, err := json.Marshal(guestExecRequest{ID: id, Kind: "stdin_close"})
	if err != nil {
		return fmt.Errorf("marshal stdin close request: %w", err)
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.writeControlPayload(append(payload, '\n'))
}

func (s *ContainerSession) streamExecEvents(ctx context.Context, start int, id string, execStart time.Time, onEvent func(client.ExecEvent) error) error {
	totalStart := time.Now()
	offset := start
	var pending string
	var loops, reads, lines, matched, ignored, sleeps int
	guestPhases := map[string]int{}
	for {
		loops++
		readStart := time.Now()
		text := s.transcript.String()
		timing.Since(ctx, "exec.stream_events.transcript_string", readStart)
		if offset < len(text) {
			reads++
			appendStart := time.Now()
			pending += text[offset:]
			offset = len(text)
			timing.Since(ctx, "exec.stream_events.append_pending", appendStart)
			for {
				lineEnd := strings.IndexByte(pending, '\n')
				if lineEnd < 0 {
					break
				}
				lines++
				lineStart := time.Now()
				line := strings.TrimSpace(pending[:lineEnd])
				pending = pending[lineEnd+1:]
				timing.Since(ctx, "exec.stream_events.next_line", lineStart)
				if phase, ms, ok := recordExecTimingLine(ctx, line, id); ok {
					recordExecObservedTiming(ctx, phase, ms, execStart, guestPhases)
				}
				parseStart := time.Now()
				event, done, ok, err := parseManagedExecEventLine(line, id)
				timing.Since(ctx, "exec.stream_events.parse_line", parseStart)
				if err != nil {
					return err
				}
				if !ok {
					ignored++
					continue
				}
				matched++
				if onEvent != nil {
					callbackStart := time.Now()
					if err := onEvent(event); err != nil {
						return err
					}
					timing.Since(ctx, "exec.stream_events.callback", callbackStart)
				}
				if done {
					recordExecStreamCounts(ctx, loops, reads, lines, matched, ignored, sleeps)
					timing.Since(ctx, "exec.stream_events.until_done", totalStart)
					return nil
				}
			}
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		sleeps++
		sleepStart := time.Now()
		time.Sleep(5 * time.Millisecond)
		timing.Since(ctx, "exec.stream_events.sleep", sleepStart)
	}
}

func recordExecTimingLine(ctx context.Context, line, id string) (string, int, bool) {
	prefix := execTimingMarker + id + ":"
	if !strings.HasPrefix(line, prefix) {
		return "", 0, false
	}
	rest := strings.TrimPrefix(line, prefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return "", 0, false
	}
	ms, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return "", 0, false
	}
	phase := strings.TrimSpace(parts[0])
	if phase == "" {
		return "", 0, false
	}
	timing.Record(ctx, "exec.guest."+phase, time.Duration(ms)*time.Millisecond)
	return phase, ms, true
}

func recordExecObservedTiming(ctx context.Context, phase string, ms int, execStart time.Time, guestPhases map[string]int) {
	timing.Since(ctx, "exec.host_observed."+phase, execStart)
	if prevPhase, ok := previousExecPhase(phase); ok {
		if prevMS, ok := guestPhases[prevPhase]; ok && ms >= prevMS {
			timing.Record(ctx, "exec.guest_delta."+prevPhase+"_to_"+phase, time.Duration(ms-prevMS)*time.Millisecond)
		}
	}
	guestPhases[phase] = ms
}

func previousExecPhase(phase string) (string, bool) {
	switch phase {
	case "start_begin":
		return "recv", true
	case "start_call":
		return "start_begin", true
	case "started":
		return "start_call", true
	case "wait_begin":
		return "started", true
	case "first_stdout":
		return "started", true
	case "first_stderr":
		return "started", true
	case "wait_done":
		return "wait_begin", true
	case "streams_done":
		return "wait_done", true
	case "exit_sent":
		return "streams_done", true
	default:
		return "", false
	}
}

func recordExecStreamCounts(ctx context.Context, loops, reads, lines, matched, ignored, sleeps int) {
	recorder := timing.FromContext(ctx)
	if recorder == nil {
		return
	}
	recordCount(recorder, "exec.stream_events.loop", loops)
	recordCount(recorder, "exec.stream_events.read", reads)
	recordCount(recorder, "exec.stream_events.line", lines)
	recordCount(recorder, "exec.stream_events.matched_line", matched)
	recordCount(recorder, "exec.stream_events.ignored_line", ignored)
	recordCount(recorder, "exec.stream_events.sleep_count", sleeps)
}

func recordCount(recorder *timing.Recorder, name string, count int) {
	if recorder == nil || count <= 0 {
		return
	}
	recorder.RecordCount(name, count)
}

func (s *ContainerSession) Close() error {
	if s.control != nil {
		_ = s.control.Close()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	if s.vsock != nil {
		_ = s.vsock.Close()
	}
	s.cancel()
	return nil
}

func (s *ContainerSession) writeControlPayload(payload []byte) error {
	if s.control != nil {
		_, err := s.control.Write(payload)
		return err
	}
	if s.uart == nil {
		return fmt.Errorf("control channel is not available")
	}
	return s.uart.InjectRXBytes(payload)
}

func (s *ContainerSession) markExecActive() {
	if s.activeExecs != nil {
		s.activeExecs.Add(1)
	}
}

func (s *ContainerSession) markExecDone() {
	if s.activeExecs != nil {
		s.activeExecs.Add(-1)
	}
}

func startPersistentContainer(ctx context.Context, req ContainerRunRequest, onEvent func(client.BootEvent) error) (*ContainerSession, error) {
	start := time.Now()
	if req.Image == nil && req.RootFS == nil {
		return nil, fmt.Errorf("image or rootfs backend is required")
	}
	if len(req.Kernel) == 0 {
		return nil, fmt.Errorf("kernel is required")
	}
	if req.CPUs > 1 {
		return nil, fmt.Errorf("only 1 CPU is supported")
	}

	user := strings.TrimSpace(req.User)
	if user == "" && req.Image != nil {
		user = strings.TrimSpace(req.Image.Config.User)
	}
	if user != "" && user != "root" && user != "0" && user != "0:0" {
		return nil, fmt.Errorf("only root user is supported")
	}

	workDir := req.WorkDir
	if workDir == "" && req.Image != nil {
		workDir = req.Image.Config.WorkingDir
	}
	if workDir == "" {
		workDir = "/"
	}
	if !strings.HasPrefix(workDir, "/") {
		return nil, fmt.Errorf("workdir must be absolute")
	}

	var baseEnv []string
	if req.Image != nil {
		baseEnv = append([]string(nil), req.Image.Config.Env...)
	}
	baseEnv = vmruntime.WithDefaultEnv(baseEnv)

	initrd, err := arm64vm.BuildPersistentInitramfs(req, baseEnv, workDir)
	if err != nil {
		return nil, fmt.Errorf("build initramfs: %w", err)
	}
	timing.Since(ctx, "hvf.build_persistent_initramfs", start)
	timingLog("hvf.StartContainer initramfs.Build took=%s size=%d", time.Since(start), len(initrd))
	start = time.Now()

	vm, err := NewVMWithContext(ctx)
	if err != nil {
		return nil, err
	}
	timing.Since(ctx, "hvf.new_vm", start)
	timingLog("hvf.StartContainer NewVM took=%s", time.Since(start))
	start = time.Now()

	memorySize := arm64vm.MemorySizeBytes(req.MemoryMB)
	mem, err := vm.MapAnonymousMemory(uintptr(memorySize), IPA(arm64vm.MemoryBase), hvMemoryRead|hvMemoryWrite|hvMemoryExec)
	if err != nil {
		vm.Close()
		return nil, fmt.Errorf("map guest memory: %w", err)
	}
	timing.Since(ctx, "hvf.map_anonymous_memory", start)
	timingLog("hvf.StartContainer MapAnonymousMemory took=%s", time.Since(start))
	start = time.Now()

	serialOut := newSerialTranscript()
	var serialWriter io.Writer = serialOut
	var bootWriter *bootEventWriter
	if onEvent != nil {
		bootWriter = newBootEventWriter(onEvent)
		serialWriter = io.MultiWriter(serialOut, bootWriter)
		defer bootWriter.Close()
	}
	var consoleOut bytes.Buffer
	var fsTrace bytes.Buffer
	var runTrace bytes.Buffer
	uart := serial.NewUART8250(arm64vm.DefaultUARTBase, arm64vm.DefaultUARTRegShift, serialWriter)
	uart.AttachIRQ(vm, arm64vm.UARTSPI)
	console := virtio.NewConsole(arm64vm.ConsoleBase, arm64vm.ConsoleSize, arm64vm.ConsoleIRQ, &consoleOut)
	console.Attach(vm, vm)
	rng := virtio.NewRNG(arm64vm.RNGBase, arm64vm.RNGSize, arm64vm.RNGIRQ)
	rng.Attach(vm, vm)
	vsockBackend := virtio.NewSimpleVsockBackend()
	listener, err := vsockBackend.Listen(vmruntime.ControlPort)
	if err != nil {
		vm.Close()
		return nil, fmt.Errorf("listen vsock control: %w", err)
	}
	vsock := virtio.NewVsock(arm64vm.VsockBase, arm64vm.VsockSize, arm64vm.VsockIRQ, vmruntime.GuestCID, vsockBackend)
	vsock.Attach(vm, vm)
	fsdevs, rootFS, err := arm64vm.BuildFSDevices(req, &fsTrace)
	if err != nil {
		_ = listener.Close()
		vm.Close()
		return nil, err
	}
	attachFSDeviceTiming(ctx, fsdevs)
	for _, fsdev := range fsdevs {
		fsdev.Attach(vm, vm)
	}
	timing.Since(ctx, "hvf.device_setup", start)
	timingLog("hvf.StartContainer device setup took=%s fsdevs=%d", time.Since(start), len(fsdevs))
	start = time.Now()

	plan, err := arm64vm.PrepareBoot(mem, req.Kernel, initrd, arm64vm.BootConfig{
		MemoryMB:   req.MemoryMB,
		Dmesg:      req.Dmesg,
		ExtraNodes: arm64vm.AppendFSNodes([]fdt.Node{console.DeviceTreeNode(), rng.DeviceTreeNode(), vsock.DeviceTreeNode()}, fsdevs),
		RecordTime: func(name string, duration time.Duration) {
			timing.Record(ctx, "hvf.prepare_boot."+name, duration)
		},
	})
	if err != nil {
		vm.Close()
		return nil, fmt.Errorf("prepare boot: %w", err)
	}
	timing.Since(ctx, "hvf.prepare_boot", start)
	timingLog("hvf.StartContainer PrepareBoot took=%s", time.Since(start))
	start = time.Now()

	if err := vm.SetReg(hvRegPC, plan.EntryGPA); err != nil {
		vm.Close()
		return nil, fmt.Errorf("set PC: %w", err)
	}
	if err := vm.SetReg(hvRegCPSR, arm64vm.DefaultPStateBits); err != nil {
		vm.Close()
		return nil, fmt.Errorf("set CPSR: %w", err)
	}
	if err := vm.SetSysReg(hvSysRegSP_EL1, plan.StackTopGPA); err != nil {
		vm.Close()
		return nil, fmt.Errorf("set SP_EL1: %w", err)
	}
	if err := vm.SetReg(hvRegX0, plan.DeviceTreeGPA); err != nil {
		vm.Close()
		return nil, fmt.Errorf("set X0: %w", err)
	}
	for _, reg := range []Reg{hvRegX1, hvRegX2, hvRegX3} {
		if err := vm.SetReg(reg, 0); err != nil {
			vm.Close()
			return nil, fmt.Errorf("clear reg %d: %w", reg, err)
		}
	}
	timing.Since(ctx, "hvf.register_setup", start)
	timingLog("hvf.StartContainer register setup took=%s", time.Since(start))
	start = time.Now()

	runCtx, cancel := context.WithCancel(context.Background())
	readyCh := make(chan error, 1)
	doneCh := make(chan sessionRunResult, 1)
	closeDone := make(chan struct{})
	controlTranscript := newSerialTranscript()
	controlAcceptCh := make(chan readyResult, 1)
	controlConnCh := make(chan readyResult, 1)
	activeExecs := &atomic.Int32{}
	guestReady := &atomic.Bool{}
	sendReady := func(err error) {
		select {
		case readyCh <- err:
		default:
		}
	}

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			controlAcceptCh <- readyResult{err: err}
			return
		}
		go func() {
			_, _ = io.Copy(controlTranscript, conn)
		}()
		controlAcceptCh <- readyResult{conn: conn}
	}()

	go func() {
		select {
		case res := <-controlAcceptCh:
			if res.err != nil {
				sendReady(res.err)
				return
			}
			text, err := controlTranscript.WaitFor(runCtx, 0, func(text string) bool {
				return strings.Contains(text, instanceReadyMarker)
			})
			if err != nil {
				_ = res.conn.Close()
				sendReady(err)
				return
			}
			if initMS, ok := parseInitDurationMarker(text); ok {
				totalMS := int(time.Since(start).Milliseconds())
				kernelMS := totalMS - initMS
				if kernelMS < 0 {
					kernelMS = 0
				}
				timingLog("hvf.StartContainer kernel-to-init=%dms init=%dms", kernelMS, initMS)
			} else {
				timingLog("hvf.StartContainer init duration marker missing")
			}
			timingLog("hvf.StartContainer guest ready marker took=%s", time.Since(start))
			guestReady.Store(true)
			controlConnCh <- res
			sendReady(nil)
		case <-runCtx.Done():
			sendReady(runCtx.Err())
		}
	}()

	if req.Dmesg {
		go func() {
			text, err := serialOut.WaitFor(runCtx, 0, hasFatalBootText)
			if err != nil || guestReady.Load() {
				return
			}
			sendReady(fmt.Errorf("guest reported boot failure\nserial:\n%s", text))
			cancel()
		}()
	}

	go func() {
		if onEvent != nil {
			_ = onEvent(client.BootEvent{Kind: "status", Message: "waiting for guest to boot"})
		}
	}()

	go func() {
		defer close(closeDone)
		defer vm.Close()
		for {
			active := activeExecs.Load() > 0
			runSlice := persistentRunSlice(guestReady.Load(), active)
			runStart := time.Now()
			exitInfo, err, stalled := runWithCancel(runCtx, vm, runSlice)
			timing.Since(ctx, "hvf.run_loop.run_with_cancel", runStart)
			if stalled {
				if active {
					recordCount(timing.FromContext(ctx), "hvf.run_loop.stalled_active_exec", 1)
					timing.Record(ctx, "hvf.run_loop.active_exec_stall_slice", runSlice)
				} else {
					recordCount(timing.FromContext(ctx), "hvf.run_loop.stalled_idle", 1)
				}
				if runCtx.Err() != nil {
					doneCh <- sessionRunResult{err: runCtx.Err()}
					return
				}
				continue
			}
			if err != nil {
				doneCh <- sessionRunResult{err: fmt.Errorf("%w\nrun:\n%sserial:\n%s\nvirtio-fs:\n%s", err, runTrace.String(), serialOut.String(), fsTrace.String())}
				return
			}
			if exitInfo == nil {
				doneCh <- sessionRunResult{err: fmt.Errorf("vcpu returned nil exit info")}
				return
			}
			if exitInfo.Reason == hvExitReasonVTimerActivated {
				if err := injectVirtualTimerPPI(vm); err != nil {
					doneCh <- sessionRunResult{err: fmt.Errorf("inject virtual timer ppi: %w", err)}
					return
				}
				continue
			}
			if exitInfo.Reason == hvExitReasonCanceled {
				// HVF can occasionally surface a canceled run even when we did not
				// explicitly cancel the vCPU slice. Treat it like a retry instead of
				// tearing down the persistent guest during startup.
				continue
			}
			if exitInfo.Reason != hvExitReasonException {
				doneCh <- sessionRunResult{err: fmt.Errorf("unexpected exit reason %v", exitInfo.Reason)}
				return
			}
			switch DecodeExceptionClass(exitInfo.Exception.Syndrome) {
			case ExceptionClassDataAbortLowerEL:
				if err := handleContainerDataAbort(ctx, vm, uart, console, rng, fsdevs, vsock, exitInfo); err != nil {
					doneCh <- sessionRunResult{err: err}
					return
				}
			case ExceptionClassSystemRegister:
				handled, err := vm.HandleSystemInstruction(exitInfo.Exception.Syndrome)
				if err != nil {
					doneCh <- sessionRunResult{err: err}
					return
				}
				if !handled {
					pc, _ := vm.GetProgramCounter()
					info, _ := DecodeSystemInstruction(exitInfo.Exception.Syndrome)
					doneCh <- sessionRunResult{err: fmt.Errorf("unsupported system instruction trap pc=%#x syndrome=%#x op0=%d op1=%d op2=%d crn=%d crm=%d rt=%d read=%t\nserial:\n%s\nvirtio-fs:\n%s",
						pc, exitInfo.Exception.Syndrome, info.Op0, info.Op1, info.Op2, info.CRn, info.CRm, info.RawRt, info.Read, serialOut.String(), fsTrace.String())}
					return
				}
			case ExceptionClassHVC64:
				halt, err := handleContainerHVC(vm)
				if err != nil {
					doneCh <- sessionRunResult{err: err}
					return
				}
				if halt {
					doneCh <- sessionRunResult{err: fmt.Errorf("guest halted while instance was running\nserial:\n%s\nvirtio-fs:\n%s", serialOut.String(), fsTrace.String())}
					return
				}
			default:
				pc, _ := vm.GetProgramCounter()
				doneCh <- sessionRunResult{err: fmt.Errorf("unexpected exception class %#x pc=%#x syndrome=%#x physical=%#x\nserial:\n%s\nvirtio-fs:\n%s",
					DecodeExceptionClass(exitInfo.Exception.Syndrome), pc, exitInfo.Exception.Syndrome, uint64(exitInfo.Exception.PhysicalAddress), serialOut.String(), fsTrace.String())}
				return
			}
		}
	}()

	select {
	case err := <-readyCh:
		timing.Since(ctx, "hvf.wait_guest_ready", start)
		if err != nil {
			cancel()
			_ = listener.Close()
			res := <-doneCh
			<-closeDone
			if res.err != nil {
				return nil, res.err
			}
			if req.Dmesg && serialOut.Len() > 0 {
				return nil, fmt.Errorf("%w\nserial:\n%s", err, serialOut.String())
			}
			return nil, err
		}
		res, ok := <-controlConnCh
		if !ok || res.err != nil || res.conn == nil {
			cancel()
			_ = listener.Close()
			resDone := <-doneCh
			<-closeDone
			if resDone.err != nil {
				return nil, resDone.err
			}
			if res.err != nil {
				return nil, res.err
			}
			return nil, fmt.Errorf("guest control connection became ready without an accepted vsock connection")
		}
		_ = listener.Close()
		timingLog("hvf.StartContainer total ready=%s", time.Since(start))
		shareState := make(map[string]client.ShareMount, len(req.Shares))
		for _, share := range req.Shares {
			shareState[strings.TrimSpace(share.Mount)] = client.ShareMount{
				Source:   share.Source,
				Mount:    share.Mount,
				Writable: share.Writable,
			}
		}
		return &ContainerSession{
			cancel:      cancel,
			doneCh:      doneCh,
			closeDone:   closeDone,
			image:       req.Image,
			baseEnv:     baseEnv,
			workDir:     workDir,
			dmesg:       req.Dmesg,
			control:     res.conn,
			transcript:  controlTranscript,
			vsock:       vsock,
			rootFS:      rootFS,
			shares:      shareState,
			activeExecs: activeExecs,
		}, nil
	case res := <-doneCh:
		cancel()
		_ = listener.Close()
		<-closeDone
		if res.err != nil {
			return nil, res.err
		}
		return nil, fmt.Errorf("guest exited before control connection became ready")
	case <-ctx.Done():
		cancel()
		_ = listener.Close()
		res := <-doneCh
		<-closeDone
		if res.err != nil {
			return nil, res.err
		}
		if req.Dmesg && serialOut.Len() > 0 {
			return nil, fmt.Errorf("%w\nserial:\n%s", ctx.Err(), serialOut.String())
		}
		return nil, ctx.Err()
	}
}

func persistentRunSlice(ready bool, active bool) time.Duration {
	switch {
	case !ready:
		return 10 * time.Millisecond
	case active:
		return 5 * time.Millisecond
	default:
		return 250 * time.Millisecond
	}
}

func RunContainer(ctx context.Context, req ContainerRunRequest) (ContainerRunResult, error) {
	return runContainer(ctx, req, nil)
}

func runContainer(ctx context.Context, req ContainerRunRequest, readyCh chan<- error) (ContainerRunResult, error) {
	if req.Image == nil && req.RootFS == nil {
		return ContainerRunResult{}, fmt.Errorf("image or rootfs backend is required")
	}
	if len(req.Kernel) == 0 {
		return ContainerRunResult{}, fmt.Errorf("kernel is required")
	}
	if req.CPUs > 1 {
		return ContainerRunResult{}, fmt.Errorf("only 1 CPU is supported")
	}
	user := strings.TrimSpace(req.User)
	if user == "" && req.Image != nil {
		user = strings.TrimSpace(req.Image.Config.User)
	}
	if user != "" && user != "root" && user != "0" && user != "0:0" {
		return ContainerRunResult{}, fmt.Errorf("only root user is supported")
	}

	var command []string
	switch {
	case req.Image != nil:
		command = req.Image.Command(req.Command)
		if len(command) == 0 {
			command = []string{"/bin/sh"}
		}
	default:
		command = append([]string(nil), req.Command...)
		if len(command) == 0 {
			return ContainerRunResult{}, fmt.Errorf("command is required when running without an image")
		}
	}
	if len(req.Init) == 0 {
		return ContainerRunResult{}, fmt.Errorf("guest init binary is required")
	}

	workDir := req.WorkDir
	if workDir == "" && req.Image != nil {
		workDir = req.Image.Config.WorkingDir
	}
	if workDir == "" {
		workDir = "/"
	}
	if !strings.HasPrefix(workDir, "/") {
		return ContainerRunResult{}, fmt.Errorf("workdir must be absolute")
	}

	var baseEnv []string
	if req.Image != nil {
		baseEnv = req.Image.Config.Env
	}
	env := vmruntime.WithDefaultEnv(vmruntime.MergeEnv(baseEnv, req.Env))

	var err error
	if req.Image != nil {
		command, err = imagefs.ResolveCommand(req.Image.RootFS, command, env)
		if err != nil {
			return ContainerRunResult{}, err
		}
	}
	initrd, err := arm64vm.BuildExecInitramfs(req, command, env, workDir)
	if err != nil {
		return ContainerRunResult{}, fmt.Errorf("build initramfs: %w", err)
	}

	vm, err := NewVM()
	if err != nil {
		return ContainerRunResult{}, err
	}
	defer vm.Close()

	memorySize := arm64vm.MemorySizeBytes(req.MemoryMB)
	mem, err := vm.MapAnonymousMemory(uintptr(memorySize), IPA(arm64vm.MemoryBase), hvMemoryRead|hvMemoryWrite|hvMemoryExec)
	if err != nil {
		return ContainerRunResult{}, fmt.Errorf("map guest memory: %w", err)
	}

	var serialOut bytes.Buffer
	var consoleOut bytes.Buffer
	var fsTrace bytes.Buffer
	var runTrace bytes.Buffer
	uart := serial.NewUART8250(arm64vm.DefaultUARTBase, arm64vm.DefaultUARTRegShift, &serialOut)
	uart.AttachIRQ(vm, arm64vm.UARTSPI)
	console := virtio.NewConsole(arm64vm.ConsoleBase, arm64vm.ConsoleSize, arm64vm.ConsoleIRQ, &consoleOut)
	console.Attach(vm, vm)
	rng := virtio.NewRNG(arm64vm.RNGBase, arm64vm.RNGSize, arm64vm.RNGIRQ)
	rng.Attach(vm, vm)
	var vsock *virtio.Vsock
	fsdevs, _, err := arm64vm.BuildFSDevices(req, &fsTrace)
	if err != nil {
		return ContainerRunResult{}, err
	}
	attachFSDeviceTiming(ctx, fsdevs)
	for _, fsdev := range fsdevs {
		fsdev.Attach(vm, vm)
	}

	plan, err := arm64vm.PrepareBoot(mem, req.Kernel, initrd, arm64vm.BootConfig{
		MemoryMB:   req.MemoryMB,
		Dmesg:      req.Dmesg,
		ExtraNodes: arm64vm.AppendFSNodes([]fdt.Node{console.DeviceTreeNode(), rng.DeviceTreeNode()}, fsdevs),
		RecordTime: func(name string, duration time.Duration) {
			timing.Record(ctx, "hvf.prepare_boot."+name, duration)
		},
	})
	if err != nil {
		return ContainerRunResult{}, fmt.Errorf("prepare boot: %w", err)
	}

	if err := vm.SetReg(hvRegPC, plan.EntryGPA); err != nil {
		return ContainerRunResult{}, fmt.Errorf("set PC: %w", err)
	}
	if err := vm.SetReg(hvRegCPSR, arm64vm.DefaultPStateBits); err != nil {
		return ContainerRunResult{}, fmt.Errorf("set CPSR: %w", err)
	}
	if err := vm.SetSysReg(hvSysRegSP_EL1, plan.StackTopGPA); err != nil {
		return ContainerRunResult{}, fmt.Errorf("set SP_EL1: %w", err)
	}
	if err := vm.SetReg(hvRegX0, plan.DeviceTreeGPA); err != nil {
		return ContainerRunResult{}, fmt.Errorf("set X0: %w", err)
	}
	for _, reg := range []Reg{hvRegX1, hvRegX2, hvRegX3} {
		if err := vm.SetReg(reg, 0); err != nil {
			return ContainerRunResult{}, fmt.Errorf("clear reg %d: %w", reg, err)
		}
	}

	readySent := false
	stallSamples := 0
	var lastSamplePC uint64
	var lastSampleCPSR uint64
	includeTraceOnExit := os.Getenv("CCX3_DEBUG_VIRTIOFS") != ""
	for {
		exitInfo, err, stalled := runWithCancel(ctx, vm, 5*time.Second)
		if stalled {
			pc, _ := vm.GetReg(hvRegPC)
			cpsr, _ := vm.GetReg(hvRegCPSR)
			if stallSamples < 16 && (stallSamples == 0 || pc != lastSamplePC || cpsr != lastSampleCPSR) {
				fmt.Fprintf(&runTrace, "stall pc=%#x cpsr=%#x transcript_len=%d\n", pc, cpsr, serialOut.Len())
				lastSamplePC = pc
				lastSampleCPSR = cpsr
				stallSamples++
			}
			if ctx.Err() != nil {
				return ContainerRunResult{}, fmt.Errorf("%w\npc=%#x cpsr=%#x\nrun:\n%sserial:\n%s\nvirtio-fs:\n%s", ctx.Err(), pc, cpsr, runTrace.String(), serialOut.String(), fsTrace.String())
			}
			continue
		}
		if err != nil {
			return ContainerRunResult{}, fmt.Errorf("%w\nrun:\n%sserial:\n%s\nvirtio-fs:\n%s", err, runTrace.String(), serialOut.String(), fsTrace.String())
		}

		transcript := serialOut.String()
		if !readySent && strings.Contains(transcript, commandBeginMarker) {
			readySent = true
			if readyCh != nil {
				readyCh <- nil
				close(readyCh)
				readyCh = nil
			}
		}
		if exitCode, output, ok := extractCommandResult(transcript, req.Dmesg); ok {
			if includeTraceOnExit {
				transcript = transcript + "\n[virtio-fs trace]\n" + fsTrace.String()
			}
			return ContainerRunResult{
				ExitCode:   exitCode,
				Output:     output,
				Transcript: transcript,
			}, nil
		}

		if exitInfo == nil {
			return ContainerRunResult{}, fmt.Errorf("vcpu returned nil exit info")
		}
		if exitInfo.Reason == hvExitReasonVTimerActivated {
			if err := injectVirtualTimerPPI(vm); err != nil {
				return ContainerRunResult{}, fmt.Errorf("inject virtual timer ppi: %w", err)
			}
			continue
		}
		if exitInfo.Reason != hvExitReasonException {
			return ContainerRunResult{}, fmt.Errorf("unexpected exit reason %v", exitInfo.Reason)
		}

		switch DecodeExceptionClass(exitInfo.Exception.Syndrome) {
		case ExceptionClassDataAbortLowerEL:
			if err := handleContainerDataAbort(ctx, vm, uart, console, rng, fsdevs, vsock, exitInfo); err != nil {
				return ContainerRunResult{}, err
			}
		case ExceptionClassSystemRegister:
			handled, err := vm.HandleSystemInstruction(exitInfo.Exception.Syndrome)
			if err != nil {
				return ContainerRunResult{}, err
			}
			if !handled {
				pc, _ := vm.GetProgramCounter()
				info, _ := DecodeSystemInstruction(exitInfo.Exception.Syndrome)
				return ContainerRunResult{}, fmt.Errorf("unsupported system instruction trap pc=%#x syndrome=%#x op0=%d op1=%d op2=%d crn=%d crm=%d rt=%d read=%t\nrun:\n%sserial:\n%s\nvirtio-fs:\n%s",
					pc, exitInfo.Exception.Syndrome, info.Op0, info.Op1, info.Op2, info.CRn, info.CRm, info.RawRt, info.Read, runTrace.String(), serialOut.String(), fsTrace.String())
			}
		case ExceptionClassHVC64:
			halt, err := handleContainerHVC(vm)
			if err != nil {
				return ContainerRunResult{}, err
			}
			if halt {
				if exitCode, output, ok := extractCommandResult(serialOut.String(), req.Dmesg); ok {
					transcript := serialOut.String()
					if includeTraceOnExit {
						transcript = transcript + "\n[virtio-fs trace]\n" + fsTrace.String()
					}
					return ContainerRunResult{
						ExitCode:   exitCode,
						Output:     output,
						Transcript: transcript,
					}, nil
				}
				return ContainerRunResult{}, fmt.Errorf("guest halted before command completed\nserial:\n%s\nvirtio-fs:\n%s", serialOut.String(), fsTrace.String())
			}
		default:
			pc, _ := vm.GetProgramCounter()
			return ContainerRunResult{}, fmt.Errorf("unexpected exception class %#x pc=%#x syndrome=%#x physical=%#x\nrun:\n%sserial:\n%s\nvirtio-fs:\n%s",
				DecodeExceptionClass(exitInfo.Exception.Syndrome), pc, exitInfo.Exception.Syndrome, uint64(exitInfo.Exception.PhysicalAddress), runTrace.String(), serialOut.String(), fsTrace.String())
		}
	}
}

func runWithCancel(ctx context.Context, vm *VM, timeout time.Duration) (*VcpuExit, error, bool) {
	resCh := make(chan runResultVM, 1)
	go func() {
		exitInfo, err := vm.Run()
		resCh <- runResultVM{exit: exitInfo, err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case res := <-resCh:
		return res.exit, res.err, false
	case <-ctx.Done():
		if err := vm.CancelRun(); err != nil {
			return nil, err, false
		}
		res := <-resCh
		if res.err != nil {
			return nil, res.err, false
		}
		return nil, ctx.Err(), false
	case <-timer.C:
		if err := vm.CancelRun(); err != nil {
			return nil, err, false
		}
		res := <-resCh
		if res.err != nil {
			return nil, res.err, false
		}
		if res.exit == nil {
			return nil, fmt.Errorf("cancelled run returned nil exit"), false
		}
		if res.exit.Reason != hvExitReasonCanceled {
			return res.exit, nil, false
		}
		return nil, nil, true
	}
}

type runResultVM struct {
	exit *VcpuExit
	err  error
}

func handleContainerDataAbort(ctx context.Context, vm *VM, uart *serial.UART8250, console *virtio.Console, rng *virtio.RNG, fsdevs []*virtio.FS, vsock *virtio.Vsock, exitInfo *VcpuExit) error {
	totalStart := time.Now()
	defer timing.Since(ctx, "hvf.data_abort.total", totalStart)
	info, err := DecodeDataAbort(exitInfo.Exception.Syndrome)
	if err != nil {
		return err
	}
	addr := uint64(exitInfo.Exception.PhysicalAddress)

	switch {
	case uart != nil && uart.Contains(addr, info.SizeBytes):
		if info.Write {
			value, err := readAbortValue(vm, info)
			if err != nil {
				return err
			}
			if err := uart.WriteValue(addr, info.SizeBytes, value); err != nil {
				return err
			}
		} else {
			value, err := uart.ReadValue(addr, info.SizeBytes)
			if err != nil {
				return err
			}
			if err := writeAbortValue(vm, info, value); err != nil {
				return err
			}
		}
	case console != nil && console.Contains(addr, info.SizeBytes):
		if info.Write {
			value, err := readAbortValue(vm, info)
			if err != nil {
				return err
			}
			if err := console.Write(addr, info.SizeBytes, value); err != nil {
				return err
			}
		} else {
			value, err := console.Read(addr, info.SizeBytes)
			if err != nil {
				return err
			}
			if err := writeAbortValue(vm, info, value); err != nil {
				return err
			}
		}
	case rng != nil && rng.Contains(addr, info.SizeBytes):
		if info.Write {
			value, err := readAbortValue(vm, info)
			if err != nil {
				return err
			}
			if err := rng.Write(addr, info.SizeBytes, value); err != nil {
				return err
			}
		} else {
			value, err := rng.Read(addr, info.SizeBytes)
			if err != nil {
				return err
			}
			if err := writeAbortValue(vm, info, value); err != nil {
				return err
			}
		}
	case hasFSDevice(fsdevs, addr, info.SizeBytes):
		start := time.Now()
		if err := handleFSDataAbort(vm, fsdevs, addr, info); err != nil {
			return err
		}
		timing.Since(ctx, "hvf.data_abort.virtio_fs", start)
	case vsock != nil && vsock.Contains(addr, info.SizeBytes):
		if info.Write {
			value, err := readAbortValue(vm, info)
			if err != nil {
				return err
			}
			if err := vsock.Write(addr, info.SizeBytes, value); err != nil {
				return err
			}
		} else {
			value, err := vsock.Read(addr, info.SizeBytes)
			if err != nil {
				return err
			}
			if err := writeAbortValue(vm, info, value); err != nil {
				return err
			}
		}
	case mmioInRange(addr, arm64vm.GICDistributorMin, arm64vm.GICDistributorMax) || mmioInRange(addr, arm64vm.GICRedistributorMin, arm64vm.GICRedistributorMax):
		value, err := handleGICAccess(vm, addr, info)
		if err != nil {
			return err
		}
		if !info.Write {
			if err := writeAbortValue(vm, info, value); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unhandled MMIO access addr=%#x size=%d write=%v", addr, info.SizeBytes, info.Write)
	}

	return vm.AdvanceProgramCounter()
}

func attachFSDeviceTiming(ctx context.Context, fsdevs []*virtio.FS) {
	for _, fsdev := range fsdevs {
		if fsdev == nil {
			continue
		}
		fsdev.RecordTiming = func(name string, duration time.Duration) {
			timing.Record(ctx, name, duration)
		}
	}
}

func hasFSDevice(fsdevs []*virtio.FS, addr uint64, size int) bool {
	for _, fsdev := range fsdevs {
		if fsdev != nil && fsdev.Contains(addr, size) {
			return true
		}
	}
	return false
}

func handleFSDataAbort(vm *VM, fsdevs []*virtio.FS, addr uint64, info DataAbortInfo) error {
	for _, fsdev := range fsdevs {
		if fsdev == nil || !fsdev.Contains(addr, info.SizeBytes) {
			continue
		}
		if info.Write {
			value, err := readAbortValue(vm, info)
			if err != nil {
				return err
			}
			return fsdev.Write(addr, info.SizeBytes, value)
		}
		value, err := fsdev.Read(addr, info.SizeBytes)
		if err != nil {
			return err
		}
		return writeAbortValue(vm, info, value)
	}
	return fmt.Errorf("unhandled virtio-fs access addr=%#x size=%d", addr, info.SizeBytes)
}

func handleContainerHVC(vm *VM) (bool, error) {
	x0, err := vm.GetReg(hvRegX0)
	if err != nil {
		return false, err
	}

	const (
		psciVersion         = 0x84000000
		psciCpuSuspend      = 0x84000001
		psciCpuOff          = 0x84000002
		psciCpuOn           = 0x84000003
		psciAffinityInfo    = 0x84000004
		psciMigrateInfoType = 0x84000006
		psciSystemOff       = 0x84000008
		psciSystemReset     = 0x84000009
		psciFeatures        = 0x8400000a
		psciSuccess         = 0
		psciNotSupported    = 0xffffffff
		psciInvalidParams   = 0xfffffffe
		psciTosNotPresent   = 2
	)

	var ret uint64
	switch x0 {
	case psciVersion:
		ret = 0x00010000
	case psciMigrateInfoType:
		ret = psciTosNotPresent
	case psciFeatures:
		ret = psciNotSupported
	case psciCpuSuspend:
		ret = psciNotSupported
	case psciCpuOff:
		ret = psciSuccess
	case psciAffinityInfo:
		ret = psciInvalidParams
	case psciCpuOn:
		ret = psciInvalidParams
	case psciSystemOff, psciSystemReset:
		return true, nil
	default:
		return false, fmt.Errorf("unsupported PSCI call %#x", x0)
	}

	return false, vm.SetReg(hvRegX0, ret)
}

func readAbortValue(vm *VM, info DataAbortInfo) (uint64, error) {
	if info.Target == hvRegXZR {
		return 0, nil
	}
	value, err := vm.GetReg(info.Target)
	if err != nil {
		return 0, err
	}
	if info.SizeBytes >= 8 {
		return value, nil
	}
	return value & ((uint64(1) << (8 * info.SizeBytes)) - 1), nil
}

func writeAbortValue(vm *VM, info DataAbortInfo, value uint64) error {
	if info.Target == hvRegXZR {
		return nil
	}
	if info.SizeBytes < 8 {
		value &= (uint64(1) << (8 * info.SizeBytes)) - 1
	}
	return vm.SetReg(info.Target, value)
}

func mmioInRange(addr, start, end uint64) bool {
	return addr >= start && addr < end
}

func handleGICAccess(vm *VM, addr uint64, info DataAbortInfo) (uint64, error) {
	var value uint64
	if info.Write {
		v, err := readAbortValue(vm, info)
		if err != nil {
			return 0, err
		}
		value = v
	}

	switch {
	case mmioInRange(addr, arm64vm.GICDistributorMin, arm64vm.GICDistributorMax):
		reg := GICDistributorReg(addr - arm64vm.GICDistributorMin)
		if info.Write {
			err := vm.SetGICDistributorReg(reg, value)
			if err != nil && strings.Contains(err.Error(), "denied") {
				return 0, nil
			}
			return 0, err
		}
		val, err := vm.GetGICDistributorReg(reg)
		if err != nil && strings.Contains(err.Error(), "denied") && reg == 0xffe8 {
			return 0x30, nil
		}
		return val, err
	case mmioInRange(addr, arm64vm.GICRedistributorMin, arm64vm.GICRedistributorMax):
		reg := GICRedistributorReg(addr - arm64vm.GICRedistributorMin)
		if info.Write {
			err := vm.SetGICRedistributorReg(reg, value)
			if err != nil && (strings.Contains(err.Error(), "denied") || strings.Contains(err.Error(), "bad argument")) {
				return 0, nil
			}
			return 0, err
		}
		val, err := vm.GetGICRedistributorReg(reg)
		if err != nil && (strings.Contains(err.Error(), "denied") || strings.Contains(err.Error(), "bad argument")) {
			switch reg {
			case 0x0:
				return 0, nil
			case 0xffe8:
				return 0x30, nil
			case 0x8:
				return 1 << 4, nil
			case 0x14:
				return 0, nil
			default:
				return 0, nil
			}
		}
		return val, err
	default:
		return 0, fmt.Errorf("address %#x outside GIC MMIO ranges", addr)
	}
}

func injectVirtualTimerPPI(vm *VM) error {
	const (
		gicrISENABLER0 = GICRedistributorReg(0x10100)
		gicrISPENDR0   = GICRedistributorReg(0x10200)
		timerMask      = uint64(1) << arm64VirtualTimerPPI
	)

	enabled, err := vm.GetGICRedistributorReg(gicrISENABLER0)
	if err == nil && enabled&timerMask == 0 {
		if err := vm.SetGICRedistributorReg(gicrISENABLER0, enabled|timerMask); err != nil {
			return err
		}
	}

	pending, err := vm.GetGICRedistributorReg(gicrISPENDR0)
	if err != nil {
		return err
	}
	if pending&timerMask != 0 {
		return nil
	}
	return vm.SetGICRedistributorReg(gicrISPENDR0, timerMask)
}

func extractCommandResult(serial string, dmesg bool) (int, string, bool) {
	begin := strings.Index(serial, strings.TrimSuffix(commandBeginMarker, ":"))
	exit := strings.Index(serial, commandExitMarkerPref)
	if begin == -1 || exit == -1 || exit < begin {
		return 0, "", false
	}

	rest := serial[exit+len(commandExitMarkerPref):]
	lineEnd := strings.IndexByte(rest, '\n')
	if lineEnd == -1 {
		return 0, "", false
	}
	code, err := strconv.Atoi(strings.TrimSpace(rest[:lineEnd]))
	if err != nil {
		return 0, "", false
	}

	output := serial
	if !dmesg {
		beginOutput := serial[begin+len(commandBeginMarker):]
		if strings.HasPrefix(beginOutput, "\r\n") {
			beginOutput = beginOutput[2:]
		} else if strings.HasPrefix(beginOutput, "\n") {
			beginOutput = beginOutput[1:]
		}
		endOffset := strings.Index(beginOutput, commandExitMarkerPref)
		if endOffset >= 0 {
			output = strings.TrimRight(beginOutput[:endOffset], "\r\n")
		} else {
			output = strings.TrimRight(beginOutput, "\r\n")
		}
		output = cleanCommandOutput(output)
	}
	return code, output, true
}

func extractManagedExecResult(serial, id string, dmesg bool) (int, string, bool) {
	beginMarker := commandBeginMarker + id
	outputPrefix := commandOutputMarker + id + ":"
	errorPrefix := commandErrorMarker + id + ":"
	exitPrefix := commandExitMarkerPref + id + ":"

	begin := strings.Index(serial, beginMarker)
	if begin == -1 {
		return 0, "", false
	}

	var output bytes.Buffer
	lines := strings.Split(serial[begin:], "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, outputPrefix); idx >= 0 {
			encoded := line[idx+len(outputPrefix):]
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				continue
			}
			output.Write(data)
			continue
		}
		if idx := strings.Index(line, errorPrefix); idx >= 0 {
			encoded := line[idx+len(errorPrefix):]
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				continue
			}
			output.Write(data)
			continue
		}
		if idx := strings.Index(line, exitPrefix); idx >= 0 {
			code, err := strconv.Atoi(strings.TrimSpace(line[idx+len(exitPrefix):]))
			if err != nil {
				return 0, "", false
			}
			if dmesg {
				return code, strings.TrimRight(serial[begin:], "\r\n"), true
			}
			return code, strings.TrimRight(output.String(), "\r\n"), true
		}
	}
	return 0, "", false
}

func parseManagedExecEventLine(line, id string) (client.ExecEvent, bool, bool, error) {
	beginMarker := commandBeginMarker + id
	stdoutPrefix := commandOutputMarker + id + ":"
	stderrPrefix := commandErrorMarker + id + ":"
	exitPrefix := commandExitMarkerPref + id + ":"

	switch {
	case line == beginMarker:
		return client.ExecEvent{}, false, false, nil
	case strings.HasPrefix(line, stdoutPrefix):
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, stdoutPrefix))
		if err != nil {
			return client.ExecEvent{}, false, false, nil
		}
		return client.ExecEvent{Kind: "stdout", Stream: "stdout", Output: string(data), Data: data}, false, true, nil
	case strings.HasPrefix(line, stderrPrefix):
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, stderrPrefix))
		if err != nil {
			return client.ExecEvent{}, false, false, nil
		}
		return client.ExecEvent{Kind: "stderr", Stream: "stderr", Output: string(data), Data: data}, false, true, nil
	case strings.HasPrefix(line, exitPrefix):
		code, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, exitPrefix)))
		if err != nil {
			return client.ExecEvent{}, false, false, err
		}
		return client.ExecEvent{Kind: "exit", ExitCode: code}, true, true, nil
	default:
		return client.ExecEvent{}, false, false, nil
	}
}

func effectiveExecEnv(base, overrides []string, replace bool) []string {
	if replace {
		return vmruntime.WithDefaultEnv(overrides)
	}
	return vmruntime.WithDefaultEnv(vmruntime.MergeEnv(base, overrides))
}

func cleanCommandOutput(output string) string {
	lines := strings.Split(output, "\n")
	cleaned := make([]string, 0, len(lines))
	last := ""
	for i, line := range lines {
		if strings.HasPrefix(line, "[") {
			if idx := strings.Index(line, "] "); idx >= 0 {
				lines[i] = line[idx+2:]
			}
		}
		line = strings.TrimSpace(lines[i])
		if line == "" || line == commandBeginMarker || strings.HasPrefix(line, commandExitMarkerPref) {
			continue
		}
		if line == last {
			continue
		}
		cleaned = append(cleaned, line)
		last = line
	}
	return strings.Join(cleaned, "\n")
}
