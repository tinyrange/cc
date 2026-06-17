package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/kernel/alpine"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

const (
	sidecarDisableEnv = "CCX3_DISABLE_SIDECARS"
	sidecarEnableEnv  = "CCX3_ENABLE_SIDECARS"
	sidecarModeEnv    = "CCX3_CCVM_SIDECAR_MODE"
	sidecarLimitEnv   = "CCX3_SIDECAR_MAX_VMS"
	sidecarControlEnv = "CCX3_WORKER_CONTROL_SOCKET"
)

type sidecarVMHost struct {
	cacheDir       string
	maxVMs         int
	kernel         *alpine.Manager
	images         *oci.Store
	guestInitCache string
}

func NewLocalSidecarVMHost(cacheDir string, kernel *alpine.Manager, images *oci.Store, guestInitCache string) VMHost {
	cleanupStaleSidecarSockets(cacheDir)
	return &sidecarVMHost{
		cacheDir:       cacheDir,
		maxVMs:         resolveSidecarLimit(),
		kernel:         kernel,
		images:         images,
		guestInitCache: guestInitCache,
	}
}

func cleanupStaleSidecarSockets(cacheDir string) {
	socketDir := filepath.Join(cacheDir, "_worker-sockets")
	entries, err := os.ReadDir(socketDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sock") {
			continue
		}
		_ = os.Remove(filepath.Join(socketDir, entry.Name()))
	}
}

func resolveSidecarLimit() int {
	raw := strings.TrimSpace(os.Getenv(sidecarLimitEnv))
	if raw == "" {
		return 63
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return 63
	}
	return limit
}

func (h *sidecarVMHost) HostCapabilities(context.Context) VMHostCapabilities {
	features := sidecarHostFeatures()
	return VMHostCapabilities{
		Backend:       "sidecar",
		MaxVMs:        h.maxVMs,
		Locality:      "sidecar",
		SupportsFSRPC: features.supportsFSRPC,
		SupportsL2:    features.supportsL2,
	}
}

func (h *sidecarVMHost) Close() error {
	return nil
}

func (h *sidecarVMHost) Start(ctx context.Context, req client.CreateInstanceRequest) (Instance, error) {
	return h.StartStream(ctx, req, nil)
}

func (h *sidecarVMHost) StartStream(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	resources, err := h.prepareCreateResources(ctx, req)
	if err != nil {
		return nil, err
	}
	sidecar, err := h.launch(ctx, resources.env)
	if err != nil {
		resources.closeAll()
		return nil, err
	}
	sidecar.cleanups = append(sidecar.cleanups, resources.close)
	req.ID = DefaultInstanceID
	if _, err := sidecar.worker.Start(ctx, req, onEvent); err != nil {
		_ = sidecar.Close()
		return nil, err
	}
	return newSidecarInstance(DefaultInstanceID, sidecar, req.Image, resources), nil
}

func (h *sidecarVMHost) StartBlank(ctx context.Context, req client.StartInstanceRequest) (Instance, error) {
	return h.StartBlankStream(ctx, req, nil)
}

func (h *sidecarVMHost) StartBlankStream(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	resources, err := h.prepareBlankResources(ctx, req)
	if err != nil {
		return nil, err
	}
	sidecar, err := h.launch(ctx, resources.env)
	if err != nil {
		resources.closeAll()
		return nil, err
	}
	sidecar.cleanups = append(sidecar.cleanups, resources.close)
	req.ID = DefaultInstanceID
	if _, err := sidecar.worker.StartBlank(ctx, req, onEvent); err != nil {
		_ = sidecar.Close()
		return nil, err
	}
	return newSidecarInstance(DefaultInstanceID, sidecar, req.Image, resources), nil
}

func (h *sidecarVMHost) Run(ctx context.Context, req client.RunRequest) (client.ExecResponse, error) {
	sidecar, err := h.launch(ctx, nil)
	if err != nil {
		return client.ExecResponse{}, err
	}
	defer sidecar.Close()
	return client.ExecResponse{}, fmt.Errorf("sidecar one-shot run requires a started VM")
}

