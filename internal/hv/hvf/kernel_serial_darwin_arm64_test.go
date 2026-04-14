//go:build darwin && arm64

package hvf

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/internal/kernel/alpine"
	bootarm64 "j5.nz/cc/internal/linux/boot/arm64"
	"j5.nz/cc/internal/linux/initramfs"
	"j5.nz/cc/internal/serial"
)

const (
	testMemoryBase    = 0xa0000000
	testMemoryBase2   = 0xc0000000
	testMemoryBase3   = 0xe0000000
	testMemorySize    = 256 << 20
	gicDistributorMin = 0x08000000
	gicDistributorMax = gicDistributorMin + 0x00010000
	gicRedistribMin   = 0x080a0000
	gicRedistribMax   = gicRedistribMin + 0x00020000
	testUARTSPI       = 33
	irqReadyMarker    = "===IRQ_READY===\n"
	irqBeforeMarker   = "===IRQ_BEFORE==="
	irqAfterMarker    = "===IRQ_AFTER==="
)

type bootProbeMode int

const (
	bootProbeFirstSerial bootProbeMode = iota
	bootProbeUntilBoundary
)

type bootProbeResult struct {
	Serial     string
	Steps      int
	LastMMIO   string
	HVCCount   int
	HaltReason string
	HVCLog     string
}

type runResult struct {
	exit *VcpuExit
	err  error
}

func testBootKernelPrintsToSerial(t *testing.T, vm *VM) {
	result, err := bootKernelProbe(t, vm, testMemoryBase, bootProbeFirstSerial, "", 90*time.Second)
	if err != nil {
		t.Fatalf("boot probe error after %d steps: %v\nserial:\n%s", result.Steps, err, result.Serial)
	}
	if result.Serial == "" {
		t.Fatalf("boot probe produced no serial output after %d steps", result.Steps)
	}
	t.Logf("serial output:\n%s", result.Serial)
}

func testBootHelloWorldInit(t *testing.T, vm *VM) {
	t.Helper()

	initBin := buildHelloWorldInit(t)
	initrd, err := initramfs.Build([]initramfs.File{
		{
			Path: "/dev",
			Mode: 0o755,
			Type: initramfs.TypeDirectory,
		},
		{
			Path:     "/dev/console",
			Mode:     0o600,
			Type:     initramfs.TypeCharDevice,
			DevMajor: 5,
			DevMinor: 1,
		},
		{
			Path:     "/dev/kmsg",
			Mode:     0o600,
			Type:     initramfs.TypeCharDevice,
			DevMajor: 1,
			DevMinor: 11,
		},
		{
			Path: "/init",
			Mode: 0o755,
			Data: initBin,
		},
	})
	if err != nil {
		t.Fatalf("initramfs.Build() error = %v", err)
	}

	result, err := bootKernelProbeWithInitrd(t, vm, testMemoryBase2, bootProbeUntilBoundary, initrd, "hello world", 90*time.Second)
	if err != nil {
		t.Fatalf("hello world boot probe error after %d steps: %v\nserial:\n%s", result.Steps, err, result.Serial)
	}
	if !strings.Contains(result.Serial, "hello world") {
		t.Fatalf("serial output did not contain hello world\nserial:\n%s", result.Serial)
	}
	t.Logf("serial output:\n%s", result.Serial)
}

