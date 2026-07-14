package vm

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"j5.nz/cc/client"
)

type snapshotScaleInstance struct {
	id      string
	inst    Instance
	elapsed time.Duration
	err     error
}

type snapshotScaleProbeResult struct {
	elapsed time.Duration
	err     error
}

type snapshotScaleTelemetry struct {
	rssKB        int64
	pssKB        int64
	privateKB    int64
	sharedKB     int64
	availableKB  int64
	swapFreeKB   int64
	threads      int64
	openFDs      int
	goRoutines   int
	heapAllocMiB uint64
}

func TestRuntimeSnapshotCloneScale(t *testing.T) {
	if os.Getenv("CC_TEST_VM_SNAPSHOT_SCALE") == "" {
		t.Skip("set CC_TEST_VM_SNAPSHOT_SCALE=1 to run snapshot clone scaling")
	}
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skip("KVM startup snapshots are implemented on Linux amd64 and arm64")
	}
	if testing.Short() {
		t.Skip("snapshot clone scaling is not a short test")
	}

	target := soakPositiveInt(t, "CC_TEST_VM_SNAPSHOT_SCALE_COUNT", 250)
	batchSize := soakPositiveInt(t, "CC_TEST_VM_SNAPSHOT_SCALE_BATCH", 25)
	finalCommandRounds := soakPositiveInt(t, "CC_TEST_VM_SNAPSHOT_SCALE_FINAL_COMMANDS", 1)
	memoryMB := uint64(soakPositiveInt(t, "CC_TEST_VM_SNAPSHOT_SCALE_MEMORY_MB", 256))
	minAvailableMB := int64(soakPositiveInt(t, "CC_TEST_VM_SNAPSHOT_SCALE_MIN_AVAILABLE_MB", 3072))
	env := newRuntimeBootEnv(t)
	snapshotRoot := t.TempDir()

	capture := startRuntimeInstance(t, env, client.CreateInstanceRequest{
		ID:          "snapshot-scale-capture",
		SnapshotDir: snapshotRoot,
		MemoryMB:    memoryMB,
		CPUs:        1,
		Network:     &client.NetworkConfig{Enabled: false},
	})
	captureConsole := runtimeConsoleHistory(capture)
	if err := capture.Close(); err != nil {
		t.Fatalf("close snapshot capture instance: %v", err)
	}
	snapshotPath := singleSnapshotPath(t, snapshotRoot, captureConsole)
	runtime.GC()
	baseline := readSnapshotScaleTelemetry()
	t.Logf("snapshot=%s memory=%dMiB target=%d batch=%d baseline=%s", snapshotPath, memoryMB, target, batchSize, baseline)

	instances := make([]snapshotScaleInstance, 0, target)
	t.Cleanup(func() {
		closeSnapshotScaleInstances(instances)
	})
	for len(instances) < target {
		if availableMB := readProcKB("/proc/meminfo", "MemAvailable:") >> 10; availableMB < minAvailableMB {
			t.Logf("safety stop before start: live=%d available=%dMiB threshold=%dMiB", len(instances), availableMB, minAvailableMB)
			break
		}
		count := batchSize
		if remaining := target - len(instances); count > remaining {
			count = remaining
		}
		started := startSnapshotScaleBatch(env, snapshotPath, memoryMB, len(instances), count)
		for _, result := range started {
			if result.err != nil {
				closeSnapshotScaleInstances(started)
				t.Fatalf("snapshot restore %s failed after %s: %v; live=%d telemetry=%s", result.id, result.elapsed, result.err, len(instances), readSnapshotScaleTelemetry())
			}
		}
		instances = append(instances, started...)
		probeSnapshotScaleBatch(t, started, 0)
		runtime.GC()
		telemetry := readSnapshotScaleTelemetry()
		p50, p95, slowest := snapshotScaleStartLatency(started)
		t.Logf("checkpoint live=%d start_latency={p50=%s p95=%s max=%s} telemetry=%s delta_per_vm={pss=%.1fMiB private=%.1fMiB host_available=%.1fMiB}",
			len(instances), p50.Round(time.Millisecond), p95.Round(time.Millisecond), slowest.Round(time.Millisecond), telemetry,
			telemetry.deltaMiB(baseline.pssKB, telemetry.pssKB, len(instances)),
			telemetry.deltaMiB(baseline.privateKB, telemetry.privateKB, len(instances)),
			telemetry.deltaMiB(telemetry.availableKB, baseline.availableKB, len(instances)))
		if telemetry.availableKB>>10 < minAvailableMB {
			t.Logf("safety stop after checkpoint: live=%d available=%dMiB threshold=%dMiB", len(instances), telemetry.availableKB>>10, minAvailableMB)
			break
		}
	}

	t.Logf("peak live=%d target=%d telemetry=%s", len(instances), target, readSnapshotScaleTelemetry())
	for round := 0; round < finalCommandRounds; round++ {
		started := time.Now()
		latencies := probeSnapshotScaleBatch(t, instances, round+1)
		p50, p95, slowest := snapshotScaleLatencies(latencies)
		t.Logf("full_command_round=%d live=%d wall=%s latency={p50=%s p95=%s max=%s} telemetry=%s", round, len(instances),
			time.Since(started).Round(time.Millisecond), p50.Round(time.Millisecond), p95.Round(time.Millisecond), slowest.Round(time.Millisecond), readSnapshotScaleTelemetry())
	}
	closeSnapshotScaleInstances(instances)
	instances = nil
	parallelWaitForTeardown(baseline.goRoutines, baseline.openFDs, 5*time.Second)
	runtime.GC()
	teardown := readSnapshotScaleTelemetry()
	t.Logf("teardown telemetry=%s", teardown)
	if teardown.threads > baseline.threads+64 {
		t.Fatalf("VM teardown retained %d OS threads above baseline; want at most 64", teardown.threads-baseline.threads)
	}
}

