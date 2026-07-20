package vm

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/nfs"
	"j5.nz/cc/internal/virtio"
)

type bsdNFSInstance struct {
	Instance
	osName  string
	nfs     *nfs.Server
	mu      sync.Mutex
	mounted map[string]client.ShareMount
}

func startBSDNFSServer(network nfs.Network) (*nfs.Server, error) {
	nfsServer := nfs.New(network)
	if err := nfsServer.Start(); err != nil {
		return nil, err
	}
	return nfsServer, nil
}

func closeBSDNFSRuntime(nfsServer *nfs.Server, closeArtifact func() error) func() error {
	return func() error {
		var nfsErr error
		if nfsServer != nil {
			nfsErr = nfsServer.Close()
		}
		var artifactErr error
		if closeArtifact != nil {
			artifactErr = closeArtifact()
		}
		if nfsErr != nil {
			return nfsErr
		}
		return artifactErr
	}
}

func wrapBSDNFSInstance(ctx context.Context, osName string, base Instance, nfsServer *nfs.Server, shares []client.ShareMount) (Instance, error) {
	inst := &bsdNFSInstance{
		Instance: base,
		osName:   osName,
		nfs:      nfsServer,
	}
	for _, share := range shares {
		if err := inst.AddShare(ctx, share); err != nil {
			_ = inst.Close()
			return nil, err
		}
	}
	return inst, nil
}

func (i *bsdNFSInstance) AddShare(ctx context.Context, share client.ShareMount) error {
	if i == nil || i.nfs == nil {
		if i != nil && i.Instance != nil {
			return i.Instance.AddShare(ctx, share)
		}
		return fmt.Errorf("BSD NFS share support is not configured")
	}
	key := path.Clean(share.Mount)
	if key == "." {
		key = "/"
	}
	i.mu.Lock()
	if existing, ok := i.mounted[key]; ok {
		i.mu.Unlock()
		if existing == share {
			return nil
		}
	} else {
		i.mu.Unlock()
	}
	exp, err := i.nfs.AddShare(share)
	if err != nil {
		return err
	}
	if err := nfs.MountShare(ctx, i.osName, i.execStreamResponse, exp); err != nil {
		return err
	}
	i.mu.Lock()
	if i.mounted == nil {
		i.mounted = map[string]client.ShareMount{}
	}
	i.mounted[key] = share
	i.mu.Unlock()
	return nil
}

func (i *bsdNFSInstance) Flush(ctx context.Context) error {
	flusher, ok := i.Instance.(instanceFlushProvider)
	if !ok {
		return fmt.Errorf("root filesystem cannot be flushed")
	}
	return flusher.Flush(ctx)
}

func (i *bsdNFSInstance) ConsoleHistory(ctx context.Context) (string, error) {
	provider, ok := i.Instance.(consoleHistoryProvider)
	if !ok {
		return "", fmt.Errorf("console history is not available")
	}
	return provider.ConsoleHistory(ctx)
}

func (i *bsdNFSInstance) RootSnapshot() (imagefs.Directory, error) {
	snapshotter, ok := i.Instance.(rootSnapshotProvider)
	if !ok {
		return nil, fmt.Errorf("root filesystem cannot be snapshotted")
	}
	return snapshotter.RootSnapshot()
}

func (i *bsdNFSInstance) RootSnapshotContext(ctx context.Context) (imagefs.Directory, error) {
	snapshotter, ok := i.Instance.(rootSnapshotContextProvider)
	if !ok {
		return nil, fmt.Errorf("root filesystem does not support cancelable snapshots")
	}
	return snapshotter.RootSnapshotContext(ctx)
}

func (i *bsdNFSInstance) SnapshotImage(imageName string) (imagefs.Directory, error) {
	snapshotter, ok := i.Instance.(imageSnapshotProvider)
	if !ok {
		return nil, fmt.Errorf("image %q cannot be snapshotted", imageName)
	}
	return snapshotter.SnapshotImage(imageName)
}

func (i *bsdNFSInstance) SnapshotImageContext(ctx context.Context, imageName string) (imagefs.Directory, error) {
	snapshotter, ok := i.Instance.(imageSnapshotContextProvider)
	if !ok {
		return nil, fmt.Errorf("image %q does not support cancelable snapshots", imageName)
	}
	return snapshotter.SnapshotImageContext(ctx, imageName)
}

func (i *bsdNFSInstance) NetworkIPv4() string {
	provider, ok := i.Instance.(networkIPv4Provider)
	if !ok {
		return ""
	}
	return provider.NetworkIPv4()
}

func (i *bsdNFSInstance) VirtioFSStats() []virtio.FSStats {
	provider, ok := i.Instance.(virtioFSStatsProvider)
	if !ok {
		return nil
	}
	return provider.VirtioFSStats()
}

func (i *bsdNFSInstance) AllowServiceProxyPort(ctx context.Context, port int) error {
	allower, ok := i.Instance.(serviceProxyPortAllower)
	if !ok {
		return fmt.Errorf("network does not support service proxy port updates")
	}
	return allower.AllowServiceProxyPort(ctx, port)
}

func (i *bsdNFSInstance) execStreamResponse(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	if i == nil || i.Instance == nil {
		return client.ExecResponse{}, fmt.Errorf("instance is not running")
	}
	var output strings.Builder
	exitCode := 0
	exitSeen := false
	err := i.ExecStream(ctx, req, nil, func(event client.ExecEvent) error {
		switch event.Kind {
		case "stdout", "stderr", "output":
			output.WriteString(event.Output)
		case "error":
			if event.Error != "" {
				output.WriteString(event.Error)
			} else {
				output.WriteString(event.Output)
			}
		case "exit":
			exitCode = event.ExitCode
			exitSeen = true
		}
		return nil
	})
	if err != nil {
		return client.ExecResponse{}, err
	}
	if !exitSeen {
		return client.ExecResponse{}, fmt.Errorf("exec stream did not report exit")
	}
	return client.ExecResponse{ExitCode: exitCode, Output: output.String()}, nil
}