func (h *sidecarVMHost) RunStream(ctx context.Context, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	sidecar, err := h.launch(ctx, nil)
	if err != nil {
		return err
	}
	defer sidecar.Close()
	return fmt.Errorf("sidecar one-shot run requires a started VM")
}

func (h *sidecarVMHost) RunInInstance(ctx context.Context, inst Instance, runningImage string, req client.RunRequest) (client.ExecResponse, error) {
	sidecarInst, ok := inst.(*sidecarInstance)
	if !ok {
		return client.ExecResponse{}, fmt.Errorf("instance is not owned by a sidecar host")
	}
	execReq, err := h.prepareRunInInstanceExec(ctx, sidecarInst, runningImage, req)
	if err != nil {
		return client.ExecResponse{}, err
	}
	return sidecarInst.Exec(ctx, execReq)
}

func (h *sidecarVMHost) RunInInstanceStream(ctx context.Context, inst Instance, runningImage string, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	sidecarInst, ok := inst.(*sidecarInstance)
	if !ok {
		return fmt.Errorf("instance is not owned by a sidecar host")
	}
	execReq, err := h.prepareRunInInstanceExec(ctx, sidecarInst, runningImage, req)
	if err != nil {
		return err
	}
	return sidecarInst.ExecStream(ctx, execReq, inputs, onEvent)
}

func (h *sidecarVMHost) ExecInInstanceStream(ctx context.Context, inst Instance, runningImage string, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	sidecarInst, ok := inst.(*sidecarInstance)
	if !ok {
		return fmt.Errorf("instance is not owned by a sidecar host")
	}
	execReq, err := h.prepareExecInInstance(ctx, sidecarInst, runningImage, req)
	if err != nil {
		return err
	}
	execReq.Image = ""
	return sidecarInst.ExecStream(ctx, execReq, inputs, onEvent)
}

func (h *sidecarVMHost) prepareExecInInstance(ctx context.Context, inst *sidecarInstance, runningImage string, req client.ExecRequest) (client.ExecRequest, error) {
	targetImage := strings.TrimSpace(req.Image)
	if targetImage == "" || targetImage == runningImage {
		return req, nil
	}
	if err := checkAlternateImageExec(inst); err != nil {
		return client.ExecRequest{}, err
	}
	if h.images == nil {
		return client.ExecRequest{}, fmt.Errorf("sidecar image store is not configured")
	}
	image, err := h.images.Open(targetImage)
	if err != nil {
		return client.ExecRequest{}, err
	}
	image = sidecarWithRuntimeMountDirs(image)
	mountPath := sidecarImageMountPath(targetImage)
	if err := inst.AddImage(ctx, mountPath, image); err != nil {
		return client.ExecRequest{}, err
	}
	req.RootDir = rootDirWithinMount(mountPath, req.RootDir)
	return req, nil
}

func rootDirWithinMount(mountPath, rootDir string) string {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" || rootDir == "/" {
		return mountPath
	}
	return filepath.Join(mountPath, strings.TrimPrefix(rootDir, "/"))
}

func (h *sidecarVMHost) prepareRunInInstanceExec(ctx context.Context, inst *sidecarInstance, runningImage string, req client.RunRequest) (client.ExecRequest, error) {
	targetImage := strings.TrimSpace(req.Image)
	if targetImage == "" || targetImage == runningImage {
		if err := addRuntimeShares(ctx, inst, req.Shares); err != nil {
			return client.ExecRequest{}, err
		}
		return runExecRequest(req), nil
	}
	if err := checkAlternateImageExec(inst); err != nil {
		return client.ExecRequest{}, err
	}
	if h.images == nil {
		return client.ExecRequest{}, fmt.Errorf("sidecar image store is not configured")
	}
	image, err := h.images.Open(targetImage)
	if err != nil {
		return client.ExecRequest{}, err
	}
	image = sidecarWithRuntimeMountDirs(image)
	mountPath := sidecarImageMountPath(targetImage)
	if err := mountAlternateImageWithShares(ctx, inst, inst, mountPath, image, req.Shares); err != nil {
		return client.ExecRequest{}, err
	}
	return resolveRunExecRequest(req, mountPath, managedExecResolver{
		root:           image.RootFS,
		baseEnv:        image.Config.Env,
		defaultWorkDir: image.Config.WorkingDir,
		env:            sidecarEffectiveExecEnv,
	})
}

