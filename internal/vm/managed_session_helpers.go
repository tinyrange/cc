package vm

import (
	"context"
	"fmt"

	managedsession "j5.nz/cc/internal/managed/session"
)

func flushManagedSession(ctx context.Context, session managedsession.Session) error {
	if session == nil {
		return fmt.Errorf("instance is not running")
	}
	return session.Flush(ctx)
}

func managedSessionConsoleHistory(ctx context.Context, session managedsession.Session) (string, error) {
	if session == nil {
		return "", nil
	}
	return session.ConsoleHistory(ctx)
}

func waitManagedSession(session managedsession.Session) error {
	if session == nil {
		return nil
	}
	return session.Wait()
}

func closeManagedSession(session managedsession.Session, cleanups ...func() error) error {
	if session == nil {
		return nil
	}
	err := session.Close()
	for _, cleanup := range cleanups {
		if cleanup == nil {
			continue
		}
		if cleanupErr := cleanup(); err == nil {
			err = cleanupErr
		}
	}
	return err
}

type managedNetworkCloser interface {
	Close() error
}

func closeManagedSessionWithNetwork(session managedsession.Session, network managedNetworkCloser) error {
	if session == nil {
		if network == nil {
			return nil
		}
		return network.Close()
	}
	return closeManagedSession(session, func() error {
		if network == nil {
			return nil
		}
		return network.Close()
	})
}
