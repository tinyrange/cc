package arm64vm

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/fdt"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

const (
	InstanceReadyMarker   = vmruntime.InstanceReadyMarker
	InitDurationMarker    = vmruntime.InitDurationMarker
	ExecTimingMarker      = vmruntime.ExecTimingMarker
	CommandBeginMarker    = vmruntime.CommandBeginMarker
	CommandOutputMarker   = vmruntime.CommandOutputMarker
	CommandErrorMarker    = vmruntime.CommandErrorMarker
	CommandExitMarkerPref = vmruntime.CommandExitMarkerPref
)

type SerialTranscript = vmruntime.SerialTranscript
type BootEventWriter = vmruntime.BootEventWriter

func NewSerialTranscript() *SerialTranscript { return vmruntime.NewSerialTranscript() }
func NewBootEventWriter(callback func(client.BootEvent) error) *BootEventWriter {
	return vmruntime.NewBootEventWriter(callback)
}
func HasFatalBootText(text string) bool               { return vmruntime.HasFatalBootText(text) }
func ParseInitDurationMarker(text string) (int, bool) { return vmruntime.ParseInitDurationMarker(text) }

func BuildPersistentInitramfs(req RunRequest, baseEnv []string, workDir string) ([]byte, error) {
	return BuildInitramfs(req.Init, req.Modules, GuestInitConfig{
		Env:              append([]string(nil), baseEnv...),
		WorkDir:          workDir,
		Modules:          ModulePaths(req.Modules),
		EmulatorTag:      EmulatorTagForPath(req.AMD64EmulatorPath),
		RootFSTag:        RootFSTag,
		Shares:           GuestShareConfigs(req.Shares),
		VsockPort:        ControlPort,
		ReadyMarker:      InstanceReadyMarker,
		BeginMarker:      CommandBeginMarker,
		OutputMarkerPref: CommandOutputMarker,
		ErrorMarkerPref:  CommandErrorMarker,
		UsageMarkerPref:  vmruntime.CommandUsageMarker,
		ExitMarkerPrefix: CommandExitMarkerPref,
		PrecopyAMD64Root: strings.TrimSpace(os.Getenv("CCX3_BENCH_PRECOPY_AMD64_ROOT")) != "",
		Network:          req.Network,
		UnixTime:         req.UnixTime,
	})
}

func BuildExecInitramfs(req RunRequest, command []string, env []string, workDir string) ([]byte, error) {
	return BuildInitramfs(req.Init, req.Modules, GuestInitConfig{
		Command:          append([]string(nil), command...),
		Env:              append([]string(nil), env...),
		WorkDir:          workDir,
		User:             req.User,
		Modules:          ModulePaths(req.Modules),
		EmulatorTag:      EmulatorTagForPath(req.AMD64EmulatorPath),
		RootFSTag:        RootFSTag,
		BeginMarker:      CommandBeginMarker,
		ErrorMarkerPref:  CommandErrorMarker,
		UsageMarkerPref:  vmruntime.CommandUsageMarker,
		ExitMarkerPrefix: CommandExitMarkerPref,
		UnixTime:         req.UnixTime,
	})
}

func BuildFSDevices(req RunRequest, trace io.Writer) ([]*virtio.FS, virtio.ShareMounter, error) {
	rootFSBackend := req.RootFS
	if rootFSBackend == nil {
		if req.Image == nil {
			return nil, nil, fmt.Errorf("image or rootfs backend is required")
		}
		rootFSBackend = virtio.NewImageFS(req.Image.RootFS, req.Image.RootFSDir)
	}
	shares := make([]virtio.ShareMount, 0, len(req.Shares))
	for i, share := range req.Shares {
		mount, err := BuildShareMount(i, share)
		if err != nil {
			return nil, nil, err
		}
		shares = append(shares, mount)
	}
	rootFSBackend = virtio.NewMountedFS(rootFSBackend, shares)
	rootFS, _ := rootFSBackend.(virtio.ShareMounter)
	devs := []*virtio.FS{
		newFSDevice(RootFSBase, RootFSIRQ, RootFSTag, rootFSBackend, trace),
	}
	if strings.TrimSpace(req.AMD64EmulatorPath) != "" {
		sourceDir := filepath.Dir(req.AMD64EmulatorPath)
		devs = append(devs, newFSDevice(ShareFSBase, ShareFSIRQ, EmulatorTag, virtio.NewImageFS(imagefs.NewHostFS(sourceDir, nil), sourceDir), trace))
	}
	return devs, rootFS, nil
}

func BuildShareMount(index int, share DirectoryShare) (virtio.ShareMount, error) {
	_, backend, err := buildShareBackend(index, share)
	if err != nil {
		return virtio.ShareMount{}, err
	}
	return virtio.ShareMount{
		GuestPath: share.Mount,
		Backend:   backend,
		Writable:  share.Writable,
		CacheMode: share.Cache,
	}, nil
}

func AppendFSNodes(nodes []fdt.Node, fsdevs []*virtio.FS) []fdt.Node {
	out := append([]fdt.Node(nil), nodes...)
	for _, fsdev := range fsdevs {
		out = append(out, fsdev.DeviceTreeNode())
	}
	return out
}

func newFSDevice(base uint64, irq uint32, tag string, backend virtio.FSBackend, trace io.Writer) *virtio.FS {
	fsdev := virtio.NewFS(base, RootFSSize, irq, tag, backend)
	if trace == nil && strings.TrimSpace(os.Getenv("CCX3_DEBUG_VIRTIOFS")) != "" {
		trace = os.Stderr
	}
	fsdev.Log = trace
	fsdev.Strict = true
	return fsdev
}

func buildShareBackend(index int, share DirectoryShare) (string, virtio.FSBackend, error) {
	source := strings.TrimSpace(share.Source)
	if source == "" {
		return "", nil, fmt.Errorf("share %d: source is required", index)
	}
	mount := strings.TrimSpace(share.Mount)
	if mount == "" || !strings.HasPrefix(mount, "/") {
		return "", nil, fmt.Errorf("share %d: mount must be an absolute guest path", index)
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", nil, fmt.Errorf("share %d: stat source: %w", index, err)
	}
	if !info.IsDir() {
		return "", nil, fmt.Errorf("share %d: source must be a directory", index)
	}
	tag := fmt.Sprintf("share%d", index)
	if share.Writable {
		if share.MapOwner {
			return tag, virtio.NewPassthroughFSWithOwner(source, nil, share.OwnerUID, share.OwnerGID), nil
		}
		return tag, virtio.NewPassthroughFS(source, nil), nil
	}
	if share.MapOwner {
		return tag, virtio.NewImageFSWithOwner(imagefs.NewHostFS(source, nil), source, share.OwnerUID, share.OwnerGID), nil
	}
	return tag, virtio.NewImageFS(imagefs.NewHostFS(source, nil), source), nil
}
