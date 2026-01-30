//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	cc "github.com/tinyrange/cc"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/kvm"
)

func main() {
	var (
		cpus     = flag.Int("cpus", 0, "Number of CPUs to test (0 = test 1,2,4,8)")
		memSize  = flag.Int("mem", 0x200000, "Memory size in bytes (for KVM test)")
		verbose  = flag.Bool("v", false, "Verbose output")
		boot     = flag.Bool("boot", false, "Boot Linux and verify CPUs are visible")
		image    = flag.String("image", "alpine", "OCI image to use for boot test")
		memoryMB = flag.Uint64("memory", 256, "Memory in MB (for boot test)")
		timeout  = flag.Duration("timeout", 30*time.Second, "Timeout for boot test")
		runs     = flag.Int("runs", 1, "Number of boot test runs")
	)
	flag.Parse()

	if *boot {
		if err := runBootTest(*cpus, *image, *memoryMB, *timeout, *runs, *verbose); err != nil {
			fmt.Fprintf(os.Stderr, "Boot test failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := runKVMTest(*cpus, *memSize, *verbose); err != nil {
		fmt.Fprintf(os.Stderr, "KVM test failed: %v\n", err)
		os.Exit(1)
	}
}

func runKVMTest(cpus int, memSize int, verbose bool) error {
	hypervisor, err := kvm.Open()
	if err != nil {
		return fmt.Errorf("open KVM: %w", err)
	}
	defer hypervisor.Close()

	fmt.Printf("KVM opened successfully (arch: %s)\n", hypervisor.Architecture())

	cpuCounts := []int{1, 2, 4, 8}
	if cpus > 0 {
		cpuCounts = []int{cpus}
	}

	failed := false
	for _, numCPUs := range cpuCounts {
		fmt.Printf("\n=== Testing %d vCPU(s) ===\n", numCPUs)

		vm, err := hypervisor.NewVirtualMachine(hv.SimpleVMConfig{
			NumCPUs: numCPUs,
			MemSize: uint64(memSize),
			MemBase: 0,
		})
		if err != nil {
			fmt.Printf("FAIL: Create VM with %d CPUs: %v\n", numCPUs, err)
			failed = true
			continue
		}

		// Verify each vCPU exists and has correct ID
		allOK := true
		for i := 0; i < numCPUs; i++ {
			err := vm.VirtualCPUCall(i, func(vcpu hv.VirtualCPU) error {
				if vcpu.ID() != i {
					return fmt.Errorf("vCPU %d has wrong ID: got %d", i, vcpu.ID())
				}
				if verbose {
					fmt.Printf("  vCPU %d: ID=%d OK\n", i, vcpu.ID())
				}
				return nil
			})
			if err != nil {
				fmt.Printf("FAIL: vCPU %d: %v\n", i, err)
				allOK = false
			}
		}

		if err := vm.Close(); err != nil {
			fmt.Printf("FAIL: Close VM: %v\n", err)
			failed = true
			continue
		}

		if allOK {
			fmt.Printf("PASS: %d vCPU(s) created and verified\n", numCPUs)
		} else {
			failed = true
		}
	}

	fmt.Println()
	if failed {
		return fmt.Errorf("some tests failed")
	}
	fmt.Println("All KVM tests PASSED")
	return nil
}

func runBootTest(cpus int, image string, memoryMB uint64, timeout time.Duration, runs int, verbose bool) error {
	cpuCounts := []int{1, 2, 4, 8}
	if cpus > 0 {
		cpuCounts = []int{cpus}
	}

	// Create cache directory
	cache, err := cc.NewCacheDir("")
	if err != nil {
		return fmt.Errorf("create cache: %w", err)
	}

	// Create OCI client
	client, err := cc.NewOCIClientWithCache(cache)
	if err != nil {
		return fmt.Errorf("create OCI client: %w", err)
	}

	// Pull image once
	fmt.Printf("Pulling image: %s\n", image)
	ctx := context.Background()
	source, err := client.Pull(ctx, image)
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	failed := 0
	passed := 0

	for _, numCPUs := range cpuCounts {
		fmt.Printf("\n=== Boot test with %d vCPU(s) ===\n", numCPUs)

		for run := 1; run <= runs; run++ {
			if runs > 1 {
				fmt.Printf("--- Run %d/%d ---\n", run, runs)
			}

			success, err := runSingleBootTest(ctx, source, cache, numCPUs, memoryMB, timeout, verbose)
			if err != nil {
				fmt.Printf("FAIL: %v\n", err)
				failed++
			} else if success {
				fmt.Printf("PASS: Linux detected %d CPU(s)\n", numCPUs)
				passed++
			} else {
				fmt.Printf("FAIL: CPU count mismatch\n")
				failed++
			}
		}
	}

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Passed: %d, Failed: %d\n", passed, failed)

	if failed > 0 {
		return fmt.Errorf("%d tests failed", failed)
	}
	fmt.Println("All boot tests PASSED")
	return nil
}

func runSingleBootTest(ctx context.Context, source cc.InstanceSource, cache cc.CacheDir, numCPUs int, memoryMB uint64, timeout time.Duration, verbose bool) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create instance with specified CPUs
	inst, err := cc.New(source,
		cc.WithMemoryMB(memoryMB),
		cc.WithCPUs(numCPUs),
		cc.WithCache(cache),
	)
	if err != nil {
		return false, fmt.Errorf("create instance: %w", err)
	}
	defer inst.Close()

	// Run command to check CPU count
	cmd := inst.Command("sh", "-c", "grep -c ^processor /proc/cpuinfo")

	// Capture output
	output := &outputCapture{}
	cmd.SetStdout(output)
	cmd.SetStderr(output)

	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("run command: %w", err)
	}

	// Parse CPU count from output
	var detectedCPUs int
	if _, err := fmt.Sscanf(string(output.data), "%d", &detectedCPUs); err != nil {
		return false, fmt.Errorf("parse CPU count from output %q: %w", string(output.data), err)
	}

	if verbose {
		fmt.Printf("  Expected: %d, Detected: %d\n", numCPUs, detectedCPUs)
	}

	return detectedCPUs == numCPUs, nil
}

type outputCapture struct {
	data []byte
}

func (o *outputCapture) Write(p []byte) (n int, err error) {
	o.data = append(o.data, p...)
	return len(p), nil
}
