//go:build darwin && arm64

package hvf

import (
	"testing"

	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

func TestValidateSnapshotRequestMatchesMemoryAndBalloon(t *testing.T) {
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
	if err := validateSnapshotRequest(manifest, vmruntime.RunRequest{MemoryMB: 4096, BalloonMB: 512}); err == nil {
		t.Fatalf("validate mismatched balloon succeeded")
	}
}