func testSerialInterruptDelivery(t *testing.T, vm *VM) {
	t.Helper()
	initBin := buildInterruptProbeInit(t)
	initrd, err := initramfs.Build([]initramfs.File{
		{Path: "/dev", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/proc", Mode: 0o755, Type: initramfs.TypeDirectory},
		{Path: "/dev/console", Mode: 0o600, Type: initramfs.TypeCharDevice, DevMajor: 5, DevMinor: 1},
		{Path: "/dev/kmsg", Mode: 0o600, Type: initramfs.TypeCharDevice, DevMajor: 1, DevMinor: 11},
		{Path: "/init", Mode: 0o755, Data: initBin},
	})
	if err != nil {
		t.Fatalf("initramfs.Build() error = %v", err)
	}

	result, err := bootKernelProbeWithInitrd(t, vm, testMemoryBase3, bootProbeUntilBoundary, initrd, irqAfterMarker, 8*time.Second)
	if err != nil {
		t.Fatalf("interrupt probe boot error after %d steps: %v\nserial:\n%s", result.Steps, err, result.Serial)
	}

	beforeCount, ok := markedValue(result.Serial, irqBeforeMarker)
	if !ok {
		t.Fatalf("missing before marker in serial output\nserial:\n%s", result.Serial)
	}
	afterCount, ok := markedValue(result.Serial, irqAfterMarker)
	if !ok {
		t.Fatalf("missing interrupt markers in serial output\nserial:\n%s", result.Serial)
	}
	if afterCount <= beforeCount {
		t.Fatalf("irq 13 count did not increase: before=%d after=%d\nserial:\n%s", beforeCount, afterCount, result.Serial)
	}
}

func TestKernelBootProgress(t *testing.T) {
	if os.Getenv("CCX3_BOOT_PROGRESS") == "" {
		t.Skip("set CCX3_BOOT_PROGRESS=1 to run the long kernel progress probe")
	}

	vm, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM() error = %v", err)
	}
	defer vm.Close()

	result, err := bootKernelProbe(t, vm, testMemoryBase2, bootProbeUntilBoundary, "", 20*time.Second)
	if err != nil {
		t.Fatalf("boot probe error after %d steps: %v\nlast mmio: %s\nhvc calls: %d\nfirst hvc: %s\nserial:\n%s",
			result.Steps, err, result.LastMMIO, result.HVCCount, result.HVCLog, result.Serial)
	}

	t.Logf("steps=%d last_mmio=%s hvc_calls=%d halt=%s first_hvc=%s\nserial:\n%s",
		result.Steps, result.LastMMIO, result.HVCCount, result.HaltReason, result.HVCLog, result.Serial)
}

func bootKernelProbe(t *testing.T, vm *VM, memoryBase uint64, mode bootProbeMode, wantSerial string, runFor time.Duration) (bootProbeResult, error) {
	return bootKernelProbeWithInitrd(t, vm, memoryBase, mode, nil, wantSerial, runFor)
}

