package main

import (
	"fmt"
	"testing"

	"j5.nz/cc/client"
)

func TestParsePortForwardSpec(t *testing.T) {
	forward, err := parsePortForwardSpec("8080:80")
	if err != nil {
		t.Fatalf("parse port forward: %v", err)
	}
	if forward.Protocol != "tcp" || forward.HostAddr != "127.0.0.1" || forward.HostPort != 8080 || forward.GuestPort != 80 {
		t.Fatalf("forward = %+v", forward)
	}

	for _, spec := range []string{"8080", "0:80", "8080:0", "not-a-port:80"} {
		if _, err := parsePortForwardSpec(spec); err == nil {
			t.Fatalf("parsePortForwardSpec(%q) unexpectedly succeeded", spec)
		}
	}
}

func TestFulltestBackendFromArgs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{args: nil, want: "ccvm"},
		{args: []string{"-backend", "docker"}, want: "docker"},
		{args: []string{"--backend=HVF"}, want: "hvf"},
		{args: []string{"-backend=qemu"}, want: "qemu"},
		{args: []string{"--", "-backend", "docker"}, want: "ccvm"},
		{args: []string{"--backend"}, want: ""},
	} {
		if got := fulltestBackendFromArgs(tc.args); got != tc.want {
			t.Fatalf("fulltestBackendFromArgs(%#v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestFormatProgressAndBootEvents(t *testing.T) {
	progress := formatProgressEvent(client.ProgressEvent{
		Status:             "downloading",
		Artifact:           "kernel",
		Blob:               "vmlinuz",
		BytesDownloaded:    1024,
		BytesTotal:         2048,
		RateBytesPerSecond: 512,
		ETASeconds:         2,
	}, "")
	if progress != "Downloading kernel | vmlinuz | 1.0 KB/2.0 KB | 512 B/s | ETA 2s" {
		t.Fatalf("progress = %q", progress)
	}
	if got := formatProgressEvent(client.ProgressEvent{Status: "error", Error: "boom"}, "image"); got != "Error image | boom" {
		t.Fatalf("error progress = %q", got)
	}

	if got := formatBootEvent(client.BootEvent{Kind: "status", Message: "starting"}); got != "Boot: starting" {
		t.Fatalf("status boot = %q", got)
	}
	if got := formatBootEvent(client.BootEvent{Kind: "ready", State: client.InstanceState{Image: "alpine"}}); got != "Boot: ready alpine" {
		t.Fatalf("ready boot = %q", got)
	}
	if got := formatBootEvent(client.BootEvent{Kind: "serial", Data: "console"}); got != "console" {
		t.Fatalf("serial boot = %q", got)
	}
}

func TestHandleVMForwardDispatchesToAPI(t *testing.T) {
	api := &fakeCCAPI{}
	if err := handleVMCommand(api, []string{"forward", "alpha", "8080:80"}); err != nil {
		t.Fatalf("handle vm forward: %v", err)
	}
	if len(api.forwards) != 1 {
		t.Fatalf("forward calls = %+v", api.forwards)
	}
	got := api.forwards[0]
	if got.id != "alpha" || got.forward.HostPort != 8080 || got.forward.GuestPort != 80 {
		t.Fatalf("forward call = %+v", got)
	}

	if err := handleVMCommand(api, []string{"forward", "alpha", "bad"}); err == nil {
		t.Fatalf("bad forward error = %v", err)
	}
}

type fakeCCAPI struct {
	forwards []struct {
		id      string
		forward client.PortForward
	}
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

func (f *fakeCCAPI) CreateInstance(client.CreateInstanceRequest) (client.InstanceState, error) {
	return client.InstanceState{}, nil
}

func (f *fakeCCAPI) CreateInstanceStream(client.CreateInstanceRequest, func(client.BootEvent) error) (client.InstanceState, error) {
	return client.InstanceState{}, nil
}

func (f *fakeCCAPI) CreateInstanceStreamWithID(string, client.CreateInstanceRequest, func(client.BootEvent) error) (client.InstanceState, error) {
	return client.InstanceState{}, nil
}

func (f *fakeCCAPI) KernelStatus() (client.KernelState, error) {
	return client.KernelState{}, nil
}

func (f *fakeCCAPI) InstanceStatus() (client.InstanceState, error) {
	return client.InstanceState{}, nil
}

func (f *fakeCCAPI) InstanceStatusOf(string) (client.InstanceState, error) {
	return client.InstanceState{Status: "running"}, nil
}

func (f *fakeCCAPI) InstanceStatuses() ([]client.InstanceState, error) {
	return nil, nil
}

func (f *fakeCCAPI) ShutdownInstance() error {
	return nil
}

func (f *fakeCCAPI) ShutdownInstanceWithID(string) error {
	return nil
}

func (f *fakeCCAPI) AddPortForwardTo(id string, forward client.PortForward) error {
	f.forwards = append(f.forwards, struct {
		id      string
		forward client.PortForward
	}{id: id, forward: forward})
	return nil
}

func (f *fakeCCAPI) RunIn(string, client.RunRequest) (client.ExecResponse, error) {
	return client.ExecResponse{}, nil
}

func (f *fakeCCAPI) ExecStream(client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error {
	return fmt.Errorf("unexpected ExecStream")
}

func (f *fakeCCAPI) ExecStreamIn(string, client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error {
	return fmt.Errorf("unexpected ExecStreamIn")
}
