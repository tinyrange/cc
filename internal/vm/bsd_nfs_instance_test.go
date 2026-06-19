package vm

import (
	"context"
	"reflect"
	"testing"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/nfs"
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

type recordingBSDNFSInstance struct {
	requests []client.ExecRequest
	commands [][]string
	closed   bool
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