func bootKernelProbeWithInitrd(t *testing.T, vm *VM, memoryBase uint64, mode bootProbeMode, initrd []byte, wantSerial string, runFor time.Duration) (bootProbeResult, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	kernelRoot := filepath.Join(t.TempDir(), "kernel")
	manager := alpine.NewManager(kernelRoot)
	if err := manager.Ensure(ctx); err != nil {
		return bootProbeResult{}, fmt.Errorf("Ensure() error = %w", err)
	}
	kernelFile, err := manager.ReadKernel()
	if err != nil {
		return bootProbeResult{}, fmt.Errorf("ReadKernel() error = %w", err)
	}

	mem, err := vm.MapAnonymousMemory(testMemorySize, IPA(memoryBase), hvMemoryRead|hvMemoryWrite|hvMemoryExec)
	if err != nil {
		return bootProbeResult{}, fmt.Errorf("MapAnonymousMemory() error = %w", err)
	}

	var serialOut bytes.Buffer
	uart := serial.NewUART8250(bootarm64.DefaultUARTBase, bootarm64.DefaultUARTRegShift, &serialOut)
	uart.AttachIRQ(vm, testUARTSPI)

	plan, err := bootarm64.PrepareBoot(mem, kernelFile, bootarm64.BootOptions{
		MemoryBase: memoryBase,
		MemorySize: testMemorySize,
		NumCPUs:    1,
		Initrd:     initrd,
		Cmdline: strings.Join([]string{
			"console=ttyS0,115200n8",
			fmt.Sprintf("earlycon=uart8250,mmio,0x%x", bootarm64.DefaultUARTBase),
			"keep_bootcon",
			"nokaslr",
			"loglevel=8",
			"rdinit=/init",
		}, " "),
	})
	if err != nil {
		return bootProbeResult{}, fmt.Errorf("PrepareBoot() error = %w", err)
	}

	if err := vm.SetReg(hvRegPC, plan.EntryGPA); err != nil {
		return bootProbeResult{}, fmt.Errorf("SetReg(PC) error = %w", err)
	}
	if err := vm.SetReg(hvRegCPSR, bootarm64.DefaultPStateBits); err != nil {
		return bootProbeResult{}, fmt.Errorf("SetReg(CPSR) error = %w", err)
	}
	if err := vm.SetSysReg(hvSysRegSP_EL1, plan.StackTopGPA); err != nil {
		return bootProbeResult{}, fmt.Errorf("SetSysReg(SP_EL1) error = %w", err)
	}
	if err := vm.SetReg(hvRegX0, plan.DeviceTreeGPA); err != nil {
		return bootProbeResult{}, fmt.Errorf("SetReg(X0) error = %w", err)
	}
	if err := vm.SetReg(hvRegX1, 0); err != nil {
		return bootProbeResult{}, fmt.Errorf("SetReg(X1) error = %w", err)
	}
	if err := vm.SetReg(hvRegX2, 0); err != nil {
		return bootProbeResult{}, fmt.Errorf("SetReg(X2) error = %w", err)
	}
	if err := vm.SetReg(hvRegX3, 0); err != nil {
		return bootProbeResult{}, fmt.Errorf("SetReg(X3) error = %w", err)
	}

	result := bootProbeResult{}
	irqInjected := false
	deadline := time.Now().Add(runFor)
	for time.Now().Before(deadline) {
		result.Steps++
		exitInfo, err, stalled := runWithTimeout(vm, 500*time.Millisecond)
		if stalled {
			if !irqInjected && strings.Contains(serialOut.String(), irqReadyMarker) {
				uart.InjectRXByte('!')
				irqInjected = true
			}
			if wantSerial != "" && hasWantedSerial(serialOut.String(), wantSerial) {
				result.Serial = serialOut.String()
				return result, nil
			}
			if mode == bootProbeFirstSerial && serialOut.Len() > 0 {
				result.Serial = serialOut.String()
				return result, nil
			}
			continue
		}
		if err != nil {
			result.Serial = serialOut.String()
			return result, fmt.Errorf("Run() error = %w", err)
		}
		if exitInfo == nil {
			result.Serial = serialOut.String()
			return result, fmt.Errorf("Run() exit info = nil")
		}
		if !irqInjected && strings.Contains(serialOut.String(), irqReadyMarker) {
			uart.InjectRXByte('!')
			irqInjected = true
		}
		if wantSerial != "" && hasWantedSerial(serialOut.String(), wantSerial) {
			result.Serial = serialOut.String()
			return result, nil
		}
		if mode == bootProbeFirstSerial && serialOut.Len() > 0 {
			result.Serial = serialOut.String()
			return result, nil
		}
		if exitInfo.Reason != hvExitReasonException {
			result.Serial = serialOut.String()
			return result, fmt.Errorf("unexpected exit reason %v", exitInfo.Reason)
		}

		class := DecodeExceptionClass(exitInfo.Exception.Syndrome)
		switch class {
		case ExceptionClassDataAbortLowerEL:
			result.LastMMIO = fmt.Sprintf("addr=%#x syndrome=%#x", uint64(exitInfo.Exception.PhysicalAddress), exitInfo.Exception.Syndrome)
			if err := handleTestDataAbort(vm, uart, exitInfo); err != nil {
				result.Serial = serialOut.String()
				return result, err
			}
		case ExceptionClassHVC64:
			result.HVCCount++
			halt, reason, hvcLog, err := handleTestHVC(vm, uart, mem, memoryBase)
			if err != nil {
				result.Serial = serialOut.String()
				result.HVCLog = hvcLog
				return result, err
			}
			if result.HVCLog == "" {
				result.HVCLog = hvcLog
			}
			if halt {
				result.HaltReason = reason
				result.Serial = serialOut.String()
				return result, nil
			}
		default:
			result.Serial = serialOut.String()
			return result, fmt.Errorf("unexpected exception class %#x syndrome=%#x physical=%#x",
				class, exitInfo.Exception.Syndrome, uint64(exitInfo.Exception.PhysicalAddress))
		}
	}

	result.Serial = serialOut.String()
	return result, nil
}

