package ccvmd

import (
	"context"
	"testing"
	"time"
)

func TestWorkerOperationsRemainResponsiveAndSerializeOneVM(t *testing.T) {
	operations := newWorkerOperationRegistry()
	defer operations.close()

	firstStarted := make(chan struct{})
	firstCanceled := make(chan struct{})
	operations.start(1, "alpha", func(ctx context.Context) {
		close(firstStarted)
		<-ctx.Done()
		close(firstCanceled)
	})
	awaitWorkerOperation(t, firstStarted, "first operation start")

	secondStarted := make(chan struct{})
	operations.start(2, "alpha", func(context.Context) { close(secondStarted) })
	unrelatedStarted := make(chan struct{})
	operations.start(3, "", func(context.Context) { close(unrelatedStarted) })

	awaitWorkerOperation(t, unrelatedStarted, "unrelated operation while start is blocked")
	select {
	case <-secondStarted:
		t.Fatal("conflicting operation started before the first operation completed")
	default:
	}

	if !operations.cancel(1) {
		t.Fatal("active operation was not cancellable")
	}
	awaitWorkerOperation(t, firstCanceled, "cancellation reaching operation context")
	awaitWorkerOperation(t, secondStarted, "queued same-VM operation after cancellation")
}

func awaitWorkerOperation(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
