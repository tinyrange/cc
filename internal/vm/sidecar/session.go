package sidecar

import (
	"context"
	"fmt"
	"strings"

	"j5.nz/cc/client"
)

type WorkerClient interface {
	Exec(context.Context, string, client.ExecRequest) ([]client.ExecEvent, error)
	ExecStream(context.Context, string, client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
	Flush(context.Context, string) error
	ConsoleHistory(context.Context, string) (string, error)
	Wait(context.Context, string) error
	Stop(context.Context, string) error
}

type ManagedSession struct {
	worker WorkerClient
	id     string
}

func NewManagedSession(worker WorkerClient, id string) *ManagedSession {
	return &ManagedSession{worker: worker, id: id}
}

func (s *ManagedSession) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	worker, err := s.workerClient()
	if err != nil {
		return client.ExecResponse{}, err
	}
	events, err := worker.Exec(ctx, s.id, req)
	if err != nil {
		return client.ExecResponse{}, err
	}
	return ExecResponse(events), ctx.Err()
}

func (s *ManagedSession) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	worker, err := s.workerClient()
	if err != nil {
		return err
	}
	return worker.ExecStream(ctx, s.id, req, inputs, onEvent)
}

func (s *ManagedSession) Flush(ctx context.Context) error {
	worker, err := s.workerClient()
	if err != nil {
		return err
	}
	return worker.Flush(ctx, s.id)
}

func (s *ManagedSession) ConsoleHistory(ctx context.Context) (string, error) {
	worker, err := s.workerClient()
	if err != nil {
		return "", err
	}
	return worker.ConsoleHistory(ctx, s.id)
}

func (s *ManagedSession) Wait() error {
	worker, err := s.workerClient()
	if err != nil {
		return err
	}
	return worker.Wait(context.Background(), s.id)
}

func (s *ManagedSession) Close() error {
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

func (s *ManagedSession) workerClient() (WorkerClient, error) {
	if s == nil || s.worker == nil {
		return nil, fmt.Errorf("sidecar worker is not connected")
	}
	return s.worker, nil
}

func ExecResponse(events []client.ExecEvent) client.ExecResponse {
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
