package initx

import (
	"context"
	"fmt"
	"time"

	"github.com/tinyrange/cc/internal/ir"
)

// Session is a small orchestration handle for booting a VM and running a payload.
// It is intentionally UI-agnostic; callers own any window/terminal resources.
type Session struct {
	VM   *VirtualMachine
	Done <-chan error

	// Stop cancels the session and waits up to timeout for completion.
	Stop func(timeout time.Duration) error
	// Wait blocks until the session completes and returns the terminal error.
	Wait func() error
}

type SessionConfig struct {
	// BootTimeout controls how long we wait for the initial boot program to run.
	// If zero, a default of 10 seconds is used.
	BootTimeout time.Duration
}

type sessionRunner interface {
	Boot(ctx context.Context) error
	StartStdinForwarding()
	Run(ctx context.Context, prog *ir.Program) error
}

func startSession(parent context.Context, vmPtr *VirtualMachine, runner sessionRunner, prog *ir.Program, cfg SessionConfig) *Session {
	if parent == nil {
		parent = context.Background()
	}

	bootTimeout := cfg.BootTimeout
	if bootTimeout == 0 {
		bootTimeout = 10 * time.Second
	}

	ctx, cancel := context.WithCancel(parent)
	doneCh := make(chan error, 1)

	go func() {
		bootCtx, bootCancel := context.WithTimeout(ctx, bootTimeout)
		defer bootCancel()

		if err := runner.Boot(bootCtx); err != nil {
			doneCh <- err
			return
		}

		runner.StartStdinForwarding()
		doneCh <- runner.Run(ctx, prog)
	}()

	waitFn := func() error { return <-doneCh }
	stopFn := func(timeout time.Duration) error {
		cancel()
		if timeout <= 0 {
			return <-doneCh
		}
		select {
		case err := <-doneCh:
			return err
		case <-time.After(timeout):
			return fmt.Errorf("initx session stop timed out after %s", timeout)
		}
	}

	return &Session{
		VM:   vmPtr,
		Done: doneCh,
		Stop: stopFn,
		Wait: waitFn,
	}
}

// StartSession boots vm and then runs prog until completion or cancellation.
// It returns immediately with a handle; the boot+run work happens in a goroutine.
func StartSession(parent context.Context, vm *VirtualMachine, prog *ir.Program, cfg SessionConfig) *Session {
	return startSession(parent, vm, vm, prog, cfg)
}
