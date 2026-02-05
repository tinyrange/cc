package main

import (
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	cc "github.com/tinyrange/cc"
	"github.com/tinyrange/cc/bindings/c/ipc"
)

// instanceProxy wraps either a direct cc.Instance or an IPC client connection.
// This allows transparent switching between direct execution (non-macOS) and
// helper process execution (macOS) based on the runtime configuration.
type instanceProxy struct {
	// Direct mode: use these
	direct cc.Instance

	// IPC mode: use these
	client *ipc.Client

	// Common
	useIPC bool
}

// helperInfo stores information needed to spawn a helper.
type helperInfo struct {
	sourceType uint8 // 0=tar, 1=dir, 2=ref (pulled from registry)
	sourcePath string
	imageRef   string // for sourceType=2: the image reference
	cacheDir   string // for sourceType=2: the cache directory
	opts       ipc.InstanceOptions
}

// libPath is the cached path to the library.
var (
	libPathOnce sync.Once
	libPath     string
)

// getLibPath returns the path to the library for helper discovery.
func getLibPath() string {
	libPathOnce.Do(func() {
		// 1. Check LIBCC_PATH environment variable (set by Python/other bindings)
		if path := os.Getenv("LIBCC_PATH"); path != "" {
			libPath = path
			return
		}

		// 2. Fall back to executable path (for statically linked binaries)
		exe, err := os.Executable()
		if err == nil {
			libPath = filepath.Dir(exe)
		}
	})
	return libPath
}

// newInstanceProxyDirect creates a direct-mode proxy.
func newInstanceProxyDirect(inst cc.Instance) *instanceProxy {
	return &instanceProxy{
		direct: inst,
		useIPC: false,
	}
}

// newInstanceProxyIPC creates an IPC-mode proxy by spawning a helper.
func newInstanceProxyIPC(info helperInfo) (*instanceProxy, error) {
	client, err := ipc.SpawnHelper(getLibPath())
	if err != nil {
		return nil, err
	}

	// Send instance creation request
	enc := ipc.NewEncoder()
	enc.Uint8(info.sourceType)
	enc.String(info.sourcePath)
	enc.String(info.imageRef)
	enc.String(info.cacheDir)
	ipc.EncodeInstanceOptions(enc, info.opts)

	resp, err := client.Call(ipc.MsgInstanceNew, enc.Bytes())
	if err != nil {
		client.Close()
		return nil, err
	}

	// Check response
	dec := ipc.NewDecoder(resp)
	if ipcErr, err := ipc.DecodeError(dec); err != nil {
		client.Close()
		return nil, err
	} else if ipcErr != nil {
		client.Close()
		return nil, ipcErr
	}

	return &instanceProxy{
		client: client,
		useIPC: true,
	}, nil
}

func (p *instanceProxy) Close() error {
	if p.useIPC {
		if p.client == nil {
			return nil
		}
		_, err := p.client.Call(ipc.MsgInstanceClose, nil)
		p.client.Close()
		p.client = nil
		return err
	}
	if p.direct == nil {
		return nil
	}
	return p.direct.Close()
}

func (p *instanceProxy) Wait() error {
	if p.useIPC {
		_, err := p.client.Call(ipc.MsgInstanceWait, nil)
		return err
	}
	return p.direct.Wait()
}

func (p *instanceProxy) ID() string {
	if p.useIPC {
		resp, err := p.client.Call(ipc.MsgInstanceID, nil)
		if err != nil {
			return ""
		}
		dec := ipc.NewDecoder(resp)
		if _, err := ipc.DecodeError(dec); err != nil {
			return ""
		}
		id, _ := dec.String()
		return id
	}
	return p.direct.ID()
}

func (p *instanceProxy) IsRunning() bool {
	if p.useIPC {
		resp, err := p.client.Call(ipc.MsgInstanceIsRunning, nil)
		if err != nil {
			return false
		}
		dec := ipc.NewDecoder(resp)
		if _, err := ipc.DecodeError(dec); err != nil {
			return false
		}
		running, _ := dec.Bool()
		return running
	}
	select {
	case <-p.direct.Done():
		return false
	default:
		return true
	}
}

func (p *instanceProxy) SetConsoleSize(cols, rows int) {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.Int32(int32(cols))
		enc.Int32(int32(rows))
		p.client.Call(ipc.MsgInstanceSetConsole, enc.Bytes())
		return
	}
	p.direct.SetConsoleSize(cols, rows)
}