func buildHelloWorldInit(t *testing.T) []byte {
	t.Helper()

	src := filepath.Join(t.TempDir(), "init.c")
	if err := os.WriteFile(src, []byte(`#define AT_FDCWD -100
#define O_WRONLY 1

typedef unsigned long size_t;

static long syscall3(long n, long a0, long a1, long a2) {
	long ret;
	__asm__ volatile(
		"mov x8, %1\n"
		"mov x0, %2\n"
		"mov x1, %3\n"
		"mov x2, %4\n"
		"svc #0\n"
		"mov %0, x0\n"
		: "=r"(ret)
		: "r"(n), "r"(a0), "r"(a1), "r"(a2)
		: "x0", "x1", "x2", "x8", "memory");
	return ret;
}

static long syscall4(long n, long a0, long a1, long a2, long a3) {
	long ret;
	__asm__ volatile(
		"mov x8, %1\n"
		"mov x0, %2\n"
		"mov x1, %3\n"
		"mov x2, %4\n"
		"mov x3, %5\n"
		"svc #0\n"
		"mov %0, x0\n"
		: "=r"(ret)
		: "r"(n), "r"(a0), "r"(a1), "r"(a2), "r"(a3)
		: "x0", "x1", "x2", "x3", "x8", "memory");
	return ret;
}

static void write_fd(long fd, const char* s, size_t n) {
	if (fd >= 0) syscall3(64, fd, (long)s, n);
}

void _start(void) {
	static const char msg[] = "hello world\n";
	static const char kmsgmsg[] = "<0>hello world\n";
	static const char console[] = "/dev/console";
	static const char kmsg[] = "/dev/kmsg";

	long fd;
	fd = syscall4(56, AT_FDCWD, (long)console, O_WRONLY, 0);
	write_fd(fd, msg, sizeof(msg) - 1);
	fd = syscall4(56, AT_FDCWD, (long)kmsg, O_WRONLY, 0);
	write_fd(fd, kmsgmsg, sizeof(kmsgmsg) - 1);
	write_fd(1, msg, sizeof(msg) - 1);
	write_fd(2, msg, sizeof(msg) - 1);
	for (;;) {
		__asm__ volatile("wfe");
	}
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(init.c) error = %v", err)
	}

	out := filepath.Join(filepath.Dir(src), "init")
	cmd := exec.Command("clang",
		"-target", "aarch64-linux-gnu",
		"-nostdlib",
		"-static",
		"-fuse-ld=lld",
		"-Wl,-e,_start",
		"-Wl,--build-id=none",
		"-o", out,
		src,
	)
	buildOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("clang build init error = %v\n%s", err, string(buildOut))
	}

	bin, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile(init) error = %v", err)
	}
	return bin
}

func buildInterruptProbeInit(t *testing.T) []byte {
	t.Helper()

	src := filepath.Join(t.TempDir(), "init.c")
	if err := os.WriteFile(src, []byte(`#define AT_FDCWD -100
#define O_WRONLY 1

typedef unsigned long size_t;

static long syscall1(long n, long a0) {
	long ret;
	__asm__ volatile(
		"mov x8, %1\n"
		"mov x0, %2\n"
		"svc #0\n"
		"mov %0, x0\n"
		: "=r"(ret)
		: "r"(n), "r"(a0)
		: "x0", "x8", "memory");
	return ret;
}

static long syscall3(long n, long a0, long a1, long a2) {
	long ret;
	__asm__ volatile(
		"mov x8, %1\n"
		"mov x0, %2\n"
		"mov x1, %3\n"
		"mov x2, %4\n"
		"svc #0\n"
		"mov %0, x0\n"
		: "=r"(ret)
		: "r"(n), "r"(a0), "r"(a1), "r"(a2)
		: "x0", "x1", "x2", "x8", "memory");
	return ret;
}

static long syscall4(long n, long a0, long a1, long a2, long a3) {
	long ret;
	__asm__ volatile(
		"mov x8, %1\n"
		"mov x0, %2\n"
		"mov x1, %3\n"
		"mov x2, %4\n"
		"mov x3, %5\n"
		"svc #0\n"
		"mov %0, x0\n"
		: "=r"(ret)
		: "r"(n), "r"(a0), "r"(a1), "r"(a2), "r"(a3)
		: "x0", "x1", "x2", "x3", "x8", "memory");
	return ret;
}

static long syscall5(long n, long a0, long a1, long a2, long a3, long a4) {
	long ret;
	__asm__ volatile(
		"mov x8, %1\n"
		"mov x0, %2\n"
		"mov x1, %3\n"
		"mov x2, %4\n"
		"mov x3, %5\n"
		"mov x4, %6\n"
		"svc #0\n"
		"mov %0, x0\n"
		: "=r"(ret)
		: "r"(n), "r"(a0), "r"(a1), "r"(a2), "r"(a3), "r"(a4)
		: "x0", "x1", "x2", "x3", "x4", "x8", "memory");
	return ret;
}

static void write_fd(long fd, const char* s, size_t n) {
	while (fd >= 0 && n > 0) {
		long written = syscall3(64, fd, (long)s, n);
		if (written <= 0) return;
		s += written;
		n -= (size_t)written;
	}
}

static void write_all(long consolefd, const char* s, size_t n) {
	write_fd(consolefd, s, n);
	write_fd(1, s, n);
	write_fd(2, s, n);
}

static size_t append_long(char* buf, long value) {
	char tmp[32];
	size_t n = 0;
	unsigned long u = (unsigned long)value;
	if (value < 0) {
		*buf++ = '-';
		u = (unsigned long)(-value);
	}
	do {
		tmp[n++] = (char)('0' + (u % 10));
		u /= 10;
	} while (u != 0);
	for (size_t i = 0; i < n; i++) {
		buf[i] = tmp[n - 1 - i];
	}
	return n + (value < 0 ? 1 : 0);
}

static void write_status(long consolefd, const char* prefix, long value) {
	char buf[96];
	size_t n = 0;
	while (prefix[n] != 0) {
		buf[n] = prefix[n];
		n++;
	}
	n += append_long(buf + n, value);
	buf[n++] = '\n';
	write_all(consolefd, buf, n);
}

static long read_irq_count(long irq) {
	char buf[8192];
	long fd = syscall4(56, AT_FDCWD, (long)"/proc/interrupts", 0, 0);
	if (fd < 0) {
		return fd;
	}
	long n = syscall3(63, fd, (long)buf, sizeof(buf));
	syscall1(57, fd);
	if (n <= 0) {
		return n;
	}
	for (long i = 0; i < n; i++) {
		long start = i;
		while (i < n && buf[i] != '\n') {
			i++;
		}
		long end = i;
		long pos = start;
		long value = 0;
		long seen = 0;
		long currentIRQ = -1;
		while (pos < end) {
			char c = buf[pos];
			if (c >= '0' && c <= '9') {
				long num = 0;
				while (pos < end && buf[pos] >= '0' && buf[pos] <= '9') {
					num = num*10 + (long)(buf[pos] - '0');
					pos++;
				}
				if (pos < end && buf[pos] == ':') {
					currentIRQ = num;
				} else if (currentIRQ == irq && seen == 0) {
					value = num;
					seen = 1;
					break;
				}
				continue;
			}
			pos++;
		}
		if (seen != 0) {
			return value;
		}
	}
	return -1000;
}

static void write_irq_count(long outfd, long kfd, const char* prefix, long value) {
	write_status(outfd, prefix, value);
	write_status(kfd, prefix, value);
}

void _start(void) {
	static const char console[] = "/dev/console";
	static const char kmsg[] = "/dev/kmsg";
	static const char start[] = "===IRQ_START===\n";
	static const char ready[] = "===IRQ_READY===\n";
	static const char before[] = "===IRQ_BEFORE===";
	static const char after[] = "===IRQ_AFTER===";

	long fd = syscall4(56, AT_FDCWD, (long)console, O_WRONLY, 0);
	long kfd = syscall4(56, AT_FDCWD, (long)kmsg, O_WRONLY, 0);
	long mountRet;
	write_all(fd, start, sizeof(start) - 1);
	write_fd(kfd, "<0>===IRQ_START===\n", sizeof("<0>===IRQ_START===\n") - 1);
	write_status(fd, "===CONSOLE_FD===", fd);
	write_status(kfd, "<0>===CONSOLE_FD===", fd);
	mountRet = syscall5(40, (long)"proc", (long)"/proc", (long)"proc", 0, 0);
	write_status(fd, "===MOUNT_PROC===", mountRet);
	write_status(kfd, "<0>===MOUNT_PROC===", mountRet);
	write_irq_count(fd, kfd, before, read_irq_count(13));
	write_all(fd, ready, sizeof(ready) - 1);
	write_fd(kfd, "<0>===IRQ_READY===\n", sizeof("<0>===IRQ_READY===\n") - 1);
	for (volatile unsigned long i = 0; i < 50000UL; i++) {
		__asm__ volatile("" ::: "memory");
	}
	write_irq_count(fd, kfd, after, read_irq_count(13));
	for (;;) {
		__asm__ volatile("wfe");
	}
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(init.c) error = %v", err)
	}

	out := filepath.Join(filepath.Dir(src), "init")
	cmd := exec.Command("clang",
		"-target", "aarch64-linux-gnu",
		"-nostdlib",
		"-static",
		"-fuse-ld=lld",
		"-Wl,-e,_start",
		"-Wl,--build-id=none",
		"-o", out,
		src,
	)
	buildOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("clang build init error = %v\n%s", err, string(buildOut))
	}

	bin, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile(init) error = %v", err)
	}
	return bin
}

