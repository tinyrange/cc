// Package helper implements the cc-helper process that runs VMs
// on behalf of libcc clients.
package helper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"sync"
	"sync/atomic"
	"time"

	cc "github.com/tinyrange/cc"
	"github.com/tinyrange/cc/internal/api"
	"github.com/tinyrange/cc/internal/ipc"
)

// Helper manages state for a single cc-helper process.
type Helper struct {
	mu sync.RWMutex

	// The main instance (one per helper process)
	instance cc.Instance
	source   cc.InstanceSource

	// Sub-resources tied to the instance
	files     map[uint64]cc.File
	cmds      map[uint64]cc.Cmd
	listeners map[uint64]net.Listener
	conns     map[uint64]net.Conn
	pipeRds   map[uint64]io.ReadCloser
	pipeWrs   map[uint64]io.WriteCloser
	snapshots map[uint64]cc.FilesystemSnapshot

	// Handle allocation
	nextHandle atomic.Uint64
}

// NewHelper creates a new helper state manager.
func NewHelper() *Helper {
	h := &Helper{
		files:     make(map[uint64]cc.File),
		cmds:      make(map[uint64]cc.Cmd),
		listeners: make(map[uint64]net.Listener),
		conns:     make(map[uint64]net.Conn),
		pipeRds:   make(map[uint64]io.ReadCloser),
		pipeWrs:   make(map[uint64]io.WriteCloser),
		snapshots: make(map[uint64]cc.FilesystemSnapshot),
	}
	h.nextHandle.Store(1)
	return h
}

func (h *Helper) newHandle() uint64 {
	return h.nextHandle.Add(1) - 1
}

