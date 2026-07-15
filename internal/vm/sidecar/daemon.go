package sidecar

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

const CommandReapTimeout = 100 * time.Millisecond

var ErrCommandTimeout = errors.New("command did not exit before timeout")

type CommandTimeoutError struct {
	Timeout      time.Duration
	KillErr      error
	WaitErr      error
	ReapTimedOut bool
}

func (e *CommandTimeoutError) Error() string {
	if e.ReapTimedOut {
		return fmt.Sprintf("command timed out after %s and did not exit within %s after kill", e.Timeout, CommandReapTimeout)
	}
	if e.KillErr != nil {
		return fmt.Sprintf("command timed out after %s; kill failed: %v", e.Timeout, e.KillErr)
	}
	return fmt.Sprintf("command timed out after %s; wait after kill: %v", e.Timeout, e.WaitErr)
}

func (e *CommandTimeoutError) Unwrap() error {
	return errors.Join(ErrCommandTimeout, e.KillErr, e.WaitErr)
}

type Daemon struct {
	cmd      *exec.Cmd
	worker   *Client
	stdout   io.ReadCloser
	once     sync.Once
	err      error
	cleanups []func()
}

func NewDaemon(cmd *exec.Cmd, worker *Client, stdout io.ReadCloser, cleanups []func()) *Daemon {
	return &Daemon{cmd: cmd, worker: worker, stdout: stdout, cleanups: cleanups}
}

func (d *Daemon) Worker() *Client {
	if d == nil {
		return nil
	}
	return d.worker
}

func (d *Daemon) AddCleanup(cleanup func()) {
	if d == nil || cleanup == nil {
		return
	}
	d.cleanups = append(d.cleanups, cleanup)
}

func (d *Daemon) Close() error {
	d.once.Do(func() {
		if d.worker != nil {
			d.err = d.worker.Close()
		}
		if d.stdout != nil {
			_ = d.stdout.Close()
		}
		CloseCleanups(d.cleanups)
		if d.cmd != nil {
			if err := WaitCommand(d.cmd, 5*time.Second); d.err == nil && err != nil {
				d.err = err
			}
		}
	})
	return d.err
}

func WaitCommand(cmd *exec.Cmd, timeout time.Duration) error {
	if cmd == nil {
		return nil
	}
	return waitCommand(timeout, cmd.Wait, func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Kill()
	})
}

func waitCommand(timeout time.Duration, wait func() error, kill func() error) error {
	done := make(chan error, 1)
	go func() {
		done <- wait()
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
	}
	killErr := kill()
	reapTimer := time.NewTimer(CommandReapTimeout)
	defer reapTimer.Stop()
	select {
	case waitErr := <-done:
		return &CommandTimeoutError{Timeout: timeout, KillErr: killErr, WaitErr: waitErr}
	case <-reapTimer.C:
		return &CommandTimeoutError{Timeout: timeout, KillErr: killErr, ReapTimedOut: true}
	}
}

func KillCommand(cmd *exec.Cmd) error {
	if cmd == nil {
		return nil
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return cmd.Wait()
}

func CloseCleanups(cleanups []func()) {
	for i := len(cleanups) - 1; i >= 0; i-- {
		if cleanups[i] != nil {
			cleanups[i]()
		}
	}
}
