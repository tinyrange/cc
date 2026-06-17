package sidecar

import (
	"io"
	"os/exec"
	"sync"
	"time"
)

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
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return <-done
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
