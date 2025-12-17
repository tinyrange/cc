//go:build guest

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestHello(t *testing.T) {
	// This is a placeholder test.
}

// interruptCounts represents parsed interrupt counts from /proc/interrupts
type interruptCounts map[string]uint64 // interrupt name -> total count across all CPUs

// parseInterrupts parses /proc/interrupts and returns a map of interrupt names to total counts
func parseInterrupts(data []byte) (interruptCounts, error) {
	lines := strings.Split(string(data), "\n")
	counts := make(interruptCounts)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "CPU") {
			continue
		}

		// Parse format: " 32:          0          0   GICv3  32 Level     virtio0-config"
		// or: "  0:         45          0   GICv2  27 Level     arch_timer"
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		// First part should be "INTID:" (e.g., "32:")
		intidPart := parts[0]
		if !strings.HasSuffix(intidPart, ":") {
			continue
		}

		// Sum up all CPU counts (skip the INTID part and find where description starts)
		var total uint64
		descStart := -1
		for i := 1; i < len(parts); i++ {
			// Try to parse as number (CPU count)
			if val, err := strconv.ParseUint(parts[i], 10, 64); err == nil {
				total += val
			} else {
				// Not a number, this is where description starts
				descStart = i
				break
			}
		}

		// Extract interrupt name (last part or combination of last parts)
		if descStart >= 0 && descStart < len(parts) {
			// Interrupt name is typically the last field(s)
			// For GIC interrupts, format is usually: "GICv3", "32", "Level", "virtio0-config"
			// We want the last meaningful part
			nameParts := parts[descStart:]
			var name string
			if len(nameParts) >= 2 {
				// Usually the last part is the device name
				name = nameParts[len(nameParts)-1]
			} else if len(nameParts) == 1 {
				name = nameParts[0]
			}

			if name != "" {
				// Use INTID:name as key to handle multiple interrupts with same name
				key := fmt.Sprintf("%s:%s", intidPart[:len(intidPart)-1], name)
				counts[key] = total
			}
		}
	}

	return counts, nil
}

// ensureProcMounted ensures /proc is mounted, mounting it if necessary
func ensureProcMounted(t *testing.T) func() {
	if _, err := os.Stat("/proc/interrupts"); os.IsNotExist(err) {
		if err := os.MkdirAll("/proc", 0755); err != nil {
			t.Fatalf("failed to create /proc directory: %v", err)
		}

		err = syscall.Mount("proc", "/proc", "proc", 0, "")
		if err != nil {
			t.Fatalf("failed to mount /proc: %v", err)
		}

		return func() {
			syscall.Unmount("/proc", 0)
		}
	}
	return func() {}
}