func (p *instanceProxy) SetNetworkEnabled(enabled bool) {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.Bool(enabled)
		p.client.Call(ipc.MsgInstanceSetNetwork, enc.Bytes())
		return
	}
	p.direct.SetNetworkEnabled(enabled)
}

func (p *instanceProxy) Exec(name string, args ...string) error {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(name)
		enc.StringSlice(args)
		_, err := p.client.Call(ipc.MsgInstanceExec, enc.Bytes())
		return err
	}
	return p.direct.Exec(name, args...)
}

// ==========================================================================
// Filesystem operations
// ==========================================================================

func (p *instanceProxy) Open(path string) (cc.File, error) {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		resp, err := p.client.Call(ipc.MsgFsOpen, enc.Bytes())
		if err != nil {
			return nil, err
		}
		dec := ipc.NewDecoder(resp)
		if ipcErr, err := ipc.DecodeError(dec); err != nil {
			return nil, err
		} else if ipcErr != nil {
			return nil, ipcErr
		}
		handle, _ := dec.Uint64()
		return &fileProxy{client: p.client, handle: handle, path: path}, nil
	}
	return p.direct.Open(path)
}

func (p *instanceProxy) Create(path string) (cc.File, error) {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		resp, err := p.client.Call(ipc.MsgFsCreate, enc.Bytes())
		if err != nil {
			return nil, err
		}
		dec := ipc.NewDecoder(resp)
		if ipcErr, err := ipc.DecodeError(dec); err != nil {
			return nil, err
		} else if ipcErr != nil {
			return nil, ipcErr
		}
		handle, _ := dec.Uint64()
		return &fileProxy{client: p.client, handle: handle, path: path}, nil
	}
	return p.direct.Create(path)
}

func (p *instanceProxy) OpenFile(path string, flags int, perm fs.FileMode) (cc.File, error) {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		enc.Int32(int32(flags))
		enc.Uint32(uint32(perm))
		resp, err := p.client.Call(ipc.MsgFsOpenFile, enc.Bytes())
		if err != nil {
			return nil, err
		}
		dec := ipc.NewDecoder(resp)
		if ipcErr, err := ipc.DecodeError(dec); err != nil {
			return nil, err
		} else if ipcErr != nil {
			return nil, ipcErr
		}
		handle, _ := dec.Uint64()
		return &fileProxy{client: p.client, handle: handle, path: path}, nil
	}
	return p.direct.OpenFile(path, flags, perm)
}

func (p *instanceProxy) ReadFile(path string) ([]byte, error) {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		resp, err := p.client.Call(ipc.MsgFsReadFile, enc.Bytes())
		if err != nil {
			return nil, err
		}
		dec := ipc.NewDecoder(resp)
		if ipcErr, err := ipc.DecodeError(dec); err != nil {
			return nil, err
		} else if ipcErr != nil {
			return nil, ipcErr
		}
		return dec.Bytes()
	}
	return p.direct.ReadFile(path)
}

func (p *instanceProxy) WriteFile(path string, data []byte, perm fs.FileMode) error {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		enc.WriteBytes(data)
		enc.Uint32(uint32(perm))
		_, err := p.client.Call(ipc.MsgFsWriteFile, enc.Bytes())
		return err
	}
	return p.direct.WriteFile(path, data, perm)
}

func (p *instanceProxy) Stat(path string) (fs.FileInfo, error) {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		resp, err := p.client.Call(ipc.MsgFsStat, enc.Bytes())
		if err != nil {
			return nil, err
		}
		dec := ipc.NewDecoder(resp)
		if ipcErr, err := ipc.DecodeError(dec); err != nil {
			return nil, err
		} else if ipcErr != nil {
			return nil, ipcErr
		}
		fi, err := ipc.DecodeFileInfo(dec)
		if err != nil {
			return nil, err
		}
		return &proxyFileInfo{fi}, nil
	}
	return p.direct.Stat(path)
}

func (p *instanceProxy) Lstat(path string) (fs.FileInfo, error) {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		resp, err := p.client.Call(ipc.MsgFsLstat, enc.Bytes())
		if err != nil {
			return nil, err
		}
		dec := ipc.NewDecoder(resp)
		if ipcErr, err := ipc.DecodeError(dec); err != nil {
			return nil, err
		} else if ipcErr != nil {
			return nil, ipcErr
		}
		fi, err := ipc.DecodeFileInfo(dec)
		if err != nil {
			return nil, err
		}
		return &proxyFileInfo{fi}, nil
	}
	return p.direct.Lstat(path)
}

