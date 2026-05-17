package main

import (
	"os"
	"strings"
	"testing"

	"j5.nz/cc/client"
)

func TestFormatProgressEvent(t *testing.T) {
	event := client.ProgressEvent{
		Status:             "downloading",
		Artifact:           "alpine",
		Blob:               "rootfs.simg",
		BytesDownloaded:    1024,
		BytesTotal:         2048,
		RateBytesPerSecond: 512,
		ETASeconds:         2,
	}
	got := formatProgressEvent(event, "fallback")
	for _, want := range []string{"Downloading alpine", "rootfs.simg", "1.0 KB/2.0 KB", "512 B/s", "ETA 2s"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatProgressEvent() = %q, missing %q", got, want)
		}
	}
}

func TestFulltestBackendFromArgs(t *testing.T) {
	if got := fulltestBackendFromArgs([]string{"-recipe", "suite.yaml"}); got != "ccvm" {
		t.Fatalf("default backend = %q", got)
	}
	if got := fulltestBackendFromArgs([]string{"--backend=docker"}); got != "docker" {
		t.Fatalf("equals backend = %q", got)
	}
	if got := fulltestBackendFromArgs([]string{"-backend", "docker"}); got != "docker" {
		t.Fatalf("split backend = %q", got)
	}
}

func TestFormatProgressEventError(t *testing.T) {
	got := formatProgressEvent(client.ProgressEvent{
		Status: "error",
		Error:  "boom",
	}, "alpine")
	if !strings.Contains(got, "Error alpine") || !strings.Contains(got, "boom") {
		t.Fatalf("formatProgressEvent(error) = %q", got)
	}
}

func TestFormatBootEvent(t *testing.T) {
	if got := formatBootEvent(client.BootEvent{Kind: "status", Message: "starting VM"}); got != "Boot: starting VM" {
		t.Fatalf("formatBootEvent(status) = %q", got)
	}
	if got := formatBootEvent(client.BootEvent{Kind: "ready", State: client.InstanceState{Image: "alpine"}}); got != "Boot: ready alpine" {
		t.Fatalf("formatBootEvent(ready) = %q", got)
	}
	if got := formatBootEvent(client.BootEvent{Kind: "error", Error: "timeout"}); got != "Boot error: timeout" {
		t.Fatalf("formatBootEvent(error) = %q", got)
	}
}

func TestStreamHostStdinReadsPipedInput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer r.Close()

	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	ch := make(chan client.ExecInput, 4)
	err = streamHostStdin(r, ch)
	if err != nil {
		t.Fatalf("streamHostStdin() error = %v", err)
	}
	got := <-ch
	if got.Kind != "stdin" || string(got.Data) != "hello\n" {
		t.Fatalf("first input = %#v, want stdin hello", got)
	}
	closed := <-ch
	if closed.Kind != "stdin_close" {
		t.Fatalf("close input = %#v, want stdin_close", closed)
	}
}

func TestSignalName(t *testing.T) {
	tests := []struct {
		sig  os.Signal
		want string
		ok   bool
	}{
		{sig: os.Interrupt, want: "INT", ok: true},
		{sig: unsupportedSignalForTest(), want: "", ok: false},
	}
	for _, tt := range tests {
		got, ok := signalName(tt.sig)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("signalName(%v) = %q, %v; want %q, %v", tt.sig, got, ok, tt.want, tt.ok)
		}
	}
}

func TestValidateServerHelloReportsStartupErrorDetail(t *testing.T) {
	err := validateServerHello(client.ServerHello{
		Kind:   "error",
		Error:  "ccvm failed to start",
		Detail: "listen on localhost: bind failed",
	}, "/tmp/cc-cache")
	if err == nil {
		t.Fatal("validateServerHello() error = nil, want startup error")
	}
	message := err.Error()
	for _, want := range []string{"ccvm daemon failed to start", "/tmp/cc-cache", "bind failed"} {
		if !strings.Contains(message, want) {
			t.Fatalf("validateServerHello() = %q, missing %q", message, want)
		}
	}
}

func TestValidateServerHelloRequiresAddress(t *testing.T) {
	err := validateServerHello(client.ServerHello{}, "/tmp/cc-cache")
	if err == nil || !strings.Contains(err.Error(), "without an address") {
		t.Fatalf("validateServerHello() error = %v, want missing address", err)
	}
}

func TestParsePortForwardSpec(t *testing.T) {
	forward, err := parsePortForwardSpec("8080:80")
	if err != nil {
		t.Fatalf("parsePortForwardSpec() error = %v", err)
	}
	if forward.Protocol != "tcp" || forward.HostAddr != "127.0.0.1" || forward.HostPort != 8080 || forward.GuestPort != 80 {
		t.Fatalf("forward = %#v, want tcp 127.0.0.1:8080 -> 80", forward)
	}

	if _, err := parsePortForwardSpec("8080"); err == nil {
		t.Fatal("parsePortForwardSpec(invalid) error = nil, want error")
	}
	if _, err := parsePortForwardSpec("0:80"); err == nil {
		t.Fatal("parsePortForwardSpec(out of range) error = nil, want error")
	}
}

