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

func TestHandleVMStartCanEnableNetwork(t *testing.T) {
	api := &fakeCCAPI{}
	if err := handleVMCommand(api, []string{
		"start", "--network", "--memory-mb", "8192", "--cpus", "4",
		"desktop", "desktop-image",
	}); err != nil {
		t.Fatalf("handle vm start: %v", err)
	}
	if len(api.creates) != 1 {
		t.Fatalf("create calls = %+v", api.creates)
	}
	got := api.creates[0]
	if got.id != "desktop" || got.req.Image != "desktop-image" {
		t.Fatalf("create call = %+v", got)
	}
	if got.req.Network == nil || !got.req.Network.Enabled || !got.req.Network.AllowInternet {
		t.Fatalf("network = %+v", got.req.Network)
	}
	if got.req.MemoryMB != 8192 || got.req.CPUs != 4 {
		t.Fatalf("resources = memory %d MiB, %d CPUs", got.req.MemoryMB, got.req.CPUs)
	}
}

type fakeCCAPI struct {
	creates []struct {
		id  string
		req client.CreateInstanceRequest
	}
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

func (f *fakeCCAPI) CreateInstanceStreamWithID(id string, req client.CreateInstanceRequest, _ func(client.BootEvent) error) (client.InstanceState, error) {
	f.creates = append(f.creates, struct {
		id  string
		req client.CreateInstanceRequest
	}{id: id, req: req})
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
