//go:build linux && amd64

package kvm

import (
	"testing"

	"j5.nz/cc/internal/amd64vm"
	"j5.nz/cc/internal/virtio"
)

func TestRestoreKVMDeviceStatesAppliesCurrentBalloonTarget(t *testing.T) {
	balloon := virtio.NewBalloon(amd64vm.BalloonBase, amd64vm.BalloonSize, amd64vm.BalloonIRQ)
	state := balloon.SnapshotState()
	state.Status = 4
	state.NumPages = balloonTargetPages(1024)

	want := balloonTargetPages(512)
	if err := restoreKVMDeviceStates(map[string]virtio.MMIOState{"balloon": state}, nil, nil, nil, balloon, nil); err != nil {
		t.Fatalf("restore balloon state: %v", err)
	}
	if err := retargetRestoredBalloon(balloon, want); err != nil {
		t.Fatalf("retarget restored balloon: %v", err)
	}
	if got := balloon.SnapshotState().NumPages; got != want {
		t.Fatalf("restored balloon target pages = %d, want %d", got, want)
	}
}