type sidecarStartResources struct {
	env         []string
	close       func()
	remote      bool
	rootFS      sidecarRootFS
	resolver    *sidecarCommandResolver
	networkIPv4 string
	network     *networkRuntime
}

type sidecarBootBundle struct {
	ImageName          string
	Architecture       string
	Config             oci.RuntimeConfig
	Kernel             []byte
	Init               []byte
	AMD64EmulatorPath  string
	Modules            []alpine.Module
	NeedsAMD64Emulator bool
}

func (r sidecarStartResources) closeAll() {
	if r.close != nil {
		r.close()
	}
}

func combineSidecarResources(resources ...sidecarStartResources) sidecarStartResources {
	var combined sidecarStartResources
	for _, resource := range resources {
		combined.env = append(combined.env, resource.env...)
		if resource.remote {
			combined.remote = true
		}
		if combined.rootFS == nil && resource.rootFS != nil {
			combined.rootFS = resource.rootFS
		}
		if combined.resolver == nil && resource.resolver != nil {
			combined.resolver = resource.resolver
		}
		if combined.networkIPv4 == "" && resource.networkIPv4 != "" {
			combined.networkIPv4 = resource.networkIPv4
		}
		if combined.network == nil && resource.network != nil {
			combined.network = resource.network
		}
		if resource.close != nil {
			combined.close = appendSidecarClose(combined.close, resource.close)
		}
	}
	return combined
}

func appendSidecarClose(existing, next func()) func() {
	if existing == nil {
		return next
	}
	return func() {
		existing()
		next()
	}
}

func (h *sidecarVMHost) prepareCreateResources(ctx context.Context, req client.CreateInstanceRequest) (sidecarStartResources, error) {
	return prepareSidecarCreateResources(h, ctx, req)
}

func (h *sidecarVMHost) prepareBlankResources(ctx context.Context, req client.StartInstanceRequest) (sidecarStartResources, error) {
	return prepareSidecarBlankResources(h, ctx, req)
}

func (h *sidecarVMHost) launch(ctx context.Context, env []string) (*sidecarDaemon, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	controlSocket, err := h.sidecarControlSocketPath()
	if err != nil {
		return nil, err
	}
	cmd := sidecarLaunchCommand(exe, h.cacheDir, controlSocket, env)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("prepare sidecar stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start sidecar ccvm: %w", err)
	}
	started := true
	defer func() {
		if !started && cmd.Process != nil {
			_ = killSidecarCommand(cmd)
		}
	}()
	select {
	case <-ctx.Done():
		started = false
		return nil, ctx.Err()
	default:
	}
	hello, err := readSidecarStartupHello(stdout)
	if err != nil {
		started = false
		return nil, err
	}
	worker, err := dialSidecarWorker(ctx, hello.Addr)
	if err != nil {
		started = false
		return nil, err
	}
	return &sidecarDaemon{cmd: cmd, worker: worker, stdout: stdout, cleanups: []func(){sidecarControlCleanup(controlSocket)}}, nil
}

func readSidecarStartupHello(r io.Reader) (client.ServerHello, error) {
	var hello client.ServerHello
	if err := json.NewDecoder(r).Decode(&hello); err != nil {
		return client.ServerHello{}, fmt.Errorf("read sidecar startup banner: %w", err)
	}
	if hello.Error != "" || hello.Kind == "error" {
		detail := firstNonEmpty(hello.Detail, hello.Error, "unknown startup error")
		return client.ServerHello{}, fmt.Errorf("sidecar ccvm failed to start: %s", detail)
	}
	if strings.TrimSpace(hello.Addr) == "" {
		return client.ServerHello{}, fmt.Errorf("sidecar ccvm did not report an address")
	}
	return hello, nil
}