func markedValue(serial, begin string) (int, bool) {
	start := strings.Index(serial, begin)
	if start < 0 {
		return 0, false
	}
	start += len(begin)
	end := start
	for end < len(serial) && serial[end] >= '0' && serial[end] <= '9' {
		end++
	}
	if end == start {
		return 0, false
	}
	n := 0
	for _, ch := range serial[start:end] {
		n = n*10 + int(ch-'0')
	}
	return n, true
}

func hasWantedSerial(serial, want string) bool {
	if !strings.HasSuffix(want, "===") {
		return strings.Contains(serial, want)
	}
	start := strings.Index(serial, want)
	if start < 0 {
		return false
	}
	start += len(want)
	return start < len(serial) && serial[start] >= '0' && serial[start] <= '9'
}

func runWithTimeout(vm *VM, timeout time.Duration) (*VcpuExit, error, bool) {
	resCh := make(chan runResult, 1)
	go func() {
		exitInfo, err := vm.Run()
		resCh <- runResult{exit: exitInfo, err: err}
	}()

	select {
	case res := <-resCh:
		return res.exit, res.err, false
	case <-time.After(timeout):
		if err := vm.CancelRun(); err != nil {
			return nil, err, false
		}
		res := <-resCh
		if res.err != nil {
			return nil, res.err, false
		}
		if res.exit == nil || res.exit.Reason != hvExitReasonCanceled {
			return nil, fmt.Errorf("cancelled run returned unexpected exit %#v", res.exit), false
		}
		return nil, nil, true
	}
}

