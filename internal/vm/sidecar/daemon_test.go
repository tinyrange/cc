package sidecar

import (
	"errors"
	"os"
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
	cmd := sidecarDaemonTestCommand("exit")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start exit helper: %v", err)
	}
	if err := WaitCommand(cmd, 5*time.Second); err != nil {
		t.Fatalf("wait exit helper: %v", err)
	}

	slow := sidecarDaemonTestCommand("sleep")
	if err := slow.Start(); err != nil {
		t.Fatalf("start sleep helper: %v", err)
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

func TestWaitCommandBoundsKillResistantReap(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	killErr := errors.New("kill refused")
	start := time.Now()
	err := waitCommand(10*time.Millisecond, func() error {
		<-release
		return nil
	}, func() error {
		return killErr
	})
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("waitCommand took %s", elapsed)
	}
	if !errors.Is(err, ErrCommandTimeout) || !errors.Is(err, killErr) {
		t.Fatalf("waitCommand error = %v", err)
	}
	var timeoutErr *CommandTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("waitCommand error type = %T", err)
	}
	if timeoutErr.Timeout != 10*time.Millisecond || !timeoutErr.ReapTimedOut {
		t.Fatalf("timeout state = %+v", timeoutErr)
	}
}

func sidecarDaemonTestCommand(mode string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=TestSidecarDaemonHelperProcess")
	cmd.Env = append(os.Environ(), "CCX3_SIDECAR_DAEMON_TEST_HELPER="+mode)
	return cmd
}

func TestSidecarDaemonHelperProcess(t *testing.T) {
	switch os.Getenv("CCX3_SIDECAR_DAEMON_TEST_HELPER") {
	case "exit":
		os.Exit(0)
	case "sleep":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	default:
		return
	}
}
