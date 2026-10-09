package sandbox

import (
	"context"
	"strings"
	"testing"

	"j5.nz/cc/client"
)

type fakeProtocolExec struct{ events []client.ExecEvent }

func (f fakeProtocolExec) ExecStream(ctx context.Context, req client.ExecRequest, _ <-chan client.ExecInput, emit func(client.ExecEvent) error) error {
	if len(req.Command) != 2 || req.Command[0] != "/proc/1/exe" || req.Command[1] != "--sa2-protocol" || !req.SkipResolve {
		panic("unsafe protocol probe")
	}
	for _, e := range f.events {
		if err := emit(e); err != nil {
			return err
		}
	}
	return nil
}

func TestVerifyProtocol(t *testing.T) {
	tests := []struct {
		name   string
		events []client.ExecEvent
		ok     bool
	}{
		{"valid", []client.ExecEvent{{Kind: "stdout", Data: []byte("1\n")}, {Kind: "exit"}}, true},
		{"wrong version", []client.ExecEvent{{Kind: "stdout", Output: "2\n"}, {Kind: "exit"}}, false},
		{"failed exit", []client.ExecEvent{{Kind: "stdout", Output: "1\n"}, {Kind: "exit", ExitCode: 1}}, false},
		{"missing exit", []client.ExecEvent{{Kind: "stdout", Output: "1\n"}}, false},
		{"stderr", []client.ExecEvent{{Kind: "stderr", Output: "1\n"}, {Kind: "exit"}}, false},
		{"oversized", []client.ExecEvent{{Kind: "stdout", Output: strings.Repeat("1", 4097)}, {Kind: "exit"}}, false},
		{"launch error", []client.ExecEvent{{Kind: "launch_error", Error: "not found"}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyProtocol(context.Background(), fakeProtocolExec{tc.events}, 1)
			if (err == nil) != tc.ok {
				t.Fatalf("result %v", err)
			}
		})
	}
}

func TestValidateCustomInitFailClosed(t *testing.T) {
	if err := validateInit(nil, 0); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		payload  []byte
		protocol int
	}{{nil, 1}, {nil, 2}, {[]byte("not ELF"), 1}} {
		if err := validateInit(tc.payload, tc.protocol); err == nil {
			t.Fatal("invalid init accepted")
		}
	}
}