// Close releases all resources.
func (h *Helper) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	var errs []error

	// Close all connections
	for _, c := range h.conns {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	h.conns = nil

	// Close all listeners
	for _, l := range h.listeners {
		if err := l.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	h.listeners = nil

	// Close all files
	for _, f := range h.files {
		if err := f.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	h.files = nil

	// Close all snapshots
	for _, s := range h.snapshots {
		if err := s.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	h.snapshots = nil

	// Close the instance
	if h.instance != nil {
		if err := h.instance.Close(); err != nil {
			errs = append(errs, err)
		}
		h.instance = nil
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// errorToIPC converts a Go error to an IPC error.
func errorToIPC(err error) *ipc.IPCError {
	if err == nil {
		return nil
	}

	code := ipc.ErrCodeUnknown
	op := ""
	path := ""

	// Check for sentinel errors
	if errors.Is(err, api.ErrHypervisorUnavailable) {
		code = ipc.ErrCodeHypervisorUnavailable
	} else if errors.Is(err, api.ErrNotRunning) {
		code = ipc.ErrCodeNotRunning
	} else if errors.Is(err, api.ErrAlreadyClosed) {
		code = ipc.ErrCodeAlreadyClosed
	} else if errors.Is(err, api.ErrTimeout) {
		code = ipc.ErrCodeTimeout
	} else if errors.Is(err, context.Canceled) {
		code = ipc.ErrCodeCancelled
	} else if errors.Is(err, context.DeadlineExceeded) {
		code = ipc.ErrCodeTimeout
	}

	// Check for api.Error
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		code = ipc.ErrCodeIO
		op = apiErr.Op
		path = apiErr.Path
	}

	return &ipc.IPCError{
		Code:    uint8(code),
		Message: err.Error(),
		Op:      op,
		Path:    path,
	}
}

// RegisterHandlers registers all IPC handlers with the mux.
func (h *Helper) RegisterHandlers(mux *ipc.Mux) {
	// Instance lifecycle
	mux.Handle(ipc.MsgInstanceNew, h.handleInstanceNew)
	mux.Handle(ipc.MsgInstanceClose, h.handleInstanceClose)
	mux.Handle(ipc.MsgInstanceWait, h.handleInstanceWait)
	mux.Handle(ipc.MsgInstanceID, h.handleInstanceID)
	mux.Handle(ipc.MsgInstanceIsRunning, h.handleInstanceIsRunning)
	mux.Handle(ipc.MsgInstanceSetConsole, h.handleInstanceSetConsole)
	mux.Handle(ipc.MsgInstanceSetNetwork, h.handleInstanceSetNetwork)
	mux.Handle(ipc.MsgInstanceExec, h.handleInstanceExec)

	// Filesystem operations
	mux.Handle(ipc.MsgFsOpen, h.handleFsOpen)
	mux.Handle(ipc.MsgFsCreate, h.handleFsCreate)
	mux.Handle(ipc.MsgFsOpenFile, h.handleFsOpenFile)
	mux.Handle(ipc.MsgFsReadFile, h.handleFsReadFile)
	mux.Handle(ipc.MsgFsWriteFile, h.handleFsWriteFile)
	mux.Handle(ipc.MsgFsStat, h.handleFsStat)
	mux.Handle(ipc.MsgFsLstat, h.handleFsLstat)
	mux.Handle(ipc.MsgFsRemove, h.handleFsRemove)
	mux.Handle(ipc.MsgFsRemoveAll, h.handleFsRemoveAll)
	mux.Handle(ipc.MsgFsMkdir, h.handleFsMkdir)
	mux.Handle(ipc.MsgFsMkdirAll, h.handleFsMkdirAll)
	mux.Handle(ipc.MsgFsRename, h.handleFsRename)
	mux.Handle(ipc.MsgFsSymlink, h.handleFsSymlink)
	mux.Handle(ipc.MsgFsReadlink, h.handleFsReadlink)
	mux.Handle(ipc.MsgFsReadDir, h.handleFsReadDir)
	mux.Handle(ipc.MsgFsChmod, h.handleFsChmod)
	mux.Handle(ipc.MsgFsChown, h.handleFsChown)
	mux.Handle(ipc.MsgFsChtimes, h.handleFsChtimes)
	mux.Handle(ipc.MsgFsSnapshot, h.handleFsSnapshot)

	// File operations
	mux.Handle(ipc.MsgFileClose, h.handleFileClose)
	mux.Handle(ipc.MsgFileRead, h.handleFileRead)
	mux.Handle(ipc.MsgFileWrite, h.handleFileWrite)
	mux.Handle(ipc.MsgFileSeek, h.handleFileSeek)
	mux.Handle(ipc.MsgFileSync, h.handleFileSync)
	mux.Handle(ipc.MsgFileTruncate, h.handleFileTruncate)
	mux.Handle(ipc.MsgFileStat, h.handleFileStat)
	mux.Handle(ipc.MsgFileName, h.handleFileName)

	// Command operations
	mux.Handle(ipc.MsgCmdNew, h.handleCmdNew)
	mux.Handle(ipc.MsgCmdEntrypoint, h.handleCmdEntrypoint)
	mux.Handle(ipc.MsgCmdFree, h.handleCmdFree)
	mux.Handle(ipc.MsgCmdSetDir, h.handleCmdSetDir)
	mux.Handle(ipc.MsgCmdSetEnv, h.handleCmdSetEnv)
	mux.Handle(ipc.MsgCmdGetEnv, h.handleCmdGetEnv)
	mux.Handle(ipc.MsgCmdEnviron, h.handleCmdEnviron)
	mux.Handle(ipc.MsgCmdStart, h.handleCmdStart)
	mux.Handle(ipc.MsgCmdWait, h.handleCmdWait)
	mux.Handle(ipc.MsgCmdRun, h.handleCmdRun)
	mux.Handle(ipc.MsgCmdOutput, h.handleCmdOutput)
	mux.Handle(ipc.MsgCmdCombinedOutput, h.handleCmdCombinedOutput)
	mux.Handle(ipc.MsgCmdExitCode, h.handleCmdExitCode)
	mux.Handle(ipc.MsgCmdKill, h.handleCmdKill)
	mux.Handle(ipc.MsgCmdStdoutPipe, h.handleCmdStdoutPipe)
	mux.Handle(ipc.MsgCmdStderrPipe, h.handleCmdStderrPipe)
	mux.Handle(ipc.MsgCmdStdinPipe, h.handleCmdStdinPipe)
	mux.HandleStreaming(ipc.MsgCmdRunStreaming, h.handleCmdRunStreaming)

	// Network operations
	mux.Handle(ipc.MsgNetListen, h.handleNetListen)
	mux.Handle(ipc.MsgListenerAccept, h.handleListenerAccept)
	mux.Handle(ipc.MsgListenerClose, h.handleListenerClose)
	mux.Handle(ipc.MsgListenerAddr, h.handleListenerAddr)
	mux.Handle(ipc.MsgConnRead, h.handleConnRead)
	mux.Handle(ipc.MsgConnWrite, h.handleConnWrite)
	mux.Handle(ipc.MsgConnClose, h.handleConnClose)
	mux.Handle(ipc.MsgConnLocalAddr, h.handleConnLocalAddr)
	mux.Handle(ipc.MsgConnRemoteAddr, h.handleConnRemoteAddr)

	// Snapshot operations
	mux.Handle(ipc.MsgSnapshotCacheKey, h.handleSnapshotCacheKey)
	mux.Handle(ipc.MsgSnapshotParent, h.handleSnapshotParent)
	mux.Handle(ipc.MsgSnapshotClose, h.handleSnapshotClose)
	mux.Handle(ipc.MsgSnapshotAsSource, h.handleSnapshotAsSource)

	// Dockerfile operations
	mux.Handle(ipc.MsgBuildDockerfile, h.handleBuildDockerfile)
}

func (h *Helper) requireInstance() error {
	if h.instance == nil {
		return &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}
	return nil
}

// ==========================================================================
// Instance lifecycle handlers
// ==========================================================================

func (h *Helper) handleInstanceNew(dec *ipc.Decoder) ([]byte, error) {
	// Decode: sourceType (0=tar, 1=dir, 2=ref), sourcePath, imageRef, cacheDir, options
	sourceType, err := dec.Uint8()
	if err != nil {
		return nil, err
	}
	sourcePath, err := dec.String()
	if err != nil {
		return nil, err
	}
	imageRef, err := dec.String()
	if err != nil {
		return nil, err
	}
	cacheDir, err := dec.String()
	if err != nil {
		return nil, err
	}
	opts, err := ipc.DecodeInstanceOptions(dec)
	if err != nil {
		return nil, err
	}

	// Create OCI client (with cache dir for ref type)
	var client cc.OCIClient
	if sourceType == 2 && cacheDir != "" {
		cache, err := cc.NewCacheDir(cacheDir)
		if err != nil {
			return nil, errorToIPC(err)
		}
		client, err = cc.NewOCIClientWithCache(cache)
		if err != nil {
			return nil, errorToIPC(err)
		}
	} else {
		client, err = cc.NewOCIClient()
		if err != nil {
			return nil, errorToIPC(err)
		}
	}

	// Load source
	var source cc.InstanceSource
	switch sourceType {
	case 0: // tar
		source, err = client.LoadFromTar(sourcePath)
	case 1: // directory
		source, err = client.LoadFromDir(sourcePath)
	case 2: // ref (pulled from registry)
		// Pull from cache - should be fast since already cached
		source, err = client.Pull(context.Background(), imageRef, cc.WithPullPolicy(cc.PullIfNotPresent))
	default:
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidArgument, Message: fmt.Sprintf("unknown source type: %d", sourceType)}
	}
	if err != nil {
		return nil, errorToIPC(err)
	}

	// Build options
	var ccOpts []cc.Option
	if opts.MemoryMB > 0 {
		ccOpts = append(ccOpts, cc.WithMemoryMB(opts.MemoryMB))
	}
	if opts.CPUs > 0 {
		ccOpts = append(ccOpts, cc.WithCPUs(opts.CPUs))
	}
	if opts.TimeoutSecs > 0 {
		ccOpts = append(ccOpts, cc.WithTimeout(time.Duration(opts.TimeoutSecs*float64(time.Second))))
	}
	if opts.User != "" {
		ccOpts = append(ccOpts, cc.WithUser(opts.User))
	}
	if opts.EnableDmesg {
		ccOpts = append(ccOpts, cc.WithDmesg())
	}
	for _, m := range opts.Mounts {
		ccOpts = append(ccOpts, cc.WithMount(cc.MountConfig{
			Tag:      m.Tag,
			HostPath: m.HostPath,
			Writable: m.Writable,
		}))
	}

	// Create instance
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.instance != nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidArgument, Message: "instance already exists"}
	}

	inst, err := cc.New(source, ccOpts...)
	if err != nil {
		return nil, errorToIPC(err)
	}

	h.instance = inst
	h.source = source

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleInstanceClose(dec *ipc.Decoder) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.instance == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	if err := h.instance.Close(); err != nil {
		return nil, errorToIPC(err)
	}
	h.instance = nil

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleInstanceWait(dec *ipc.Decoder) ([]byte, error) {
	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	if err := inst.Wait(); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleInstanceID(dec *ipc.Decoder) ([]byte, error) {
	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	return ipc.NewResponseBuilder().Success().String(inst.ID()).Build(), nil
}

func (h *Helper) handleInstanceIsRunning(dec *ipc.Decoder) ([]byte, error) {
	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return ipc.NewResponseBuilder().Success().Bool(false).Build(), nil
	}

	select {
	case <-inst.Done():
		return ipc.NewResponseBuilder().Success().Bool(false).Build(), nil
	default:
		return ipc.NewResponseBuilder().Success().Bool(true).Build(), nil
	}
}

func (h *Helper) handleInstanceSetConsole(dec *ipc.Decoder) ([]byte, error) {
	cols, err := dec.Int32()
	if err != nil {
		return nil, err
	}
	rows, err := dec.Int32()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	inst.SetConsoleSize(int(cols), int(rows))
	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleInstanceSetNetwork(dec *ipc.Decoder) ([]byte, error) {
	enabled, err := dec.Bool()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	inst.SetNetworkEnabled(enabled)
	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleInstanceExec(dec *ipc.Decoder) ([]byte, error) {
	name, err := dec.String()
	if err != nil {
		return nil, err
	}
	args, err := dec.StringSlice()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	if err := inst.Exec(name, args...); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

// ==========================================================================
// Filesystem handlers
// ==========================================================================

func (h *Helper) handleFsOpen(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if err := h.requireInstance(); err != nil {
		return nil, err
	}

	f, err := h.instance.Open(path)
	if err != nil {
		return nil, errorToIPC(err)
	}

	handle := h.newHandle()
	h.files[handle] = f

	return ipc.NewResponseBuilder().Success().Uint64(handle).Build(), nil
}

func (h *Helper) handleFsCreate(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if err := h.requireInstance(); err != nil {
		return nil, err
	}

	f, err := h.instance.Create(path)
	if err != nil {
		return nil, errorToIPC(err)
	}

	handle := h.newHandle()
	h.files[handle] = f

	return ipc.NewResponseBuilder().Success().Uint64(handle).Build(), nil
}

func (h *Helper) handleFsOpenFile(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}
	flags, err := dec.Int32()
	if err != nil {
		return nil, err
	}
	perm, err := dec.Uint32()
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if err := h.requireInstance(); err != nil {
		return nil, err
	}

	f, err := h.instance.OpenFile(path, int(flags), fs.FileMode(perm))
	if err != nil {
		return nil, errorToIPC(err)
	}

	handle := h.newHandle()
	h.files[handle] = f

	return ipc.NewResponseBuilder().Success().Uint64(handle).Build(), nil
}

func (h *Helper) handleFsReadFile(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	data, err := inst.ReadFile(path)
	if err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Bytes(data).Build(), nil
}

func (h *Helper) handleFsWriteFile(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}
	data, err := dec.Bytes()
	if err != nil {
		return nil, err
	}
	perm, err := dec.Uint32()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	if err := inst.WriteFile(path, data, fs.FileMode(perm)); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleFsStat(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	info, err := inst.Stat(path)
	if err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().FileInfo(ipc.FileInfo{
		Name:      info.Name(),
		Size:      info.Size(),
		Mode:      info.Mode(),
		ModTime:   info.ModTime().Unix(),
		IsDir:     info.IsDir(),
		IsSymlink: info.Mode()&fs.ModeSymlink != 0,
	}).Build(), nil
}

func (h *Helper) handleFsLstat(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	info, err := inst.Lstat(path)
	if err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().FileInfo(ipc.FileInfo{
		Name:      info.Name(),
		Size:      info.Size(),
		Mode:      info.Mode(),
		ModTime:   info.ModTime().Unix(),
		IsDir:     info.IsDir(),
		IsSymlink: info.Mode()&fs.ModeSymlink != 0,
	}).Build(), nil
}

func (h *Helper) handleFsRemove(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	if err := inst.Remove(path); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleFsRemoveAll(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	if err := inst.RemoveAll(path); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleFsMkdir(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}
	perm, err := dec.Uint32()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	if err := inst.Mkdir(path, fs.FileMode(perm)); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleFsMkdirAll(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}
	perm, err := dec.Uint32()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	if err := inst.MkdirAll(path, fs.FileMode(perm)); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleFsRename(dec *ipc.Decoder) ([]byte, error) {
	oldpath, err := dec.String()
	if err != nil {
		return nil, err
	}
	newpath, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	if err := inst.Rename(oldpath, newpath); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleFsSymlink(dec *ipc.Decoder) ([]byte, error) {
	oldname, err := dec.String()
	if err != nil {
		return nil, err
	}
	newname, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	if err := inst.Symlink(oldname, newname); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleFsReadlink(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	target, err := inst.Readlink(path)
	if err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().String(target).Build(), nil
}

func (h *Helper) handleFsReadDir(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	entries, err := inst.ReadDir(path)
	if err != nil {
		return nil, errorToIPC(err)
	}

	enc := ipc.NewEncoder()
	enc.Uint8(ipc.ErrCodeOK)
	enc.Uint32(uint32(len(entries)))
	for _, e := range entries {
		info, _ := e.Info()
		mode := fs.FileMode(0)
		if info != nil {
			mode = info.Mode()
		}
		ipc.EncodeDirEntry(enc, ipc.DirEntry{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Mode:  mode,
		})
	}

	return enc.Bytes(), nil
}

func (h *Helper) handleFsChmod(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}
	mode, err := dec.Uint32()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	if err := inst.Chmod(path, fs.FileMode(mode)); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleFsChown(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}
	uid, err := dec.Int32()
	if err != nil {
		return nil, err
	}
	gid, err := dec.Int32()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	if err := inst.Chown(path, int(uid), int(gid)); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleFsChtimes(dec *ipc.Decoder) ([]byte, error) {
	path, err := dec.String()
	if err != nil {
		return nil, err
	}
	atime, err := dec.Int64()
	if err != nil {
		return nil, err
	}
	mtime, err := dec.Int64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	inst := h.instance
	h.mu.RUnlock()

	if inst == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	if err := inst.Chtimes(path, time.Unix(atime, 0), time.Unix(mtime, 0)); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleFsSnapshot(dec *ipc.Decoder) ([]byte, error) {
	opts, err := ipc.DecodeSnapshotOptions(dec)
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.instance == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	var snapOpts []cc.FilesystemSnapshotOption
	if len(opts.Excludes) > 0 {
		snapOpts = append(snapOpts, cc.WithSnapshotExcludes(opts.Excludes...))
	}
	if opts.CacheDir != "" {
		snapOpts = append(snapOpts, cc.WithSnapshotCacheDir(opts.CacheDir))
	}

	snap, err := h.instance.SnapshotFilesystem(snapOpts...)
	if err != nil {
		return nil, errorToIPC(err)
	}

	handle := h.newHandle()
	h.snapshots[handle] = snap

	return ipc.NewResponseBuilder().Success().Uint64(handle).Build(), nil
}

// ==========================================================================
// File handlers
// ==========================================================================

func (h *Helper) handleFileClose(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	f, ok := h.files[handle]
	if ok {
		delete(h.files, handle)
	}
	h.mu.Unlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid file handle"}
	}

	if err := f.Close(); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleFileRead(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}
	length, err := dec.Uint32()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	f, ok := h.files[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid file handle"}
	}

	buf := make([]byte, length)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, api.ErrAlreadyClosed) {
		// Include partial data in response
		enc := ipc.NewEncoder()
		enc.Uint8(ipc.ErrCodeOK)
		enc.WriteBytes(buf[:n])
		return enc.Bytes(), nil
	}

	return ipc.NewResponseBuilder().Success().Bytes(buf[:n]).Build(), nil
}

func (h *Helper) handleFileWrite(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}
	data, err := dec.Bytes()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	f, ok := h.files[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid file handle"}
	}

	n, err := f.Write(data)
	if err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Uint32(uint32(n)).Build(), nil
}

func (h *Helper) handleFileSeek(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}
	offset, err := dec.Int64()
	if err != nil {
		return nil, err
	}
	whence, err := dec.Int32()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	f, ok := h.files[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid file handle"}
	}

	newOffset, err := f.Seek(offset, int(whence))
	if err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Int64(newOffset).Build(), nil
}

func (h *Helper) handleFileSync(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	f, ok := h.files[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid file handle"}
	}

	if err := f.Sync(); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleFileTruncate(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}
	size, err := dec.Int64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	f, ok := h.files[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid file handle"}
	}

	if err := f.Truncate(size); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleFileStat(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	f, ok := h.files[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid file handle"}
	}

	info, err := f.Stat()
	if err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().FileInfo(ipc.FileInfo{
		Name:      info.Name(),
		Size:      info.Size(),
		Mode:      info.Mode(),
		ModTime:   info.ModTime().Unix(),
		IsDir:     info.IsDir(),
		IsSymlink: info.Mode()&fs.ModeSymlink != 0,
	}).Build(), nil
}

func (h *Helper) handleFileName(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	f, ok := h.files[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid file handle"}
	}

	return ipc.NewResponseBuilder().Success().String(f.Name()).Build(), nil
}

// ==========================================================================
// Command handlers
// ==========================================================================

func (h *Helper) handleCmdNew(dec *ipc.Decoder) ([]byte, error) {
	name, err := dec.String()
	if err != nil {
		return nil, err
	}
	args, err := dec.StringSlice()
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.instance == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	cmd := h.instance.Command(name, args...)
	handle := h.newHandle()
	h.cmds[handle] = cmd

	return ipc.NewResponseBuilder().Success().Uint64(handle).Build(), nil
}

func (h *Helper) handleCmdEntrypoint(dec *ipc.Decoder) ([]byte, error) {
	args, err := dec.StringSlice()
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.instance == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	cmd := h.instance.EntrypointCommand(args...)
	handle := h.newHandle()
	h.cmds[handle] = cmd

	return ipc.NewResponseBuilder().Success().Uint64(handle).Build(), nil
}

func (h *Helper) handleCmdFree(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	_, ok := h.cmds[handle]
	if ok {
		delete(h.cmds, handle)
	}
	h.mu.Unlock()

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleCmdSetDir(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}
	dir, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	cmd, ok := h.cmds[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid cmd handle"}
	}

	cmd.SetDir(dir)
	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleCmdSetEnv(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}
	key, err := dec.String()
	if err != nil {
		return nil, err
	}
	value, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	cmd, ok := h.cmds[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid cmd handle"}
	}

	cmd.SetEnv(key, value)
	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleCmdGetEnv(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}
	key, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	cmd, ok := h.cmds[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid cmd handle"}
	}

	value := cmd.GetEnv(key)
	return ipc.NewResponseBuilder().Success().String(value).Build(), nil
}

func (h *Helper) handleCmdEnviron(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	cmd, ok := h.cmds[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid cmd handle"}
	}

	env := cmd.Environ()

	enc := ipc.NewEncoder()
	enc.Uint8(ipc.ErrCodeOK)
	enc.StringSlice(env)
	return enc.Bytes(), nil
}

func (h *Helper) handleCmdStart(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	cmd, ok := h.cmds[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid cmd handle"}
	}

	if err := cmd.Start(); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleCmdWait(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	cmd, ok := h.cmds[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid cmd handle"}
	}

	err = cmd.Wait()
	exitCode := cmd.ExitCode()

	// Non-zero exit code is not an IPC error
	if err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) && exitCode != 0 {
			// Expected - return success with exit code
			return ipc.NewResponseBuilder().Success().Int32(int32(exitCode)).Build(), nil
		}
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Int32(int32(exitCode)).Build(), nil
}

func (h *Helper) handleCmdRun(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	cmd, ok := h.cmds[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid cmd handle"}
	}

	err = cmd.Run()
	exitCode := cmd.ExitCode()

	if err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) && exitCode != 0 {
			return ipc.NewResponseBuilder().Success().Int32(int32(exitCode)).Build(), nil
		}
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Int32(int32(exitCode)).Build(), nil
}

func (h *Helper) handleCmdOutput(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	cmd, ok := h.cmds[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid cmd handle"}
	}

	output, err := cmd.Output()
	exitCode := cmd.ExitCode()

	if err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) && exitCode != 0 {
			return ipc.NewResponseBuilder().Success().Bytes(output).Int32(int32(exitCode)).Build(), nil
		}
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Bytes(output).Int32(int32(exitCode)).Build(), nil
}

func (h *Helper) handleCmdCombinedOutput(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	cmd, ok := h.cmds[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid cmd handle"}
	}

	output, err := cmd.CombinedOutput()
	exitCode := cmd.ExitCode()

	if err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) && exitCode != 0 {
			return ipc.NewResponseBuilder().Success().Bytes(output).Int32(int32(exitCode)).Build(), nil
		}
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Bytes(output).Int32(int32(exitCode)).Build(), nil
}

func (h *Helper) handleCmdExitCode(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	cmd, ok := h.cmds[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid cmd handle"}
	}

	return ipc.NewResponseBuilder().Success().Int32(int32(cmd.ExitCode())).Build(), nil
}

func (h *Helper) handleCmdKill(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	_, ok := h.cmds[handle]
	if ok {
		delete(h.cmds, handle)
	}
	h.mu.Unlock()

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleCmdStdoutPipe(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	cmd, ok := h.cmds[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid cmd handle"}
	}

	reader, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errorToIPC(err)
	}

	pipeHandle := h.nextHandle.Add(1)
	h.mu.Lock()
	h.pipeRds[pipeHandle] = reader
	h.mu.Unlock()

	return ipc.NewResponseBuilder().Success().Uint64(pipeHandle).Build(), nil
}

func (h *Helper) handleCmdStderrPipe(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	cmd, ok := h.cmds[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid cmd handle"}
	}

	reader, err := cmd.StderrPipe()
	if err != nil {
		return nil, errorToIPC(err)
	}

	pipeHandle := h.nextHandle.Add(1)
	h.mu.Lock()
	h.pipeRds[pipeHandle] = reader
	h.mu.Unlock()

	return ipc.NewResponseBuilder().Success().Uint64(pipeHandle).Build(), nil
}

func (h *Helper) handleCmdStdinPipe(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	cmd, ok := h.cmds[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid cmd handle"}
	}

	writer, err := cmd.StdinPipe()
	if err != nil {
		return nil, errorToIPC(err)
	}

	pipeHandle := h.nextHandle.Add(1)
	h.mu.Lock()
	h.pipeWrs[pipeHandle] = writer
	h.mu.Unlock()

	return ipc.NewResponseBuilder().Success().Uint64(pipeHandle).Build(), nil
}

func (h *Helper) handleCmdRunStreaming(dec *ipc.Decoder, sw *ipc.StreamWriter) error {
	handle, err := dec.Uint64()
	if err != nil {
		return err
	}

	h.mu.RLock()
	cmd, ok := h.cmds[handle]
	h.mu.RUnlock()

	if !ok {
		return &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid cmd handle"}
	}

	// Create writers that forward to StreamWriter
	stdoutWriter := &streamForwarder{sw: sw, streamType: 1}
	stderrWriter := &streamForwarder{sw: sw, streamType: 2}

	cmd.SetStdout(stdoutWriter)
	cmd.SetStderr(stderrWriter)

	runErr := cmd.Run()
	exitCode := int32(cmd.ExitCode())

	// If Run returns an error that's just a non-zero exit code, that's expected
	if runErr != nil {
		var apiErr *api.Error
		if !errors.As(runErr, &apiErr) || exitCode == 0 {
			// Real error - send via stream end
			return sw.WriteEnd(exitCode)
		}
	}

	return sw.WriteEnd(exitCode)
}

// streamForwarder is an io.Writer that sends data as stream chunks via IPC.
type streamForwarder struct {
	sw         *ipc.StreamWriter
	streamType uint8
}

func (f *streamForwarder) Write(p []byte) (int, error) {
	if err := f.sw.WriteStreamChunk(f.streamType, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// ==========================================================================
// Network handlers
// ==========================================================================

func (h *Helper) handleNetListen(dec *ipc.Decoder) ([]byte, error) {
	network, err := dec.String()
	if err != nil {
		return nil, err
	}
	address, err := dec.String()
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.instance == nil {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "no instance"}
	}

	ln, err := h.instance.Listen(network, address)
	if err != nil {
		return nil, errorToIPC(err)
	}

	handle := h.newHandle()
	h.listeners[handle] = ln

	return ipc.NewResponseBuilder().Success().Uint64(handle).Build(), nil
}

func (h *Helper) handleListenerAccept(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	ln, ok := h.listeners[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid listener handle"}
	}

	conn, err := ln.Accept()
	if err != nil {
		return nil, errorToIPC(err)
	}

	h.mu.Lock()
	connHandle := h.newHandle()
	h.conns[connHandle] = conn
	h.mu.Unlock()

	return ipc.NewResponseBuilder().Success().Uint64(connHandle).Build(), nil
}

func (h *Helper) handleListenerClose(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	ln, ok := h.listeners[handle]
	if ok {
		delete(h.listeners, handle)
	}
	h.mu.Unlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid listener handle"}
	}

	if err := ln.Close(); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleListenerAddr(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	ln, ok := h.listeners[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid listener handle"}
	}

	return ipc.NewResponseBuilder().Success().String(ln.Addr().String()).Build(), nil
}

func (h *Helper) handleConnRead(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}
	length, err := dec.Uint32()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	conn, ok := h.conns[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid conn handle"}
	}

	buf := make([]byte, length)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Bytes(buf[:n]).Build(), nil
}

func (h *Helper) handleConnWrite(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}
	data, err := dec.Bytes()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	conn, ok := h.conns[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid conn handle"}
	}

	n, err := conn.Write(data)
	if err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Uint32(uint32(n)).Build(), nil
}

func (h *Helper) handleConnClose(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	conn, ok := h.conns[handle]
	if ok {
		delete(h.conns, handle)
	}
	h.mu.Unlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid conn handle"}
	}

	if err := conn.Close(); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleConnLocalAddr(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	conn, ok := h.conns[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid conn handle"}
	}

	return ipc.NewResponseBuilder().Success().String(conn.LocalAddr().String()).Build(), nil
}

func (h *Helper) handleConnRemoteAddr(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	conn, ok := h.conns[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid conn handle"}
	}

	return ipc.NewResponseBuilder().Success().String(conn.RemoteAddr().String()).Build(), nil
}

// ==========================================================================
// Snapshot handlers
// ==========================================================================

func (h *Helper) handleSnapshotCacheKey(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	snap, ok := h.snapshots[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid snapshot handle"}
	}

	return ipc.NewResponseBuilder().Success().String(snap.CacheKey()).Build(), nil
}

func (h *Helper) handleSnapshotParent(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	snap, ok := h.snapshots[handle]
	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid snapshot handle"}
	}

	parent := snap.Parent()
	if parent == nil {
		return ipc.NewResponseBuilder().Success().Uint64(0).Build(), nil
	}

	parentHandle := h.newHandle()
	h.snapshots[parentHandle] = parent

	return ipc.NewResponseBuilder().Success().Uint64(parentHandle).Build(), nil
}

func (h *Helper) handleSnapshotClose(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	snap, ok := h.snapshots[handle]
	if ok {
		delete(h.snapshots, handle)
	}
	h.mu.Unlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid snapshot handle"}
	}

	if err := snap.Close(); err != nil {
		return nil, errorToIPC(err)
	}

	return ipc.NewResponseBuilder().Success().Build(), nil
}

