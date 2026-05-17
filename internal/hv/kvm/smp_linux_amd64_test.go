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

func TestManagedSessionSMPNativeThreadStress(t *testing.T) {
	if os.Getenv("CCX3_KVM_SMP_NATIVE") == "" {
		t.Skip("set CCX3_KVM_SMP_NATIVE=1 to run the linux amd64 native pthread SMP probe")
	}
	nativeCC := os.Getenv("CCX3_KVM_NATIVE_CC")
	if nativeCC == "" {
		nativeCC = "musl-gcc"
	}
	if _, err := exec.LookPath(nativeCC); err != nil {
		t.Skipf("%s is required to build the static native SMP probe", nativeCC)
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
	probeBin := buildNativeSMPProbe(t, ctx)
	initrd, err := buildManagedExecProbeInitramfs(initBin, probeBin, modules)
	if err != nil {
		t.Fatalf("buildManagedExecProbeInitramfs() error = %v", err)
	}

	session, err := StartManagedSession(ctx, kernelFile, initrd, 512, 5, false, nil, nil)
	if err != nil {
		t.Fatalf("StartManagedSession() error = %v", err)
	}
	defer session.Close()

	execCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	res, err := session.Exec(execCtx, client.ExecRequest{
		Command: []string{"/bin/smp-probe-native", "stress", "5", "8"},
		Env:     vmruntime.WithDefaultEnv(nil),
		WorkDir: "/tmp",
	})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("native SMP probe exit code %d:\n%s", res.ExitCode, res.Output)
	}
	if !strings.Contains(res.Output, "NATIVE_SMP_OK") {
		t.Fatalf("native SMP probe missing success marker:\n%s", res.Output)
	}
}

func TestManagedSessionExecHandlesLeakedDescendantStdio(t *testing.T) {
	if os.Getenv("CCX3_KVM_LEAK_STDIO") == "" {
		t.Skip("set CCX3_KVM_LEAK_STDIO=1 to run the linux amd64 leaked-stdio exec probe")
	}
	if _, err := exec.LookPath("musl-gcc"); err != nil {
		t.Skip("musl-gcc is required to build the static native probe")
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
	probeBin := buildNativeSMPProbe(t, ctx)
	initrd, err := buildManagedExecProbeInitramfs(initBin, probeBin, modules)
	if err != nil {
		t.Fatalf("buildManagedExecProbeInitramfs() error = %v", err)
	}

	session, err := StartManagedSession(ctx, kernelFile, initrd, 512, 5, false, nil, nil)
	if err != nil {
		t.Fatalf("StartManagedSession() error = %v", err)
	}
	defer session.Close()

	execCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	res, err := session.Exec(execCtx, client.ExecRequest{
		Command: []string{"/bin/smp-probe-native", "leak-stdio"},
		Env:     vmruntime.WithDefaultEnv(nil),
		WorkDir: "/tmp",
	})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("leaked-stdio probe exit code %d:\n%s", res.ExitCode, res.Output)
	}
	if !strings.Contains(res.Output, "NATIVE_SMP_LEAK_PARENT_EXIT") {
		t.Fatalf("leaked-stdio probe missing marker:\n%s", res.Output)
	}
}

