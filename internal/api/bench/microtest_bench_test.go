package bench

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	cc "github.com/tinyrange/cc"
)

// Microtest suite for diagnosing intermittent benchmark failures.
// Each test is designed to isolate a specific potential failure point
// with short timeouts to quickly identify where hangs occur.

const (
	instanceTimeout = 5 * time.Second
	commandTimeout  = 2 * time.Second
	closeTimeout    = 3 * time.Second
	testTimeout     = 30 * time.Second
)

// helper function to create a client and pull alpine
func setupAlpine(t *testing.T, ctx context.Context) (cc.OCIClient, cc.InstanceSource) {
	t.Helper()

	client, err := cc.NewOCIClient()
	if err != nil {
		t.Fatalf("NewOCIClient() error = %v", err)
	}

	source, err := client.Pull(ctx, "alpine:latest")
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}

	return client, source
}

func TestMain(m *testing.M) {
	if err := cc.EnsureExecutableIsSigned(); err != nil {
		log.Fatalf("Failed to sign executable: %v", err)
	}
	os.Exit(m.Run())
}

// TestMicroBench_RapidInstanceCreateDestroy tests if rapid instance
// creation/destruction causes corruption or hangs.
func TestMicroBench_RapidInstanceCreateDestroy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	_, source := setupAlpine(t, ctx)

	const iterations = 5
	for i := 0; i < iterations; i++ {
		t.Logf("[%d/%d] Creating instance...", i+1, iterations)
		startCreate := time.Now()

		inst, err := cc.New(source, cc.WithMemoryMB(128))
		if err != nil {
			if errors.Is(err, cc.ErrHypervisorUnavailable) {
				t.Skip("Skipping: hypervisor unavailable")
			}
			t.Fatalf("[%d] New() error = %v", i, err)
		}

		t.Logf("[%d/%d] Instance created in %v", i+1, iterations, time.Since(startCreate))

		// Run a quick command
		cmdStart := time.Now()
		cmd := inst.Command("/bin/true")
		cmdCtx, cmdCancel := context.WithTimeout(ctx, commandTimeout)

		done := make(chan error, 1)
		go func() {
			done <- cmd.Run()
		}()

		select {
		case err := <-done:
			if err != nil {
				cmdCancel()
				inst.Close()
				t.Fatalf("[%d] Command() error = %v", i, err)
			}
		case <-cmdCtx.Done():
			cmdCancel()
			inst.Close()
			t.Fatalf("[%d] Command timed out after %v", i, commandTimeout)
		}
		cmdCancel()

		t.Logf("[%d/%d] Command completed in %v", i+1, iterations, time.Since(cmdStart))

		// Close with timeout
		closeStart := time.Now()
		closeCtx, closeCancel := context.WithTimeout(ctx, closeTimeout)

		closeDone := make(chan struct{})
		go func() {
			inst.Close()
			close(closeDone)
		}()

		select {
		case <-closeDone:
			t.Logf("[%d/%d] Close completed in %v", i+1, iterations, time.Since(closeStart))
		case <-closeCtx.Done():
			closeCancel()
			t.Fatalf("[%d] Close timed out after %v", i, closeTimeout)
		}
		closeCancel()
	}

	t.Log("All iterations completed successfully")
}

