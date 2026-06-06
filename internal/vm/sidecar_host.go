package vm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"j5.nz/cc/client"
)

const (
	sidecarDisableEnv = "CCX3_DISABLE_SIDECARS"
	sidecarModeEnv    = "CCX3_CCVM_SIDECAR_MODE"
	sidecarLimitEnv   = "CCX3_SIDECAR_MAX_VMS"
)

type sidecarVMHost struct {
	cacheDir string
	maxVMs   int
}

func NewLocalSidecarVMHost(cacheDir string) VMHost {
	return &sidecarVMHost{
		cacheDir: cacheDir,
		maxVMs:   resolveSidecarLimit(),
	}
}

func resolveSidecarLimit() int {
	raw := strings.TrimSpace(os.Getenv(sidecarLimitEnv))
	if raw == "" {
		return 63
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return 63
	}
	return limit
}

func (h *sidecarVMHost) HostCapabilities(context.Context) VMHostCapabilities {
	return VMHostCapabilities{
		Backend:       "sidecar",
		MaxVMs:        h.maxVMs,
		Locality:      "sidecar",
		SupportsFSRPC: false,
		SupportsL2:    false,
	}
}

func (h *sidecarVMHost) Close() error {
	return nil
}

func (h *sidecarVMHost) Start(ctx context.Context, req client.CreateInstanceRequest) (Instance, error) {
	return h.StartStream(ctx, req, nil)
}

func (h *sidecarVMHost) StartStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	sidecar, err := h.launch(ctx)
	if err != nil {
		return nil, err
	}
	req.ID = DefaultInstanceID
	if _, err := sidecar.api.CreateInstanceStream(req, onEvent); err != nil {
		_ = sidecar.Close()
		return nil, err
	}
	return &sidecarInstance{id: DefaultInstanceID, sidecar: sidecar}, nil
}

func (h *sidecarVMHost) StartBlank(ctx context.Context, req client.StartInstanceRequest) (Instance, error) {
	return h.StartBlankStream(ctx, req, nil)
}

func (h *sidecarVMHost) StartBlankStream(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	sidecar, err := h.launch(ctx)
	if err != nil {
		return nil, err
	}
	req.ID = DefaultInstanceID
	if _, err := sidecar.api.StartInstanceStream(req, onEvent); err != nil {
		_ = sidecar.Close()
		return nil, err
	}
	return &sidecarInstance{id: DefaultInstanceID, sidecar: sidecar}, nil
}

func (h *sidecarVMHost) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	sidecar, err := h.launch(ctx)
	if err != nil {
		return client.ExecResponse{}, err
	}
	defer sidecar.Close()
	return sidecar.api.Run(req)
}

func (h *sidecarVMHost) RunStream(ctx context.Context, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	sidecar, err := h.launch(ctx)
	if err != nil {
		return err
	}
	defer sidecar.Close()
	return sidecar.api.RunInteractiveStream(req, inputs, onEvent)
}

func (h *sidecarVMHost) RunInInstance(ctx context.Context, inst Instance, runningImage string, req client.RunRequest) (client.ExecResponse, error) {
	_ = ctx
	_ = runningImage
	sidecarInst, ok := inst.(*sidecarInstance)
	if !ok {
		return client.ExecResponse{}, fmt.Errorf("instance is not owned by a sidecar host")
	}
	return sidecarInst.sidecar.api.RunIn(sidecarInst.id, req)
}

func (h *sidecarVMHost) RunInInstanceStream(ctx context.Context, inst Instance, runningImage string, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	_ = ctx
	_ = runningImage
	sidecarInst, ok := inst.(*sidecarInstance)
	if !ok {
		return fmt.Errorf("instance is not owned by a sidecar host")
	}
	return sidecarInst.sidecar.api.RunInteractiveStreamIn(sidecarInst.id, req, inputs, onEvent)
}