func (h *sidecarVMHost) sidecarControlSocketPath() (string, error) {
	if runtime.GOOS == "windows" {
		return "tcp://127.0.0.1:0", nil
	}
	socketDir := filepath.Join(h.cacheDir, "_worker-sockets")
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return "", err
	}
	socketPath := filepath.Join(socketDir, fmt.Sprintf("control-%d-%d.sock", os.Getpid(), time.Now().UnixNano()))
	_ = os.Remove(socketPath)
	return socketPath, nil
}

func prepareSidecarUnixListener(cacheDir, prefix string) (string, net.Listener, func(), error) {
	socketDir := filepath.Join(cacheDir, "_worker-sockets")
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return "", nil, nil, err
	}
	socketPath := filepath.Join(socketDir, fmt.Sprintf("%s-%d-%d.sock", prefix, os.Getpid(), time.Now().UnixNano()))
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return "", nil, nil, err
	}
	cleanup := func() {
		_ = ln.Close()
		_ = os.Remove(socketPath)
	}
	return socketPath, ln, cleanup, nil
}

func serveSidecarUnixOnce(cacheDir, prefix string, serve func(net.Conn) error) (string, func(), error) {
	return serveSidecarUnixOnceConn(cacheDir, prefix, true, serve)
}

func serveSidecarUnixOnceConn(cacheDir, prefix string, closeConn bool, serve func(net.Conn) error) (string, func(), error) {
	socketPath, ln, cleanupListener, err := prepareSidecarUnixListener(cacheDir, prefix)
	if err != nil {
		return "", nil, err
	}
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		if closeConn {
			defer conn.Close()
		}
		done <- serve(conn)
	}()
	closeFn := func() {
		cleanupListener()
		select {
		case <-done:
		default:
		}
	}
	return socketPath, closeFn, nil
}

func sidecarControlCleanup(address string) func() {
	return func() {
		if strings.HasPrefix(address, "tcp://") {
			return
		}
		_ = os.Remove(address)
	}
}

func sidecarLaunchCommand(exe, cacheDir, controlSocket string, env []string) *exec.Cmd {
	args := sidecarLaunchArgs()
	args = append(args, "-worker", "-cache-dir", cacheDir)
	cmd := exec.Command(exe, args...)
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), sidecarDisableEnv+"=1", sidecarControlEnv+"="+controlSocket)
	cmd.Env = append(cmd.Env, env...)
	return cmd
}

func sidecarLaunchArgs() []string {
	switch strings.TrimSpace(os.Getenv(sidecarModeEnv)) {
	case "vmsh-internal":
		return nil
	default:
		return nil
	}
}

type sidecarDaemon struct {
	cmd      *exec.Cmd
	worker   *sidecarWorkerClient
	stdout   io.ReadCloser
	once     sync.Once
	err      error
	cleanups []func()
}

func (d *sidecarDaemon) Close() error {
	d.once.Do(func() {
		if d.worker != nil {
			d.err = d.worker.Close()
		}
		if d.stdout != nil {
			_ = d.stdout.Close()
		}
		closeSidecarCleanups(d.cleanups)
		if d.cmd != nil {
			if err := waitSidecarCommand(d.cmd, 5*time.Second); d.err == nil && err != nil {
				d.err = err
			}
		}
	})
	return d.err
}

func waitSidecarCommand(cmd *exec.Cmd, timeout time.Duration) error {
	if cmd == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return <-done
	}
}

