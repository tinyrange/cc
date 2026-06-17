package vm

import (
	"context"
	"fmt"
	"strings"

	"j5.nz/cc/client"
)

type sidecarManagedSession struct {
	worker *sidecarWorkerClient
	id     string
}

func (s *sidecarManagedSession) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	worker, err := s.workerClient()
	if err != nil {
		return client.ExecResponse{}, err
	}
	events, err := worker.Exec(ctx, s.id, req)
	if err != nil {
		return client.ExecResponse{}, err
	}
	return sidecarExecResponse(events), ctx.Err()
}

func (s *sidecarManagedSession) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	worker, err := s.workerClient()
	if err != nil {
		return err
	}
	return worker.ExecStream(ctx, s.id, req, inputs, onEvent)
}

func (s *sidecarManagedSession) Flush(ctx context.Context) error {
	worker, err := s.workerClient()
	if err != nil {
		return err
	}
	return worker.Flush(ctx, s.id)
}

func (s *sidecarManagedSession) ConsoleHistory(ctx context.Context) (string, error) {
	worker, err := s.workerClient()
	if err != nil {
		return "", err
	}
	return worker.ConsoleHistory(ctx, s.id)
}

func (s *sidecarManagedSession) Wait() error {
	worker, err := s.workerClient()
	if err != nil {
		return err
	}
	return worker.Wait(context.Background(), s.id)
}

func (s *sidecarManagedSession) Close() error {
	worker, err := s.workerClient()
	if err != nil {
		return err
	}
	err = worker.Stop(context.Background(), s.id)
	if err != nil && strings.Contains(err.Error(), "no VM") {
		return nil
	}
	return err
}

func (s *sidecarManagedSession) workerClient() (*sidecarWorkerClient, error) {
	if s == nil || s.worker == nil {
		return nil, fmt.Errorf("sidecar worker is not connected")
	}
	return s.worker, nil
}

func sidecarExecResponse(events []client.ExecEvent) client.ExecResponse {
	var resp client.ExecResponse
	for _, event := range events {
		if event.Kind == "stdout" || event.Kind == "stderr" {
			resp.Output += event.Output
		}
		if event.Kind == "exit" {
			resp.ExitCode = event.ExitCode
		}
	}
	return resp
}