func handleTestDataAbort(vm *VM, uart *serial.UART8250, exitInfo *VcpuExit) error {
	info, err := DecodeDataAbort(exitInfo.Exception.Syndrome)
	if err != nil {
		return err
	}
	addr := uint64(exitInfo.Exception.PhysicalAddress)

	switch {
	case uart.Contains(addr, info.SizeBytes):
		if info.Write {
			value, err := readDataAbortValue(vm, info)
			if err != nil {
				return err
			}
			if err := uart.WriteValue(addr, info.SizeBytes, value); err != nil {
				return err
			}
		} else {
			value, err := uart.ReadValue(addr, info.SizeBytes)
			if err != nil {
				return err
			}
			if err := writeDataAbortValue(vm, info, value); err != nil {
				return err
			}
		}
	case inRange(addr, gicDistributorMin, gicDistributorMax) || inRange(addr, gicRedistribMin, gicRedistribMax):
		value, err := handleTestGICAccess(vm, addr, info)
		if err != nil {
			return err
		}
		if !info.Write {
			if err := writeDataAbortValue(vm, info, value); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unhandled MMIO access addr=%#x size=%d write=%v", addr, info.SizeBytes, info.Write)
	}

	return vm.AdvanceProgramCounter()
}

func handleTestHVC(vm *VM, uart *serial.UART8250, mem []byte, memoryBase uint64) (halt bool, reason string, hvcLog string, err error) {
	x0, err := vm.GetReg(hvRegX0)
	if err != nil {
		return false, "", "", err
	}

	const (
		psciVersion         = 0x84000000
		psciCpuSuspend      = 0x84000001
		psciCpuOff          = 0x84000002
		psciCpuOn           = 0x84000003
		psciAffinityInfo    = 0x84000004
		psciMigrateInfoType = 0x84000006
		psciSystemOff       = 0x84000008
		psciSystemReset     = 0x84000009
		psciFeatures        = 0x8400000a
		psciSuccess         = 0
		psciNotSupported    = 0xffffffff
		psciInvalidParams   = 0xfffffffe
		psciTosNotPresent   = 2
		testInjectSerialIRQ = 0xcc03000d
	)

	var ret uint64
	switch x0 {
	case testInjectSerialIRQ:
		uart.InjectRXByte('!')
		ret = 0
	case psciVersion:
		ret = 0x00010000
	case psciMigrateInfoType:
		ret = psciTosNotPresent
	case psciFeatures:
		ret = psciNotSupported
	case psciCpuSuspend:
		ret = psciNotSupported
	case psciCpuOff:
		ret = psciSuccess
	case psciAffinityInfo:
		ret = psciInvalidParams
	case psciCpuOn:
		ret = psciInvalidParams
	case psciSystemOff:
		return true, "psci_system_off", "", nil
	case psciSystemReset:
		return true, "psci_system_reset", "", nil
	default:
		return false, "", "", fmt.Errorf("unsupported PSCI call %#x", x0)
	}

	pc, _ := vm.GetReg(hvRegPC)
	cpsr, _ := vm.GetReg(hvRegCPSR)
	sp, _ := vm.GetSysReg(hvSysRegSP_EL1)
	ttbr1, _ := vm.GetSysReg(hvSysRegTTBR1EL1)
	x1, _ := vm.GetReg(hvRegX1)
	x2, _ := vm.GetReg(hvRegX2)
	x3, _ := vm.GetReg(hvRegX3)
	x4, _ := vm.GetReg(hvRegX4)
	x29, _ := vm.GetReg(hvRegX29)
	x30, _ := vm.GetReg(hvRegX30)
	stackLog := ""
	if phys, err := translateVA4K48(mem, memoryBase, ttbr1, sp); err == nil && phys >= memoryBase && phys+16 <= memoryBase+uint64(len(mem)) {
		off := phys - memoryBase
		slot0 := binary.LittleEndian.Uint64(mem[off : off+8])
		slot1 := binary.LittleEndian.Uint64(mem[off+8 : off+16])
		stackLog = fmt.Sprintf(" sp_phys=%#x stack0=%#x stack1=%#x", phys, slot0, slot1)
	} else if err != nil {
		stackLog = fmt.Sprintf(" ttbr1=%#x stack_walk_err=%v", ttbr1, err)
	}
	hvcLog = fmt.Sprintf("pc=%#x sp_el1=%#x cpsr=%#x x0=%#x x1=%#x x2=%#x x3=%#x x4=%#x x29=%#x x30=%#x%s", pc, sp, cpsr, x0, x1, x2, x3, x4, x29, x30, stackLog)

	if err := vm.SetReg(hvRegX0, ret); err != nil {
		return false, "", hvcLog, err
	}
	return false, "", hvcLog, nil
}

func readDataAbortValue(vm *VM, info DataAbortInfo) (uint64, error) {
	if info.Target == hvRegXZR {
		return 0, nil
	}
	value, err := vm.GetReg(info.Target)
	if err != nil {
		return 0, err
	}
	if info.SizeBytes >= 8 {
		return value, nil
	}
	return value & ((uint64(1) << (8 * info.SizeBytes)) - 1), nil
}

func writeDataAbortValue(vm *VM, info DataAbortInfo, value uint64) error {
	if info.Target == hvRegXZR {
		return nil
	}
	if info.SizeBytes < 8 {
		value &= (uint64(1) << (8 * info.SizeBytes)) - 1
	}
	return vm.SetReg(info.Target, value)
}

func inRange(addr, start, end uint64) bool {
	return addr >= start && addr < end
}

func handleTestGICAccess(vm *VM, addr uint64, info DataAbortInfo) (uint64, error) {
	var value uint64
	if info.Write {
		v, err := readDataAbortValue(vm, info)
		if err != nil {
			return 0, err
		}
		value = v
	}

	switch {
	case inRange(addr, gicDistributorMin, gicDistributorMax):
		reg := GICDistributorReg(addr - gicDistributorMin)
		if info.Write {
			err := vm.SetGICDistributorReg(reg, value)
			if err != nil && strings.Contains(err.Error(), "denied") {
				return 0, nil
			}
			return 0, err
		}
		val, err := vm.GetGICDistributorReg(reg)
		if err != nil && strings.Contains(err.Error(), "denied") && reg == 0xffe8 {
			return 0x30, nil
		}
		return val, err
	case inRange(addr, gicRedistribMin, gicRedistribMax):
		reg := GICRedistributorReg(addr - gicRedistribMin)
		if info.Write {
			err := vm.SetGICRedistributorReg(reg, value)
			if err != nil && (strings.Contains(err.Error(), "denied") || strings.Contains(err.Error(), "bad argument")) {
				return 0, nil
			}
			return 0, err
		}
		val, err := vm.GetGICRedistributorReg(reg)
		if err != nil && (strings.Contains(err.Error(), "denied") || strings.Contains(err.Error(), "bad argument")) {
			switch reg {
			case 0x0:
				return 0, nil
			case 0xffe8:
				return 0x30, nil
			case 0x8:
				return 1 << 4, nil
			case 0x14:
				return 0, nil
			default:
				return 0, nil
			}
		}
		return val, err
	default:
		return 0, fmt.Errorf("address %#x outside GIC MMIO ranges", addr)
	}
}

func translateVA4K48(mem []byte, memoryBase, ttbr1, va uint64) (uint64, error) {
	table := ttbr1 &^ 0xfff
	if table == 0 {
		return 0, fmt.Errorf("ttbr1 is zero")
	}
	indexes := [4]uint64{
		(va >> 39) & 0x1ff,
		(va >> 30) & 0x1ff,
		(va >> 21) & 0x1ff,
		(va >> 12) & 0x1ff,
	}
	for level, index := range indexes {
		if table < memoryBase || table+0x1000 > memoryBase+uint64(len(mem)) {
			return 0, fmt.Errorf("table %#x outside guest RAM", table)
		}
		off := table - memoryBase + index*8
		desc := binary.LittleEndian.Uint64(mem[off : off+8])
		if desc&1 == 0 {
			return 0, fmt.Errorf("invalid l%d descriptor for va %#x", level, va)
		}
		addr := desc & 0x0000fffffffff000
		isTable := (desc & 0x2) != 0
		switch level {
		case 0, 1, 2:
			if isTable {
				table = addr
				continue
			}
			var blockMask uint64
			switch level {
			case 1:
				blockMask = (1 << 30) - 1
			case 2:
				blockMask = (1 << 21) - 1
			default:
				return 0, fmt.Errorf("block descriptor at l0 unsupported")
			}
			return addr | (va & blockMask), nil
		case 3:
			return addr | (va & 0xfff), nil
		}
	}
	return 0, fmt.Errorf("walk fell through")
}
