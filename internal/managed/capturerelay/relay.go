//go:build !windows

package capturerelay

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const pollInterval = 10 * time.Millisecond

// Run serves a private command-capture control FIFO. One process owns every
// capture in a persistent shell context; retired streams keep draining guest
// writers without retaining their spool files or a process per stream.
func Run(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: capture-relay CONTROL_FIFO SHELL_CONTROL MAX_STORED")
	}
	maxStored, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil || maxStored < 0 {
		return fmt.Errorf("invalid maximum stored size %q", args[2])
	}
	return newRelay(args[0], args[1], maxStored).run()
}

type relay struct {
	controlPath string
	shellPath   string
	maxStored   int64
	ctx         context.Context
	cancel      context.CancelFunc

	mu       sync.Mutex
	captures map[string]*capture
}

type capture struct {
	owner      *relay
	account    string
	outputPath string
	fifoPath   string
	closedPath string
	overflow   string
	retired    string
	registered string

	mu          sync.Mutex
	spool       *os.File
	reader      *os.File
	total       int64
	stored      int64
	primaryDone bool
	isRetired   bool
	overflowed  bool
}

func newRelay(controlPath, shellPath string, maxStored int64) *relay {
	ctx, cancel := context.WithCancel(context.Background())
	return &relay{controlPath: controlPath, shellPath: shellPath, maxStored: maxStored, ctx: ctx, cancel: cancel, captures: make(map[string]*capture)}
}

func (r *relay) run() error {
	_ = os.Remove(r.controlPath)
	if err := syscall.Mkfifo(r.controlPath, 0o600); err != nil {
		return fmt.Errorf("create capture control fifo: %w", err)
	}
	defer os.Remove(r.controlPath)
	if info, err := os.Stat(filepath.Dir(r.controlPath)); err == nil {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			uid, gid := int(stat.Uid), int(stat.Gid)
			if err := os.Chown(r.controlPath, uid, gid); err != nil {
				return fmt.Errorf("assign capture control fifo: %w", err)
			}
			if os.Geteuid() == 0 && (uid != 0 || gid != 0) {
				if err := syscall.Setgroups([]int{gid}); err != nil {
					return fmt.Errorf("set capture relay groups: %w", err)
				}
				if err := syscall.Setgid(gid); err != nil {
					return fmt.Errorf("set capture relay gid: %w", err)
				}
				if err := syscall.Setuid(uid); err != nil {
					return fmt.Errorf("set capture relay uid: %w", err)
				}
			}
		}
	}
	control, err := os.OpenFile(r.controlPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open capture control fifo: %w", err)
	}
	defer control.Close()
	readyPath := r.controlPath + ".ready"
	if err := os.WriteFile(readyPath, []byte("1\n"), 0o600); err != nil {
		return fmt.Errorf("publish capture relay readiness: %w", err)
	}
	defer os.Remove(readyPath)
	fmt.Println("vmsh-capture-relay-ready")

	scanner := bufio.NewScanner(control)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		switch fields[0] {
		case "register":
			if len(fields) != 8 {
				return fmt.Errorf("invalid capture registration")
			}
			r.register(fields[1:])
		case "finish":
			if len(fields) != 2 {
				return fmt.Errorf("invalid capture finish")
			}
			if capture := r.capture(fields[1]); capture != nil {
				capture.finishPrimary()
			}
		case "retire":
			if len(fields) != 2 {
				return fmt.Errorf("invalid capture retirement")
			}
			if capture := r.capture(fields[1]); capture != nil {
				capture.retire()
			}
		case "stop":
			r.cancel()
			return nil
		default:
			return fmt.Errorf("unknown capture relay operation %q", fields[0])
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read capture control: %w", err)
	}
	return io.ErrUnexpectedEOF
}