func (p *instanceProxy) Remove(path string) error {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		_, err := p.client.Call(ipc.MsgFsRemove, enc.Bytes())
		return err
	}
	return p.direct.Remove(path)
}

func (p *instanceProxy) RemoveAll(path string) error {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		_, err := p.client.Call(ipc.MsgFsRemoveAll, enc.Bytes())
		return err
	}
	return p.direct.RemoveAll(path)
}

func (p *instanceProxy) Mkdir(path string, perm fs.FileMode) error {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		enc.Uint32(uint32(perm))
		_, err := p.client.Call(ipc.MsgFsMkdir, enc.Bytes())
		return err
	}
	return p.direct.Mkdir(path, perm)
}

func (p *instanceProxy) MkdirAll(path string, perm fs.FileMode) error {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		enc.Uint32(uint32(perm))
		_, err := p.client.Call(ipc.MsgFsMkdirAll, enc.Bytes())
		return err
	}
	return p.direct.MkdirAll(path, perm)
}

func (p *instanceProxy) Rename(oldpath, newpath string) error {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(oldpath)
		enc.String(newpath)
		_, err := p.client.Call(ipc.MsgFsRename, enc.Bytes())
		return err
	}
	return p.direct.Rename(oldpath, newpath)
}

func (p *instanceProxy) Symlink(oldname, newname string) error {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(oldname)
		enc.String(newname)
		_, err := p.client.Call(ipc.MsgFsSymlink, enc.Bytes())
		return err
	}
	return p.direct.Symlink(oldname, newname)
}

func (p *instanceProxy) Readlink(path string) (string, error) {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		resp, err := p.client.Call(ipc.MsgFsReadlink, enc.Bytes())
		if err != nil {
			return "", err
		}
		dec := ipc.NewDecoder(resp)
		if ipcErr, err := ipc.DecodeError(dec); err != nil {
			return "", err
		} else if ipcErr != nil {
			return "", ipcErr
		}
		return dec.String()
	}
	return p.direct.Readlink(path)
}

func (p *instanceProxy) ReadDir(path string) ([]fs.DirEntry, error) {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		resp, err := p.client.Call(ipc.MsgFsReadDir, enc.Bytes())
		if err != nil {
			return nil, err
		}
		dec := ipc.NewDecoder(resp)
		if ipcErr, err := ipc.DecodeError(dec); err != nil {
			return nil, err
		} else if ipcErr != nil {
			return nil, ipcErr
		}
		count, err := dec.Uint32()
		if err != nil {
			return nil, err
		}
		entries := make([]fs.DirEntry, count)
		for i := range entries {
			de, err := ipc.DecodeDirEntry(dec)
			if err != nil {
				return nil, err
			}
			entries[i] = &proxyDirEntry{de}
		}
		return entries, nil
	}
	return p.direct.ReadDir(path)
}

func (p *instanceProxy) Chmod(path string, mode fs.FileMode) error {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		enc.Uint32(uint32(mode))
		_, err := p.client.Call(ipc.MsgFsChmod, enc.Bytes())
		return err
	}
	return p.direct.Chmod(path, mode)
}

func (p *instanceProxy) Chown(path string, uid, gid int) error {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		enc.Int32(int32(uid))
		enc.Int32(int32(gid))
		_, err := p.client.Call(ipc.MsgFsChown, enc.Bytes())
		return err
	}
	return p.direct.Chown(path, uid, gid)
}

func (p *instanceProxy) Chtimes(path string, atime, mtime time.Time) error {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(path)
		enc.Int64(atime.Unix())
		enc.Int64(mtime.Unix())
		_, err := p.client.Call(ipc.MsgFsChtimes, enc.Bytes())
		return err
	}
	return p.direct.Chtimes(path, atime, mtime)
}

// ==========================================================================
// Command operations
// ==========================================================================

func (p *instanceProxy) Command(name string, args ...string) cc.Cmd {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(name)
		enc.StringSlice(args)
		resp, err := p.client.Call(ipc.MsgCmdNew, enc.Bytes())
		if err != nil {
			return nil
		}
		dec := ipc.NewDecoder(resp)
		if _, err := ipc.DecodeError(dec); err != nil {
			return nil
		}
		handle, _ := dec.Uint64()
		return &cmdProxy{client: p.client, handle: handle}
	}
	return p.direct.Command(name, args...)
}