func (h *Helper) handleSnapshotAsSource(dec *ipc.Decoder) ([]byte, error) {
	handle, err := dec.Uint64()
	if err != nil {
		return nil, err
	}

	h.mu.RLock()
	snap, ok := h.snapshots[handle]
	h.mu.RUnlock()

	if !ok {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidHandle, Message: "invalid snapshot handle"}
	}

	// FilesystemSnapshot implements InstanceSource, so we just need to
	// return a reference to it. The snapshot handle can be used as a source.
	_ = snap
	return ipc.NewResponseBuilder().Success().Uint64(handle).Build(), nil
}

// ==========================================================================
// Dockerfile handlers
// ==========================================================================

func (h *Helper) handleBuildDockerfile(dec *ipc.Decoder) ([]byte, error) {
	opts, err := ipc.DecodeDockerfileOptions(dec)
	if err != nil {
		return nil, err
	}

	if opts.CacheDir == "" {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidArgument, Message: "cache_dir is required"}
	}

	if len(opts.Dockerfile) == 0 {
		return nil, &ipc.IPCError{Code: ipc.ErrCodeInvalidArgument, Message: "dockerfile is empty"}
	}

	// Create OCI client with cache dir
	cache, err := cc.NewCacheDir(opts.CacheDir)
	if err != nil {
		return nil, errorToIPC(err)
	}
	client, err := cc.NewOCIClientWithCache(cache)
	if err != nil {
		return nil, errorToIPC(err)
	}

	// Build DockerfileOption slice
	var dockerfileOpts []cc.DockerfileOption
	dockerfileOpts = append(dockerfileOpts, cc.WithDockerfileCacheDir(opts.CacheDir))

	if opts.ContextDir != "" {
		dockerfileOpts = append(dockerfileOpts, cc.WithBuildContextDir(opts.ContextDir))
	}

	for k, v := range opts.BuildArgs {
		dockerfileOpts = append(dockerfileOpts, cc.WithBuildArg(k, v))
	}

	// Build the Dockerfile
	snap, err := cc.BuildDockerfileSource(context.Background(), opts.Dockerfile, client, dockerfileOpts...)
	if err != nil {
		return nil, errorToIPC(err)
	}

	// Store the snapshot and return handle
	h.mu.Lock()
	handle := h.newHandle()
	h.snapshots[handle] = snap
	h.mu.Unlock()

	return ipc.NewResponseBuilder().Success().Uint64(handle).Build(), nil
}
