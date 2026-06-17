package vm

import (
	"strings"
	"testing"

	"j5.nz/cc/internal/imagefs"
)

type recordingSnapshotter struct {
	rootCalls int
	atPaths   []string
}

func (s *recordingSnapshotter) RootSnapshot() (imagefs.Directory, error) {
	s.rootCalls++
	return nil, nil
}

func (s *recordingSnapshotter) RootSnapshotAt(path string) (imagefs.Directory, error) {
	s.atPaths = append(s.atPaths, path)
	return nil, nil
}

func TestManagedSnapshotHelpers(t *testing.T) {
	if _, err := managedRootSnapshot(nil, ""); err == nil || !strings.Contains(err.Error(), "root filesystem cannot be snapshotted") {
		t.Fatalf("nil root snapshot error = %v", err)
	}
	if _, err := managedImageSnapshot(nil, "tools", "/mnt/tools"); err == nil || !strings.Contains(err.Error(), "root filesystem cannot be snapshotted") {
		t.Fatalf("nil image snapshot error = %v", err)
	}

	snapshotter := &recordingSnapshotter{}
	if _, err := managedRootSnapshot(snapshotter, ""); err != nil {
		t.Fatalf("root snapshot: %v", err)
	}
	if snapshotter.rootCalls != 1 {
		t.Fatalf("root snapshot calls = %d, want 1", snapshotter.rootCalls)
	}
	if _, err := managedRootSnapshot(snapshotter, "/rootfs"); err != nil {
		t.Fatalf("root snapshot at: %v", err)
	}
	if _, err := managedImageSnapshot(snapshotter, "tools", "/mnt/tools"); err != nil {
		t.Fatalf("image snapshot: %v", err)
	}
	if got := strings.Join(snapshotter.atPaths, ","); got != "/rootfs,/mnt/tools" {
		t.Fatalf("snapshot paths = %q", got)
	}

	if _, err := managedImageSnapshot(struct{}{}, "tools", "/mnt/tools"); err == nil || !strings.Contains(err.Error(), `image mount "tools" cannot be snapshotted`) {
		t.Fatalf("unsupported image snapshot error = %v", err)
	}
}

func TestManagedSnapshotCapabilityHelpers(t *testing.T) {
	snapshotter := &recordingSnapshotter{}
	caps := guestCapabilities{RootSnapshot: true, ImageSnapshot: true}
	if _, err := managedRootSnapshotWithCapabilities("TestOS", caps, snapshotter, ""); err != nil {
		t.Fatalf("root snapshot with capability: %v", err)
	}
	if _, err := managedImageSnapshotWithCapabilities("TestOS", caps, snapshotter, "tools", "/mnt/tools"); err != nil {
		t.Fatalf("image snapshot with capability: %v", err)
	}

	if _, err := managedRootSnapshotWithCapabilities("TestOS", guestCapabilities{}, snapshotter, ""); err == nil || !strings.Contains(err.Error(), "root snapshots") {
		t.Fatalf("root snapshot disabled error = %v", err)
	}
	if _, err := managedImageSnapshotWithCapabilities("TestOS", guestCapabilities{}, snapshotter, "tools", "/mnt/tools"); err == nil || !strings.Contains(err.Error(), "image snapshots") {
		t.Fatalf("image snapshot disabled error = %v", err)
	}
}
