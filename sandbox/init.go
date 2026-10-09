package sandbox

import (
	"bytes"
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"strconv"
	"time"

	"j5.nz/cc/client"
)

func validateInit(payload []byte, protocol int) error {
	if protocol != 0 && protocol != 1 {
		return fmt.Errorf("unsupported sandbox protocol %d", protocol)
	}
	if len(payload) == 0 {
		if protocol != 0 {
			return errors.New("sandbox protocol requires a custom init payload")
		}
		return nil
	}
	f, err := elf.NewFile(bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("custom init must be static Linux amd64 ELF: %w", err)
	}
	defer f.Close()
	if f.Class != elf.ELFCLASS64 || f.Machine != elf.EM_X86_64 || (f.Type != elf.ET_EXEC && f.Type != elf.ET_DYN) {
		return errors.New("custom init must be static Linux amd64 ELF")
	}
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			return errors.New("custom init must not require a dynamic linker")
		}
	}
	return nil
}

type protocolExecutor interface {
	ExecStream(context.Context, client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
}

func verifyProtocol(ctx context.Context, s protocolExecutor, version int) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	exited := false
	err := s.ExecStream(ctx, client.ExecRequest{Command: []string{"/proc/1/exe", "--sa2-protocol"}, User: "root", SkipResolve: true}, nil, func(event client.ExecEvent) error {
		switch event.Kind {
		case "output", "stdout", "stderr":
			data := event.Data
			if data == nil {
				data = []byte(event.Output)
			}
			if stdout.Len()+stderr.Len()+len(data) > 4096 {
				return errors.New("sandbox protocol response exceeds limit")
			}
			if event.Stream == "stderr" || event.Kind == "stderr" {
				stderr.Write(data)
			} else {
				stdout.Write(data)
			}
		case "launch_error":
			return fmt.Errorf("sandbox protocol helper: %s", event.Error)
		case "exit":
			exited = true
			if event.ExitCode != 0 {
				return fmt.Errorf("sandbox protocol helper exited %d", event.ExitCode)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("verify sandbox protocol: %w", err)
	}
	if !exited || stdout.String() != strconv.Itoa(version)+"\n" || stderr.Len() != 0 {
		return errors.New("sandbox init protocol mismatch")
	}
	return nil
}