func (h *sidecarVMHost) launch(ctx context.Context) (*sidecarDaemon, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := sidecarLaunchArgs()
	args = append(args, "-worker", "-cache-dir", h.cacheDir)
	cmd := exec.Command(exe, args...)
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), sidecarDisableEnv+"=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("prepare sidecar stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start sidecar ccvm: %w", err)
	}
	started := true
	defer func() {
		if !started && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	select {
	case <-ctx.Done():
		started = false
		return nil, ctx.Err()
	default:
	}
	var hello client.ServerHello
	if err := json.NewDecoder(stdout).Decode(&hello); err != nil {
		started = false
		return nil, fmt.Errorf("read sidecar startup banner: %w", err)
	}
	if hello.Error != "" || hello.Kind == "error" {
		started = false
		detail := firstNonEmpty(hello.Detail, hello.Error, "unknown startup error")
		return nil, fmt.Errorf("sidecar ccvm failed to start: %s", detail)
	}
	if strings.TrimSpace(hello.Addr) == "" {
		started = false
		return nil, fmt.Errorf("sidecar ccvm did not report an address")
	}
	api := client.NewClient("http://"+hello.Addr, func() (net.Conn, error) {
		return net.Dial("tcp", hello.Addr)
	})
	if err := api.HealthCheck(); err != nil {
		started = false
		return nil, fmt.Errorf("sidecar ccvm health check failed: %w", err)
	}
	return &sidecarDaemon{cmd: cmd, api: api, stdout: stdout}, nil
}

func sidecarLaunchArgs() []string {
	switch strings.TrimSpace(os.Getenv(sidecarModeEnv)) {
	case "vsh-internal":
		return []string{"--vsh-internal-ccvm"}
	default:
		return nil
	}
}

type sidecarDaemon struct {
	cmd    *exec.Cmd
	api    *client.Client
	stdout io.ReadCloser
	once   sync.Once
	err    error
}

func (d *sidecarDaemon) Close() error {
	d.once.Do(func() {
		if d.api != nil {
			d.err = d.api.Shutdown()
		}
		if d.stdout != nil {
			_ = d.stdout.Close()
		}
		if d.cmd != nil {
			done := make(chan error, 1)
			go func() {
				done <- d.cmd.Wait()
			}()
			select {
			case err := <-done:
				if d.err == nil && err != nil {
					d.err = err
				}
			case <-time.After(5 * time.Second):
				if d.cmd.Process != nil {
					_ = d.cmd.Process.Kill()
				}
				if err := <-done; d.err == nil && err != nil {
					d.err = err
				}
			}
		}
	})
	return d.err
}

type sidecarInstance struct {
	id      string
	sidecar *sidecarDaemon
}

func (i *sidecarInstance) AddShare(context.Context, client.ShareMount) error {
	return fmt.Errorf("runtime shares must be sent with the sidecar exec request")
}

func (i *sidecarInstance) AddPortForward(ctx context.Context, forward client.PortForward) error {
	_ = ctx
	return i.sidecar.api.AddPortForwardTo(i.id, forward)
}

func (i *sidecarInstance) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	events, err := i.sidecar.api.ExecEventsIn(i.id, req)
	if err != nil {
		return client.ExecResponse{}, err
	}
	var resp client.ExecResponse
	for _, event := range events {
		if event.Kind == "stdout" || event.Kind == "stderr" {
			resp.Output += event.Output
		}
		if event.Kind == "exit" {
			resp.ExitCode = event.ExitCode
		}
	}
	return resp, ctx.Err()
}

func (i *sidecarInstance) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	_ = ctx
	return i.sidecar.api.ExecStreamIn(i.id, req, inputs, onEvent)
}

func (i *sidecarInstance) Wait() error {
	for {
		state, err := i.sidecar.api.InstanceStatusOf(i.id)
		if err != nil {
			return err
		}
		if state.Status != "running" && state.Status != "starting" {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (i *sidecarInstance) Close() error {
	shutdownErr := i.sidecar.api.ShutdownInstanceWithID(i.id)
	closeErr := i.sidecar.Close()
	if shutdownErr != nil && !strings.Contains(shutdownErr.Error(), "no VM") {
		return errors.Join(shutdownErr, closeErr)
	}
	return closeErr
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