func killSidecarCommand(cmd *exec.Cmd) error {
	if cmd == nil {
		return nil
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return cmd.Wait()
}

func closeSidecarCleanups(cleanups []func()) {
	for i := len(cleanups) - 1; i >= 0; i-- {
		if cleanups[i] != nil {
			cleanups[i]()
		}
	}
}

type sidecarInstance struct {
	*managedInstanceCore
	id          string
	sidecar     *sidecarDaemon
	rootFS      sidecarRootFS
	imageName   string
	resolver    *sidecarCommandResolver
	mounts      managedMountState
	networkIPv4 string
	network     *networkRuntime
}

func newSidecarInstance(id string, sidecar *sidecarDaemon, imageName string, resources sidecarStartResources) *sidecarInstance {
	inst := &sidecarInstance{
		id:          id,
		sidecar:     sidecar,
		rootFS:      resources.rootFS,
		imageName:   imageName,
		resolver:    resources.resolver,
		networkIPv4: resources.networkIPv4,
		network:     resources.network,
	}
	inst.managedInstanceCore = newSidecarManagedCore(inst.managedSession(), resources.resolver)
	return inst
}

func newSidecarManagedCore(session *sidecarManagedSession, resolver *sidecarCommandResolver) *managedInstanceCore {
	core := &managedInstanceCore{
		osName:       "sidecar",
		session:      session,
		caps:         managedguest.LinuxProfile.Caps,
		env:          sidecarEffectiveExecEnv,
		markResolved: true,
	}
	if resolver != nil {
		core.root = resolver.root
		core.baseEnv = append([]string(nil), resolver.baseEnv...)
		core.workDir = resolver.workDir
	}
	return core
}

func (i *sidecarInstance) ManagedCapabilities() guestCapabilities {
	return i.managedCore().ManagedCapabilities()
}

type sidecarRootFS interface {
	virtio.FSBackend
	RootSnapshot() (imagefs.Directory, error)
	RootSnapshotAt(string) (imagefs.Directory, error)
}

type sidecarCommandResolver struct {
	root    imagefs.Directory
	baseEnv []string
	workDir string
}

func newSidecarCommandResolver(image *oci.Image) *sidecarCommandResolver {
	if image == nil || image.RootFS == nil {
		return nil
	}
	return &sidecarCommandResolver{
		root:    image.RootFS,
		baseEnv: append([]string(nil), image.Config.Env...),
		workDir: image.Config.WorkingDir,
	}
}

func (r *sidecarCommandResolver) resolve(req client.ExecRequest) (client.ExecRequest, error) {
	if req.Kind != "" && req.Kind != "exec" {
		return req, nil
	}
	if r == nil || req.SkipResolve {
		return req, nil
	}
	resolved, err := resolveManagedExecRequest(req, managedExecResolver{
		root:           r.root,
		baseEnv:        r.baseEnv,
		defaultWorkDir: r.workDir,
		env:            sidecarEffectiveExecEnv,
	})
	if err != nil {
		return client.ExecRequest{}, err
	}
	resolved.SkipResolve = true
	return resolved, nil
}

func sidecarEffectiveExecEnv(base, overrides []string, replace bool) []string {
	if replace {
		return vmruntime.WithDefaultEnv(overrides)
	}
	return vmruntime.WithDefaultEnv(vmruntime.MergeEnv(base, overrides))
}

func (i *sidecarInstance) addCoordinatorShare(share client.ShareMount) error {
	if i == nil || i.rootFS == nil {
		return addRuntimeShareMount(nil, nil, nil, share, "runtime shares", nil)
	}
	mounter, ok := i.rootFS.(virtio.ShareMounter)
	if !ok {
		return addRuntimeShareMount(nil, nil, nil, share, "runtime shares", nil)
	}
	return i.mounts.AddShare(mounter, share, "runtime shares", sidecarRuntimeShareMount)
}

func (i *sidecarInstance) AddShare(ctx context.Context, share client.ShareMount) error {
	_ = ctx
	return i.addCoordinatorShare(share)
}

func (i *sidecarInstance) AddImage(ctx context.Context, mountPath string, image *oci.Image) error {
	_ = ctx
	if i == nil || i.rootFS == nil {
		return addImageMount(nil, nil, nil, mountPath, image, imageFSBackend(image))
	}
	mounter, ok := i.rootFS.(virtio.ShareMounter)
	if !ok {
		return addImageMount(nil, nil, nil, mountPath, image, imageFSBackend(image))
	}
	return i.mounts.AddImage(mounter, mountPath, image, imageFSBackend(image))
}

func (i *sidecarInstance) AddPortForward(ctx context.Context, forward client.PortForward) error {
	if i == nil {
		return addManagedNetworkPortForward(ctx, nil, forward)
	}
	return addManagedNetworkPortForward(ctx, i.network, forward)
}

func (i *sidecarInstance) AllowServiceProxyPort(ctx context.Context, port int) error {
	if i == nil {
		return allowManagedNetworkServiceProxyPort(ctx, nil, port)
	}
	return allowManagedNetworkServiceProxyPort(ctx, i.network, port)
}

func (i *sidecarInstance) resolveExecRequest(req client.ExecRequest) (client.ExecRequest, error) {
	core := i.managedCore()
	if sidecarShouldPassthroughToWorker(core, req) {
		return req, nil
	}
	resolved, err := core.execRequest(req)
	if err != nil {
		return client.ExecRequest{}, err
	}
	return resolved, nil
}

func (i *sidecarInstance) managedSession() *sidecarManagedSession {
	if i == nil || i.sidecar == nil {
		return &sidecarManagedSession{}
	}
	return &sidecarManagedSession{worker: i.sidecar.worker, id: i.id}
}

func (i *sidecarInstance) managedCore() *managedInstanceCore {
	if i == nil {
		return nil
	}
	if i.managedInstanceCore != nil {
		return i.managedInstanceCore
	}
	return newSidecarManagedCore(i.managedSession(), i.resolver)
}

func sidecarShouldPassthroughToWorker(core *managedInstanceCore, req client.ExecRequest) bool {
	return core == nil || (req.Kind == "" && !req.SkipResolve && core.root == nil)
}

func (i *sidecarInstance) Flush(ctx context.Context) error {
	return i.managedCore().Flush(ctx)
}

func (i *sidecarInstance) ConsoleHistory(ctx context.Context) (string, error) {
	return i.managedCore().ConsoleHistory(ctx)
}

func (i *sidecarInstance) RootSnapshot() (imagefs.Directory, error) {
	if i == nil || i.rootFS == nil {
		return managedRootSnapshot(nil, "")
	}
	return managedRootSnapshotWithCapabilities("sidecar", i.ManagedCapabilities(), i.rootFS, "")
}

func (i *sidecarInstance) SnapshotImage(imageName string) (imagefs.Directory, error) {
	if i == nil || i.rootFS == nil {
		return managedRootSnapshot(nil, "")
	}
	if strings.TrimSpace(i.imageName) == imageName {
		return i.RootSnapshot()
	}
	return managedImageSnapshotWithCapabilities("sidecar", i.ManagedCapabilities(), i.rootFS, imageName, sidecarImageMountPath(imageName))
}

func (i *sidecarInstance) NetworkIPv4() string {
	if i == nil {
		return ""
	}
	return managedNetworkIPv4(i.network, i.networkIPv4)
}

func (i *sidecarInstance) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	core := i.managedCore()
	if sidecarShouldPassthroughToWorker(core, req) {
		return i.managedSession().Exec(ctx, req)
	}
	return core.Exec(ctx, req)
}

