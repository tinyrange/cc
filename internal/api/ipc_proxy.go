package api

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"time"

	"github.com/tinyrange/cc/internal/ipc"
)

// instanceIPCProxy implements Instance over an IPC connection to cc-helper.
type instanceIPCProxy struct {
	client *ipc.Client
	done   chan error
}

func newInstanceIPCProxy(client *ipc.Client) *instanceIPCProxy {
	return &instanceIPCProxy{
		client: client,
		done:   make(chan error, 1),
	}
}

// decodeError is a helper that decodes the standard error prefix from an IPC response.
func decodeError(resp []byte) (*ipc.Decoder, error) {
	dec := ipc.NewDecoder(resp)
	ipcErr, err := ipc.DecodeError(dec)
	if err != nil {
		return nil, err
	}
	if ipcErr != nil {
		return nil, ipcErr
	}
	return dec, nil
}

// ==========================================================================
// Instance lifecycle
// ==========================================================================

func (p *instanceIPCProxy) Close() error {
	_, err := p.client.Call(ipc.MsgInstanceClose, nil)
	return err
}

func (p *instanceIPCProxy) Wait() error {
	_, err := p.client.Call(ipc.MsgInstanceWait, nil)
	select {
	case p.done <- err:
	default:
	}
	return err
}

func (p *instanceIPCProxy) ID() string {
	resp, err := p.client.Call(ipc.MsgInstanceID, nil)
	if err != nil {
		return ""
	}
	dec, err := decodeError(resp)
	if err != nil {
		return ""
	}
	id, _ := dec.String()
	return id
}

func (p *instanceIPCProxy) Done() <-chan error {
	return p.done
}

func (p *instanceIPCProxy) SetConsoleSize(cols, rows int) {
	enc := ipc.NewEncoder()
	enc.Int32(int32(cols))
	enc.Int32(int32(rows))
	p.client.Call(ipc.MsgInstanceSetConsole, enc.Bytes())
}

func (p *instanceIPCProxy) SetNetworkEnabled(enabled bool) {
	enc := ipc.NewEncoder()
	enc.Bool(enabled)
	p.client.Call(ipc.MsgInstanceSetNetwork, enc.Bytes())
}

func (p *instanceIPCProxy) GPU() GPU {
	return nil // GPU not supported over IPC
}

// ==========================================================================
// FS interface
// ==========================================================================

func (p *instanceIPCProxy) WithContext(_ context.Context) FS {
	return p // Context not propagated over IPC yet
}

func (p *instanceIPCProxy) Open(path string) (File, error) {
	enc := ipc.NewEncoder()
	enc.String(path)
	resp, err := p.client.Call(ipc.MsgFsOpen, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil, err
	}
	handle, _ := dec.Uint64()
	return &fileIPCProxy{client: p.client, handle: handle, path: path}, nil
}

func (p *instanceIPCProxy) Create(path string) (File, error) {
	enc := ipc.NewEncoder()
	enc.String(path)
	resp, err := p.client.Call(ipc.MsgFsCreate, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil, err
	}
	handle, _ := dec.Uint64()
	return &fileIPCProxy{client: p.client, handle: handle, path: path}, nil
}

func (p *instanceIPCProxy) OpenFile(path string, flags int, perm fs.FileMode) (File, error) {
	enc := ipc.NewEncoder()
	enc.String(path)
	enc.Int32(int32(flags))
	enc.Uint32(uint32(perm))
	resp, err := p.client.Call(ipc.MsgFsOpenFile, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil, err
	}
	handle, _ := dec.Uint64()
	return &fileIPCProxy{client: p.client, handle: handle, path: path}, nil
}

func (p *instanceIPCProxy) ReadFile(path string) ([]byte, error) {
	enc := ipc.NewEncoder()
	enc.String(path)
	resp, err := p.client.Call(ipc.MsgFsReadFile, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil, err
	}
	return dec.Bytes()
}