func (p *instanceProxy) EntrypointCommand(args ...string) cc.Cmd {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.StringSlice(args)
		resp, err := p.client.Call(ipc.MsgCmdEntrypoint, enc.Bytes())
		if err != nil {
			return nil
		}
		dec := ipc.NewDecoder(resp)
		if _, err := ipc.DecodeError(dec); err != nil {
			return nil
		}
		handle, _ := dec.Uint64()
		return &cmdProxy{client: p.client, handle: handle}
	}
	return p.direct.EntrypointCommand(args...)
}

// ==========================================================================
// Network operations
// ==========================================================================

func (p *instanceProxy) Listen(network, address string) (net.Listener, error) {
	if p.useIPC {
		enc := ipc.NewEncoder()
		enc.String(network)
		enc.String(address)
		resp, err := p.client.Call(ipc.MsgNetListen, enc.Bytes())
		if err != nil {
			return nil, err
		}
		dec := ipc.NewDecoder(resp)
		if ipcErr, err := ipc.DecodeError(dec); err != nil {
			return nil, err
		} else if ipcErr != nil {
			return nil, ipcErr
		}
		handle, _ := dec.Uint64()
		return &listenerProxy{client: p.client, handle: handle}, nil
	}
	return p.direct.Listen(network, address)
}

// ==========================================================================
// Snapshot operations
// ==========================================================================

func (p *instanceProxy) SnapshotFilesystem(opts ...cc.FilesystemSnapshotOption) (cc.FilesystemSnapshot, error) {
	if p.useIPC {
		var ipcOpts ipc.SnapshotOptions
		for _, opt := range opts {
			if ex, ok := opt.(interface{ Excludes() []string }); ok {
				ipcOpts.Excludes = ex.Excludes()
			}
			if cd, ok := opt.(interface{ CacheDir() string }); ok {
				ipcOpts.CacheDir = cd.CacheDir()
			}
		}
		enc := ipc.NewEncoder()
		ipc.EncodeSnapshotOptions(enc, ipcOpts)
		resp, err := p.client.Call(ipc.MsgFsSnapshot, enc.Bytes())
		if err != nil {
			return nil, err
		}
		dec := ipc.NewDecoder(resp)
		if ipcErr, err := ipc.DecodeError(dec); err != nil {
			return nil, err
		} else if ipcErr != nil {
			return nil, ipcErr
		}
		handle, _ := dec.Uint64()
		return &snapshotProxy{client: p.client, handle: handle}, nil
	}
	return p.direct.SnapshotFilesystem(opts...)
}

// ==========================================================================
// Proxy types for sub-resources
// ==========================================================================

// proxyFileInfo implements fs.FileInfo using IPC data.
type proxyFileInfo struct {
	fi ipc.FileInfo
}

func (p *proxyFileInfo) Name() string       { return p.fi.Name }
func (p *proxyFileInfo) Size() int64        { return p.fi.Size }
func (p *proxyFileInfo) Mode() fs.FileMode  { return p.fi.Mode }
func (p *proxyFileInfo) ModTime() time.Time { return time.Unix(p.fi.ModTime, 0) }
func (p *proxyFileInfo) IsDir() bool        { return p.fi.IsDir }
func (p *proxyFileInfo) Sys() any           { return nil }

// proxyDirEntry implements fs.DirEntry using IPC data.
type proxyDirEntry struct {
	de ipc.DirEntry
}

func (p *proxyDirEntry) Name() string               { return p.de.Name }
func (p *proxyDirEntry) IsDir() bool                { return p.de.IsDir }
func (p *proxyDirEntry) Type() fs.FileMode          { return p.de.Mode.Type() }
func (p *proxyDirEntry) Info() (fs.FileInfo, error) { return nil, nil } // Not fully implemented

// fileProxy wraps file operations for IPC mode.
// Note: In IPC mode, we store files as instanceFile in the handle table,
// not as fileProxy directly. The instanceFile wrapper handles both modes.
type fileProxy struct {
	client *ipc.Client
	handle uint64
	path   string
}

func (f *fileProxy) Close() error {
	enc := ipc.NewEncoder()
	enc.Uint64(f.handle)
	_, err := f.client.Call(ipc.MsgFileClose, enc.Bytes())
	return err
}