// TestMicroBench_SequentialCommandsInSingleInstance tests if the issue
// is within a single instance running multiple commands.
func TestMicroBench_SequentialCommandsInSingleInstance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	_, source := setupAlpine(t, ctx)

	t.Log("Creating instance...")
	inst, err := cc.New(source, cc.WithMemoryMB(128))
	if err != nil {
		if errors.Is(err, cc.ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	t.Log("Instance created, running sequential commands...")

	const iterations = 10
	for i := 0; i < iterations; i++ {
		cmdStart := time.Now()
		cmd := inst.Command("/bin/true")

		cmdCtx, cmdCancel := context.WithTimeout(ctx, commandTimeout)
		done := make(chan error, 1)
		go func() {
			done <- cmd.Run()
		}()

		select {
		case err := <-done:
			if err != nil {
				cmdCancel()
				t.Fatalf("[%d] Command() error = %v", i, err)
			}
			t.Logf("[%d/%d] Command completed in %v", i+1, iterations, time.Since(cmdStart))
		case <-cmdCtx.Done():
			cmdCancel()
			t.Fatalf("[%d] Command timed out after %v", i, commandTimeout)
		}
		cmdCancel()
	}

	t.Log("All commands completed successfully")
}

// TestMicroBench_FreshSourcePerInstance tests if shared ociSource.cfs
// is the culprit by creating a fresh Pull() for each instance.
func TestMicroBench_FreshSourcePerInstance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	const iterations = 3
	for i := 0; i < iterations; i++ {
		iterStart := time.Now()
		t.Logf("[%d/%d] Creating fresh client and pulling...", i+1, iterations)

		// Create fresh client and pull for each iteration
		client, err := cc.NewOCIClient()
		if err != nil {
			t.Fatalf("[%d] NewOCIClient() error = %v", i, err)
		}
		t.Logf("[%d/%d] Client created at %v", i+1, iterations, time.Since(iterStart))

		source, err := client.Pull(ctx, "alpine:latest")
		if err != nil {
			t.Fatalf("[%d] Pull() error = %v", i, err)
		}
		t.Logf("[%d/%d] Pull completed at %v", i+1, iterations, time.Since(iterStart))

		t.Logf("[%d/%d] Creating instance from fresh source...", i+1, iterations)
		inst, err := cc.New(source, cc.WithMemoryMB(128))
		if err != nil {
			if errors.Is(err, cc.ErrHypervisorUnavailable) {
				t.Skip("Skipping: hypervisor unavailable")
			}
			t.Fatalf("[%d] New() error = %v", i, err)
		}
		t.Logf("[%d/%d] Instance created at %v", i+1, iterations, time.Since(iterStart))

		// Run command with detailed timing
		cmdStart := time.Now()
		cmd := inst.Command("/bin/true")
		cmdCtx, cmdCancel := context.WithTimeout(ctx, commandTimeout)
		done := make(chan error, 1)
		go func() {
			done <- cmd.Run()
		}()

		select {
		case err := <-done:
			if err != nil {
				cmdCancel()
				t.Logf("[%d] FAILED: Command error after %v (iter total: %v)", i, time.Since(cmdStart), time.Since(iterStart))
				inst.Close()
				t.Fatalf("[%d] Command() error = %v", i, err)
			}
			t.Logf("[%d/%d] Command completed in %v", i+1, iterations, time.Since(cmdStart))
		case <-cmdCtx.Done():
			cmdCancel()
			t.Logf("[%d] FAILED: Command timeout after %v (iter total: %v)", i, time.Since(cmdStart), time.Since(iterStart))
			inst.Close()
			t.Fatalf("[%d] Command timed out after %v", i, commandTimeout)
		}
		cmdCancel()

		inst.Close()
		t.Logf("[%d/%d] Instance completed in %v", i+1, iterations, time.Since(iterStart))
	}

	t.Log("All fresh-source iterations completed successfully")
}

// TestMicroBench_InstanceCloseWaitsForVMDone verifies that Close()
// properly waits for the VM to fully stop.
func TestMicroBench_InstanceCloseWaitsForVMDone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	_, source := setupAlpine(t, ctx)

	t.Log("Creating instance...")
	inst, err := cc.New(source, cc.WithMemoryMB(128))
	if err != nil {
		if errors.Is(err, cc.ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}

	// Run a command to ensure VM is fully initialized
	t.Log("Running command to ensure VM is initialized...")
	cmd := inst.Command("/bin/true")
	if err := cmd.Run(); err != nil {
		inst.Close()
		t.Fatalf("Command() error = %v", err)
	}

	// Measure close timing
	t.Log("Closing instance...")
	closeStart := time.Now()

	closeCtx, closeCancel := context.WithTimeout(ctx, closeTimeout)
	defer closeCancel()

	closeDone := make(chan struct{})
	go func() {
		inst.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
		closeDuration := time.Since(closeStart)
		t.Logf("Close completed in %v", closeDuration)

		// Verify we can create a new instance immediately
		t.Log("Creating second instance to verify cleanup...")
		inst2, err := cc.New(source, cc.WithMemoryMB(128))
		if err != nil {
			t.Fatalf("Second New() error = %v (cleanup may be incomplete)", err)
		}
		defer inst2.Close()

		cmd2 := inst2.Command("/bin/true")
		if err := cmd2.Run(); err != nil {
			t.Fatalf("Second Command() error = %v", err)
		}
		t.Log("Second instance works correctly")

	case <-closeCtx.Done():
		t.Fatalf("Close timed out after %v", closeTimeout)
	}
}

