// helper-bench spawns multiple cc-helper processes concurrently, each running
// its own VM via the IPC protocol. This validates that the helper architecture
// supports concurrent multi-VM workloads (important on macOS where hypervisor
// entitlements are per-process).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	cc "github.com/tinyrange/cc"
	"github.com/tinyrange/cc/internal/ipc"
)

func main() {
	n := flag.Int("n", 4, "number of concurrent VMs")
	image := flag.String("image", "alpine", "OCI image reference")
	cacheDir := flag.String("cache-dir", "", "cache directory for OCI images")
	flag.Parse()

	if err := run(*n, *image, *cacheDir); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(n int, imageRef, cacheDir string) error {
	// Prepare image source once so all VMs share the same cached image.
	fmt.Printf("Preparing image %q...\n", imageRef)
	prepStart := time.Now()

	var client cc.OCIClient
	var err error
	if cacheDir != "" {
		cache, err := cc.NewCacheDir(cacheDir)
		if err != nil {
			return fmt.Errorf("create cache dir: %w", err)
		}
		client, err = cc.NewOCIClientWithCache(cache)
		if err != nil {
			return fmt.Errorf("create OCI client: %w", err)
		}
	} else {
		client, err = cc.NewOCIClient()
		if err != nil {
			return fmt.Errorf("create OCI client: %w", err)
		}
	}

	_, err = client.Pull(context.Background(), imageRef, cc.WithPullPolicy(cc.PullIfNotPresent))
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	fmt.Printf("Image ready (%v)\n", time.Since(prepStart))

	// Spawn N helpers concurrently.
	fmt.Printf("Spawning %d VMs...\n", n)
	totalStart := time.Now()

	var mu sync.Mutex
	type result struct {
		id       int
		duration time.Duration
		err      error
	}
	results := make([]result, n)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			start := time.Now()
			err := runOneVM(imageRef, cacheDir)
			mu.Lock()
			results[idx] = result{id: idx, duration: time.Since(start), err: err}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	totalDuration := time.Since(totalStart)

	// Report results.
	fmt.Println()
	failed := 0
	for _, r := range results {
		status := "OK"
		if r.err != nil {
			status = fmt.Sprintf("FAIL: %v", r.err)
			failed++
		}
		fmt.Printf("  VM %d: %v [%s]\n", r.id, r.duration, status)
	}
	fmt.Printf("\nTotal: %v (%d/%d succeeded)\n", totalDuration, n-failed, n)

	if failed > 0 {
		return fmt.Errorf("%d/%d VMs failed", failed, n)
	}
	return nil
}

// runOneVM spawns a helper, creates an instance, runs whoami, and cleans up.
func runOneVM(imageRef, cacheDir string) error {
	client, err := ipc.SpawnHelper("")
	if err != nil {
		return fmt.Errorf("spawn helper: %w", err)
	}
	defer client.Close()

	// Create instance: sourceType=2 (ref), sourcePath="", imageRef, cacheDir, default options
	enc := ipc.NewEncoder()
	enc.Uint8(2) // ref
	enc.String("")
	enc.String(imageRef)
	enc.String(cacheDir)
	ipc.EncodeInstanceOptions(enc, ipc.InstanceOptions{})

	resp, err := client.Call(ipc.MsgInstanceNew, enc.Bytes())
	if err != nil {
		return fmt.Errorf("instance new: %w", err)
	}
	dec := ipc.NewDecoder(resp)
	if code, err := dec.Uint8(); err != nil {
		return err
	} else if code != ipc.ErrCodeOK {
		return fmt.Errorf("instance new: error code %d", code)
	}

	// Create command: whoami
	enc = ipc.NewEncoder()
	enc.String("whoami")
	enc.StringSlice(nil)
	resp, err = client.Call(ipc.MsgCmdNew, enc.Bytes())
	if err != nil {
		return fmt.Errorf("cmd new: %w", err)
	}
	dec = ipc.NewDecoder(resp)
	if code, err := dec.Uint8(); err != nil {
		return err
	} else if code != ipc.ErrCodeOK {
		return fmt.Errorf("cmd new: error code %d", code)
	}
	cmdHandle, err := dec.Uint64()
	if err != nil {
		return err
	}

	// Run command
	enc = ipc.NewEncoder()
	enc.Uint64(cmdHandle)
	resp, err = client.Call(ipc.MsgCmdRun, enc.Bytes())
	if err != nil {
		return fmt.Errorf("cmd run: %w", err)
	}
	dec = ipc.NewDecoder(resp)
	if code, err := dec.Uint8(); err != nil {
		return err
	} else if code != ipc.ErrCodeOK {
		return fmt.Errorf("cmd run: error code %d", code)
	}
	exitCode, err := dec.Int32()
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("whoami exited with code %d", exitCode)
	}

	return nil
}
