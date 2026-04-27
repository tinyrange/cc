//go:build darwin && arm64

package vm

import (
	"context"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/hv/hvf"
)

func BenchmarkAlpineSIMGWhoamiBootDetailedDarwin(b *testing.B) {
	if err := Supports(); err != nil {
		b.Skipf("VM backend is not supported on this host: %v", err)
	}
	requireSingleIterationColdBootBenchmark(b)
	setup := setupAlpineSIMGWhoamiBenchmark(b)
	backend := &runtimeBackend{
		kernel:         setup.kernel,
		images:         setup.store,
		guestInitCache: setup.guestInitCache,
	}

	var totals detailedBootBenchmarkTotals
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		iterStart := time.Now()

		buildBegin := time.Now()
		runReq, err := backend.buildStartRequest(ctx, client.CreateInstanceRequest{
			Image:    "alpine",
			MemoryMB: 256,
		})
		buildDuration := time.Since(buildBegin)
		if err != nil {
			cancel()
			b.Fatalf("build start request: %v", err)
		}

		startBegin := time.Now()
		inst, err := hvf.StartContainerStream(ctx, runReq, nil)
		startDuration := time.Since(startBegin)
		if err != nil {
			cancel()
			b.Fatalf("start hvf container: %v", err)
		}

		execDuration, err := benchmarkExecWhoami(ctx, inst)
		closeBegin := time.Now()
		closeErr := inst.Close()
		closeDuration := time.Since(closeBegin)
		waitBegin := time.Now()
		waitErr := waitForBenchmarkInstanceClose(inst)
		waitDuration := time.Since(waitBegin)
		iterDuration := time.Since(iterStart)
		cancel()
		if err != nil {
			b.Fatal(err)
		}
		if closeErr != nil {
			b.Fatalf("close alpine VM: %v", closeErr)
		}
		if waitErr != nil {
			b.Fatalf("wait for alpine VM close: %v", waitErr)
		}
		totals.add(buildDuration, startDuration, execDuration, closeDuration, waitDuration, iterDuration)
	}
	totals.report(b)
}

type detailedBootBenchmarkTotals struct {
	buildStartRequest time.Duration
	hvfStartContainer time.Duration
	exec              time.Duration
	close             time.Duration
	wait              time.Duration
	total             time.Duration
}

func (t *detailedBootBenchmarkTotals) add(buildStartRequest, hvfStartContainer, exec, close, wait, total time.Duration) {
	t.buildStartRequest += buildStartRequest
	t.hvfStartContainer += hvfStartContainer
	t.exec += exec
	t.close += close
	t.wait += wait
	t.total += total
}

func (t *detailedBootBenchmarkTotals) report(b *testing.B) {
	if b.N <= 0 {
		return
	}
	n := float64(b.N)
	reportDurationMetric(b, "build_start_request_ms/op", time.Duration(float64(t.buildStartRequest)/n))
	reportDurationMetric(b, "hvf_start_container_ms/op", time.Duration(float64(t.hvfStartContainer)/n))
	reportDurationMetric(b, "exec_ms/op", time.Duration(float64(t.exec)/n))
	reportDurationMetric(b, "close_ms/op", time.Duration(float64(t.close)/n))
	reportDurationMetric(b, "wait_ms/op", time.Duration(float64(t.wait)/n))
	reportDurationMetric(b, "total_ms/op", time.Duration(float64(t.total)/n))
}