func TestHandleVMCommandDispatch(t *testing.T) {
	api := &fakeCCAPI{
		statuses: []client.InstanceState{{ID: "alpha", Status: "running"}},
		status:   client.InstanceState{ID: "alpha", Status: "running"},
	}

	if err := handleCommand(api, []string{"vm", "list"}); err != nil {
		t.Fatalf("vm list error = %v", err)
	}
	if !api.listCalled {
		t.Fatal("vm list did not call InstanceStatuses")
	}

	if err := handleCommand(api, []string{"vm", "status", "alpha"}); err != nil {
		t.Fatalf("vm status error = %v", err)
	}
	if api.statusID != "alpha" {
		t.Fatalf("status id = %q, want alpha", api.statusID)
	}

	if err := handleCommand(api, []string{"vm", "start", "alpha", "alpine"}); err != nil {
		t.Fatalf("vm start error = %v", err)
	}
	if api.startID != "alpha" || api.startReq.Image != "alpine" {
		t.Fatalf("start id=%q req=%#v, want alpha/alpine", api.startID, api.startReq)
	}

	if err := handleCommand(api, []string{"vm", "forward", "alpha", "8080:80"}); err != nil {
		t.Fatalf("vm forward error = %v", err)
	}
	if api.forwardID != "alpha" || api.forward.HostPort != 8080 || api.forward.GuestPort != 80 {
		t.Fatalf("forward id=%q forward=%#v, want alpha 8080:80", api.forwardID, api.forward)
	}

	if err := handleCommand(api, []string{"vm", "stop", "alpha"}); err != nil {
		t.Fatalf("vm stop error = %v", err)
	}
	if api.shutdownID != "alpha" {
		t.Fatalf("shutdown id = %q, want alpha", api.shutdownID)
	}
}

func TestHandleVMRunRequiresRunningInstance(t *testing.T) {
	api := &fakeCCAPI{status: client.InstanceState{ID: "alpha", Status: "stopped"}}
	err := handleCommand(api, []string{"vm", "run", "alpha", "--", "true"})
	if err == nil || !strings.Contains(err.Error(), `VM "alpha" is not running`) {
		t.Fatalf("vm run error = %v, want not running", err)
	}
	if api.statusID != "alpha" {
		t.Fatalf("status id = %q, want alpha", api.statusID)
	}
}

type fakeCCAPI struct {
	statuses []client.InstanceState
	status   client.InstanceState

	listCalled bool
	statusID   string
	startID    string
	startReq   client.CreateInstanceRequest
	shutdownID string
	forwardID  string
	forward    client.PortForward
}

func (f *fakeCCAPI) DownloadKernelStream(client.DownloadRequest, func(client.ProgressEvent) error) error {
	return nil
}

func (f *fakeCCAPI) VMSupported() (client.VMSupportedResponse, error) {
	return client.VMSupportedResponse{}, nil
}

func (f *fakeCCAPI) ListImages() ([]client.ImageState, error) {
	return nil, nil
}

func (f *fakeCCAPI) GetImage(string) (client.ImageState, error) {
	return client.ImageState{}, nil
}

func (f *fakeCCAPI) PullImageStream(string, client.PullImageRequest, func(client.ProgressEvent) error) error {
	return nil
}

func (f *fakeCCAPI) CreateInstance(req client.CreateInstanceRequest) (client.InstanceState, error) {
	return client.InstanceState{Status: "running", Image: req.Image}, nil
}

func (f *fakeCCAPI) CreateInstanceStream(req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (client.InstanceState, error) {
	return client.InstanceState{Status: "running", Image: req.Image}, nil
}

func (f *fakeCCAPI) CreateInstanceStreamWithID(id string, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (client.InstanceState, error) {
	f.startID = id
	f.startReq = req
	return client.InstanceState{ID: id, Status: "running", Image: req.Image}, nil
}

func (f *fakeCCAPI) KernelStatus() (client.KernelState, error) {
	return client.KernelState{}, nil
}

func (f *fakeCCAPI) InstanceStatus() (client.InstanceState, error) {
	return f.status, nil
}

func (f *fakeCCAPI) InstanceStatusOf(id string) (client.InstanceState, error) {
	f.statusID = id
	return f.status, nil
}

func (f *fakeCCAPI) InstanceStatuses() ([]client.InstanceState, error) {
	f.listCalled = true
	return f.statuses, nil
}

func (f *fakeCCAPI) ShutdownInstance() error {
	return nil
}

func (f *fakeCCAPI) ShutdownInstanceWithID(id string) error {
	f.shutdownID = id
	return nil
}

func (f *fakeCCAPI) AddPortForwardTo(id string, forward client.PortForward) error {
	f.forwardID = id
	f.forward = forward
	return nil
}

func (f *fakeCCAPI) RunIn(string, client.RunRequest) (client.ExecResponse, error) {
	return client.ExecResponse{}, nil
}

func (f *fakeCCAPI) ExecStream(client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error {
	return nil
}

func (f *fakeCCAPI) ExecStreamIn(string, client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error {
	return nil
}
