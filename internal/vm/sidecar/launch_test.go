package sidecar

import (
	"os"
	"testing"
)

func TestLaunchCommand(t *testing.T) {
	t.Setenv("CCX3_TEST_MODE", "")
	cmd := LaunchCommand("/tmp/ccvm", "/cache", "/tmp/control.sock", []string{"EXTRA=1"}, LaunchOptions{
		DisableEnv: "CCX3_TEST_DISABLE",
		ControlEnv: "CCX3_TEST_CONTROL",
		ModeEnv:    "CCX3_TEST_MODE",
	})
	if cmd.Path != "/tmp/ccvm" {
		t.Fatalf("path = %q", cmd.Path)
	}
	if !containsArg(cmd.Args, "-worker") || !containsArgPair(cmd.Args, "-cache-dir", "/cache") {
		t.Fatalf("worker launch args = %#v", cmd.Args)
	}
	if cmd.Stderr != os.Stderr {
		t.Fatalf("stderr was not inherited")
	}
	if !envHas(cmd.Env, "CCX3_TEST_DISABLE=1") {
		t.Fatalf("env missing disable flag: %#v", cmd.Env)
	}
	if !envHas(cmd.Env, "CCX3_TEST_CONTROL=/tmp/control.sock") {
		t.Fatalf("env missing control socket: %#v", cmd.Env)
	}
	if !envHas(cmd.Env, "EXTRA=1") {
		t.Fatalf("env missing extra entry: %#v", cmd.Env)
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func containsArgPair(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func envHas(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
