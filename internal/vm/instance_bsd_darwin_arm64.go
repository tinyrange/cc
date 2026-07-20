//go:build darwin && arm64

package vm

import (
	"context"
	"fmt"
	"sync"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/hv/hvf"
	"j5.nz/cc/internal/imagefs"
	hostmanaged "j5.nz/cc/internal/vm/host/managed"
)

type darwinBSDInstance struct {
	*managedInstanceCore
	osName       string
	session      *hvf.ManagedSession
	closeRuntime func() error
	root         imagefs.Directory
	network      *darwinNetworkRuntime
	caps         guestCapabilities
	netOnce      sync.Once
}

type darwinBSDInstanceConfig struct {
	OSName       string
	Session      *hvf.ManagedSession
	CloseRuntime func() error
	Root         imagefs.Directory
	BaseEnv      []string
	WorkDir      string
	Network      *darwinNetworkRuntime
	Capabilities guestCapabilities
	Env          func(base, overrides []string, replace bool) []string
}

func newDarwinBSDInstance(cfg darwinBSDInstanceConfig) *darwinBSDInstance {
	inst := &darwinBSDInstance{
		osName:       cfg.OSName,
		session:      cfg.Session,
		closeRuntime: cfg.CloseRuntime,
		root:         cfg.Root,
		network:      cfg.Network,
		caps:         cfg.Capabilities,
	}
	inst.managedInstanceCore = hostmanaged.NewCore(hostmanaged.Config{
		OSName:       cfg.OSName,
		Session:      cfg.Session,
		Root:         cfg.Root,
		BaseEnv:      cfg.BaseEnv,
		WorkDir:      cfg.WorkDir,
		Capabilities: cfg.Capabilities,
		Env:          cfg.Env,
	})
	return inst
}

func (i *darwinBSDInstance) ManagedCapabilities() guestCapabilities {
	if i == nil {
		return guestCapabilities{}
	}
	return i.caps
}

func (i *darwinBSDInstance) AddPortForward(ctx context.Context, forward client.PortForward) error {
	if i == nil || i.network == nil {
		return fmt.Errorf("network is not enabled")
	}
	return i.network.AddPortForward(forward)
}

func (i *darwinBSDInstance) Wait() error {
	if i == nil {
		return nil
	}
	defer i.closeNetwork()
	return hostmanaged.WaitSession(i.session)
}

func (i *darwinBSDInstance) Close() error {
	if i == nil {
		return nil
	}
	return hostmanaged.CloseSession(i.session, func() error {
		i.closeNetwork()
		return nil
	}, i.closeRuntime)
}

func (i *darwinBSDInstance) NetworkIPv4() string {
	if i == nil {
		return ""
	}
	return darwinNetworkGuestAddress(i.network)
}

func (i *darwinBSDInstance) RootSnapshot() (imagefs.Directory, error) {
	return i.RootSnapshotContext(context.Background())
}

func (i *darwinBSDInstance) RootSnapshotContext(ctx context.Context) (imagefs.Directory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if i == nil {
		return nil, fmt.Errorf("instance is not running")
	}
	if !i.caps.RootSnapshot {
		return nil, i.managedInstanceCore.Unsupported("root snapshots")
	}
	if i.root == nil {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	return i.root, nil
}

func (i *darwinBSDInstance) SnapshotImage(imageName string) (imagefs.Directory, error) {
	return i.SnapshotImageContext(context.Background(), imageName)
}

func (i *darwinBSDInstance) SnapshotImageContext(ctx context.Context, imageName string) (imagefs.Directory, error) {
	return i.RootSnapshotContext(ctx)
}

func (i *darwinBSDInstance) closeNetwork() {
	if i == nil {
		return
	}
	i.netOnce.Do(func() {
		if i.network != nil {
			_ = i.network.Close()
			i.network = nil
		}
	})
}
