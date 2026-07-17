package vm

import (
	"slices"
	"testing"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/vm/execplan"
)

func TestLinuxDefaultEnvAvoidsLibuvIOUringOnVirtioFS(t *testing.T) {
	if got := withLinuxDefaultEnv(nil); !slices.Contains(got, "UV_USE_IO_URING=0") {
		t.Fatalf("Linux default environment = %#v, want UV_USE_IO_URING=0", got)
	}
	if got := withLinuxDefaultEnv([]string{"UV_USE_IO_URING=1"}); !slices.Contains(got, "UV_USE_IO_URING=1") {
		t.Fatalf("explicit libuv setting was not preserved: %#v", got)
	}
}

func TestRunningVMDefersCommandResolutionToGuest(t *testing.T) {
	req := runningVMExecRequest(client.RunRequest{Command: []string{"/usr/bin/python3", "server.py"}})
	resolved, err := execplan.ResolveExecRequest(req, execplan.Resolver{
		MissingRootErr: "the original image does not contain the command installed after boot",
	})
	if err != nil {
		t.Fatalf("resolve running-VM command: %v", err)
	}
	if resolved.Command[0] != "/usr/bin/python3" {
		t.Fatalf("running-VM command = %q, want guest path unchanged", resolved.Command)
	}
}
