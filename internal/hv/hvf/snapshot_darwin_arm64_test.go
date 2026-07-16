//go:build darwin && arm64

package hvf

import (
	"testing"

	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

func TestValidateSnapshotRequestRequiresMatchingMemory(t *testing.T) {
	manifest := snapshotManifest{
		MemorySize: arm64vm.MemorySizeBytes(4096),
		Devices: map[string]virtio.MMIOState{
			"balloon": {NumPages: balloonTargetPages(1024)},
		},
	}
	req := vmruntime.RunRequest{MemoryMB: 4096, BalloonMB: 1024}
	if err := validateSnapshotRequest(manifest, req); err != nil {
		t.Fatalf("validate matching snapshot: %v", err)
	}
	if err := validateSnapshotRequest(manifest, vmruntime.RunRequest{MemoryMB: 2048, BalloonMB: 1024}); err == nil {
		t.Fatalf("validate mismatched memory succeeded")
	}
	if err := validateSnapshotRequest(manifest, vmruntime.RunRequest{MemoryMB: 4096, BalloonMB: 512}); err != nil {
		t.Fatalf("validate snapshot with a new balloon target: %v", err)
	}
}

func TestRestoreDeviceStatesAppliesCurrentBalloonTarget(t *testing.T) {
	balloon := virtio.NewBalloon(arm64vm.BalloonBase, arm64vm.BalloonSize, arm64vm.BalloonIRQ)
	state := balloon.SnapshotState()
	state.Status = 4
	state.NumPages = balloonTargetPages(1024)

	want := balloonTargetPages(512)
	if err := restoreDeviceStates(map[string]virtio.MMIOState{"balloon": state}, nil, nil, balloon, want, nil, nil, nil); err != nil {
		t.Fatalf("restore balloon state: %v", err)
	}
	if got := balloon.SnapshotState().NumPages; got != want {
		t.Fatalf("restored balloon target pages = %d, want %d", got, want)
	}
}

func TestSnapshotTriggerWaitsForBalloonTarget(t *testing.T) {
	balloon := virtio.NewBalloon(arm64vm.BalloonBase, arm64vm.BalloonSize, arm64vm.BalloonIRQ)
	state := balloon.SnapshotState()
	state.Status = 4
	state.NumPages = balloonTargetPages(160)
	state.ActualPages = balloonTargetPages(128)
	if err := balloon.RestoreState(state); err != nil {
		t.Fatalf("restore balloon state: %v", err)
	}
	trigger := &snapshotTrigger{devices: snapshotDevices{balloon: balloon}}
	if got := trigger.readyValue(); got != 0 {
		t.Fatalf("snapshot trigger ready value = %#x before balloon target, want 0", got)
	}

	state.ActualPages = state.NumPages
	if err := balloon.RestoreState(state); err != nil {
		t.Fatalf("restore balloon target state: %v", err)
	}
	if got := trigger.readyValue(); got != snapshotTriggerMagic {
		t.Fatalf("snapshot trigger ready value = %#x at balloon target, want %#x", got, snapshotTriggerMagic)
	}
}