func (i *sidecarInstance) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	core := i.managedCore()
	if sidecarShouldPassthroughToWorker(core, req) {
		return i.managedSession().ExecStream(ctx, req, inputs, onEvent)
	}
	return core.ExecStream(ctx, req, inputs, onEvent)
}

func (i *sidecarInstance) Wait() error {
	return i.managedCore().Wait()
}

func (i *sidecarInstance) Close() error {
	if i == nil || i.sidecar == nil {
		return nil
	}
	return closeManagedSession(i.managedSession(), i.sidecar.Close)
}

type sidecarWorkerClient struct {
	conn   net.Conn
	codec  *WorkerCodec
	callMu sync.Mutex
	idMu   sync.Mutex
	next   uint64
}

func dialSidecarWorker(ctx context.Context, socketPath string) (*sidecarWorkerClient, error) {
	var conn net.Conn
	var err error
	network, address := sidecarWorkerDialTarget(socketPath)
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err = net.Dial(network, address)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("dial sidecar worker control socket: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	client := &sidecarWorkerClient{conn: conn, codec: NewWorkerCodec(conn)}
	frame, err := client.codec.Receive()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read sidecar worker hello: %w", err)
	}
	if frame.Type != WorkerFrameHello {
		_ = conn.Close()
		return nil, fmt.Errorf("sidecar worker sent %q before hello", frame.Type)
	}
	return client, nil
}