func (p *instanceIPCProxy) WriteFile(path string, data []byte, perm fs.FileMode) error {
	enc := ipc.NewEncoder()
	enc.String(path)
	enc.WriteBytes(data)
	enc.Uint32(uint32(perm))
	_, err := p.client.Call(ipc.MsgFsWriteFile, enc.Bytes())
	return err
}

func (p *instanceIPCProxy) Stat(path string) (fs.FileInfo, error) {
	enc := ipc.NewEncoder()
	enc.String(path)
	resp, err := p.client.Call(ipc.MsgFsStat, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil, err
	}
	fi, err := ipc.DecodeFileInfo(dec)
	if err != nil {
		return nil, err
	}
	return &proxyFileInfo{fi}, nil
}

func (p *instanceIPCProxy) Lstat(path string) (fs.FileInfo, error) {
	enc := ipc.NewEncoder()
	enc.String(path)
	resp, err := p.client.Call(ipc.MsgFsLstat, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil, err
	}
	fi, err := ipc.DecodeFileInfo(dec)
	if err != nil {
		return nil, err
	}
	return &proxyFileInfo{fi}, nil
}

func (p *instanceIPCProxy) Remove(path string) error {
	enc := ipc.NewEncoder()
	enc.String(path)
	_, err := p.client.Call(ipc.MsgFsRemove, enc.Bytes())
	return err
}

func (p *instanceIPCProxy) RemoveAll(path string) error {
	enc := ipc.NewEncoder()
	enc.String(path)
	_, err := p.client.Call(ipc.MsgFsRemoveAll, enc.Bytes())
	return err
}

func (p *instanceIPCProxy) Mkdir(path string, perm fs.FileMode) error {
	enc := ipc.NewEncoder()
	enc.String(path)
	enc.Uint32(uint32(perm))
	_, err := p.client.Call(ipc.MsgFsMkdir, enc.Bytes())
	return err
}

func (p *instanceIPCProxy) MkdirAll(path string, perm fs.FileMode) error {
	enc := ipc.NewEncoder()
	enc.String(path)
	enc.Uint32(uint32(perm))
	_, err := p.client.Call(ipc.MsgFsMkdirAll, enc.Bytes())
	return err
}

func (p *instanceIPCProxy) Rename(oldpath, newpath string) error {
	enc := ipc.NewEncoder()
	enc.String(oldpath)
	enc.String(newpath)
	_, err := p.client.Call(ipc.MsgFsRename, enc.Bytes())
	return err
}

func (p *instanceIPCProxy) Symlink(oldname, newname string) error {
	enc := ipc.NewEncoder()
	enc.String(oldname)
	enc.String(newname)
	_, err := p.client.Call(ipc.MsgFsSymlink, enc.Bytes())
	return err
}

func (p *instanceIPCProxy) Readlink(path string) (string, error) {
	enc := ipc.NewEncoder()
	enc.String(path)
	resp, err := p.client.Call(ipc.MsgFsReadlink, enc.Bytes())
	if err != nil {
		return "", err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return "", err
	}
	return dec.String()
}