func startSnapshotScaleBatch(env *runtimeBootEnv, snapshotPath string, memoryMB uint64, offset, count int) []snapshotScaleInstance {
	results := make(chan snapshotScaleInstance, count)
	var ready sync.WaitGroup
	ready.Add(count)
	release := make(chan struct{})
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("snapshot-scale-%05d", offset+i)
		go func() {
			ready.Done()
			<-release
			started := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			inst, err := env.backend.StartStream(ctx, client.CreateInstanceRequest{
				ID:              id,
				Image:           env.imageName,
				RestoreSnapshot: snapshotPath,
				MemoryMB:        memoryMB,
				CPUs:            1,
				Network:         &client.NetworkConfig{Enabled: false},
			}, nil)
			results <- snapshotScaleInstance{id: id, inst: inst, elapsed: time.Since(started), err: err}
		}()
	}
	ready.Wait()
	close(release)
	started := make([]snapshotScaleInstance, 0, count)
	for len(started) < count {
		started = append(started, <-results)
	}
	return started
}

func probeSnapshotScaleBatch(t *testing.T, instances []snapshotScaleInstance, round int) []time.Duration {
	t.Helper()
	results := make(chan snapshotScaleProbeResult, len(instances))
	for _, instance := range instances {
		instance := instance
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			started := time.Now()
			marker := fmt.Sprintf("ready-%s-%03d", instance.id, round)
			resp, err := instance.inst.Exec(ctx, client.ExecRequest{
				Command: []string{"sh", "-lc", "test \"$(uname -s)\" = Linux; printf '%s\\n' \"$1\"", "snapshot-scale", marker},
			})
			if err == nil && (resp.ExitCode != 0 || strings.TrimSpace(resp.Output) != marker) {
				err = fmt.Errorf("exit=%d output=%q, want %q", resp.ExitCode, resp.Output, marker)
			}
			if err != nil {
				err = fmt.Errorf("%s: %w", instance.id, err)
			}
			results <- snapshotScaleProbeResult{elapsed: time.Since(started), err: err}
		}()
	}
	latencies := make([]time.Duration, 0, len(instances))
	for range instances {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		latencies = append(latencies, result.elapsed)
	}
	return latencies
}