func sidecarWorkerDialTarget(address string) (string, string) {
	if strings.HasPrefix(address, "tcp://") {
		return "tcp", strings.TrimPrefix(address, "tcp://")
	}
	return "unix", address
}

func (c *sidecarWorkerClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *sidecarWorkerClient) Start(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (client.InstanceState, error) {
	var resp WorkerStartResponse
	err := c.call(ctx, WorkerFrameStart, req, func(frame WorkerFrame) error {
		if frame.Type != WorkerFrameEvent || onEvent == nil {
			return nil
		}
		var event client.BootEvent
		if err := frame.DecodePayload(&event); err != nil {
			return err
		}
		return onEvent(event)
	}, &resp)
	return resp.State, err
}

func (c *sidecarWorkerClient) StartBlank(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (client.InstanceState, error) {
	var resp WorkerStartResponse
	err := c.call(ctx, WorkerFrameStartBlank, req, func(frame WorkerFrame) error {
		if frame.Type != WorkerFrameEvent || onEvent == nil {
			return nil
		}
		var event client.BootEvent
		if err := frame.DecodePayload(&event); err != nil {
			return err
		}
		return onEvent(event)
	}, &resp)
	return resp.State, err
}

func (c *sidecarWorkerClient) Status(ctx context.Context, id string) (client.InstanceState, error) {
	var resp WorkerStatusResponse
	err := c.call(ctx, WorkerFrameStatus, WorkerStatusRequest{ID: id}, nil, &resp)
	return resp.State, err
}

func (c *sidecarWorkerClient) Stop(ctx context.Context, id string) error {
	var resp WorkerStatusResponse
	return c.call(ctx, WorkerFrameStop, WorkerStopRequest{ID: id}, nil, &resp)
}

func (c *sidecarWorkerClient) Wait(ctx context.Context, id string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := c.Status(ctx, id)
		if err != nil {
			return err
		}
		if state.Status != "running" && state.Status != "starting" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *sidecarWorkerClient) Flush(ctx context.Context, id string) error {
	var resp map[string]string
	return c.call(ctx, WorkerFrameFlush, WorkerFlushRequest{ID: id}, nil, &resp)
}

func (c *sidecarWorkerClient) ConsoleHistory(ctx context.Context, id string) (string, error) {
	var resp WorkerConsoleResponse
	err := c.call(ctx, WorkerFrameConsole, WorkerConsoleRequest{ID: id}, nil, &resp)
	return resp.History, err
}

func (c *sidecarWorkerClient) Exec(ctx context.Context, id string, req client.ExecRequest) ([]client.ExecEvent, error) {
	var events []client.ExecEvent
	err := c.ExecStream(ctx, id, req, nil, func(event client.ExecEvent) error {
		events = append(events, event)
		return nil
	})
	return events, err
}

func (c *sidecarWorkerClient) ExecStream(ctx context.Context, id string, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	if c == nil || c.codec == nil {
		return fmt.Errorf("sidecar worker is not connected")
	}
	c.callMu.Lock()
	defer c.callMu.Unlock()

	requestID := c.nextID()
	frame, err := NewWorkerFrame(requestID, WorkerServiceControl, WorkerFrameExec, WorkerExecRequest{
		ID:          id,
		Request:     req,
		InputStream: inputs != nil,
	})
	if err != nil {
		return err
	}
	if err := c.codec.Send(frame); err != nil {
		return err
	}

	var stopInputs chan struct{}
	if inputs != nil {
		stopInputs = make(chan struct{})
		defer close(stopInputs)
		go c.forwardWorkerExecInputs(requestID, inputs, stopInputs)
	}

	cancelDone := make(chan struct{})
	cancelSent := make(chan struct{}, 1)
	go func() {
		select {
		case <-ctx.Done():
			_ = c.conn.SetReadDeadline(time.Now())
			_ = c.sendWorkerCancel(requestID)
			cancelSent <- struct{}{}
		case <-cancelDone:
		}
	}()
	defer func() {
		close(cancelDone)
		_ = c.conn.SetReadDeadline(time.Time{})
	}()

	for {
		got, err := c.codec.Receive()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if got.ID != requestID {
			continue
		}
		switch got.Type {
		case WorkerFrameError:
			var workerErr WorkerError
			if err := got.DecodePayload(&workerErr); err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("%s", workerErr.Error)
		case WorkerFrameDone:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		case WorkerFrameEvent:
			var event client.ExecEvent
			if err := got.DecodePayload(&event); err != nil {
				return err
			}
			if onEvent != nil {
				if err := onEvent(event); err != nil {
					return err
				}
			}
		}
	}
}

func (c *sidecarWorkerClient) call(ctx context.Context, frameType string, payload any, onFrame func(WorkerFrame) error, out any) error {
	if c == nil || c.codec == nil {
		return fmt.Errorf("sidecar worker is not connected")
	}
	c.callMu.Lock()
	defer c.callMu.Unlock()
	cancelDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = c.conn.SetReadDeadline(time.Now())
		case <-cancelDone:
		}
	}()
	defer func() {
		close(cancelDone)
		_ = c.conn.SetReadDeadline(time.Time{})
	}()
	id := c.nextID()
	frame, err := NewWorkerFrame(id, WorkerServiceControl, frameType, payload)
	if err != nil {
		return err
	}
	if err := c.codec.Send(frame); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		got, err := c.codec.Receive()
		if err != nil {
			return err
		}
		if got.ID != id {
			continue
		}
		switch got.Type {
		case WorkerFrameError:
			var workerErr WorkerError
			if err := got.DecodePayload(&workerErr); err != nil {
				return err
			}
			return fmt.Errorf("%s", workerErr.Error)
		case WorkerFrameDone:
			if out != nil && len(got.Payload) != 0 {
				return got.DecodePayload(out)
			}
			return nil
		default:
			if onFrame != nil {
				if err := onFrame(got); err != nil {
					return err
				}
			}
		}
	}
}

func (c *sidecarWorkerClient) nextID() uint64 {
	c.idMu.Lock()
	defer c.idMu.Unlock()
	c.next++
	return c.next
}

func (c *sidecarWorkerClient) forwardWorkerExecInputs(id uint64, inputs <-chan client.ExecInput, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case input, ok := <-inputs:
			if !ok {
				frame, err := NewWorkerFrame(id, WorkerServiceControl, WorkerFrameExecInput, WorkerExecInput{Closed: true})
				if err == nil {
					_ = c.codec.Send(frame)
				}
				return
			}
			frame, err := NewWorkerFrame(id, WorkerServiceControl, WorkerFrameExecInput, WorkerExecInput{Input: input})
			if err != nil {
				return
			}
			if err := c.codec.Send(frame); err != nil {
				return
			}
		}
	}
}

func (c *sidecarWorkerClient) sendWorkerCancel(id uint64) error {
	frame, err := NewWorkerFrame(id, WorkerServiceControl, WorkerFrameCancel, WorkerCancelRequest{})
	if err != nil {
		return err
	}
	return c.codec.Send(frame)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
