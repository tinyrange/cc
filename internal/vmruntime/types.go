package vmruntime

import (
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
)

const (
	RootFSTag   = "rootfs"
	EmulatorTag = "ccx3"
	GuestCID    = 3
	ControlPort = 10777
)

// DirectoryShare describes a host directory exposed inside the guest.
type DirectoryShare struct {
	Source   string
	Mount    string
	Writable bool
}

// RunRequest is the backend-neutral request shape for the managed guest runtime.
type RunRequest struct {
	Kernel            []byte
	Init              []byte
	AMD64EmulatorPath string
	Modules           []alpine.Module
	Image             *oci.Image
	RootFS            virtio.FSBackend
	Shares            []DirectoryShare
	Command           []string
	Env               []string
	WorkDir           string
	User              string
	MemoryMB          uint64
	CPUs              int
	Dmesg             bool
	Persistent        bool
	Network           *GuestNetworkConfig
	NetDevice         *virtio.Net
	UnixTime          int64
}

// RunResult is the backend-neutral result shape for one-shot guest execution.
type RunResult struct {
	ExitCode   int
	Output     string
	Transcript string
}