// TestMicroBench_VsockServerReset tests VsockProgramServer state between programs.
func TestMicroBench_VsockServerReset(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	_, source := setupAlpine(t, ctx)

	t.Log("Creating instance...")
	inst, err := cc.New(source, cc.WithMemoryMB(128))
	if err != nil {
		if errors.Is(err, cc.ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("New() error = %v", err)
	}
	defer inst.Close()

	// Run different commands to exercise vsock
	commands := []string{
		"/bin/true",
		"/bin/false", // Expected to fail with exit code 1
		"/bin/echo", "hello",
		"/bin/true",
		"/bin/true",
	}

	for i := 0; i < len(commands); i++ {
		cmdName := commands[i]
		t.Logf("[%d] Running: %s", i, cmdName)

		cmdStart := time.Now()
		var cmd cc.Cmd
		if cmdName == "/bin/echo" && i+1 < len(commands) {
			cmd = inst.Command(cmdName, commands[i+1])
			i++ // Skip the argument
		} else {
			cmd = inst.Command(cmdName)
		}

		cmdCtx, cmdCancel := context.WithTimeout(ctx, commandTimeout)
		done := make(chan error, 1)
		go func() {
			done <- cmd.Run()
		}()

		select {
		case err := <-done:
			// /bin/false is expected to return exit code 1
			if cmdName == "/bin/false" {
				if err == nil {
					t.Logf("[%d] /bin/false unexpectedly succeeded", i)
				} else {
					t.Logf("[%d] /bin/false returned error (expected): %v", i, err)
				}
			} else if err != nil {
				cmdCancel()
				t.Fatalf("[%d] %s error = %v", i, cmdName, err)
			} else {
				t.Logf("[%d] %s completed in %v", i, cmdName, time.Since(cmdStart))
			}
		case <-cmdCtx.Done():
			cmdCancel()
			t.Fatalf("[%d] %s timed out after %v", i, cmdName, commandTimeout)
		}
		cmdCancel()
	}

	t.Log("All vsock commands completed successfully")
}

// TestMicroBench_ParallelInstancesFromSameSource tests if concurrent access
// to shared cfs causes issues.
// NOTE: This test is skipped on platforms that only support one VM per process
// (e.g., macOS HVF). This is a platform limitation, not a bug.
func TestMicroBench_ParallelInstancesFromSameSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	_, source := setupAlpine(t, ctx)

	// First, test if parallel VMs are supported by trying to create two
	t.Log("Testing if parallel VMs are supported...")
	inst1, err := cc.New(source, cc.WithMemoryMB(128))
	if err != nil {
		if errors.Is(err, cc.ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("First instance error: %v", err)
	}

	inst2, err := cc.New(source, cc.WithMemoryMB(128))
	if err != nil {
		inst1.Close()
		// Check if this is a "VM already exists" error (single-VM platform)
		if errors.Is(err, cc.ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		// HVF on macOS only supports one VM per process
		t.Skipf("Skipping: platform may only support one VM per process: %v", err)
	}

	// Both created successfully, now test parallel execution
	t.Log("Parallel VMs supported, testing concurrent access...")

	const parallelCount = 2
	instances := []cc.Instance{inst1, inst2}

	var wg sync.WaitGroup
	errChan := make(chan error, parallelCount)

	for i := 0; i < parallelCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			t.Logf("[%d] Running command...", idx)
			cmd := instances[idx].Command("/bin/true")

			cmdCtx, cmdCancel := context.WithTimeout(ctx, commandTimeout)
			defer cmdCancel()

			done := make(chan error, 1)
			go func() {
				done <- cmd.Run()
			}()

			select {
			case err := <-done:
				if err != nil {
					errChan <- err
					return
				}
			case <-cmdCtx.Done():
				errChan <- cmdCtx.Err()
				return
			}

			t.Logf("[%d] Instance completed successfully", idx)
		}(i)
	}

	// Wait for all goroutines
	wg.Wait()
	close(errChan)

	// Clean up
	inst1.Close()
	inst2.Close()

	// Check for errors
	for err := range errChan {
		t.Errorf("Parallel instance error: %v", err)
	}

	t.Log("Parallel instances completed successfully")
}

