//go:build linux && amd64

package kvm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/linux/initramfs"
	"j5.nz/cc/internal/vmruntime"
)

func TestManagedSessionSMP16Stress(t *testing.T) {
	if os.Getenv("CCX3_KVM_SMP") == "" {
		t.Skip("set CCX3_KVM_SMP=1 to run the linux amd64 16-vCPU managed exec stress probe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	manager := alpine.NewManager(t.TempDir())
	if err := manager.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	kernelFile, err := manager.ReadKernel()
	if err != nil {
		t.Fatalf("ReadKernel() error = %v", err)
	}
	modules, err := manager.PlanModuleLoad(
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_VSOCKETS", "CONFIG_VIRTIO_VSOCKETS", "CONFIG_HW_RANDOM", "CONFIG_HW_RANDOM_VIRTIO"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO":      "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_VSOCKETS":         "kernel/net/vmw_vsock/vsock.ko.gz",
			"CONFIG_VIRTIO_VSOCKETS":  "kernel/net/vmw_vsock/vmw_vsock_virtio_transport.ko.gz",
			"CONFIG_HW_RANDOM":        "kernel/drivers/char/hw_random/rng-core.ko.gz",
			"CONFIG_HW_RANDOM_VIRTIO": "kernel/drivers/char/hw_random/virtio-rng.ko.gz",
		},
	)
	if err != nil {
		t.Fatalf("PlanModuleLoad() error = %v", err)
	}
	initBin := buildLinuxAMD64Binary(t, ctx, "guest-init", "j5.nz/cc/internal/cmd/init")
	probeBin := buildSMPProbe(t, ctx)
	initrd, err := buildManagedExecProbeInitramfs(initBin, probeBin, modules)
	if err != nil {
		t.Fatalf("buildManagedExecProbeInitramfs() error = %v", err)
	}

	session, err := StartManagedSession(ctx, kernelFile, initrd, 512, 16, false, nil, nil)
	if err != nil {
		t.Fatalf("StartManagedSession() error = %v", err)
	}
	defer session.Close()

	const execs = 4
	errCh := make(chan error, execs)
	var wg sync.WaitGroup
	for i := 0; i < execs; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			res, err := session.Exec(execCtx, client.ExecRequest{
				Command: []string{"/bin/smp-probe", "16", fmt.Sprintf("exec-%d", index)},
				Env:     vmruntime.WithDefaultEnv(nil),
				WorkDir: "/tmp",
			})
			if err != nil {
				errCh <- fmt.Errorf("exec %d: %w", index, err)
				return
			}
			if res.ExitCode != 0 {
				errCh <- fmt.Errorf("exec %d exit code %d:\n%s", index, res.ExitCode, res.Output)
				return
			}
			if !strings.Contains(res.Output, "SMP_STRESS_OK") {
				errCh <- fmt.Errorf("exec %d missing success marker:\n%s", index, res.Output)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func buildSMPProbe(t *testing.T, ctx context.Context) []byte {
	t.Helper()

	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(sourcePath, []byte(smpProbeSource), 0o644); err != nil {
		t.Fatalf("write probe source: %v", err)
	}
	return buildLinuxAMD64Binary(t, ctx, "smp-probe", sourcePath)
}

func buildLinuxAMD64Binary(t *testing.T, ctx context.Context, name, pkg string) []byte {
	t.Helper()

	binPath := filepath.Join(t.TempDir(), name)
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", binPath, pkg)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, output)
	}
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read %s binary: %v", name, err)
	}
	return data
}

func buildManagedExecProbeInitramfs(initPayload, probePayload []byte, modules []alpine.Module) ([]byte, error) {
	configJSON, err := json.Marshal(vmruntime.GuestInitConfig{
		Modules:          vmruntime.ModulePaths(modules),
		Env:              vmruntime.WithDefaultEnv(nil),
		WorkDir:          "/tmp",
		VsockPort:        vmruntime.ControlPort,
		ReadyMarker:      vmruntime.InstanceReadyMarker,
		BeginMarker:      vmruntime.CommandBeginMarker,
		OutputMarkerPref: vmruntime.CommandOutputMarker,
		ErrorMarkerPref:  vmruntime.CommandErrorMarker,
		ExitMarkerPrefix: vmruntime.CommandExitMarkerPref,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal guest init config: %w", err)
	}

	files := []initramfs.File{
		{Path: "/dev", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/proc", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/sys", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/run", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/tmp", Mode: 0o1777, Type: initramfs.TypeDirectory},
		{Path: "/etc", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/bin", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/ccx3", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/ccx3/modules", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/dev/console", Mode: 0o600, Type: initramfs.TypeCharDevice, DevMajor: 5, DevMinor: 1},
		{Path: "/dev/null", Mode: 0o666, Type: initramfs.TypeCharDevice, DevMajor: 1, DevMinor: 3},
		{Path: "/dev/kmsg", Mode: 0o600, Type: initramfs.TypeCharDevice, DevMajor: 1, DevMinor: 11},
		{Path: "/etc/ccx3-init.json", Mode: 0o600, Data: configJSON, Type: initramfs.TypeRegular},
		{Path: "/init", Mode: 0o755, Data: initPayload, Type: initramfs.TypeRegular},
		{Path: "/bin/smp-probe", Mode: 0o755, Data: probePayload, Type: initramfs.TypeRegular},
	}
	for _, mod := range modules {
		files = append(files, initramfs.File{
			Path: "/ccx3/modules/" + mod.Name + ".ko",
			Mode: 0o644,
			Data: mod.Data,
			Type: initramfs.TypeRegular,
		})
	}
	return initramfs.Build(files)
}

const smpProbeSource = `package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	want := 16
	if len(os.Args) > 1 {
		if parsed, err := strconv.Atoi(os.Args[1]); err == nil {
			want = parsed
		}
	}
	label := "probe"
	if len(os.Args) > 2 {
		label = os.Args[2]
	}

	got := runtime.NumCPU()
	runtime.GOMAXPROCS(got)
	fmt.Printf("SMP_PROBE_START label=%s ncpu=%d gomax=%d\n", label, got, runtime.GOMAXPROCS(0))
	if got < want {
		fmt.Printf("SMP_PROBE_FAIL ncpu=%d want_at_least=%d\n", got, want)
		os.Exit(2)
	}

	var ticks atomic.Uint64
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ticks.Add(1)
			case <-done:
				return
			}
		}
	}()

	var progress atomic.Uint64
	var wg sync.WaitGroup
	start := time.Now()
	until := start.Add(2 * time.Second)
	for i := 0; i < want; i++ {
		wg.Add(1)
		go func(seed uint64) {
			defer wg.Done()
			x := seed + 1
			for time.Now().Before(until) {
				x = x*1664525 + 1013904223
				if x%4096 == 0 {
					runtime.Gosched()
				}
				progress.Add(1)
			}
		}(uint64(i))
	}
	wg.Wait()
	close(done)

	elapsed := time.Since(start)
	gotTicks := ticks.Load()
	gotProgress := progress.Load()
	fmt.Printf("SMP_PROBE_DONE label=%s elapsed_ms=%d ticks=%d progress=%d\n", label, elapsed.Milliseconds(), gotTicks, gotProgress)
	if gotTicks < 50 {
		fmt.Printf("SMP_PROBE_FAIL timer_ticks=%d\n", gotTicks)
		os.Exit(3)
	}
	if gotProgress == 0 {
		fmt.Println("SMP_PROBE_FAIL no_progress")
		os.Exit(4)
	}
	fmt.Println("SMP_STRESS_OK")
}
`