func (r *relay) register(fields []string) {
	c := &capture{
		owner: r, account: fields[0], outputPath: fields[1], fifoPath: fields[2],
		closedPath: fields[3], overflow: fields[4], retired: fields[5], registered: fields[6],
	}
	var err error
	c.spool, err = os.OpenFile(c.outputPath, os.O_WRONLY|os.O_TRUNC, 0)
	if err == nil {
		c.reader, err = os.OpenFile(c.fifoPath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	}
	if err != nil {
		if c.spool != nil {
			_ = c.spool.Close()
		}
		_ = writeExisting(c.registered, "error: "+err.Error()+"\n")
		return
	}
	r.mu.Lock()
	if _, exists := r.captures[c.account]; exists {
		r.mu.Unlock()
		_ = c.spool.Close()
		_ = c.reader.Close()
		_ = writeExisting(c.registered, "error: duplicate account\n")
		return
	}
	r.captures[c.account] = c
	r.mu.Unlock()
	if err := writeExisting(c.registered, "ok\n"); err != nil {
		c.retire()
		return
	}
	go c.drain()
}

func (r *relay) capture(account string) *capture {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.captures[account]
}

func (r *relay) remove(c *capture) {
	r.mu.Lock()
	if r.captures[c.account] == c {
		delete(r.captures, c.account)
	}
	r.mu.Unlock()
}

func (c *capture) drain() {
	defer c.owner.remove(c)
	defer c.reader.Close()
	buf := make([]byte, 32<<10)
	for {
		n, err := c.reader.Read(buf)
		if n > 0 {
			c.consume(buf[:n])
			continue
		}
		if err == nil || errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			select {
			case <-c.owner.ctx.Done():
				return
			case <-time.After(pollInterval):
			}
			continue
		}
		if errors.Is(err, io.EOF) {
			c.mu.Lock()
			done := c.primaryDone
			c.mu.Unlock()
			if done {
				c.complete()
				return
			}
			select {
			case <-c.owner.ctx.Done():
				return
			case <-time.After(pollInterval):
			}
			continue
		}
		c.complete()
		return
	}
}

func (c *capture) consume(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total += int64(len(data))
	if c.isRetired || c.spool == nil || c.stored >= c.owner.maxStored {
		c.markOverflowLocked()
		return
	}
	writable := int64(len(data))
	if remaining := c.owner.maxStored - c.stored; writable > remaining {
		writable = remaining
	}
	if writable > 0 {
		n, err := c.spool.Write(data[:writable])
		c.stored += int64(n)
		if err != nil || int64(n) != writable {
			c.markOverflowLocked()
		}
	}
	if writable != int64(len(data)) {
		c.markOverflowLocked()
	}
}

func (c *capture) markOverflowLocked() {
	if c.overflowed {
		return
	}
	c.overflowed = true
	_ = writeExisting(c.overflow, "1\n")
}

func (c *capture) finishPrimary() {
	c.mu.Lock()
	c.primaryDone = true
	c.mu.Unlock()
}

func (c *capture) retire() {
	c.mu.Lock()
	if !c.isRetired {
		c.isRetired = true
		c.markOverflowLocked()
		if c.spool != nil {
			_ = c.spool.Close()
			c.spool = nil
		}
	}
	c.mu.Unlock()
	_ = writeExisting(c.retired, "1\n")
}

func (c *capture) complete() {
	c.mu.Lock()
	if c.spool != nil {
		_ = c.spool.Close()
		c.spool = nil
	}
	total := c.total
	retired := c.isRetired
	c.mu.Unlock()
	if !retired {
		_ = writeExisting(c.closedPath, "1\n")
	}
	control, err := os.OpenFile(c.owner.shellPath, os.O_WRONLY, 0)
	if err == nil {
		_, _ = fmt.Fprintf(control, "\x1dvmsh-capture:%s:%d\x1f\n", c.account, total)
		_ = control.Close()
	}
}

func writeExisting(path, value string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(file, value)
	closeErr := file.Close()
	return errors.Join(writeErr, closeErr)
}