// TestMicroBench_InstanceAfterPreviousFailed tests recovery after a failure.
func TestMicroBench_InstanceAfterPreviousFailed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	_, source := setupAlpine(t, ctx)

	// Create first instance
	t.Log("Creating first instance...")
	inst1, err := cc.New(source, cc.WithMemoryMB(128))
	if err != nil {
		if errors.Is(err, cc.ErrHypervisorUnavailable) {
			t.Skip("Skipping: hypervisor unavailable")
		}
		t.Fatalf("First New() error = %v", err)
	}

	// Run a command that will "fail" (exit code 1)
	t.Log("Running command that exits with error...")
	cmd := inst1.Command("/bin/false")
	err = cmd.Run()
	if err == nil {
		t.Log("/bin/false unexpectedly succeeded, continuing anyway")
	} else {
		t.Logf("/bin/false returned error (expected): %v", err)
	}

	// Also try a command that doesn't exist (should fail)
	t.Log("Running non-existent command...")
	cmd = inst1.Command("/nonexistent/command")
	err = cmd.Run()
	if err != nil {
		t.Logf("Non-existent command error (expected): %v", err)
	}

	// Close the first instance
	t.Log("Closing first instance...")
	closeStart := time.Now()
	inst1.Close()
	t.Logf("First instance closed in %v", time.Since(closeStart))

	// Create a new instance and verify it works
	t.Log("Creating second instance after failure...")
	inst2, err := cc.New(source, cc.WithMemoryMB(128))
	if err != nil {
		t.Fatalf("Second New() error = %v (recovery failed)", err)
	}
	defer inst2.Close()

	t.Log("Running command in second instance...")
	cmd = inst2.Command("/bin/true")
	cmdCtx, cmdCancel := context.WithTimeout(ctx, commandTimeout)
	defer cmdCancel()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Second instance command error = %v", err)
		}
		t.Log("Second instance command succeeded")
	case <-cmdCtx.Done():
		t.Fatalf("Second instance command timed out after %v", commandTimeout)
	}

	t.Log("Recovery test completed successfully")
}

// TestMicroBench_FilesystemVerification checks that the container filesystem
// is properly mounted by reading a known file before running commands.
func TestMicroBench_FilesystemVerification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	_, source := setupAlpine(t, ctx)

	const iterations = 5
	for i := 0; i < iterations; i++ {
		iterStart := time.Now()
		t.Logf("[%d/%d] Creating instance...", i+1, iterations)

		inst, err := cc.New(source, cc.WithMemoryMB(128))
		if err != nil {
			if errors.Is(err, cc.ErrHypervisorUnavailable) {
				t.Skip("Skipping: hypervisor unavailable")
			}
			t.Fatalf("[%d] New() error = %v", i, err)
		}
		t.Logf("[%d/%d] Instance created at %v", i+1, iterations, time.Since(iterStart))

		// First, verify filesystem is accessible by reading a known file
		readStart := time.Now()
		data, err := inst.ReadFile("/etc/os-release")
		if err != nil {
			inst.Close()
			t.Fatalf("[%d] ReadFile(/etc/os-release) error = %v (filesystem may not be mounted)", i, err)
		}
		if len(data) == 0 {
			inst.Close()
			t.Fatalf("[%d] ReadFile returned empty data", i)
		}
		t.Logf("[%d/%d] ReadFile completed in %v (%d bytes)", i+1, iterations, time.Since(readStart), len(data))

		// Now try the command
		cmdStart := time.Now()
		cmd := inst.Command("/bin/true")
		if err := cmd.Run(); err != nil {
			t.Logf("[%d] FAILED: Command error after %v (FS was readable)", i, time.Since(cmdStart))
			inst.Close()
			t.Fatalf("[%d] Command() error = %v (but FS was readable!)", i, err)
		}
		t.Logf("[%d/%d] Command completed in %v", i+1, iterations, time.Since(cmdStart))

		inst.Close()
		t.Logf("[%d/%d] Total iteration: %v", i+1, iterations, time.Since(iterStart))
	}

	t.Log("All filesystem verification iterations completed")
}

// TestMicroBench_SharedSourceMultipleRuns is the most direct analog to
// BenchmarkCommandWithNewGuest - testing sequential instance create/destroy
// from the same source.
func TestMicroBench_SharedSourceMultipleRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	_, source := setupAlpine(t, ctx)

	const iterations = 5
	for i := 0; i < iterations; i++ {
		iterStart := time.Now()
		t.Logf("[%d/%d] Starting iteration...", i+1, iterations)

		// Create instance
		createStart := time.Now()
		inst, err := cc.New(source, cc.WithMemoryMB(128))
		if err != nil {
			if errors.Is(err, cc.ErrHypervisorUnavailable) {
				t.Skip("Skipping: hypervisor unavailable")
			}
			t.Fatalf("[%d] New() error = %v", i, err)
		}
		t.Logf("[%d/%d] Create: %v", i+1, iterations, time.Since(createStart))

		// Run command
		cmdStart := time.Now()
		cmd := inst.Command("/bin/true")
		if err := cmd.Run(); err != nil {
			inst.Close()
			t.Fatalf("[%d] Command() error = %v", i, err)
		}
		t.Logf("[%d/%d] Command: %v", i+1, iterations, time.Since(cmdStart))

		// Close instance
		closeStart := time.Now()
		inst.Close()
		t.Logf("[%d/%d] Close: %v", i+1, iterations, time.Since(closeStart))

		t.Logf("[%d/%d] Total iteration: %v", i+1, iterations, time.Since(iterStart))
	}

	t.Log("All shared-source iterations completed successfully")
}
