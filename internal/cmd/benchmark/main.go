package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	cc "github.com/tinyrange/cc"
	"github.com/tinyrange/cc/internal/timeslice"
)

func main() {
	if err := cc.EnsureExecutableIsSigned(); err != nil {
		fmt.Fprintf(os.Stderr, "benchmark: ensure executable is signed: %v\n", err)
		os.Exit(1)
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "benchmark: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	nVMs := flag.Int("n", 10, "Number of VMs to start")
	imageRef := flag.String("image", "alpine:latest", "OCI image to run")
	timesliceFile := flag.String("timeslice", "", "Timeslice output file")
	parallel := flag.Bool("parallel", false, "Start VMs in parallel")
	memoryMB := flag.Uint64("memory", 256, "Memory per VM in MB")
	cpus := flag.Int("cpus", 1, "vCPUs per VM")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Benchmark VM startup times.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  %s -n 10 -timeslice bench.ts\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -n 5 -parallel -image alpine:latest\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// Start timeslice recording if requested
	var tsCloser interface{ Close() error }
	if *timesliceFile != "" {
		f, err := os.Create(*timesliceFile)
		if err != nil {
			return fmt.Errorf("create timeslice file: %w", err)
		}
		defer f.Close()

		closer, err := timeslice.StartRecording(f)
		if err != nil {
			return fmt.Errorf("start timeslice recording: %w", err)
		}
		tsCloser = closer
		defer tsCloser.Close()
	}

	// Check hypervisor availability
	if err := cc.SupportsHypervisor(); err != nil {
		return fmt.Errorf("hypervisor unavailable: %w", err)
	}

	// Create OCI client
	cache, err := cc.NewCacheDir("")
	if err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	client, err := cc.NewOCIClientWithCache(cache)
	if err != nil {
		return fmt.Errorf("create OCI client: %w", err)
	}

	// Pull image once
	ctx := context.Background()
	fmt.Printf("Pulling image %s...\n", *imageRef)
	pullStart := time.Now()
	source, err := client.Pull(ctx, *imageRef)
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}
	fmt.Printf("Image pulled in %v\n", time.Since(pullStart))

	// Prepare options - boot snapshots are enabled by default with cache
	opts := []cc.Option{
		cc.WithMemoryMB(*memoryMB),
		cc.WithCPUs(*cpus),
		cc.WithCache(cache),
		cc.WithBootSnapshot(), // Explicit enable for testing
	}

	fmt.Printf("\nStarting %d VMs (parallel=%v, memory=%dMB, cpus=%d)...\n",
		*nVMs, *parallel, *memoryMB, *cpus)

	totalStart := time.Now()
	var vmTimes []time.Duration

	if *parallel {
		vmTimes, err = startVMsParallel(*nVMs, source, opts)
	} else {
		vmTimes, err = startVMsSequential(*nVMs, source, opts)
	}
	if err != nil {
		return err
	}

	totalDuration := time.Since(totalStart)

	// Print results
	fmt.Printf("\n=== Results ===\n")
	var sum time.Duration
	var min, max time.Duration
	for i, d := range vmTimes {
		fmt.Printf("VM %2d: %v\n", i+1, d)
		sum += d
		if i == 0 || d < min {
			min = d
		}
		if d > max {
			max = d
		}
	}

	avg := sum / time.Duration(len(vmTimes))
	fmt.Printf("\nTotal time:   %v\n", totalDuration)
	fmt.Printf("Average:      %v\n", avg)
	fmt.Printf("Min:          %v\n", min)
	fmt.Printf("Max:          %v\n", max)

	if *timesliceFile != "" {
		fmt.Printf("\nTimeslice data written to: %s\n", *timesliceFile)
	}

	return nil
}

func startVMsSequential(n int, source cc.InstanceSource, opts []cc.Option) ([]time.Duration, error) {
	times := make([]time.Duration, n)

	for i := 0; i < n; i++ {
		start := time.Now()
		inst, err := cc.New(source, opts...)
		if err != nil {
			return nil, fmt.Errorf("create VM %d: %w", i+1, err)
		}

		times[i] = time.Since(start)

		// Print boot stats to verify snapshot behavior
		stats := inst.BootStats()
		if stats != nil {
			if stats.ColdBoot {
				fmt.Printf("VM %d started in %v (cold boot, kernel=%v, init=%v)\n",
					i+1, times[i], stats.KernelBootTime, stats.ContainerInitTime)
			} else {
				fmt.Printf("VM %d started in %v (warm boot, restore=%v, init=%v)\n",
					i+1, times[i], stats.SnapshotRestoreTime, stats.ContainerInitTime)
			}
		} else {
			fmt.Printf("VM %d started in %v\n", i+1, times[i])
		}

		// Close immediately after measuring startup time
		if err := inst.Close(); err != nil {
			return nil, fmt.Errorf("close VM %d: %w", i+1, err)
		}
	}

	return times, nil
}

func startVMsParallel(n int, source cc.InstanceSource, opts []cc.Option) ([]time.Duration, error) {
	times := make([]time.Duration, n)
	instances := make([]cc.Instance, n)
	errors := make([]error, n)

	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()

			start := time.Now()
			inst, err := cc.New(source, opts...)
			times[idx] = time.Since(start)

			if err != nil {
				errors[idx] = err
				return
			}
			instances[idx] = inst
			fmt.Printf("VM %d started in %v\n", idx+1, times[idx])
		}(i)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			// Clean up any successfully created instances
			for _, inst := range instances {
				if inst != nil {
					inst.Close()
				}
			}
			return nil, fmt.Errorf("create VM %d: %w", i+1, err)
		}
	}

	// Close all instances
	for i, inst := range instances {
		if err := inst.Close(); err != nil {
			return nil, fmt.Errorf("close VM %d: %w", i+1, err)
		}
	}

	return times, nil
}
