//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestConfigureMemoryOvercommit(t *testing.T) {
	procRoot := t.TempDir()
	vmDir := filepath.Join(procRoot, "sys", "vm")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	target := filepath.Join(vmDir, "overcommit_memory")
	if err := os.WriteFile(target, []byte("0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	configureMemoryOvercommit(procRoot)

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "1\n" {
		t.Fatalf("overcommit_memory = %q, want %q", got, "1\n")
	}
}

func TestConfigureClockSkipsUnsetTime(t *testing.T) {
	called := false
	old := setTimeOfDay
	setTimeOfDay = func(*unix.Timeval) error {
		called = true
		return nil
	}
	t.Cleanup(func() { setTimeOfDay = old })

	if err := configureClock(0); err != nil {
		t.Fatalf("configureClock(0) error = %v", err)
	}
	if called {
		t.Fatal("configureClock(0) called setTimeOfDay")
	}
}

func TestConfigureClockSetsUnixTime(t *testing.T) {
	const wantUnix = int64(1_700_000_123)
	var got unix.Timeval
	old := setTimeOfDay
	setTimeOfDay = func(tv *unix.Timeval) error {
		got = *tv
		return nil
	}
	t.Cleanup(func() { setTimeOfDay = old })

	if err := configureClock(wantUnix); err != nil {
		t.Fatalf("configureClock() error = %v", err)
	}
	if got.Sec != wantUnix || got.Usec != 0 {
		t.Fatalf("timeval = {%d, %d}, want {%d, 0}", got.Sec, got.Usec, wantUnix)
	}
}

func TestConfigureClockReportsSettimeofdayError(t *testing.T) {
	want := errors.New("boom")
	old := setTimeOfDay
	setTimeOfDay = func(*unix.Timeval) error {
		return want
	}
	t.Cleanup(func() { setTimeOfDay = old })

	err := configureClock(1_700_000_123)
	if !errors.Is(err, want) {
		t.Fatalf("configureClock() error = %v, want wrapping %v", err, want)
	}
}