func closeSnapshotScaleInstances(instances []snapshotScaleInstance) {
	const closeConcurrency = 32
	sem := make(chan struct{}, closeConcurrency)
	var wg sync.WaitGroup
	for _, instance := range instances {
		if instance.inst == nil {
			continue
		}
		wg.Add(1)
		go func(inst Instance) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			_ = inst.Close()
		}(instance.inst)
	}
	wg.Wait()
}

func snapshotScaleStartLatency(instances []snapshotScaleInstance) (time.Duration, time.Duration, time.Duration) {
	latencies := make([]time.Duration, 0, len(instances))
	for _, instance := range instances {
		latencies = append(latencies, instance.elapsed)
	}
	return snapshotScaleLatencies(latencies)
}

func snapshotScaleLatencies(latencies []time.Duration) (time.Duration, time.Duration, time.Duration) {
	if len(latencies) == 0 {
		return 0, 0, 0
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	return latencies[len(latencies)/2], latencies[(len(latencies)-1)*95/100], latencies[len(latencies)-1]
}

func readSnapshotScaleTelemetry() snapshotScaleTelemetry {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	smaps := readProcValues("/proc/self/smaps_rollup", "Rss:", "Pss:", "Private_Dirty:", "Shared_Clean:")
	host := readProcValues("/proc/meminfo", "MemAvailable:", "SwapFree:")
	status := readProcValues("/proc/self/status", "Threads:")
	return snapshotScaleTelemetry{
		rssKB:        smaps["Rss:"],
		pssKB:        smaps["Pss:"],
		privateKB:    smaps["Private_Dirty:"],
		sharedKB:     smaps["Shared_Clean:"],
		availableKB:  host["MemAvailable:"],
		swapFreeKB:   host["SwapFree:"],
		threads:      status["Threads:"],
		openFDs:      soakOpenFDs(),
		goRoutines:   runtime.NumGoroutine(),
		heapAllocMiB: mem.HeapAlloc >> 20,
	}
}

func readProcKB(path, prefix string) int64 {
	return readProcValues(path, prefix)[prefix]
}

func readProcValues(path string, prefixes ...string) map[string]int64 {
	values := make(map[string]int64, len(prefixes))
	for _, prefix := range prefixes {
		values[prefix] = -1
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return values
	}
	for _, line := range strings.Split(string(data), "\n") {
		for _, prefix := range prefixes {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			fields := strings.Fields(strings.TrimPrefix(line, prefix))
			if len(fields) == 0 {
				break
			}
			value, err := strconv.ParseInt(fields[0], 10, 64)
			if err == nil {
				values[prefix] = value
			}
			break
		}
	}
	return values
}

func (s snapshotScaleTelemetry) deltaMiB(baseline, current int64, count int) float64 {
	if baseline < 0 || current < 0 || count == 0 {
		return -1
	}
	return float64(current-baseline) / 1024 / float64(count)
}

func (s snapshotScaleTelemetry) String() string {
	return fmt.Sprintf("rss=%.1fMiB pss=%.1fMiB private_dirty=%.1fMiB shared_clean=%.1fMiB heap=%dMiB host_available=%.1fMiB swap_free=%.1fMiB threads=%d goroutines=%d open_fds=%d",
		float64(s.rssKB)/1024, float64(s.pssKB)/1024, float64(s.privateKB)/1024, float64(s.sharedKB)/1024,
		s.heapAllocMiB, float64(s.availableKB)/1024, float64(s.swapFreeKB)/1024, s.threads, s.goRoutines, s.openFDs)
}
