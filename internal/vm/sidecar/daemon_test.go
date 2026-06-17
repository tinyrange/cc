package sidecar

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDaemonCloseRunsCleanupsOnceInReverseOrder(t *testing.T) {
	var order []string
	daemon := NewDaemon(nil, nil, nil, []func(){
		func() { order = append(order, "first") },
		nil,
		func() { order = append(order, "second") },
	})
	if err := daemon.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := daemon.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := strings.Join(order, ","); got != "second,first" {
		t.Fatalf("cleanup order = %q", got)
	}
}

func TestWaitCommand(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start true: %v", err)
	}
	if err := WaitCommand(cmd, time.Second); err != nil {
		t.Fatalf("wait true: %v", err)
	}

	slow := exec.Command("sh", "-c", "sleep 10")
	if err := slow.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	start := time.Now()
	err := WaitCommand(slow, 10*time.Millisecond)
	if err == nil {
		t.Fatalf("wait sleep unexpectedly succeeded")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("wait sleep took %s, want timeout kill", elapsed)
	}
}