func (p *instanceIPCProxy) ReadDir(path string) ([]fs.DirEntry, error) {
	enc := ipc.NewEncoder()
	enc.String(path)
	resp, err := p.client.Call(ipc.MsgFsReadDir, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil, err
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

func (p *instanceIPCProxy) Chmod(path string, mode fs.FileMode) error {
	enc := ipc.NewEncoder()
	enc.String(path)
	enc.Uint32(uint32(mode))
	_, err := p.client.Call(ipc.MsgFsChmod, enc.Bytes())
	return err
}

func (p *instanceIPCProxy) Chown(path string, uid, gid int) error {
	enc := ipc.NewEncoder()
	enc.String(path)
	enc.Int32(int32(uid))
	enc.Int32(int32(gid))
	_, err := p.client.Call(ipc.MsgFsChown, enc.Bytes())
	return err
}

func (p *instanceIPCProxy) Chtimes(path string, atime, mtime time.Time) error {
	enc := ipc.NewEncoder()
	enc.String(path)
	enc.Int64(atime.Unix())
	enc.Int64(mtime.Unix())
	_, err := p.client.Call(ipc.MsgFsChtimes, enc.Bytes())
	return err
}

func (p *instanceIPCProxy) SnapshotFilesystem(opts ...FilesystemSnapshotOption) (FilesystemSnapshot, error) {
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
	dec, err := decodeError(resp)
	if err != nil {
		return nil, err
	}
	handle, _ := dec.Uint64()
	return &snapshotIPCProxy{client: p.client, handle: handle}, nil
}

// ==========================================================================
// Exec interface
// ==========================================================================

func (p *instanceIPCProxy) Command(name string, args ...string) Cmd {
	enc := ipc.NewEncoder()
	enc.String(name)
	enc.StringSlice(args)
	resp, err := p.client.Call(ipc.MsgCmdNew, enc.Bytes())
	if err != nil {
		return nil
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil
	}
	handle, _ := dec.Uint64()
	return &cmdIPCProxy{client: p.client, handle: handle}
}

func (p *instanceIPCProxy) CommandContext(_ context.Context, name string, args ...string) Cmd {
	return p.Command(name, args...) // Context not propagated over IPC yet
}

func (p *instanceIPCProxy) EntrypointCommand(args ...string) Cmd {
	enc := ipc.NewEncoder()
	enc.StringSlice(args)
	resp, err := p.client.Call(ipc.MsgCmdEntrypoint, enc.Bytes())
	if err != nil {
		return nil
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil
	}
	handle, _ := dec.Uint64()
	return &cmdIPCProxy{client: p.client, handle: handle}
}

func (p *instanceIPCProxy) EntrypointCommandContext(_ context.Context, args ...string) Cmd {
	return p.EntrypointCommand(args...) // Context not propagated over IPC yet
}

func (p *instanceIPCProxy) Exec(name string, args ...string) error {
	enc := ipc.NewEncoder()
	enc.String(name)
	enc.StringSlice(args)
	_, err := p.client.Call(ipc.MsgInstanceExec, enc.Bytes())
	return err
}

func (p *instanceIPCProxy) ExecContext(_ context.Context, name string, args ...string) error {
	return p.Exec(name, args...) // Context not propagated over IPC yet
}

// ==========================================================================
// Net interface
// ==========================================================================

func (p *instanceIPCProxy) Dial(network, address string) (net.Conn, error) {
	return nil, fmt.Errorf("Dial not yet supported over IPC")
}

func (p *instanceIPCProxy) DialContext(_ context.Context, network, address string) (net.Conn, error) {
	return nil, fmt.Errorf("DialContext not yet supported over IPC")
}

func (p *instanceIPCProxy) Listen(network, address string) (net.Listener, error) {
	enc := ipc.NewEncoder()
	enc.String(network)
	enc.String(address)
	resp, err := p.client.Call(ipc.MsgNetListen, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil, err
	}
	handle, _ := dec.Uint64()
	return &listenerIPCProxy{client: p.client, handle: handle}, nil
}

func (p *instanceIPCProxy) ListenPacket(network, address string) (net.PacketConn, error) {
	return nil, fmt.Errorf("ListenPacket not yet supported over IPC")
}

func (p *instanceIPCProxy) Expose(guestNetwork, guestAddress string, host net.Listener) (io.Closer, error) {
	return nil, fmt.Errorf("Expose not yet supported over IPC")
}

func (p *instanceIPCProxy) Forward(guest net.Listener, hostNetwork, hostAddress string) (io.Closer, error) {
	return nil, fmt.Errorf("Forward not yet supported over IPC")
}

// ==========================================================================
// fileIPCProxy implements File over IPC
// ==========================================================================

type fileIPCProxy struct {
	client *ipc.Client
	handle uint64
	path   string
}

func (f *fileIPCProxy) Close() error {
	enc := ipc.NewEncoder()
	enc.Uint64(f.handle)
	_, err := f.client.Call(ipc.MsgFileClose, enc.Bytes())
	return err
}

func (f *fileIPCProxy) Read(b []byte) (int, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(f.handle)
	enc.Uint32(uint32(len(b)))
	resp, err := f.client.Call(ipc.MsgFileRead, enc.Bytes())
	if err != nil {
		return 0, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return 0, err
	}
	data, err := dec.Bytes()
	if err != nil {
		return 0, err
	}
	copy(b, data)
	return len(data), nil
}

func (f *fileIPCProxy) Write(b []byte) (int, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(f.handle)
	enc.WriteBytes(b)
	resp, err := f.client.Call(ipc.MsgFileWrite, enc.Bytes())
	if err != nil {
		return 0, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return 0, err
	}
	n, _ := dec.Uint32()
	return int(n), nil
}

func (f *fileIPCProxy) ReadAt(b []byte, off int64) (int, error) {
	_, err := f.Seek(off, io.SeekStart)
	if err != nil {
		return 0, err
	}
	return f.Read(b)
}

func (f *fileIPCProxy) WriteAt(b []byte, off int64) (int, error) {
	_, err := f.Seek(off, io.SeekStart)
	if err != nil {
		return 0, err
	}
	return f.Write(b)
}

func (f *fileIPCProxy) Seek(offset int64, whence int) (int64, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(f.handle)
	enc.Int64(offset)
	enc.Int32(int32(whence))
	resp, err := f.client.Call(ipc.MsgFileSeek, enc.Bytes())
	if err != nil {
		return 0, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return 0, err
	}
	newOffset, _ := dec.Int64()
	return newOffset, nil
}

func (f *fileIPCProxy) Stat() (fs.FileInfo, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(f.handle)
	resp, err := f.client.Call(ipc.MsgFileStat, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil, err
	}
	fi, err := ipc.DecodeFileInfo(dec)
	if err != nil {
		return nil, err
	}
	return &proxyFileInfo{fi}, nil
}

func (f *fileIPCProxy) Sync() error {
	enc := ipc.NewEncoder()
	enc.Uint64(f.handle)
	_, err := f.client.Call(ipc.MsgFileSync, enc.Bytes())
	return err
}

func (f *fileIPCProxy) Truncate(size int64) error {
	enc := ipc.NewEncoder()
	enc.Uint64(f.handle)
	enc.Int64(size)
	_, err := f.client.Call(ipc.MsgFileTruncate, enc.Bytes())
	return err
}

func (f *fileIPCProxy) Name() string { return f.path }

// ==========================================================================
// cmdIPCProxy implements Cmd over IPC
// ==========================================================================

type cmdIPCProxy struct {
	client   *ipc.Client
	handle   uint64
	exitCode int
}

func (c *cmdIPCProxy) SetDir(dir string) Cmd {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	enc.String(dir)
	c.client.Call(ipc.MsgCmdSetDir, enc.Bytes())
	return c
}

func (c *cmdIPCProxy) SetEnv(key, value string) Cmd {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	enc.String(key)
	enc.String(value)
	c.client.Call(ipc.MsgCmdSetEnv, enc.Bytes())
	return c
}

func (c *cmdIPCProxy) GetEnv(key string) string {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	enc.String(key)
	resp, err := c.client.Call(ipc.MsgCmdGetEnv, enc.Bytes())
	if err != nil {
		return ""
	}
	dec, err := decodeError(resp)
	if err != nil {
		return ""
	}
	val, _ := dec.String()
	return val
}

func (c *cmdIPCProxy) Environ() []string {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	resp, err := c.client.Call(ipc.MsgCmdEnviron, enc.Bytes())
	if err != nil {
		return nil
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil
	}
	env, _ := dec.StringSlice()
	return env
}

func (c *cmdIPCProxy) Start() error {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	_, err := c.client.Call(ipc.MsgCmdStart, enc.Bytes())
	return err
}

func (c *cmdIPCProxy) Wait() error {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	resp, err := c.client.Call(ipc.MsgCmdWait, enc.Bytes())
	if err != nil {
		return err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return err
	}
	exitCode, _ := dec.Int32()
	c.exitCode = int(exitCode)
	return nil
}

func (c *cmdIPCProxy) Run() error {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	resp, err := c.client.Call(ipc.MsgCmdRun, enc.Bytes())
	if err != nil {
		return err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return err
	}
	exitCode, _ := dec.Int32()
	c.exitCode = int(exitCode)
	return nil
}

func (c *cmdIPCProxy) Output() ([]byte, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	resp, err := c.client.Call(ipc.MsgCmdOutput, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil, err
	}
	output, _ := dec.Bytes()
	exitCode, _ := dec.Int32()
	c.exitCode = int(exitCode)
	return output, nil
}

func (c *cmdIPCProxy) CombinedOutput() ([]byte, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	resp, err := c.client.Call(ipc.MsgCmdCombinedOutput, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil, err
	}
	output, _ := dec.Bytes()
	exitCode, _ := dec.Int32()
	c.exitCode = int(exitCode)
	return output, nil
}

func (c *cmdIPCProxy) ExitCode() int {
	return c.exitCode
}

func (c *cmdIPCProxy) StdinPipe() (io.WriteCloser, error) {
	return nil, fmt.Errorf("StdinPipe not supported over IPC")
}

func (c *cmdIPCProxy) StdoutPipe() (io.ReadCloser, error) {
	return nil, fmt.Errorf("StdoutPipe not supported over IPC")
}

func (c *cmdIPCProxy) StderrPipe() (io.ReadCloser, error) {
	return nil, fmt.Errorf("StderrPipe not supported over IPC")
}

func (c *cmdIPCProxy) SetStdin(_ io.Reader) Cmd  { return c }
func (c *cmdIPCProxy) SetStdout(_ io.Writer) Cmd { return c }
func (c *cmdIPCProxy) SetStderr(_ io.Writer) Cmd { return c }
func (c *cmdIPCProxy) SetUser(_ string) Cmd      { return c }

// ==========================================================================
// listenerIPCProxy implements net.Listener over IPC
// ==========================================================================

type listenerIPCProxy struct {
	client *ipc.Client
	handle uint64
}

func (l *listenerIPCProxy) Accept() (net.Conn, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(l.handle)
	resp, err := l.client.Call(ipc.MsgListenerAccept, enc.Bytes())
	if err != nil {
		return nil, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil, err
	}
	connHandle, _ := dec.Uint64()
	return &connIPCProxy{client: l.client, handle: connHandle}, nil
}

func (l *listenerIPCProxy) Close() error {
	enc := ipc.NewEncoder()
	enc.Uint64(l.handle)
	_, err := l.client.Call(ipc.MsgListenerClose, enc.Bytes())
	return err
}

func (l *listenerIPCProxy) Addr() net.Addr {
	enc := ipc.NewEncoder()
	enc.Uint64(l.handle)
	resp, err := l.client.Call(ipc.MsgListenerAddr, enc.Bytes())
	if err != nil {
		return nil
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil
	}
	addr, _ := dec.String()
	return &proxyAddr{addr: addr}
}

// ==========================================================================
// connIPCProxy implements net.Conn over IPC
// ==========================================================================

type connIPCProxy struct {
	client *ipc.Client
	handle uint64
}

func (c *connIPCProxy) Read(b []byte) (int, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	enc.Uint32(uint32(len(b)))
	resp, err := c.client.Call(ipc.MsgConnRead, enc.Bytes())
	if err != nil {
		return 0, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return 0, err
	}
	data, _ := dec.Bytes()
	copy(b, data)
	return len(data), nil
}

func (c *connIPCProxy) Write(b []byte) (int, error) {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	enc.WriteBytes(b)
	resp, err := c.client.Call(ipc.MsgConnWrite, enc.Bytes())
	if err != nil {
		return 0, err
	}
	dec, err := decodeError(resp)
	if err != nil {
		return 0, err
	}
	n, _ := dec.Uint32()
	return int(n), nil
}

func (c *connIPCProxy) Close() error {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	_, err := c.client.Call(ipc.MsgConnClose, enc.Bytes())
	return err
}

func (c *connIPCProxy) LocalAddr() net.Addr {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	resp, err := c.client.Call(ipc.MsgConnLocalAddr, enc.Bytes())
	if err != nil {
		return nil
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil
	}
	addr, _ := dec.String()
	return &proxyAddr{addr: addr}
}

func (c *connIPCProxy) RemoteAddr() net.Addr {
	enc := ipc.NewEncoder()
	enc.Uint64(c.handle)
	resp, err := c.client.Call(ipc.MsgConnRemoteAddr, enc.Bytes())
	if err != nil {
		return nil
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil
	}
	addr, _ := dec.String()
	return &proxyAddr{addr: addr}
}

func (c *connIPCProxy) SetDeadline(_ time.Time) error      { return nil }
func (c *connIPCProxy) SetReadDeadline(_ time.Time) error  { return nil }
func (c *connIPCProxy) SetWriteDeadline(_ time.Time) error { return nil }

// ==========================================================================
// snapshotIPCProxy implements FilesystemSnapshot over IPC
// ==========================================================================

type snapshotIPCProxy struct {
	client *ipc.Client
	handle uint64
}

func (s *snapshotIPCProxy) IsInstanceSource() {}

func (s *snapshotIPCProxy) CacheKey() string {
	enc := ipc.NewEncoder()
	enc.Uint64(s.handle)
	resp, err := s.client.Call(ipc.MsgSnapshotCacheKey, enc.Bytes())
	if err != nil {
		return ""
	}
	dec, err := decodeError(resp)
	if err != nil {
		return ""
	}
	key, _ := dec.String()
	return key
}

func (s *snapshotIPCProxy) Parent() FilesystemSnapshot {
	enc := ipc.NewEncoder()
	enc.Uint64(s.handle)
	resp, err := s.client.Call(ipc.MsgSnapshotParent, enc.Bytes())
	if err != nil {
		return nil
	}
	dec, err := decodeError(resp)
	if err != nil {
		return nil
	}
	parentHandle, _ := dec.Uint64()
	if parentHandle == 0 {
		return nil
	}
	return &snapshotIPCProxy{client: s.client, handle: parentHandle}
}

func (s *snapshotIPCProxy) Close() error {
	enc := ipc.NewEncoder()
	enc.Uint64(s.handle)
	_, err := s.client.Call(ipc.MsgSnapshotClose, enc.Bytes())
	return err
}

// ==========================================================================
// Shared proxy types
// ==========================================================================

type proxyFileInfo struct {
	fi ipc.FileInfo
}

func (p *proxyFileInfo) Name() string       { return p.fi.Name }
func (p *proxyFileInfo) Size() int64        { return p.fi.Size }
func (p *proxyFileInfo) Mode() fs.FileMode  { return p.fi.Mode }
func (p *proxyFileInfo) ModTime() time.Time { return time.Unix(p.fi.ModTime, 0) }
func (p *proxyFileInfo) IsDir() bool        { return p.fi.IsDir }
func (p *proxyFileInfo) Sys() any           { return nil }

type proxyDirEntry struct {
	de ipc.DirEntry
}

func (p *proxyDirEntry) Name() string               { return p.de.Name }
func (p *proxyDirEntry) IsDir() bool                { return p.de.IsDir }
func (p *proxyDirEntry) Type() fs.FileMode          { return p.de.Mode.Type() }
func (p *proxyDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

type proxyAddr struct {
	addr string
}

func (a *proxyAddr) Network() string { return "tcp" }
func (a *proxyAddr) String() string  { return a.addr }

// Interface compliance checks.
var (
	_ Instance           = (*instanceIPCProxy)(nil)
	_ File               = (*fileIPCProxy)(nil)
	_ Cmd                = (*cmdIPCProxy)(nil)
	_ net.Listener       = (*listenerIPCProxy)(nil)
	_ net.Conn           = (*connIPCProxy)(nil)
	_ FilesystemSnapshot = (*snapshotIPCProxy)(nil)
	_ fs.FileInfo        = (*proxyFileInfo)(nil)
	_ fs.DirEntry        = (*proxyDirEntry)(nil)
)