func TestManagedSessionSMPSignalTimerStress(t *testing.T) {
	if os.Getenv("CCX3_KVM_SIGNAL_TIMERS") == "" {
		t.Skip("set CCX3_KVM_SIGNAL_TIMERS=1 to run the linux amd64 signal/timer SMP probe")
	}
	if _, err := exec.LookPath("musl-gcc"); err != nil {
		t.Skip("musl-gcc is required to build the static native probe")
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
	probeBin := buildNativeSMPProbe(t, ctx)
	initrd, err := buildManagedExecProbeInitramfs(initBin, probeBin, modules)
	if err != nil {
		t.Fatalf("buildManagedExecProbeInitramfs() error = %v", err)
	}

	session, err := StartManagedSession(ctx, kernelFile, initrd, 512, 5, false, nil, nil)
	if err != nil {
		t.Fatalf("StartManagedSession() error = %v", err)
	}
	defer session.Close()

	execCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	res, err := session.Exec(execCtx, client.ExecRequest{
		Command: []string{"/bin/smp-probe-native", "signal-timers", "200"},
		Env:     vmruntime.WithDefaultEnv(nil),
		WorkDir: "/tmp",
	})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("signal/timer probe exit code %d:\n%s", res.ExitCode, res.Output)
	}
	if !strings.Contains(res.Output, "NATIVE_SMP_SIGNAL_OK") {
		t.Fatalf("signal/timer probe missing success marker:\n%s", res.Output)
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

func buildNativeSMPProbe(t *testing.T, ctx context.Context) []byte {
	t.Helper()

	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "smp_probe_native.c")
	if err := os.WriteFile(sourcePath, []byte(nativeSMPProbeSource), 0o644); err != nil {
		t.Fatalf("write native probe source: %v", err)
	}
	binPath := filepath.Join(t.TempDir(), "smp-probe-native")
	nativeCC := os.Getenv("CCX3_KVM_NATIVE_CC")
	if nativeCC == "" {
		nativeCC = "musl-gcc"
	}
	cmd := exec.CommandContext(ctx, nativeCC, "-O2", "-static", "-pthread", "-o", binPath, sourcePath)
	if os.Getenv("CCX3_KVM_NATIVE_OPENMP") != "" {
		cmd = exec.CommandContext(ctx, nativeCC, "-O2", "-static", "-pthread", "-fopenmp", "-DCCX3_OPENMP=1", "-o", binPath, sourcePath)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build native SMP probe: %v\n%s", err, output)
	}
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read native SMP probe: %v", err)
	}
	return data
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
		UsageMarkerPref:  vmruntime.CommandUsageMarker,
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
		{Path: "/bin/smp-probe-native", Mode: 0o755, Data: probePayload, Type: initramfs.TypeRegular},
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

const nativeSMPProbeSource = `#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <pthread.h>
#include <sched.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/syscall.h>
#include <sys/time.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>
#ifdef CCX3_OPENMP
#include <omp.h>
#endif

static pthread_barrier_t start_barrier;
static pthread_mutex_t mutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t cond = PTHREAD_COND_INITIALIZER;
static volatile uint64_t progress;
static volatile int stop_workers;

static void bind_to_cpu(int cpu) {
	cpu_set_t set;
	CPU_ZERO(&set);
	CPU_SET(cpu, &set);
	(void)sched_setaffinity(0, sizeof(set), &set);
}

static uint64_t monotonic_ms(void) {
	struct timespec ts;
	if (clock_gettime(CLOCK_MONOTONIC, &ts) != 0) {
		perror("clock_gettime");
		exit(20);
	}
	return (uint64_t)ts.tv_sec * 1000 + (uint64_t)ts.tv_nsec / 1000000;
}

struct worker_arg {
	int cpu;
	int index;
};

static void *worker_main(void *opaque) {
	struct worker_arg *arg = (struct worker_arg *)opaque;
	bind_to_cpu(arg->cpu);
	pthread_barrier_wait(&start_barrier);

	uint64_t x = (uint64_t)arg->index + 1;
	while (!__atomic_load_n(&stop_workers, __ATOMIC_RELAXED)) {
		for (int i = 0; i < 4096; i++) {
			x = x * 2862933555777941757ULL + 3037000493ULL;
			__atomic_fetch_add(&progress, x & 1, __ATOMIC_RELAXED);
		}
		pthread_mutex_lock(&mutex);
		pthread_cond_signal(&cond);
		pthread_mutex_unlock(&mutex);
		syscall(SYS_sched_yield);
	}
	return (void *)(uintptr_t)(x != 0);
}

int main(int argc, char **argv) {
	if (argc > 1 && strcmp(argv[1], "leak-stdio") == 0) {
		pid_t pid = fork();
		if (pid < 0) {
			perror("fork");
			return 30;
		}
		if (pid == 0) {
			sleep(60);
			_exit(0);
		}
		printf("NATIVE_SMP_LEAK_PARENT_EXIT child=%d\n", (int)pid);
		fflush(stdout);
		return 0;
	}
	if (argc > 1 && strcmp(argv[1], "signal-timers") == 0) {
		int rounds = argc > 2 ? atoi(argv[2]) : 200;
		int ncpu = (int)sysconf(_SC_NPROCESSORS_ONLN);
		printf("NATIVE_SMP_SIGNAL_START ncpu=%d rounds=%d\n", ncpu, rounds);
		fflush(stdout);
		for (int i = 0; i < rounds; i++) {
			pid_t pid = fork();
			if (pid < 0) {
				perror("fork");
				return 40;
			}
			if (pid == 0) {
				bind_to_cpu(i % ncpu);
				for (;;) {
					__atomic_fetch_add(&progress, 1, __ATOMIC_RELAXED);
				}
			}
			struct timespec tiny = {.tv_sec = 0, .tv_nsec = 1000000};
			nanosleep(&tiny, NULL);
			if (kill(pid, SIGTERM) != 0) {
				perror("kill");
				return 41;
			}
			uint64_t start = monotonic_ms();
			int status = 0;
			for (;;) {
				pid_t got = waitpid(pid, &status, WNOHANG);
				if (got == pid) {
					break;
				}
				if (got < 0) {
					perror("waitpid");
					return 42;
				}
				if (monotonic_ms() - start > 1000) {
					kill(pid, SIGKILL);
					printf("NATIVE_SMP_SIGNAL_FAIL round=%d status=%d progress=%llu\n", i, status, (unsigned long long)__atomic_load_n(&progress, __ATOMIC_RELAXED));
					return 43;
				}
				nanosleep(&tiny, NULL);
			}
		}
		printf("NATIVE_SMP_SIGNAL_OK progress=%llu\n", (unsigned long long)__atomic_load_n(&progress, __ATOMIC_RELAXED));
		return 0;
	}

	int arg_base = argc > 1 && strcmp(argv[1], "stress") == 0 ? 2 : 1;
	int want_cpus = argc > arg_base ? atoi(argv[arg_base]) : 5;
	int workers = argc > arg_base + 1 ? atoi(argv[arg_base + 1]) : 8;
	int ncpu = (int)sysconf(_SC_NPROCESSORS_ONLN);
	printf("NATIVE_SMP_START ncpu=%d want=%d workers=%d\n", ncpu, want_cpus, workers);
	fflush(stdout);
	if (ncpu < want_cpus) {
		printf("NATIVE_SMP_FAIL ncpu=%d want=%d\n", ncpu, want_cpus);
		return 2;
	}

	size_t map_len = 64 * 1024 * 1024;
	char *map = mmap(NULL, map_len, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
	if (map == MAP_FAILED) {
		perror("mmap");
		return 3;
	}
	for (size_t off = 0; off < map_len; off += 4096) {
		map[off] = (char)off;
	}

#ifdef CCX3_OPENMP
	omp_set_dynamic(0);
	omp_set_num_threads(workers);
	for (int round = 0; round < 200; round++) {
		uint64_t total = 0;
#pragma omp parallel reduction(+:total)
		{
			int tid = omp_get_thread_num();
			bind_to_cpu(tid % ncpu);
			uint64_t local = (uint64_t)tid + (uint64_t)round + 1;
#pragma omp for schedule(dynamic, 7)
			for (int i = 0; i < 200000; i++) {
				local = local * 2862933555777941757ULL + (uint64_t)i;
				total += local & 0xff;
			}
#pragma omp barrier
#pragma omp single
			{
				__atomic_fetch_add(&progress, total | 1, __ATOMIC_RELAXED);
			}
		}
	}
	printf("NATIVE_SMP_OPENMP progress=%llu\n", (unsigned long long)__atomic_load_n(&progress, __ATOMIC_RELAXED));
#endif

	pthread_t tids[64];
	struct worker_arg args[64];
	if (workers > 64) {
		workers = 64;
	}
	if (pthread_barrier_init(&start_barrier, NULL, workers + 1) != 0) {
		perror("pthread_barrier_init");
		return 4;
	}
	for (int i = 0; i < workers; i++) {
		args[i].cpu = i % ncpu;
		args[i].index = i;
		int err = pthread_create(&tids[i], NULL, worker_main, &args[i]);
		if (err != 0) {
			fprintf(stderr, "pthread_create %d: %s\n", i, strerror(err));
			return 5;
		}
	}
	pthread_barrier_wait(&start_barrier);

	uint64_t start = monotonic_ms();
	uint64_t last = __atomic_load_n(&progress, __ATOMIC_RELAXED);
	for (;;) {
		struct timespec deadline;
		clock_gettime(CLOCK_REALTIME, &deadline);
		deadline.tv_nsec += 20 * 1000 * 1000;
		if (deadline.tv_nsec >= 1000 * 1000 * 1000) {
			deadline.tv_sec++;
			deadline.tv_nsec -= 1000 * 1000 * 1000;
		}
		pthread_mutex_lock(&mutex);
		(void)pthread_cond_timedwait(&cond, &mutex, &deadline);
		pthread_mutex_unlock(&mutex);

		uint64_t now = monotonic_ms();
		uint64_t current = __atomic_load_n(&progress, __ATOMIC_RELAXED);
		if (current == last && now - start > 500) {
			printf("NATIVE_SMP_FAIL stalled progress=%llu\n", (unsigned long long)current);
			return 6;
		}
		last = current;
		if (now - start >= 3000) {
			break;
		}
	}

	__atomic_store_n(&stop_workers, 1, __ATOMIC_RELAXED);
	for (int i = 0; i < workers; i++) {
		void *ret = NULL;
		int err = pthread_join(tids[i], &ret);
		if (err != 0 || ret == NULL) {
			fprintf(stderr, "pthread_join %d: err=%d ret=%p\n", i, err, ret);
			return 7;
		}
	}
	printf("NATIVE_SMP_DONE progress=%llu\n", (unsigned long long)__atomic_load_n(&progress, __ATOMIC_RELAXED));
	printf("NATIVE_SMP_OK\n");
	return 0;
}
`

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
