//go:build !windows

package guestagent

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ProcessFamily tracks descendants while their parentage is still observable.
// Unlike a process group, the retained PID set follows children across setsid
// and daemon reparenting so command cancellation can contain the whole workload.
type ProcessFamily struct {
	root  int
	token string
	done  chan struct{}
	once  sync.Once
	mu    sync.Mutex
	pids  map[int]struct{}
}

const processFamilyEnvironmentName = "CC_EXEC_FAMILY"

func ProcessFamilyToken(id string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(id)))[:24]
}

func WithProcessFamilyEnvironment(env []string, token string) []string {
	if len(env) == 0 {
		env = os.Environ()
	} else {
		env = append([]string(nil), env...)
	}
	prefix := processFamilyEnvironmentName + "="
	for i := range env {
		if strings.HasPrefix(env[i], prefix) {
			env[i] = prefix + token
			return env
		}
	}
	return append(env, prefix+token)
}

func NewProcessFamily(root int, token ...string) *ProcessFamily {
	familyToken := ""
	if len(token) != 0 {
		familyToken = token[0]
	}
	if root <= 1 || root == os.Getpid() {
		return &ProcessFamily{done: make(chan struct{}), pids: make(map[int]struct{})}
	}
	f := &ProcessFamily{root: root, token: familyToken, done: make(chan struct{}), pids: map[int]struct{}{root: {}}}
	f.refresh()
	go f.watch()
	return f
}

func (f *ProcessFamily) watch() {
	ticker := time.NewTicker(processFamilyPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			f.refresh()
		case <-f.done:
			return
		}
	}
}

func (f *ProcessFamily) refresh() {
	table, tagged := processSnapshot(f.token)
	if len(table) == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for pid := range f.pids {
		if _, exists := table[pid]; !exists {
			delete(f.pids, pid)
		}
	}
	for pid := range tagged {
		if pid > 1 && pid != os.Getpid() {
			f.pids[pid] = struct{}{}
		}
	}
	changed := true
	for changed {
		changed = false
		for pid, ppid := range table {
			if _, known := f.pids[pid]; known {
				continue
			}
			if _, parentKnown := f.pids[ppid]; parentKnown {
				f.pids[pid] = struct{}{}
				changed = true
			}
		}
	}
}

func (f *ProcessFamily) Signal(sig syscall.Signal) error {
	f.refresh()
	f.mu.Lock()
	pids := make([]int, 0, len(f.pids))
	for pid := range f.pids {
		pids = append(pids, pid)
	}
	f.mu.Unlock()
	var firstErr error
	for _, pid := range pids {
		// The guest agent is PID 1. It is the containment boundary, never part
		// of a command family, even if a transient process-table observation is
		// malformed or a PID is recycled.
		if pid <= 1 || pid == os.Getpid() {
			continue
		}
		if err := syscall.Kill(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (f *ProcessFamily) Terminate() {
	_ = f.Signal(syscall.SIGTERM)
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !f.alive() {
			f.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = f.Signal(syscall.SIGKILL)
	deadline = time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		f.reapExited()
		if !f.alive() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.Close()
}

func (f *ProcessFamily) reapExited() {
	if os.Getpid() != 1 {
		return
	}
	f.mu.Lock()
	pids := make([]int, 0, len(f.pids))
	for pid := range f.pids {
		if pid > 1 {
			pids = append(pids, pid)
		}
	}
	f.mu.Unlock()
	for _, pid := range pids {
		var status syscall.WaitStatus
		_, _ = syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	}
}

func (f *ProcessFamily) alive() bool {
	f.refresh()
	f.mu.Lock()
	defer f.mu.Unlock()
	for pid := range f.pids {
		if pid == f.root {
			continue
		}
		if err := syscall.Kill(pid, 0); err == nil || errors.Is(err, syscall.EPERM) {
			return true
		}
	}
	return false
}

func (f *ProcessFamily) Close() {
	f.once.Do(func() { close(f.done) })
}