func TestInterruptDelivery(t *testing.T) {
	// 1. Mount /proc if needed
	unmountProc := ensureProcMounted(t)
	defer unmountProc()

	// 2. Parse initial interrupt counts from /proc/interrupts
	initialData, err := os.ReadFile("/proc/interrupts")
	if err != nil {
		t.Fatalf("failed to read /proc/interrupts: %v", err)
	}

	initialCounts, err := parseInterrupts(initialData)
	if err != nil {
		t.Fatalf("failed to parse initial interrupts: %v", err)
	}

	t.Logf("Initial interrupt counts:")
	for name, count := range initialCounts {
		if count > 0 {
			t.Logf("  %s: %d", name, count)
		}
	}

	// 3. Sleep to allow timer interrupts to fire
	// Timer interrupts (arch_timer on ARM64) fire periodically for the kernel scheduler.
	// If these don't work, no interrupts are being delivered to the guest.
	t.Logf("Sleeping for 1 second to allow timer interrupts to accumulate...")
	time.Sleep(1 * time.Second)
	t.Logf("Done sleeping")

	// 4. Parse interrupt counts again
	finalData, err := os.ReadFile("/proc/interrupts")
	if err != nil {
		t.Fatalf("failed to read /proc/interrupts after sleep: %v", err)
	}

	finalCounts, err := parseInterrupts(finalData)
	if err != nil {
		t.Fatalf("failed to parse final interrupts: %v", err)
	}

	t.Logf("\nFinal interrupt counts:")
	for name, count := range finalCounts {
		if count > 0 {
			t.Logf("  %s: %d", name, count)
		}
	}

	// 5. Compare counts and verify at least one interrupt was delivered
	interruptsIncreased := false
	maxIncrease := uint64(0)
	var increasedInterrupts []string

	for name, finalCount := range finalCounts {
		initialCount := initialCounts[name]
		if finalCount > initialCount {
			increase := finalCount - initialCount
			interruptsIncreased = true
			if increase > maxIncrease {
				maxIncrease = increase
			}
			increasedInterrupts = append(increasedInterrupts, fmt.Sprintf("%s: +%d (from %d to %d)", name, increase, initialCount, finalCount))
		}
	}

	// Also check for new interrupts that weren't present initially
	for name, finalCount := range finalCounts {
		if _, existed := initialCounts[name]; !existed && finalCount > 0 {
			interruptsIncreased = true
			increasedInterrupts = append(increasedInterrupts, fmt.Sprintf("%s: new interrupt with count %d", name, finalCount))
		}
	}

	// 6. Log detailed before/after for debugging
	t.Logf("\nInterrupt delivery summary:")
	if interruptsIncreased {
		t.Logf("✓ Interrupts were delivered:")
		for _, info := range increasedInterrupts {
			t.Logf("  %s", info)
		}
	} else {
		t.Logf("✗ No interrupts were delivered")
		t.Logf("\nFull /proc/interrupts before sleep:")
		t.Logf("%s", string(initialData))
		t.Logf("\nFull /proc/interrupts after sleep:")
		t.Logf("%s", string(finalData))
	}

	// Fail the test if no interrupts were delivered
	if !interruptsIncreased {
		t.Errorf("interrupt delivery test failed: no interrupt counts increased after sleep. Timer interrupts should fire periodically - if they don't, interrupts are not being delivered to the guest kernel.")
	}
}

func TestInterruptMapping(t *testing.T) {
	// mount /proc if not already mounted
	if _, err := os.Stat("/proc/interrupts"); os.IsNotExist(err) {
		if err := os.MkdirAll("/proc", 0755); err != nil {
			t.Fatalf("failed to create /proc directory: %v", err)
		}

		err = syscall.Mount("proc", "/proc", "proc", 0, "")
		if err != nil {
			t.Fatalf("failed to mount /proc: %v", err)
		}

		defer syscall.Unmount("/proc", 0)
	}

	data, err := os.ReadFile("/proc/interrupts")
	if err != nil {
		t.Fatalf("failed to read /proc/interrupts: %v", err)
	}

	t.Logf("guest /proc/interrupts:\n%s", data)
}

func TestKernelLog(t *testing.T) {
	// use syscalls to read kernel log
	const klogSize = 1024 * 1024
	buf := make([]byte, klogSize)
	n, err := unix.Klogctl(unix.SYSLOG_ACTION_READ_ALL, buf)
	if err != nil {
		t.Fatalf("failed to read kernel log: %v", err)
	}

	logData := buf[:n]
	t.Logf("kernel log:\n%s", logData)
}

func TestVirtioFs(t *testing.T) {
	// mount the virtio-fs filesystem and verify it works
	tmpDir := "/mnt/virtiofs"
	err := os.MkdirAll(tmpDir, 0755)
	if err != nil {
		t.Fatalf("failed to create mount point: %v", err)
	}

	err = syscall.Mount("bringup", tmpDir, "virtiofs", 0, "")
	if err != nil {
		t.Fatalf("failed to mount virtio-fs: %v", err)
	}
	defer syscall.Unmount(tmpDir, 0)

	t.Logf("virtio-fs mounted at %s", tmpDir)

	testFS(t, tmpDir)
}
