package vm

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/kernel/alpine"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vm/execplan"
	hostmanaged "j5.nz/cc/internal/vm/host/managed"
	"j5.nz/cc/internal/vm/mounts"
	"j5.nz/cc/internal/vm/netstate"
	sidecarpkg "j5.nz/cc/internal/vm/sidecar"
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
	if inst, ok, err := h.startBuiltinGuestStream(ctx, req, onEvent); ok || err != nil {
		return inst, err
	}
	resources, err := h.prepareCreateResources(ctx, req)
	if err != nil {
		return nil, err
	}
	sidecar, err := h.launch(ctx, resources.env)
	if err != nil {
		resources.closeAll()
		return nil, err
	}
	sidecar.AddCleanup(resources.close)
	req.ID = DefaultInstanceID
	if _, err := sidecar.Worker().Start(ctx, req, onEvent); err != nil {
		_ = sidecar.Close()
		return nil, err
	}
	return newSidecarInstance(DefaultInstanceID, sidecar, req.Image, resources), nil
}

func (h *sidecarVMHost) StartBlank(ctx context.Context, req client.StartInstanceRequest) (Instance, error) {
	return h.StartBlankStream(ctx, req, nil)
}

func (h *sidecarVMHost) StartBlankStream(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (Instance, error) {
	if inst, ok, err := h.startBuiltinGuestBlankStream(ctx, req, onEvent); ok || err != nil {
		return inst, err
	}
	resources, err := h.prepareBlankResources(ctx, req)
	if err != nil {
		return nil, err
	}
	sidecar, err := h.launch(ctx, resources.env)
	if err != nil {
		resources.closeAll()
		return nil, err
	}
	sidecar.AddCleanup(resources.close)
	req.ID = DefaultInstanceID
	if _, err := sidecar.Worker().StartBlank(ctx, req, onEvent); err != nil {
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
	if err := execplan.CheckAlternateImageExec(inst); err != nil {
		return client.ExecRequest{}, err
	}
	if err := h.rejectBuiltinGuestAlternateImage(targetImage); err != nil {
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
		if err := mounts.AddRuntimeShares(ctx, inst, req.Shares); err != nil {
			return client.ExecRequest{}, err
		}
		return runningVMExecRequest(req), nil
	}
	if err := execplan.CheckAlternateImageExec(inst); err != nil {
		return client.ExecRequest{}, err
	}
	if err := h.rejectBuiltinGuestAlternateImage(targetImage); err != nil {
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
	if err := mounts.MountAlternateImageWithShares(ctx, inst, inst, mountPath, image, req.Shares); err != nil {
		return client.ExecRequest{}, err
	}
	return execplan.ResolveRunRequest(req, mountPath, execplan.Resolver{
		Root:           image.RootFS,
		BaseEnv:        image.Config.Env,
		DefaultWorkDir: image.Config.WorkingDir,
		Env:            sidecarEffectiveExecEnv,
	})
}

type sidecarStartResources struct {
	env          []string
	close        func()
	remote       bool
	rootFS       sidecarRootFS
	resolver     *sidecarCommandResolver
	networkIPv4  string
	network      *networkRuntime
	osName       string
	capabilities guestCapabilities
	execEnv      func(base, overrides []string, replace bool) []string
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
		if combined.osName == "" && resource.osName != "" {
			combined.osName = resource.osName
		}
		if isZeroGuestCapabilities(combined.capabilities) && !isZeroGuestCapabilities(resource.capabilities) {
			combined.capabilities = resource.capabilities
		}
		if combined.execEnv == nil && resource.execEnv != nil {
			combined.execEnv = resource.execEnv
		}
		if resource.close != nil {
			combined.close = appendSidecarClose(combined.close, resource.close)
		}
	}
	return combined
}

func isZeroGuestCapabilities(caps guestCapabilities) bool {
	return reflect.DeepEqual(caps, guestCapabilities{})
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

func (h *sidecarVMHost) launch(ctx context.Context, env []string) (*sidecarpkg.Daemon, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	control, err := h.sidecarControlEndpoint()
	if err != nil {
		return nil, err
	}
	keepControl := false
	defer func() {
		if !keepControl {
			control.Close()
		}
	}()
	cmd := sidecarpkg.LaunchCommand(exe, h.cacheDir, control.address, env, sidecarpkg.LaunchOptions{
		DisableEnv:    sidecarDisableEnv,
		ControlEnv:    sidecarControlEnv,
		ModeEnv:       sidecarModeEnv,
		TLSConfigPath: control.serverTLSConfig,
	})
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
			_ = sidecarpkg.KillCommand(cmd)
		}
	}()
	select {
	case <-ctx.Done():
		started = false
		return nil, ctx.Err()
	default:
	}
	hello, err := sidecarpkg.ReadStartupHello(stdout)
	if err != nil {
		started = false
		return nil, err
	}
	features := sidecarHostFeatures()
	requirements := sidecarpkg.WorkerRequirements{
		SupportsFSRPC: features.supportsFSRPC,
		SupportsL2:    features.supportsL2,
	}
	var worker *sidecarpkg.Client
	if control.clientTLSConfig == "" {
		worker, err = sidecarpkg.DialWorkerWithRequirements(ctx, hello.Addr, requirements)
	} else {
		worker, err = sidecarpkg.DialWorkerTLSWithRequirements(ctx, hello.Addr, control.clientTLSConfig, requirements)
	}
	if err != nil {
		started = false
		return nil, err
	}
	keepControl = true
	return sidecarpkg.NewDaemon(cmd, worker, stdout, []func(){control.Close}), nil
}

type sidecarControlEndpoint struct {
	address         string
	serverTLSConfig string
	clientTLSConfig string
	cleanup         func()
}

func (e sidecarControlEndpoint) Close() {
	if e.cleanup != nil {
		e.cleanup()
	}
}

func (h *sidecarVMHost) sidecarControlEndpoint() (sidecarControlEndpoint, error) {
	if runtime.GOOS == "windows" {
		security, err := sidecarpkg.NewEphemeralWorkerSecurity(h.cacheDir)
		if err != nil {
			return sidecarControlEndpoint{}, err
		}
		return sidecarControlEndpoint{
			address:         sidecarpkg.WorkerTLSScheme + "127.0.0.1:0",
			serverTLSConfig: security.ServerConfigPath,
			clientTLSConfig: security.ClientConfigPath,
			cleanup:         security.Close,
		}, nil
	}
	socketDir := filepath.Join(h.cacheDir, "_worker-sockets")
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return sidecarControlEndpoint{}, err
	}
	socketPath := filepath.Join(socketDir, fmt.Sprintf("control-%d-%d.sock", os.Getpid(), time.Now().UnixNano()))
	_ = os.Remove(socketPath)
	return sidecarControlEndpoint{
		address: socketPath,
		cleanup: sidecarControlCleanup(socketPath),
	}, nil
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

type sidecarInstance struct {
	*managedInstanceCore
	id           string
	sidecar      *sidecarpkg.Daemon
	rootFS       sidecarRootFS
	imageName    string
	resolver     *sidecarCommandResolver
	mounts       mounts.State
	networkIPv4  string
	network      *networkRuntime
	hasImageRoot bool
}

func newSidecarInstance(id string, sidecar *sidecarpkg.Daemon, imageName string, resources sidecarStartResources) *sidecarInstance {
	inst := &sidecarInstance{
		id:           id,
		sidecar:      sidecar,
		rootFS:       resources.rootFS,
		imageName:    imageName,
		resolver:     resources.resolver,
		networkIPv4:  resources.networkIPv4,
		network:      resources.network,
		hasImageRoot: resources.resolver != nil,
	}
	inst.managedInstanceCore = newSidecarManagedCore(inst.managedSession(), resources)
	return inst
}

func newSidecarManagedCore(session *sidecarpkg.ManagedSession, resources sidecarStartResources) *managedInstanceCore {
	osName := resources.osName
	if osName == "" {
		osName = "sidecar"
	}
	caps := resources.capabilities
	if isZeroGuestCapabilities(caps) {
		caps = managedguest.LinuxProfile.Caps
	}
	env := resources.execEnv
	if env == nil {
		env = sidecarEffectiveExecEnv
	}
	cfg := hostmanaged.Config{
		OSName:       osName,
		Session:      session,
		Capabilities: caps,
		Env:          env,
		MarkResolved: true,
	}
	if resources.resolver != nil {
		cfg.Root = resources.resolver.root
		cfg.BaseEnv = resources.resolver.baseEnv
		cfg.WorkDir = resources.resolver.workDir
	}
	return hostmanaged.NewCore(cfg)
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
	resolved, err := execplan.ResolveExecRequest(req, execplan.Resolver{
		Root:           r.root,
		BaseEnv:        r.baseEnv,
		DefaultWorkDir: r.workDir,
		Env:            sidecarEffectiveExecEnv,
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
		return mounts.AddRuntimeShareMount(nil, nil, nil, share, "runtime shares", nil)
	}
	mounter, ok := i.rootFS.(virtio.ShareMounter)
	if !ok {
		return mounts.AddRuntimeShareMount(nil, nil, nil, share, "runtime shares", nil)
	}
	return i.mounts.AddShare(mounter, share, "runtime shares", sidecarRuntimeShareMount)
}

func (i *sidecarInstance) AddShare(ctx context.Context, share client.ShareMount) error {
	if i != nil && i.sidecar != nil {
		return i.sidecar.Worker().AddShare(ctx, i.id, share)
	}
	return i.addCoordinatorShare(share)
}

func (i *sidecarInstance) AddImage(ctx context.Context, mountPath string, image *oci.Image) error {
	_ = ctx
	if i == nil || i.rootFS == nil {
		return mounts.AddImageMount(nil, nil, nil, mountPath, image, mounts.ImageFSBackend(image))
	}
	mounter, ok := i.rootFS.(virtio.ShareMounter)
	if !ok {
		return mounts.AddImageMount(nil, nil, nil, mountPath, image, mounts.ImageFSBackend(image))
	}
	return i.mounts.AddImage(mounter, mountPath, image, mounts.ImageFSBackend(image))
}

func (i *sidecarInstance) AddPortForward(ctx context.Context, forward client.PortForward) error {
	if i == nil {
		return netstate.AddManagedNetworkPortForward(ctx, nil, forward)
	}
	return netstate.AddManagedNetworkPortForward(ctx, i.network, forward)
}

func (i *sidecarInstance) AllowServiceProxyPort(ctx context.Context, port int) error {
	if i == nil {
		return netstate.AllowManagedNetworkServiceProxyPort(ctx, nil, port)
	}
	return netstate.AllowManagedNetworkServiceProxyPort(ctx, i.network, port)
}

func (i *sidecarInstance) resolveExecRequest(req client.ExecRequest) (client.ExecRequest, error) {
	core := i.managedCore()
	if sidecarShouldPassthroughToWorker(i.hasImageRoot, core, req) {
		return req, nil
	}
	resolved, err := core.ExecRequest(req)
	if err != nil {
		return client.ExecRequest{}, err
	}
	return resolved, nil
}

func (i *sidecarInstance) managedSession() *sidecarpkg.ManagedSession {
	if i == nil || i.sidecar == nil {
		return sidecarpkg.NewManagedSession(nil, "")
	}
	return sidecarpkg.NewManagedSession(i.sidecar.Worker(), i.id)
}

func (i *sidecarInstance) managedCore() *managedInstanceCore {
	if i == nil {
		return nil
	}
	if i.managedInstanceCore != nil {
		return i.managedInstanceCore
	}
	return newSidecarManagedCore(i.managedSession(), sidecarStartResources{resolver: i.resolver})
}

func sidecarShouldPassthroughToWorker(hasImageRoot bool, core *managedInstanceCore, req client.ExecRequest) bool {
	return core == nil || ((req.Kind == "" || req.Kind == "exec") && !req.SkipResolve && !hasImageRoot)
}

func (i *sidecarInstance) Flush(ctx context.Context) error {
	return i.managedCore().Flush(ctx)
}

func (i *sidecarInstance) ConsoleHistory(ctx context.Context) (string, error) {
	return i.managedCore().ConsoleHistory(ctx)
}

func (i *sidecarInstance) RootSnapshot() (imagefs.Directory, error) {
	if i == nil || i.rootFS == nil {
		return mounts.RootSnapshot(nil, "")
	}
	return mounts.RootSnapshotWithCapabilities("sidecar", i.ManagedCapabilities(), i.rootFS, "")
}

func (i *sidecarInstance) SnapshotImage(imageName string) (imagefs.Directory, error) {
	if i == nil || i.rootFS == nil {
		return mounts.RootSnapshot(nil, "")
	}
	if strings.TrimSpace(i.imageName) == imageName {
		return i.RootSnapshot()
	}
	return mounts.ImageSnapshotWithCapabilities("sidecar", i.ManagedCapabilities(), i.rootFS, imageName, sidecarImageMountPath(imageName))
}

func (i *sidecarInstance) NetworkIPv4() string {
	if i == nil {
		return ""
	}
	return netstate.IPv4(i.network, i.networkIPv4)
}

func (i *sidecarInstance) Exec(ctx context.Context, req client.ExecRequest) (client.ExecResponse, error) {
	core := i.managedCore()
	if sidecarShouldPassthroughToWorker(i.hasImageRoot, core, req) {
		return i.managedSession().Exec(ctx, req)
	}
	return core.Exec(ctx, req)
}

func (i *sidecarInstance) ExecStream(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	core := i.managedCore()
	if sidecarShouldPassthroughToWorker(i.hasImageRoot, core, req) {
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
	return hostmanaged.CloseSession(i.managedSession(), i.sidecar.Close)
}
