package vm

import (
	"context"
	"reflect"
	"testing"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/nfs"
	"j5.nz/cc/internal/virtio"
)

func TestBSDNFSInstanceAddShareMountsExport(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	base := &recordingBSDNFSInstance{}
	inst := &bsdNFSInstance{
		Instance: base,
		osName:   "FreeBSD",
		nfs:      nfs.New(nil),
	}

	if err := inst.AddShare(ctx, client.ShareMount{Source: source, Mount: "/mnt/host", Writable: true}); err != nil {
		t.Fatalf("AddShare: %v", err)
	}

	want := [][]string{
		{"/bin/mkdir", "-p", "/mnt/host"},
		{"/sbin/mount", "-t", "nfs", "-o", "nfsv3,proto=tcp,soft,retrycnt=1,port=2049,mountport=20048,nolockd", "10.42.0.1:/ccx3/1", "/mnt/host"},
	}
	if !reflect.DeepEqual(base.commands, want) {
		t.Fatalf("commands mismatch\n got: %#v\nwant: %#v", base.commands, want)
	}
	for _, req := range base.requests {
		if !req.SkipResolve {
			t.Fatalf("request %#v did not skip resolve", req.Command)
		}
	}
}

func TestWrapBSDNFSInstanceClosesBaseOnInitialShareFailure(t *testing.T) {
	ctx := context.Background()
	base := &recordingBSDNFSInstance{}
	inst, err := wrapBSDNFSInstance(ctx, "OpenBSD", base, nfs.New(nil), []client.ShareMount{{Source: "/does/not/exist", Mount: "/mnt/host"}})
	if err == nil {
		t.Fatalf("wrapBSDNFSInstance succeeded with missing source")
	}
	if inst != nil {
		t.Fatalf("wrapBSDNFSInstance returned instance on error")
	}
	if !base.closed {
		t.Fatalf("base instance was not closed after initial share failure")
	}
}

func TestBSDNFSInstanceAddShareSkipsAlreadyMountedShare(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	base := &recordingBSDNFSInstance{}
	inst := &bsdNFSInstance{
		Instance: base,
		osName:   "FreeBSD",
		nfs:      nfs.New(nil),
	}
	share := client.ShareMount{Source: source, Mount: "/mnt/host", Writable: true}

	if err := inst.AddShare(ctx, share); err != nil {
		t.Fatalf("first AddShare: %v", err)
	}
	if err := inst.AddShare(ctx, share); err != nil {
		t.Fatalf("second AddShare: %v", err)
	}
	if got, want := len(base.commands), 2; got != want {
		t.Fatalf("commands after duplicate AddShare = %d, want %d", got, want)
	}
}

func TestBSDNFSInstanceForwardsOptionalInstanceCapabilities(t *testing.T) {
	ctx := context.Background()
	base := &recordingBSDNFSInstance{
		networkIPv4: "10.42.0.2",
		stats:       []virtio.FSStats{{Tag: "root"}},
	}
	inst := &bsdNFSInstance{Instance: base, osName: "OpenBSD", nfs: nfs.New(nil)}

	flusher, ok := any(inst).(instanceFlushProvider)
	if !ok {
		t.Fatalf("bsdNFSInstance does not expose flush")
	}
	if err := flusher.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !base.flushed {
		t.Fatalf("base instance was not flushed")
	}

	history, err := inst.ConsoleHistory(ctx)
	if err != nil {
		t.Fatalf("ConsoleHistory: %v", err)
	}
	if history != "console" {
		t.Fatalf("ConsoleHistory = %q, want console", history)
	}
	if _, err := inst.RootSnapshot(); err != nil {
		t.Fatalf("RootSnapshot: %v", err)
	}
	if _, err := inst.SnapshotImage("alternate"); err != nil {
		t.Fatalf("SnapshotImage: %v", err)
	}
	if got := inst.NetworkIPv4(); got != "10.42.0.2" {
		t.Fatalf("NetworkIPv4 = %q, want 10.42.0.2", got)
	}
	if !reflect.DeepEqual(inst.VirtioFSStats(), base.stats) {
		t.Fatalf("VirtioFSStats = %+v, want %+v", inst.VirtioFSStats(), base.stats)
	}
	if err := inst.AllowServiceProxyPort(ctx, 8080); err != nil {
		t.Fatalf("AllowServiceProxyPort: %v", err)
	}
	if base.allowedPort != 8080 {
		t.Fatalf("allowed port = %d, want 8080", base.allowedPort)
	}
}

type recordingBSDNFSInstance struct {
	requests    []client.ExecRequest
	commands    [][]string
	closed      bool
	flushed     bool
	networkIPv4 string
	stats       []virtio.FSStats
	allowedPort int
}

func (i *recordingBSDNFSInstance) AddShare(context.Context, client.ShareMount) error {
	return nil
}

func (i *recordingBSDNFSInstance) AddPortForward(context.Context, client.PortForward) error {
	return nil
}

func (i *recordingBSDNFSInstance) Exec(_ context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	i.requests = append(i.requests, req)
	i.commands = append(i.commands, append([]string(nil), req.Command...))
	return client.ExecResponse{}, nil
}

func (i *recordingBSDNFSInstance) ExecStream(_ context.Context, req client.ExecRequest, _ <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	i.requests = append(i.requests, req)
	i.commands = append(i.commands, append([]string(nil), req.Command...))
	if onEvent != nil {
		if err := onEvent(client.ExecEvent{Kind: "exit", ExitCode: 0}); err != nil {
			return err
		}
	}
	return nil
}

func (i *recordingBSDNFSInstance) Wait() error {
	return nil
}

func (i *recordingBSDNFSInstance) Close() error {
	i.closed = true
	return nil
}

func (i *recordingBSDNFSInstance) Flush(context.Context) error {
	i.flushed = true
	return nil
}

func (i *recordingBSDNFSInstance) ConsoleHistory(context.Context) (string, error) {
	return "console", nil
}

func (i *recordingBSDNFSInstance) RootSnapshot() (imagefs.Directory, error) {
	return nil, nil
}

func (i *recordingBSDNFSInstance) SnapshotImage(string) (imagefs.Directory, error) {
	return nil, nil
}

func (i *recordingBSDNFSInstance) NetworkIPv4() string {
	return i.networkIPv4
}

func (i *recordingBSDNFSInstance) VirtioFSStats() []virtio.FSStats {
	return i.stats
}

func (i *recordingBSDNFSInstance) AllowServiceProxyPort(_ context.Context, port int) error {
	i.allowedPort = port
	return nil
}
