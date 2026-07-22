//go:build freebsd || netbsd || openbsd

package guestagent

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type bsdProcessFamilyPreparation struct {
	reader *os.File
	writer *os.File
}

func prepareProcessFamily(cmd *exec.Cmd, _ string) (processFamilyPreparation, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	childFD := 3 + len(cmd.ExtraFiles)
	cmd.ExtraFiles = append(cmd.ExtraFiles, reader)
	original := append([]string(nil), cmd.Args...)
	cmd.Path = "/bin/sh"
	cmd.Args = append([]string{"sh", "-c", fmt.Sprintf("IFS= read -r _ <&%d || exit 126; exec %d<&-; exec \"$@\"", childFD, childFD), "sh"}, original...)
	cmd.Err = nil
	return &bsdProcessFamilyPreparation{reader: reader, writer: writer}, nil
}

func (p *bsdProcessFamilyPreparation) Start(root int) (processFamilyTracker, error) {
	_ = p.reader.Close()
	tracker, err := newBSDProcessTracker(root)
	if err != nil {
		_ = p.writer.Close()
		return nil, err
	}
	tracker.gate = p.writer
	return tracker, nil
}

func (p *bsdProcessFamilyPreparation) Abort() {
	_ = p.reader.Close()
	_ = p.writer.Close()
}

type bsdProcessTracker struct {
	kq   int
	done chan struct{}
	once sync.Once
	open sync.Once
	mu   sync.Mutex
	pids map[int]struct{}
	gate *os.File
}

func newBSDProcessTracker(root int) (*bsdProcessTracker, error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	event := unix.Kevent_t{Ident: uint64(root), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ENABLE | unix.EV_CLEAR, Fflags: unix.NOTE_FORK | unix.NOTE_TRACK | unix.NOTE_EXIT}
	if _, err := unix.Kevent(kq, []unix.Kevent_t{event}, nil, nil); err != nil {
		_ = unix.Close(kq)
		return nil, err
	}
	t := &bsdProcessTracker{kq: kq, done: make(chan struct{}), pids: map[int]struct{}{root: {}}}
	go t.watch()
	return t, nil
}

func (t *bsdProcessTracker) watch() {
	events := make([]unix.Kevent_t, 32)
	for {
		timeout := unix.NsecToTimespec((20 * time.Millisecond).Nanoseconds())
		n, err := unix.Kevent(t.kq, nil, events, &timeout)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return
		}
		t.mu.Lock()
		for _, event := range events[:n] {
			pid := int(event.Ident)
			if pid > 1 {
				t.pids[pid] = struct{}{}
			}
		}
		t.mu.Unlock()
		select {
		case <-t.done:
			return
		default:
		}
	}
}

func (t *bsdProcessTracker) Snapshot() map[int]struct{} {
	t.open.Do(func() {
		_, _ = t.gate.Write([]byte("\n"))
		_ = t.gate.Close()
	})
	t.mu.Lock()
	defer t.mu.Unlock()
	copy := make(map[int]struct{}, len(t.pids))
	for pid := range t.pids {
		copy[pid] = struct{}{}
	}
	return copy
}

func (t *bsdProcessTracker) Close() {
	t.once.Do(func() {
		close(t.done)
		t.open.Do(func() { _ = t.gate.Close() })
		_ = unix.Close(t.kq)
	})
}
