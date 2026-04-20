//go:build !darwin || !arm64

package hvf

import (
	"context"
	"fmt"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/oci"
)

type ContainerRunRequest struct {
	Kernel     []byte
	Image      *oci.Image
	Shares     []DirectoryShare
	Command    []string
	Env        []string
	WorkDir    string
	User       string
	MemoryMB   uint64
	CPUs       int
	Dmesg      bool
	Persistent bool
}

type DirectoryShare struct {
	Source   string
	Mount    string
	Writable bool
}

type ContainerRunResult struct {
	ExitCode   int
	Output     string
	Transcript string
}

type ContainerSession struct{}

func (s *ContainerSession) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	_ = ctx
	_ = req
	return client.ExecResponse{}, fmt.Errorf("hvf container runner is unsupported on this host")
}

func StartContainer(ctx context.Context, req ContainerRunRequest) (*ContainerSession, error) {
	return StartContainerStream(ctx, req, nil)
}

func StartContainerStream(ctx context.Context, req ContainerRunRequest, onEvent func(client.BootEvent) error) (*ContainerSession, error) {
	_ = ctx
	_ = req
	_ = onEvent
	return nil, fmt.Errorf("hvf container runner is unsupported on this host")
}

func (s *ContainerSession) Wait() error {
	return fmt.Errorf("hvf container runner is unsupported on this host")
}

func (s *ContainerSession) Close() error {
	return nil
}

func RunContainer(ctx context.Context, req ContainerRunRequest) (ContainerRunResult, error) {
	_ = ctx
	_ = req
	return ContainerRunResult{}, fmt.Errorf("hvf container runner is unsupported on this host")
}
