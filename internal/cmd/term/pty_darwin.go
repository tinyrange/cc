//go:build darwin

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

type PtyShell struct {
	mu sync.Mutex

	master *os.File
	slave  *os.File
	cmd    *exec.Cmd
}

var (
	ptyInitOnce sync.Once
	ptyInitErr  error

	libSystem uintptr
	openptyFn  func(master, slave *int32, name *byte, termp *unix.Termios, winp *unix.Winsize) int32
)

func ensurePty() error {
	ptyInitOnce.Do(func() {
		var err error
		libSystem, err = purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_GLOBAL)
		if err != nil {
			ptyInitErr = err
			return
		}
		purego.RegisterLibFunc(&openptyFn, libSystem, "openpty")
	})
	return ptyInitErr
}

func startLoginShell() (*PtyShell, error) {
	if err := ensurePty(); err != nil {
		return nil, fmt.Errorf("load openpty: %w", err)
	}

	var masterFD, slaveFD int32
	if rc := openptyFn(&masterFD, &slaveFD, nil, nil, nil); rc != 0 {
		return nil, fmt.Errorf("openpty failed: %d", rc)
	}

	master := os.NewFile(uintptr(masterFD), "pty-master")
	slave := os.NewFile(uintptr(slaveFD), "pty-slave")
	if master == nil || slave == nil {
		if master != nil {
			_ = master.Close()
		}
		if slave != nil {
			_ = slave.Close()
		}
		return nil, errors.New("failed to create pty files")
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}

	cmd := exec.Command(shell, "-l")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		// The exec package remaps cmd.Stdin onto fd 0 in the child. On darwin,
		// using the original slave fd number here can fail because it may not
		// exist after the remapping. Using fd 0 (stdin) matches the controlling
		// TTY we want.
		Ctty: 0,
	}

	if err := cmd.Start(); err != nil {
		_ = master.Close()
		_ = slave.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}

	// The child has inherited the slave FD; the parent should close it to avoid
	// keeping the slave side alive unnecessarily.
	_ = slave.Close()
	slave = nil

	return &PtyShell{
		master: master,
		slave:  slave,
		cmd:    cmd,
	}, nil
}

func (p *PtyShell) Read(b []byte) (int, error) {
	p.mu.Lock()
	f := p.master
	p.mu.Unlock()
	if f == nil {
		return 0, io.EOF
	}
	return f.Read(b)
}

func (p *PtyShell) Write(b []byte) (int, error) {
	p.mu.Lock()
	f := p.master
	p.mu.Unlock()
	if f == nil {
		return 0, io.EOF
	}
	return f.Write(b)
}

func (p *PtyShell) Resize(cols, rows int) error {
	p.mu.Lock()
	f := p.master
	p.mu.Unlock()
	if f == nil {
		return io.EOF
	}
	ws := &unix.Winsize{
		Col: uint16(cols),
		Row: uint16(rows),
	}
	if err := unix.IoctlSetWinsize(int(f.Fd()), unix.TIOCSWINSZ, ws); err != nil {
		return fmt.Errorf("set winsz: %w", err)
	}
	return nil
}

func (p *PtyShell) Close() error {
	p.mu.Lock()
	master := p.master
	slave := p.slave
	cmd := p.cmd
	p.master = nil
	p.slave = nil
	p.cmd = nil
	p.mu.Unlock()

	var firstErr error
	if master != nil {
		if err := master.Close(); err != nil {
			firstErr = err
		}
	}
	if slave != nil {
		if err := slave.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if cmd != nil && cmd.Process != nil {
		// Best-effort; shell exits when PTY closes, but don’t hang.
		_ = cmd.Process.Kill()
	}
	return firstErr
}

func (p *PtyShell) Wait() error {
	p.mu.Lock()
	cmd := p.cmd
	p.mu.Unlock()
	if cmd == nil {
		return nil
	}
	return cmd.Wait()
}


