package vm

import (
	"context"
	"strings"
	"testing"

	"j5.nz/cc/client"
)

func TestManagedInstanceCoreControlRequestsRespectCapabilities(t *testing.T) {
	ctx := context.Background()
	denySession := &helperManagedSession{}
	denyInst := &managedInstanceCore{
		osName:  "TestOS",
		session: denySession,
	}
	if _, err := denyInst.Exec(ctx, client.ExecRequest{Kind: "fs_write"}); err == nil || !strings.Contains(err.Error(), "copy into guest") {
		t.Fatalf("denied control request error = %v", err)
	}

	allowSession := &helperManagedSession{}
	allowInst := &managedInstanceCore{
		osName:  "TestOS",
		session: allowSession,
		caps: guestCapabilities{
			CopyIn:         true,
			CopyOut:        true,
			ArchiveExtract: true,
		},
	}
	if _, err := allowInst.Exec(ctx, client.ExecRequest{Kind: "fs_write"}); err != nil {
		t.Fatalf("allowed control request: %v", err)
	}
}

func TestManagedInstanceCoreUnsupportedExtensionsUseCapabilities(t *testing.T) {
	inst := &managedInstanceCore{
		osName: "TestOS",
		caps:   guestCapabilities{DynamicShares: true},
	}
	if err := inst.AddShare(context.Background(), client.ShareMount{}); err == nil || !strings.Contains(err.Error(), "advertises filesystem shares") {
		t.Fatalf("AddShare error = %v", err)
	}
}
