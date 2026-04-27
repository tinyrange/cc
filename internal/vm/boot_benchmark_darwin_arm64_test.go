//go:build darwin && arm64

package vm

import (
	"context"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/hv/hvf"
	"j5.nz/cc/internal/timing"
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
		recorder := timing.NewRecorder()
		ctx = timing.WithRecorder(ctx, recorder)
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
		totals.addTrace(recorder.Snapshots())
	}
	totals.report(b)
}

func BenchmarkAlpineSIMGWhoamiBootDetailedDarwinWarmKernelCache(b *testing.B) {
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

	primeCtx, primeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	primeReq, err := backend.buildStartRequest(primeCtx, client.CreateInstanceRequest{
		Image:    "alpine",
		MemoryMB: 256,
	})
	if err != nil {
		primeCancel()
		b.Fatalf("build warmup start request: %v", err)
	}
	_, err = arm64vm.PrepareBoot(
		make([]byte, arm64vm.MemorySizeBytes(256)),
		primeReq.Kernel,
		primeReq.Init,
		arm64vm.BootConfig{MemoryMB: 256},
	)
	if err != nil {
		primeCancel()
		b.Fatalf("prime warm kernel cache: %v", err)
	}
	primeCancel()

	var totals detailedBootBenchmarkTotals
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		recorder := timing.NewRecorder()
		ctx = timing.WithRecorder(ctx, recorder)
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
		totals.addTrace(recorder.Snapshots())
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
	trace             map[string]time.Duration
	traceCounts       map[string]int
}

func (t *detailedBootBenchmarkTotals) add(buildStartRequest, hvfStartContainer, exec, close, wait, total time.Duration) {
	t.buildStartRequest += buildStartRequest
	t.hvfStartContainer += hvfStartContainer
	t.exec += exec
	t.close += close
	t.wait += wait
	t.total += total
}

func (t *detailedBootBenchmarkTotals) addTrace(snapshots []timing.Snapshot) {
	if t.trace == nil {
		t.trace = map[string]time.Duration{}
		t.traceCounts = map[string]int{}
	}
	for _, snapshot := range snapshots {
		if snapshot.Count == 0 {
			continue
		}
		t.trace[snapshot.Name] += snapshot.Duration
		t.traceCounts[snapshot.Name] += snapshot.Count
	}
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
	for name, duration := range t.trace {
		reportDurationMetric(b, "trace_"+benchmarkMetricName(name)+"_ms/op", time.Duration(float64(duration)/n))
		if count := t.traceCounts[name]; count > b.N {
			b.ReportMetric(float64(count)/n, "trace_"+benchmarkMetricName(name)+"_calls/op")
		}
	}
}

func benchmarkMetricName(name string) string {
	replacer := strings.NewReplacer(".", "_", "-", "_", " ", "_", "/", "_")
	return replacer.Replace(name)
}
