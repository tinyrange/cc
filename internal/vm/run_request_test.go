package vm

import (
	"testing"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/vm/execplan"
)

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
