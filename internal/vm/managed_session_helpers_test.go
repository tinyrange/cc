package vm

import (
	"context"
	"strings"
	"testing"

	"j5.nz/cc/client"
)

type helperManagedSession struct {
	flushes int
	history string
}

func (s *helperManagedSession) Exec(context.Context, client.ExecRequest) (client.ExecResponse, error) {
	return client.ExecResponse{}, nil
}

func (s *helperManagedSession) ExecStream(context.Context, client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error {
	return nil
}

func (s *helperManagedSession) Flush(context.Context) error {
	s.flushes++
	return nil
}

func (s *helperManagedSession) ConsoleHistory(context.Context) (string, error) {
	return s.history, nil
}

func (s *helperManagedSession) Wait() error {
	return nil
}

func (s *helperManagedSession) Close() error {
	return nil
}

func TestManagedSessionHelpers(t *testing.T) {
	if err := flushManagedSession(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "instance is not running") {
		t.Fatalf("nil flush error = %v", err)
	}
	history, err := managedSessionConsoleHistory(context.Background(), nil)
	if err != nil || history != "" {
		t.Fatalf("nil history = %q, %v", history, err)
	}

	session := &helperManagedSession{history: "console"}
	if err := flushManagedSession(context.Background(), session); err != nil {
		t.Fatalf("flush session: %v", err)
	}
	if session.flushes != 1 {
		t.Fatalf("flushes = %d, want 1", session.flushes)
	}
	history, err = managedSessionConsoleHistory(context.Background(), session)
	if err != nil || history != "console" {
		t.Fatalf("history = %q, %v", history, err)
	}
	if err := waitManagedSession(session); err != nil {
		t.Fatalf("wait session: %v", err)
	}
	if err := waitManagedSession(nil); err != nil {
		t.Fatalf("wait nil session: %v", err)
	}
	cleanups := 0
	if err := closeManagedSession(session, func() error {
		cleanups++
		return nil
	}); err != nil {
		t.Fatalf("close session: %v", err)
	}
	if cleanups != 1 {
		t.Fatalf("cleanups = %d, want 1", cleanups)
	}
	if err := closeManagedSession(nil, func() error {
		t.Fatal("nil session should not run cleanups")
		return nil
	}); err != nil {
		t.Fatalf("close nil session: %v", err)
	}
}

func TestCloseManagedSessionWithNetwork(t *testing.T) {
	network := &helperNetworkCloser{}
	if err := closeManagedSessionWithNetwork(nil, network); err != nil {
		t.Fatalf("close nil session with network: %v", err)
	}
	if network.closes != 1 {
		t.Fatalf("network closes = %d, want 1", network.closes)
	}

	network = &helperNetworkCloser{}
	if err := closeManagedSessionWithNetwork(&helperManagedSession{}, network); err != nil {
		t.Fatalf("close session with network: %v", err)
	}
	if network.closes != 1 {
		t.Fatalf("network closes with session = %d, want 1", network.closes)
	}
}

type helperNetworkCloser struct {
	closes int
}

func (n *helperNetworkCloser) Close() error {
	n.closes++
	return nil
}
