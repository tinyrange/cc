package vm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
)

func BenchmarkAlpineSIMGWhoamiBoot(b *testing.B) {
	if err := Supports(); err != nil {
		b.Skipf("VM backend is not supported on this host: %v", err)
	}
	requireSingleIterationColdBootBenchmark(b)
	setup := setupAlpineSIMGWhoamiBenchmark(b)
	backend := NewRuntimeBackend(setup.kernel, setup.store, setup.guestInitCache)

	var totals bootBenchmarkTotals
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		iterStart := time.Now()
		startBegin := time.Now()
		inst, err := backend.StartStream(ctx, client.CreateInstanceRequest{
			Image:    "alpine",
			MemoryMB: 256,
		}, nil)
		startDuration := time.Since(startBegin)
		if err != nil {
			cancel()
			b.Fatalf("boot alpine.simg: %v", err)
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
		totals.add(startDuration, execDuration, closeDuration, waitDuration, iterDuration)
	}
	totals.report(b)
}

func requireSingleIterationColdBootBenchmark(b *testing.B) {
	b.Helper()
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" && b.N != 1 {
		b.Skip("Darwin HVF cold boot benchmarks must run with -benchtime=1x; use separate go test invocations for repeated samples")
	}
}

type alpineSIMGWhoamiBenchmarkSetup struct {
	kernel         *alpine.Manager
	store          *oci.Store
	guestInitCache string
}

func setupAlpineSIMGWhoamiBenchmark(b *testing.B) alpineSIMGWhoamiBenchmarkSetup {
	b.Helper()
	fixture, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "alpine.simg"))
	if err != nil {
		b.Fatalf("resolve alpine.simg fixture: %v", err)
	}

	setupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	setupStart := time.Now()
	root := alpineSIMGWhoamiBenchmarkCacheRoot(b)
	kernel := alpine.NewManager(filepath.Join(root, "kernel"))
	kernelStart := time.Now()
	if err := kernel.Ensure(setupCtx); err != nil {
		b.Fatalf("prepare kernel: %v", err)
	}
	kernelSetup := time.Since(kernelStart)
	store := oci.NewStore(filepath.Join(root, "images"))
	importStart := time.Now()
	if _, err := store.Pull(setupCtx, "alpine", fixture); err != nil {
		b.Fatalf("import alpine.simg: %v", err)
	}
	imageSetup := time.Since(importStart)
	b.Logf("setup kernel=%s image_import=%s total=%s", kernelSetup, imageSetup, time.Since(setupStart))
	return alpineSIMGWhoamiBenchmarkSetup{
		kernel:         kernel,
		store:          store,
		guestInitCache: filepath.Join(root, "guestinit"),
	}
}

func alpineSIMGWhoamiBenchmarkCacheRoot(b *testing.B) string {
	b.Helper()
	if root := strings.TrimSpace(os.Getenv("CCX3_BENCH_CACHE_DIR")); root != "" {
		root = filepath.Join(root, "alpine-simg-whoami")
		if err := os.MkdirAll(root, 0o755); err != nil {
			b.Fatalf("create benchmark cache dir: %v", err)
		}
		return root
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil || cacheRoot == "" {
		root := filepath.Join(os.TempDir(), "ccx3", "benchmarks", "alpine-simg-whoami")
		if err := os.MkdirAll(root, 0o755); err != nil {
			b.Fatalf("create benchmark cache dir: %v", err)
		}
		return root
	}
	root := filepath.Join(cacheRoot, "ccx3", "benchmarks", "alpine-simg-whoami")
	if err := os.MkdirAll(root, 0o755); err != nil {
		b.Fatalf("create benchmark cache dir: %v", err)
	}
	return root
}

func benchmarkExecWhoami(ctx context.Context, inst Instance) (time.Duration, error) {
	var output strings.Builder
	exitCode := 0
	command := []string{"sh", "-c", "whoami"}
	expectedOutput := "root"
	if raw := strings.TrimSpace(os.Getenv("CCX3_BENCH_COMMAND")); raw != "" {
		command = strings.Fields(raw)
		if expected, ok := os.LookupEnv("CCX3_BENCH_EXPECT"); ok {
			expectedOutput = strings.TrimSpace(expected)
		}
	}
	execBegin := time.Now()
	err := inst.ExecStream(ctx, client.ExecRequest{
		Command:     command,
		SkipResolve: strings.TrimSpace(os.Getenv("CCX3_BENCH_SKIP_RESOLVE")) != "",
	}, nil, func(event client.ExecEvent) error {
		switch event.Kind {
		case "stdout", "output":
			if len(event.Data) > 0 {
				output.Write(event.Data)
			} else {
				output.WriteString(event.Output)
			}
		case "exit":
			exitCode = event.ExitCode
		}
		return nil
	})
	execDuration := time.Since(execBegin)
	if err != nil {
		return execDuration, err
	}
	if exitCode != 0 {
		return execDuration, &benchmarkWhoamiError{message: "unexpected exit code", exitCode: exitCode, output: output.String()}
	}
	if strings.TrimSpace(output.String()) != expectedOutput {
		return execDuration, &benchmarkWhoamiError{message: "unexpected output", exitCode: exitCode, output: output.String()}
	}
	return execDuration, nil
}

func waitForBenchmarkInstanceClose(inst Instance) error {
	err := inst.Wait()
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

type benchmarkWhoamiError struct {
	message  string
	exitCode int
	output   string
}

func (e *benchmarkWhoamiError) Error() string {
	return e.message + ": exit_code=" + strconv.Itoa(e.exitCode) + " output=" + strconv.Quote(e.output)
}

type bootBenchmarkTotals struct {
	start time.Duration
	exec  time.Duration
	close time.Duration
	wait  time.Duration
	total time.Duration
}

func (t *bootBenchmarkTotals) add(start, exec, close, wait, total time.Duration) {
	t.start += start
	t.exec += exec
	t.close += close
	t.wait += wait
	t.total += total
}

func (t *bootBenchmarkTotals) report(b *testing.B) {
	if b.N <= 0 {
		return
	}
	n := float64(b.N)
	reportDurationMetric(b, "start_ms/op", time.Duration(float64(t.start)/n))
	reportDurationMetric(b, "exec_ms/op", time.Duration(float64(t.exec)/n))
	reportDurationMetric(b, "close_ms/op", time.Duration(float64(t.close)/n))
	reportDurationMetric(b, "wait_ms/op", time.Duration(float64(t.wait)/n))
	reportDurationMetric(b, "total_ms/op", time.Duration(float64(t.total)/n))
}

func reportDurationMetric(b *testing.B, name string, duration time.Duration) {
	b.ReportMetric(float64(duration)/float64(time.Millisecond), name)
}
