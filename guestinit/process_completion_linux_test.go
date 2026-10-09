//go:build linux

package guestinit

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"j5.nz/cc/internal/vmruntime"
)

// Use the test executable so setsid and inherited control FD behavior do not
// depend on shell utilities. The escaped child retains stdout, stderr and FD 3.
func TestManagedExecEscapedHelper(t *testing.T) {
	role := os.Getenv("CC_TEST_ESCAPED_ROLE")
	if role == "" {
		return
	}
	path := os.Getenv("CC_TEST_ESCAPED_PATH")
	if role == "child" {
		fmt.Fprintln(os.Stdout, "escaped stdout")
		fmt.Fprintln(os.Stderr, "escaped stderr")
		ctl := os.NewFile(3, "control")
		fmt.Fprintln(ctl, `{"escaped":true}`)
		if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			os.Exit(91)
		}
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		os.Exit(92)
	}
	cmd := exec.Command(exe, "-test.run=^TestManagedExecEscapedHelper$")
	cmd.Env = append(os.Environ(), "CC_TEST_ESCAPED_ROLE=child")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.ExtraFiles = []*os.File{os.NewFile(3, "control")}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		os.Exit(93)
	}
	if role != "fast" {
		for {
			if _, err := os.Stat(path + ".release"); err == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
	}
	os.Exit(0)
}

func TestManagedExecEscapedCompletion(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"single", "group", ""} {
		for _, action := range []string{"exit", "cancel", "fast", "tty"} {
			t.Run(mode+"/"+action, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "escaped.pid")
				role := "parent"
				if action == "fast" {
					role = "fast"
				}
				env := append(os.Environ(), "CC_TEST_ESCAPED_ROLE="+role, "CC_TEST_ESCAPED_PATH="+path, "GORACE=atexit_sleep_ms=0")
				cfg := config{Protocol: 1, OutputMarkerPref: vmruntime.CommandOutputMarker, ErrorMarkerPref: vmruntime.CommandErrorMarker, ControlMarkerPref: vmruntime.CommandControlMarker, ExitMarkerPrefix: vmruntime.CommandExitMarkerPref}
				managed := &managedExec{mode: mode}
				var output bytes.Buffer
				done := make(chan struct{})
				go func() {
					runManagedExec(cfg, &output, "escaped", []string{exe, "-test.run=^TestManagedExecEscapedHelper$"}, env, "", "/", "", nil, managed, action == "tty", true, 80, 24, nil, func() {})
					close(done)
				}()
				// Always clean both processes, including on a regression timeout; do not
				// inspect the output buffer until the worker has stopped writing it.
				child := 0
				defer func() {
					if child > 0 {
						_ = syscall.Kill(child, syscall.SIGKILL)
					}
					managed.stdinMu.Lock()
					parent := managed.process
					managed.stdinMu.Unlock()
					if parent != nil {
						_ = parent.Kill()
					}
					select {
					case <-done:
					case <-time.After(4 * time.Second):
						t.Error("worker did not stop after test cleanup")
					}
				}()
				deadline := time.Now().Add(4 * time.Second)
				for time.Now().Before(deadline) {
					data, _ := os.ReadFile(path)
					child, _ = strconv.Atoi(strings.TrimSpace(string(data)))
					managed.stdinMu.Lock()
					started := managed.process != nil
					managed.stdinMu.Unlock()
					if child > 0 && started {
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
				if child <= 0 {
					t.Fatal("escaped descendant did not start")
				}
				pgid, err := syscall.Getpgid(child)
				if err != nil || pgid != child {
					t.Fatalf("descendant did not setsid: pgid=%d err=%v", pgid, err)
				}
				if action == "tty" {
					if err := managed.resize(100, 30); err != nil {
						t.Fatal(err)
					}
				}
				if action != "fast" {
					// Give the /proc fallback an observable parent/child interval. Fast exit
					// separately exercises the case where that best-effort tracking may miss.
					time.Sleep(100 * time.Millisecond)
					if action == "cancel" {
						if err := managed.signal("KILL"); err != nil {
							t.Fatal(err)
						}
					} else if err := os.WriteFile(path+".release", nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				select {
				case <-done:
				case <-time.After(4 * time.Second):
					t.Fatal("completion hung on setsid descendant descriptors")
				}
				state, _, exists := procProcessState("/proc/" + strconv.Itoa(child) + "/stat")
				alive := exists && state != 'Z'
				if mode == "single" && !alive {
					t.Fatal("single killed escaped descendant")
				}
				if mode != "single" && action != "fast" && alive {
					t.Fatal("group/family failed to kill observed escaped descendant")
				}
				// A fast-reparented child can survive without cgroups, but its inherited
				// descriptors must not prevent the direct process's exit event.
				foundExit := false
				for _, line := range strings.Split(output.String(), "\n") {
					event, _, ok, err := vmruntime.ParseManagedExecEventLine(line, "escaped")
					if err != nil {
						t.Fatal(err)
					}
					if ok && event.Kind == "exit" {
						foundExit = true
					}
				}
				if !foundExit {
					t.Fatalf("missing exit event: %s", output.String())
				}
			})
		}
	}
}
