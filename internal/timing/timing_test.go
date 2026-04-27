package timing

import (
	"context"
	"testing"
	"time"
)

func TestRecorderAccumulatesDurations(t *testing.T) {
	recorder := NewRecorder()
	recorder.Record("phase", 10*time.Millisecond)
	recorder.Record("phase", 15*time.Millisecond)

	snapshots := recorder.Snapshots()
	if len(snapshots) != 1 {
		t.Fatalf("Snapshots() length = %d, want 1", len(snapshots))
	}
	if snapshots[0].Name != "phase" {
		t.Fatalf("snapshot name = %q, want phase", snapshots[0].Name)
	}
	if snapshots[0].Duration != 25*time.Millisecond {
		t.Fatalf("snapshot duration = %s, want 25ms", snapshots[0].Duration)
	}
	if snapshots[0].Count != 2 {
		t.Fatalf("snapshot count = %d, want 2", snapshots[0].Count)
	}
}

func TestContextRecorder(t *testing.T) {
	recorder := NewRecorder()
	ctx := WithRecorder(context.Background(), recorder)

	Record(ctx, "phase", time.Millisecond)
	Record(context.Background(), "ignored", time.Hour)

	snapshots := recorder.Snapshots()
	if len(snapshots) != 1 {
		t.Fatalf("Snapshots() length = %d, want 1", len(snapshots))
	}
	if snapshots[0].Name != "phase" {
		t.Fatalf("snapshot name = %q, want phase", snapshots[0].Name)
	}
}