func (f *fileProxy) Read(b []byte) (int, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(f.handle)
	enc.Uint32(uint32(len(b)))
	resp, err := f.client.Call(ipc.MsgFileRead, enc.Bytes())
	if err != nil {
		return 0, err
	}
	dec := ipc.NewDecoder(resp)
	if ipcErr, err := ipc.DecodeError(dec); err != nil {
		return 0, err
	} else if ipcErr != nil {
		return 0, ipcErr
	}
	data, err := dec.Bytes()
	if err != nil {
		return 0, err
	}
	copy(b, data)
	return len(data), nil
}

func (f *fileProxy) Write(b []byte) (int, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(f.handle)
	enc.WriteBytes(b)
	resp, err := f.client.Call(ipc.MsgFileWrite, enc.Bytes())
	if err != nil {
		return 0, err
	}
	dec := ipc.NewDecoder(resp)
	if ipcErr, err := ipc.DecodeError(dec); err != nil {
		return 0, err
	} else if ipcErr != nil {
		return 0, ipcErr
	}
	n, _ := dec.Uint32()
	return int(n), nil
}

func (f *fileProxy) ReadAt(b []byte, off int64) (int, error) {
	// ReadAt = Seek + Read (not atomic, but functional)
	_, err := f.Seek(off, 0) // SEEK_SET
	if err != nil {
		return 0, err
	}
	return f.Read(b)
}

func (f *fileProxy) WriteAt(b []byte, off int64) (int, error) {
	// WriteAt = Seek + Write (not atomic, but functional)
	_, err := f.Seek(off, 0) // SEEK_SET
	if err != nil {
		return 0, err
	}
	return f.Write(b)
}

func (f *fileProxy) Seek(offset int64, whence int) (int64, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(f.handle)
	enc.Int64(offset)
	enc.Int32(int32(whence))
	resp, err := f.client.Call(ipc.MsgFileSeek, enc.Bytes())
	if err != nil {
		return 0, err
	}
	dec := ipc.NewDecoder(resp)
	if ipcErr, err := ipc.DecodeError(dec); err != nil {
		return 0, err
	} else if ipcErr != nil {
		return 0, ipcErr
	}
	newOffset, _ := dec.Int64()
	return newOffset, nil
}

func (f *fileProxy) Sync() error {
	enc := ipc.NewEncoder()
	enc.Uint64(f.handle)
	_, err := f.client.Call(ipc.MsgFileSync, enc.Bytes())
	return err
}

func (f *fileProxy) Truncate(size int64) error {
	enc := ipc.NewEncoder()
	enc.Uint64(f.handle)
	enc.Int64(size)
	_, err := f.client.Call(ipc.MsgFileTruncate, enc.Bytes())
	return err
}

