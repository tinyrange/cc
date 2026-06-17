package vm

import (
	"context"
	"fmt"
	"strings"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
	managedsession "j5.nz/cc/internal/managed/session"
)

type managedInstanceCore struct {
	osName         string
	session        managedsession.Session
	root           imagefs.Directory
	baseEnv        []string
	defaultRootDir string
	workDir        string
	caps           guestCapabilities
	env            func(base, overrides []string, replace bool) []string
	user           func(string) (string, error)
	missingRootErr string
	markResolved   bool
}

func (i *managedInstanceCore) ManagedCapabilities() guestCapabilities {
	if i == nil {
		return guestCapabilities{}
	}
	return i.caps
}

func (i *managedInstanceCore) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	if i == nil || i.session == nil {
		return client.ExecResponse{}, fmt.Errorf("instance is not running")
	}
	if req.Kind != "" && req.Kind != "exec" {
		if err := i.checkControlRequest(req.Kind); err != nil {
			return client.ExecResponse{}, err
		}
		return i.session.Exec(ctx, req)
	}
	execReq, err := i.execRequest(req)
	if err != nil {
		return client.ExecResponse{}, err
	}
	return i.session.Exec(ctx, execReq)
}

func (i *managedInstanceCore) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	if i == nil || i.session == nil {
		return fmt.Errorf("instance is not running")
	}
	if req.Kind != "" && req.Kind != "exec" {
		if err := i.checkControlRequest(req.Kind); err != nil {
			return err
		}
		return i.session.ExecStream(ctx, controlExecRequest(req, i.workDir), inputs, onEvent)
	}
	execReq, err := i.execRequest(req)
	if err != nil {
		return err
	}
	return i.session.ExecStream(ctx, execReq, inputs, onEvent)
}

func (i *managedInstanceCore) execRequest(req client.ExecRequest) (client.ExecRequest, error) {
	if i == nil {
		return client.ExecRequest{}, fmt.Errorf("instance is not running")
	}
	return resolveManagedExecRequest(req, managedExecResolver{
		root:           i.root,
		baseEnv:        i.baseEnv,
		defaultWorkDir: i.workDir,
		defaultRootDir: i.defaultRootDir,
		missingRootErr: i.missingRootError(),
		env:            i.env,
		user:           i.user,
		markResolved:   i.markResolved,
	})
}

func (i *managedInstanceCore) AddShare(context.Context, client.ShareMount) error {
	return i.unsupported("filesystem shares")
}

func (i *managedInstanceCore) AddPortForward(context.Context, client.PortForward) error {
	return i.unsupported("port forwards")
}

func (i *managedInstanceCore) Flush(ctx context.Context) error {
	if i == nil {
		return flushManagedSession(ctx, nil)
	}
	return flushManagedSession(ctx, i.session)
}

func (i *managedInstanceCore) ConsoleHistory(ctx context.Context) (string, error) {
	if i == nil {
		return managedSessionConsoleHistory(ctx, nil)
	}
	return managedSessionConsoleHistory(ctx, i.session)
}

func (i *managedInstanceCore) Wait() error {
	if i == nil {
		return nil
	}
	return waitManagedSession(i.session)
}

func (i *managedInstanceCore) Close() error {
	if i == nil {
		return nil
	}
	return closeManagedSession(i.session)
}

func (i *managedInstanceCore) missingRootError() string {
	if i == nil {
		return ""
	}
	if strings.TrimSpace(i.missingRootErr) != "" {
		return i.missingRootErr
	}
	return fmt.Sprintf("running %s instance does not have a root filesystem", i.displayName())
}

func (i *managedInstanceCore) unsupported(feature string) error {
	if i == nil {
		return unsupportedManagedFeature("", guestCapabilities{}, feature)
	}
	return unsupportedManagedFeature(i.displayName(), i.caps, feature)
}

func (i *managedInstanceCore) checkControlRequest(kind string) error {
	if i == nil {
		return checkManagedControlRequest("", guestCapabilities{}, kind)
	}
	return checkManagedControlRequest(i.displayName(), i.caps, kind)
}

func (i *managedInstanceCore) displayName() string {
	if i == nil {
		return managedDisplayName("")
	}
	return managedDisplayName(i.osName)
}
