package api

import (
	"context"
	"fmt"
	"time"

	"github.com/tinyrange/cc/internal/ipc"
)

// Helper manages a connection to a cc-helper process.
type Helper interface {
	// Pull pulls an OCI image and returns a virtual InstanceSource.
	Pull(ctx context.Context, ref string, opts ...OCIPullOption) (InstanceSource, error)

	// New creates and starts a new Instance from the given source.
	New(source InstanceSource, opts ...Option) (Instance, error)

	// Close shuts down the helper connection and process.
	Close() error
}

// helperImpl implements Helper using an IPC client connection to cc-helper.
type helperImpl struct {
	client    *ipc.Client
	ociClient OCIClient
}

// SpawnHelper starts a new cc-helper process and returns a Helper
// for creating and managing VM instances through it.
func SpawnHelper() (Helper, error) {
	client, err := ipc.SpawnHelper("")
	if err != nil {
		return nil, fmt.Errorf("spawn helper: %w", err)
	}

	ociClient, err := NewOCIClient()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("create OCI client: %w", err)
	}

	return &helperImpl{
		client:    client,
		ociClient: ociClient,
	}, nil
}

// Pull pulls an OCI image using the local OCI client and returns a
// virtualSource that can be passed to New.
func (h *helperImpl) Pull(ctx context.Context, ref string, opts ...OCIPullOption) (InstanceSource, error) {
	// Pull the image locally so it's in the shared cache.
	_, err := h.ociClient.Pull(ctx, ref, opts...)
	if err != nil {
		return nil, err
	}

	return &virtualSource{
		sourceType: 2, // ref
		imageRef:   ref,
		cacheDir:   h.ociClient.CacheDir(),
	}, nil
}

// New creates and starts a new Instance from the given source via IPC.
func (h *helperImpl) New(source InstanceSource, opts ...Option) (Instance, error) {
	var sourceType uint8
	var sourcePath, imageRef, cacheDir string

	switch s := source.(type) {
	case *virtualSource:
		sourceType = s.sourceType
		sourcePath = s.sourcePath
		imageRef = s.imageRef
		cacheDir = s.cacheDir
	default:
		return nil, fmt.Errorf("unsupported source type %T for helper", source)
	}

	// Convert public options to IPC options.
	ipcOpts := optsToIPC(opts)

	enc := ipc.NewEncoder()
	enc.Uint8(sourceType)
	enc.String(sourcePath)
	enc.String(imageRef)
	enc.String(cacheDir)
	ipc.EncodeInstanceOptions(enc, ipcOpts)

	resp, err := h.client.Call(ipc.MsgInstanceNew, enc.Bytes())
	if err != nil {
		return nil, fmt.Errorf("instance new: %w", err)
	}

	dec, err := decodeError(resp)
	if err != nil {
		return nil, err
	}
	_ = dec // no further data in response

	return newInstanceIPCProxy(h.client), nil
}

// Close shuts down the helper connection and process.
func (h *helperImpl) Close() error {
	return h.client.Close()
}

// optsToIPC converts public Option values to ipc.InstanceOptions.
func optsToIPC(opts []Option) ipc.InstanceOptions {
	var out ipc.InstanceOptions
	for _, opt := range opts {
		switch o := opt.(type) {
		case interface{ SizeMB() uint64 }:
			out.MemoryMB = o.SizeMB()
		case interface{ CPUs() int }:
			out.CPUs = o.CPUs()
		case interface{ Duration() time.Duration }:
			out.TimeoutSecs = o.Duration().Seconds()
		case interface{ User() string }:
			out.User = o.User()
		case interface{ Dmesg() bool }:
			out.EnableDmesg = o.Dmesg()
		case interface {
			Mount() struct {
				Tag      string
				HostPath string
				Writable bool
			}
		}:
			m := o.Mount()
			out.Mounts = append(out.Mounts, ipc.MountConfig{
				Tag:      m.Tag,
				HostPath: m.HostPath,
				Writable: m.Writable,
			})
		}
	}
	return out
}