func (f *fileProxy) Stat() (fs.FileInfo, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(f.handle)
	resp, err := f.client.Call(ipc.MsgFileStat, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec := ipc.NewDecoder(resp)
	if ipcErr, err := ipc.DecodeError(dec); err != nil {
		return nil, err
	} else if ipcErr != nil {
		return nil, ipcErr
	}
	fi, err := ipc.DecodeFileInfo(dec)
	if err != nil {
		return nil, err
	}
	return &proxyFileInfo{fi}, nil
}

func (f *fileProxy) Name() string { return f.path }

// cmdProxy wraps command operations for IPC mode.
// Note: In IPC mode, we store commands as cc.Cmd in the handle table.
// The cmdProxy is used internally by instanceProxy.
type cmdProxy struct {
	client   *ipc.Client
	handle   uint64
	exitCode int
}

func (c *cmdProxy) SetDir(dir string) cc.Cmd {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	enc.String(dir)
	c.client.Call(ipc.MsgCmdSetDir, enc.Bytes())
	return c
}

func (c *cmdProxy) SetEnv(key, value string) cc.Cmd {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	enc.String(key)
	enc.String(value)
	c.client.Call(ipc.MsgCmdSetEnv, enc.Bytes())
	return c
}

func (c *cmdProxy) GetEnv(key string) string {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	enc.String(key)
	resp, err := c.client.Call(ipc.MsgCmdGetEnv, enc.Bytes())
	if err != nil {
		return ""
	}
	dec := ipc.NewDecoder(resp)
	if _, err := ipc.DecodeError(dec); err != nil {
		return ""
	}
	val, _ := dec.String()
	return val
}

func (c *cmdProxy) Environ() []string {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	resp, err := c.client.Call(ipc.MsgCmdEnviron, enc.Bytes())
	if err != nil {
		return nil
	}
	dec := ipc.NewDecoder(resp)
	if _, err := ipc.DecodeError(dec); err != nil {
		return nil
	}
	env, _ := dec.StringSlice()
	return env
}

func (c *cmdProxy) Start() error {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	_, err := c.client.Call(ipc.MsgCmdStart, enc.Bytes())
	return err
}

func (c *cmdProxy) Wait() error {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	resp, err := c.client.Call(ipc.MsgCmdWait, enc.Bytes())
	if err != nil {
		return err
	}
	dec := ipc.NewDecoder(resp)
	if ipcErr, err := ipc.DecodeError(dec); err != nil {
		return err
	} else if ipcErr != nil {
		return ipcErr
	}
	exitCode, _ := dec.Int32()
	c.exitCode = int(exitCode)
	return nil
}

func (c *cmdProxy) Run() error {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	resp, err := c.client.Call(ipc.MsgCmdRun, enc.Bytes())
	if err != nil {
		return err
	}
	dec := ipc.NewDecoder(resp)
	if ipcErr, err := ipc.DecodeError(dec); err != nil {
		return err
	} else if ipcErr != nil {
		return ipcErr
	}
	exitCode, _ := dec.Int32()
	c.exitCode = int(exitCode)
	return nil
}

func (c *cmdProxy) Output() ([]byte, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	resp, err := c.client.Call(ipc.MsgCmdOutput, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec := ipc.NewDecoder(resp)
	if ipcErr, err := ipc.DecodeError(dec); err != nil {
		return nil, err
	} else if ipcErr != nil {
		return nil, ipcErr
	}
	output, _ := dec.Bytes()
	exitCode, _ := dec.Int32()
	c.exitCode = int(exitCode)
	return output, nil
}

func (c *cmdProxy) CombinedOutput() ([]byte, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	resp, err := c.client.Call(ipc.MsgCmdCombinedOutput, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec := ipc.NewDecoder(resp)
	if ipcErr, err := ipc.DecodeError(dec); err != nil {
		return nil, err
	} else if ipcErr != nil {
		return nil, ipcErr
	}
	output, _ := dec.Bytes()
	exitCode, _ := dec.Int32()
	c.exitCode = int(exitCode)
	return output, nil
}

func (c *cmdProxy) ExitCode() int {
	return c.exitCode
}

// Pipe methods - not supported over IPC (return errors)
func (c *cmdProxy) StdinPipe() (io.WriteCloser, error) {
	return nil, fmt.Errorf("StdinPipe not supported over IPC")
}

func (c *cmdProxy) StdoutPipe() (io.ReadCloser, error) {
	return nil, fmt.Errorf("StdoutPipe not supported over IPC")
}

func (c *cmdProxy) StderrPipe() (io.ReadCloser, error) {
	return nil, fmt.Errorf("StderrPipe not supported over IPC")
}

func (c *cmdProxy) SetStdin(r io.Reader) cc.Cmd {
	// Not supported over IPC - silently ignore
	return c
}

func (c *cmdProxy) SetStdout(w io.Writer) cc.Cmd {
	// Not supported over IPC - silently ignore
	return c
}

func (c *cmdProxy) SetStderr(w io.Writer) cc.Cmd {
	// Not supported over IPC - silently ignore
	return c
}

func (c *cmdProxy) SetUser(user string) cc.Cmd {
	// Not supported over IPC - silently ignore
	return c
}

// listenerProxy implements net.Listener over IPC.
type listenerProxy struct {
	client *ipc.Client
	handle uint64
}

func (l *listenerProxy) Accept() (net.Conn, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(l.handle)
	resp, err := l.client.Call(ipc.MsgListenerAccept, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec := ipc.NewDecoder(resp)
	if ipcErr, err := ipc.DecodeError(dec); err != nil {
		return nil, err
	} else if ipcErr != nil {
		return nil, ipcErr
	}
	connHandle, _ := dec.Uint64()
	return &connProxy{client: l.client, handle: connHandle}, nil
}

func (l *listenerProxy) Close() error {
	enc := ipc.NewEncoder()
	enc.Uint64(l.handle)
	_, err := l.client.Call(ipc.MsgListenerClose, enc.Bytes())
	return err
}

func (l *listenerProxy) Addr() net.Addr {
	enc := ipc.NewEncoder()
	enc.Uint64(l.handle)
	resp, err := l.client.Call(ipc.MsgListenerAddr, enc.Bytes())
	if err != nil {
		return nil
	}
	dec := ipc.NewDecoder(resp)
	if _, err := ipc.DecodeError(dec); err != nil {
		return nil
	}
	addr, _ := dec.String()
	return &proxyAddr{addr: addr}
}

// connProxy implements net.Conn over IPC.
type connProxy struct {
	client *ipc.Client
	handle uint64
}

func (c *connProxy) Read(b []byte) (int, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	enc.Uint32(uint32(len(b)))
	resp, err := c.client.Call(ipc.MsgConnRead, enc.Bytes())
	if err != nil {
		return 0, err
	}
	dec := ipc.NewDecoder(resp)
	if ipcErr, err := ipc.DecodeError(dec); err != nil {
		return 0, err
	} else if ipcErr != nil {
		return 0, ipcErr
	}
	data, _ := dec.Bytes()
	copy(b, data)
	return len(data), nil
}

func (c *connProxy) Write(b []byte) (int, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	enc.WriteBytes(b)
	resp, err := c.client.Call(ipc.MsgConnWrite, enc.Bytes())
	if err != nil {
		return 0, err
	}
	dec := ipc.NewDecoder(resp)
	if ipcErr, err := ipc.DecodeError(dec); err != nil {
		return 0, err
	} else if ipcErr != nil {
		return 0, ipcErr
	}
	n, _ := dec.Uint32()
	return int(n), nil
}

func (c *connProxy) Close() error {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	_, err := c.client.Call(ipc.MsgConnClose, enc.Bytes())
	return err
}

func (c *connProxy) LocalAddr() net.Addr {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	resp, err := c.client.Call(ipc.MsgConnLocalAddr, enc.Bytes())
	if err != nil {
		return nil
	}
	dec := ipc.NewDecoder(resp)
	if _, err := ipc.DecodeError(dec); err != nil {
		return nil
	}
	addr, _ := dec.String()
	return &proxyAddr{addr: addr}
}

func (c *connProxy) RemoteAddr() net.Addr {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	resp, err := c.client.Call(ipc.MsgConnRemoteAddr, enc.Bytes())
	if err != nil {
		return nil
	}
	dec := ipc.NewDecoder(resp)
	if _, err := ipc.DecodeError(dec); err != nil {
		return nil
	}
	addr, _ := dec.String()
	return &proxyAddr{addr: addr}
}

func (c *connProxy) SetDeadline(t time.Time) error      { return nil }
func (c *connProxy) SetReadDeadline(t time.Time) error  { return nil }
func (c *connProxy) SetWriteDeadline(t time.Time) error { return nil }

// proxyAddr implements net.Addr.
type proxyAddr struct {
	addr string
}

func (a *proxyAddr) Network() string { return "tcp" }
func (a *proxyAddr) String() string  { return a.addr }

// snapshotProxy implements cc.FilesystemSnapshot over IPC.
type snapshotProxy struct {
	client *ipc.Client
	handle uint64
}

func (s *snapshotProxy) CacheKey() string {
	enc := ipc.NewEncoder()
	enc.Uint64(s.handle)
	resp, err := s.client.Call(ipc.MsgSnapshotCacheKey, enc.Bytes())
	if err != nil {
		return ""
	}
	dec := ipc.NewDecoder(resp)
	if _, err := ipc.DecodeError(dec); err != nil {
		return ""
	}
	key, _ := dec.String()
	return key
}

func (s *snapshotProxy) Parent() cc.FilesystemSnapshot {
	enc := ipc.NewEncoder()
	enc.Uint64(s.handle)
	resp, err := s.client.Call(ipc.MsgSnapshotParent, enc.Bytes())
	if err != nil {
		return nil
	}
	dec := ipc.NewDecoder(resp)
	if _, err := ipc.DecodeError(dec); err != nil {
		return nil
	}
	parentHandle, _ := dec.Uint64()
	if parentHandle == 0 {
		return nil
	}
	return &snapshotProxy{client: s.client, handle: parentHandle}
}

func (s *snapshotProxy) Close() error {
	enc := ipc.NewEncoder()
	enc.Uint64(s.handle)
	_, err := s.client.Call(ipc.MsgSnapshotClose, enc.Bytes())
	return err
}

// Implement InstanceSource interface for snapshotProxy
func (s *snapshotProxy) IsInstanceSource() {}
