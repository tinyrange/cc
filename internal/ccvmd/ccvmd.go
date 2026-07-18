package ccvmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/net/websocket"
	"j5.nz/cc/client"
	"j5.nz/cc/internal/cachepath"
	intcvmfs "j5.nz/cc/internal/cvmfs"
	"j5.nz/cc/internal/hv/hvf"
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/macos"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/oci"
	"j5.nz/cc/internal/vm"
)

func isBuiltInBSDImage(name string) bool {
	return managedguest.IsBuiltinBSDImage(name)
}

var debugTiming = strings.TrimSpace(os.Getenv("CCX3_DEBUG_TIMING")) != ""

const (
	defaultVMBootTimeout = 5 * time.Second

	// ccvmd has JSON control requests, not bulk upload request bodies. The
	// largest legitimate body is inline run stdin, so 64 MiB leaves ample room
	// while bounding allocations. Streaming stdin is framed into smaller
	// WebSocket messages.
	maxHTTPRequestBytes      int64 = 64 << 20
	maxWebSocketMessageBytes       = 8 << 20
	serverReadHeaderTimeout        = 10 * time.Second
	serverRequestReadTimeout       = 30 * time.Second
	serverIdleTimeout              = 2 * time.Minute
)

func bootTimeoutFromRequest(seconds float64) time.Duration {
	if seconds <= 0 {
		return resolveVMBootTimeout()
	}
	return time.Duration(seconds * float64(time.Second))
}

func timingLog(format string, args ...any) {
	if !debugTiming {
		return
	}
	fmt.Fprintf(os.Stderr, "ccx3 timing: "+format+"\n", args...)
}

func resolveVMBootTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("CCX3_VM_BOOT_TIMEOUT"))
	if raw == "" {
		return defaultVMBootTimeout
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || seconds <= 0 {
		return defaultVMBootTimeout
	}
	return time.Duration(seconds * float64(time.Second))
}

type server struct {
	kernel        *alpine.Manager
	images        *oci.Store
	vms           *vm.Manager
	cvmfsCacheDir string
}

type ServerOptions struct {
	Kind string
	// TokenPath is advertised to clients but remains owned by the caller that
	// created it. RunServer never unlinks it: only the publisher can safely
	// coordinate token generation and discovery-state rotation.
	TokenPath              string
	Authentication         *ServerAuthentication
	Persistent             bool
	OnStartup              func(client.ServerHello) error
	RegisterHandlers       func(*http.ServeMux, RuntimeView)
	WrapHandler            func(http.Handler) http.Handler
	NormalizeCreateRequest func(*client.CreateInstanceRequest, RuntimeView) error
	NormalizeStartRequest  func(*client.StartInstanceRequest, RuntimeView) error
	NormalizeRunRequest    func(*client.RunRequest, RuntimeView) error
}

type RuntimeView interface {
	InstanceStatuses() []client.InstanceState
	RunStreamIn(context.Context, string, client.RunRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
	ShutdownInstance(context.Context, string) error
	AllowServiceProxyPort(context.Context, string, int) error
}

func (s *server) InstanceStatuses() []client.InstanceState {
	if s == nil || s.vms == nil {
		return nil
	}
	return s.vms.Statuses()
}

func (s *server) RunStreamIn(ctx context.Context, id string, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	if s == nil || s.vms == nil {
		return fmt.Errorf("runtime is not available")
	}
	runCtx, cancel := runRequestContext(ctx, req)
	defer cancel()
	return s.vms.RunStreamIn(runCtx, id, req, inputs, onEvent)
}

func (s *server) ShutdownInstance(ctx context.Context, id string) error {
	if s == nil || s.vms == nil {
		return fmt.Errorf("runtime is not available")
	}
	return s.vms.ShutdownInstance(ctx, id)
}

func (s *server) AllowServiceProxyPort(ctx context.Context, id string, port int) error {
	if s == nil || s.vms == nil {
		return fmt.Errorf("runtime is not available")
	}
	return s.vms.AllowServiceProxyPortTo(ctx, id, port)
}

type watchdogController struct {
	mu          sync.Mutex
	timeout     time.Duration
	deadline    time.Time
	timer       watchdogTimer
	active      bool
	leases      map[string]watchdogLease
	onExpired   func()
	persistent  bool
	now         func() time.Time
	afterFunc   func(time.Duration, func()) watchdogTimer
	cvmfsEvents uint64
	cvmfsBytes  int64
	cvmfsLast   time.Time
}

type watchdogLease struct {
	timeout  time.Duration
	deadline time.Time
}

type watchdogTimer interface {
	Stop() bool
	Reset(time.Duration) bool
}

type watchdogRequest struct {
	TimeoutSeconds float64 `json:"timeout_seconds,omitempty"`
}

func newWatchdogController(onExpired func()) *watchdogController {
	return &watchdogController{
		onExpired: onExpired,
		now:       time.Now,
		afterFunc: func(delay time.Duration, fn func()) watchdogTimer {
			return time.AfterFunc(delay, fn)
		},
	}
}

func newPersistentWatchdogController(onExpired func()) *watchdogController {
	w := newWatchdogController(onExpired)
	w.persistent = true
	return w
}

func (w *watchdogController) Create(timeout time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timeout = timeout
	now := w.now()
	w.deadline = now.Add(timeout)
	w.active = true
	if w.timer == nil {
		w.timer = w.afterFunc(timeout, w.expire)
		return
	}
	w.resetTimerLocked(now)
}

func (w *watchdogController) Feed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.feedLocked()
}

func (w *watchdogController) RecordCVMFSActivity(bytes int) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cvmfsEvents++
	w.cvmfsBytes += int64(bytes)
	w.cvmfsLast = w.now()
}

func (w *watchdogController) ActivityState() client.WatchdogActivityState {
	w.mu.Lock()
	defer w.mu.Unlock()
	cvmfs := client.WatchdogActivityCounter{
		Events: w.cvmfsEvents,
		Bytes:  w.cvmfsBytes,
	}
	if !w.cvmfsLast.IsZero() {
		cvmfs.LastActivityUnix = w.cvmfsLast.Unix()
		cvmfs.SecondsSinceLast = w.now().Sub(w.cvmfsLast).Seconds()
	}
	return client.WatchdogActivityState{CVMFS: cvmfs}
}

func (w *watchdogController) CreateLease(timeout time.Duration) (string, error) {
	if timeout <= 0 {
		return "", fmt.Errorf("watchdog lease timeout must be positive")
	}
	id, err := newWatchdogLeaseID()
	if err != nil {
		return "", err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.leases == nil {
		w.leases = map[string]watchdogLease{}
	}
	now := w.now()
	w.leases[id] = watchdogLease{timeout: timeout, deadline: now.Add(timeout)}
	w.resetTimerLocked(now)
	return id, nil
}

func (w *watchdogController) FeedLease(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.leases == nil {
		return false
	}
	lease, ok := w.leases[id]
	if !ok {
		return false
	}
	now := w.now()
	lease.deadline = now.Add(lease.timeout)
	w.leases[id] = lease
	w.resetTimerLocked(now)
	return true
}

func (w *watchdogController) ReleaseLease(id string) bool {
	var onExpired func()
	w.mu.Lock()
	if w.leases == nil {
		w.mu.Unlock()
		return false
	}
	if _, ok := w.leases[id]; !ok {
		w.mu.Unlock()
		return false
	}
	delete(w.leases, id)
	if len(w.leases) == 0 && !w.active {
		if w.timer != nil {
			w.timer.Stop()
		}
		if !w.persistent {
			onExpired = w.onExpired
		}
	} else {
		w.resetTimerLocked(w.now())
	}
	w.mu.Unlock()
	if onExpired != nil {
		go onExpired()
	}
	return true
}

func (w *watchdogController) feedLocked() bool {
	if !w.active || w.timer == nil {
		return false
	}
	now := w.now()
	w.deadline = now.Add(w.timeout)
	w.resetTimerLocked(now)
	return true
}

func (w *watchdogController) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.active = false
	if w.timer != nil {
		w.timer.Stop()
	}
}

func (w *watchdogController) expire() {
	var onExpired func()
	w.mu.Lock()
	now := w.now()
	for id, lease := range w.leases {
		if !lease.deadline.After(now) {
			delete(w.leases, id)
		}
	}
	if len(w.leases) > 0 {
		w.resetTimerLocked(now)
		w.mu.Unlock()
		return
	}
	if w.active && w.deadline.After(now) {
		w.resetTimerLocked(now)
		w.mu.Unlock()
		return
	}
	if w.active || w.leases != nil {
		w.active = false
		if !w.persistent {
			onExpired = w.onExpired
		}
	}
	w.mu.Unlock()

	if onExpired != nil {
		onExpired()
	}
}

func (w *watchdogController) resetTimerLocked(now time.Time) {
	var next time.Time
	if w.active && !w.deadline.IsZero() {
		next = w.deadline
	}
	for _, lease := range w.leases {
		if next.IsZero() || lease.deadline.Before(next) {
			next = lease.deadline
		}
	}
	if next.IsZero() {
		if w.timer != nil {
			w.timer.Stop()
		}
		return
	}
	delay := next.Sub(now)
	if delay <= 0 {
		delay = time.Millisecond
	}
	if w.timer == nil {
		w.timer = w.afterFunc(delay, w.expire)
		return
	}
	w.timer.Stop()
	w.timer.Reset(delay)
}

func newWatchdogLeaseID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func Main(args []string) {
	started, err := RunServer(args, ServerOptions{})
	if err == nil {
		return
	}
	if !started {
		_ = writeStartupError(os.Stdout, err)
	}
	fmt.Fprintf(os.Stderr, "ccvm startup failed: %v\n", err)
	os.Exit(1)
}

func RunServer(args []string, opts ServerOptions) (bool, error) {
	if err := macos.EnsureExecutableIsSigned(); err != nil {
		return false, fmt.Errorf("prepare ccvm executable: %w", err)
	}
	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	addr := fs.String("addr", "localhost:0", "Address to listen on")
	cacheDir := fs.String("cache-dir", "", "Cache directory")
	tlsConfigPath := fs.String("tls-config", "", "Path to mutual TLS listener configuration")
	worker := fs.Bool("worker", false, "Run as a single-process VM worker")
	workerTLSConfigPath := fs.String("worker-tls-config", "", "Path to worker-control mutual TLS configuration")

	if err := fs.Parse(args); err != nil {
		return false, fmt.Errorf("parse ccvm flags: %w", err)
	}
	authentication, err := resolveServerAuthentication(*addr, *tlsConfigPath, opts.Authentication)
	if err != nil {
		return false, err
	}

	rootCache, err := resolveCacheDir(*cacheDir)
	if err != nil {
		return false, fmt.Errorf("resolve cache directory %q: %w", *cacheDir, err)
	}

	srvState := &server{
		kernel:        alpine.NewManager(filepath.Join(sharedRuntimeRoot(), "kernel")),
		images:        oci.NewStore(filepath.Join(rootCache, "images")),
		cvmfsCacheDir: filepath.Join(rootCache, "_cvmfs_cache"),
	}
	srvState.vms = vm.NewRuntimeManager(
		srvState.kernel,
		srvState.images,
		filepath.Join(sharedRuntimeRoot(), "guestinit"),
		rootCache,
		*worker,
	)

	if *worker {
		if socketPath := strings.TrimSpace(os.Getenv("CCX3_WORKER_CONTROL_SOCKET")); socketPath != "" {
			return runWorkerControlSocket(socketPath, *workerTLSConfigPath, srvState, opts)
		}
	}

	l, err := net.Listen("tcp", *addr)
	if err != nil {
		return false, fmt.Errorf("listen on %q: %w", *addr, err)
	}
	if listenerAddrRequiresAuthentication(l.Addr()) && authentication == nil {
		_ = l.Close()
		return false, &ListenerSecurityError{
			Address: *addr,
			Reason:  ListenerSecurityRemoteAuthenticationRequired,
		}
	}
	scheme := "http"
	if authentication != nil {
		l = authentication.listener(l)
		scheme = "https"
	}

	hello := client.ServerHello{
		Addr:      l.Addr().String(),
		Scheme:    scheme,
		Kind:      opts.Kind,
		TokenPath: opts.TokenPath,
	}
	if err := json.NewEncoder(os.Stdout).Encode(hello); err != nil {
		_ = l.Close()
		return false, fmt.Errorf("write startup banner: %w", err)
	}
	if opts.OnStartup != nil {
		if err := opts.OnStartup(hello); err != nil {
			_ = l.Close()
			return false, fmt.Errorf("startup callback: %w", err)
		}
	}

	var httpServer http.Server
	shutdown := newServerShutdown(srvState, &httpServer)
	watchdog := newWatchdogController(shutdown.Shutdown)
	if opts.Persistent {
		watchdog = newPersistentWatchdogController(shutdown.Shutdown)
	}
	defer watchdog.Stop()
	srvState.images.CVMFSActivity = watchdog.RecordCVMFSActivity
	mux := newMux(srvState, watchdog, shutdown.Shutdown, opts)
	if opts.RegisterHandlers != nil {
		opts.RegisterHandlers(mux, srvState)
	}

	var handler http.Handler = mux
	if opts.WrapHandler != nil {
		handler = opts.WrapHandler(handler)
	}
	handler = http.MaxBytesHandler(handler, maxHTTPRequestBytes)
	httpServer = http.Server{
		Handler:           handler,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverRequestReadTimeout,
		IdleTimeout:       serverIdleTimeout,
	}
	serveErr := daemonServeError(httpServer.Serve(l))
	shutdownErr := shutdown.Err()
	return true, errors.Join(serveErr, shutdownErr)
}

func writeStartupError(w interface{ Write([]byte) (int, error) }, err error) error {
	return json.NewEncoder(w).Encode(client.ServerHello{
		Kind:   "error",
		Error:  "ccvm failed to start",
		Detail: err.Error(),
	})
}

func runWorkerControlSocket(socketPath, tlsConfigPath string, srvState *server, opts ServerOptions) (bool, error) {
	listenNetwork, listenAddress, secure, cleanup, err := workerControlListenEndpoint(socketPath, tlsConfigPath)
	if err != nil {
		return false, err
	}
	var security *vm.WorkerTransportSecurity
	if secure {
		security, err = vm.LoadWorkerServerSecurity(tlsConfigPath)
		if err != nil {
			return false, err
		}
	}
	l, err := net.Listen(listenNetwork, listenAddress)
	if err != nil {
		return false, fmt.Errorf("listen worker control socket: %w", err)
	}
	if unixListener, ok := l.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(false)
	}
	defer l.Close()
	if listenNetwork == "unix" {
		cleanup, err = workerControlSocketCleanup(listenNetwork, listenAddress)
		if err != nil {
			return false, fmt.Errorf("capture worker control socket ownership: %w", err)
		}
		if err := secureWorkerControlSocket(listenAddress); err != nil {
			return false, fmt.Errorf("secure worker control socket: %w", err)
		}
	}
	defer cleanup()
	endpoint := workerControlDialEndpoint(listenNetwork, l.Addr().String(), secure)
	if err := json.NewEncoder(os.Stdout).Encode(client.ServerHello{Kind: "worker", Addr: endpoint}); err != nil {
		return false, fmt.Errorf("write worker startup banner: %w", err)
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			return true, fmt.Errorf("accept worker control connection: %w", err)
		}
		if secure {
			authenticated, handshakeErr := vm.HandshakeWorkerServer(context.Background(), conn, endpoint, security)
			if handshakeErr != nil {
				_ = conn.Close()
				continue
			}
			conn = authenticated
		}
		workerID := "local-sidecar"
		if security != nil {
			workerID = security.Scope
		}
		err = serveAuthenticatedWorkerControl(conn, workerID, srvState, opts)
		_ = conn.Close()
		return true, err
	}
}

func serveAuthenticatedWorkerControl(conn net.Conn, workerID string, srvState *server, opts ServerOptions) error {
	codec := vm.NewWorkerCodec(conn)
	hello, err := vm.NewWorkerFrame(0, vm.WorkerServiceControl, vm.WorkerFrameHello, vm.WorkerHello{
		Version:  vm.WorkerProtocolVersion,
		WorkerID: workerID,
		Backend:  "worker",
		Capabilities: vm.VMHostCapabilities{
			Backend:       "worker",
			MaxVMs:        1,
			Locality:      "sidecar",
			SupportsFSRPC: true,
			SupportsL2:    true,
		},
	})
	if err != nil {
		return err
	}
	if err := codec.Send(hello); err != nil {
		return fmt.Errorf("send worker hello: %w", err)
	}
	return serveWorkerControl(codec, srvState, opts)
}

func workerControlSocketCleanup(network, address string) (func(), error) {
	if network != "unix" {
		return func() {}, nil
	}
	return ownedWorkerUnixSocketCleanup(address)
}

func workerControlListenEndpoint(address, tlsConfigPath string) (network string, listenAddress string, secure bool, cleanup func(), err error) {
	cleanup = func() {}
	if strings.HasPrefix(address, "tcp://") {
		return "", "", false, cleanup, &vm.WorkerSecurityError{
			Endpoint: address,
			Reason:   vm.WorkerSecurityPlaintextTCPRejected,
		}
	}
	if strings.HasPrefix(address, vm.WorkerTLSScheme) {
		if strings.TrimSpace(tlsConfigPath) == "" {
			return "", "", false, cleanup, &vm.WorkerSecurityError{
				Endpoint: address,
				Reason:   vm.WorkerSecurityTLSConfigRequired,
			}
		}
		listenAddress = strings.TrimPrefix(address, vm.WorkerTLSScheme)
		if _, _, err := net.SplitHostPort(listenAddress); err != nil {
			return "", "", false, cleanup, fmt.Errorf("parse worker TLS endpoint: %w", err)
		}
		return "tcp", listenAddress, true, cleanup, nil
	}
	if strings.TrimSpace(tlsConfigPath) != "" {
		return "", "", false, cleanup, &vm.WorkerSecurityError{
			Endpoint: address,
			Reason:   vm.WorkerSecurityInvalidTLSConfig,
			Detail:   "TLS configuration cannot be used with a Unix socket",
		}
	}
	if err := os.MkdirAll(filepath.Dir(address), 0o700); err != nil {
		return "", "", false, cleanup, fmt.Errorf("prepare worker control socket dir: %w", err)
	}
	if err := validateWorkerControlDirectory(filepath.Dir(address)); err != nil {
		return "", "", false, cleanup, fmt.Errorf("validate worker control socket dir: %w", err)
	}
	if err := prepareWorkerUnixSocket(address); err != nil {
		return "", "", false, cleanup, err
	}
	return "unix", address, false, cleanup, nil
}

func workerControlDialEndpoint(network string, address string, secure bool) string {
	if network == "tcp" {
		if secure {
			return vm.WorkerTLSScheme + address
		}
		return "tcp://" + address
	}
	return address
}

type workerActiveExec struct {
	cancel context.CancelFunc
	inputs chan client.ExecInput
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	cond   *sync.Cond
	queue  []workerQueuedExecInput
	closed bool
}

type workerQueuedExecInput struct {
	input     client.ExecInput
	delivered chan struct{}
}

type workerOperationRegistry struct {
	mu      sync.Mutex
	active  map[uint64]context.CancelFunc
	vmTails map[string]chan struct{}
}

func newWorkerOperationRegistry() *workerOperationRegistry {
	return &workerOperationRegistry{
		active:  make(map[uint64]context.CancelFunc),
		vmTails: make(map[string]chan struct{}),
	}
}

func (r *workerOperationRegistry) start(frameID uint64, vmID string, run func(context.Context)) {
	ctx, cancel := context.WithCancel(context.Background())
	var previous <-chan struct{}
	var done chan struct{}
	r.mu.Lock()
	r.active[frameID] = cancel
	if vmID != "" {
		previous = r.vmTails[vmID]
		done = make(chan struct{})
		r.vmTails[vmID] = done
	}
	r.mu.Unlock()

	go func() {
		defer cancel()
		defer func() {
			r.mu.Lock()
			delete(r.active, frameID)
			if done != nil {
				close(done)
				if r.vmTails[vmID] == done {
					delete(r.vmTails, vmID)
				}
			}
			r.mu.Unlock()
		}()
		if previous != nil {
			select {
			case <-previous:
			case <-ctx.Done():
				return
			}
		}
		run(ctx)
	}()
}

func (r *workerOperationRegistry) cancel(frameID uint64) bool {
	r.mu.Lock()
	cancel := r.active[frameID]
	r.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (r *workerOperationRegistry) close() {
	r.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(r.active))
	for _, cancel := range r.active {
		cancels = append(cancels, cancel)
	}
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func workerVMKey(id string) string {
	if id == "" {
		return vm.DefaultInstanceID
	}
	return id
}

const (
	workerExecInputQueueCapacity   = 64
	workerExecStdinQueueLimit      = 56
	workerExecControlQueueLimit    = workerExecInputQueueCapacity - 1
	workerExecInputDeliveryTimeout = 5 * time.Second
)

var (
	errWorkerExecInputsClosed  = errors.New("worker exec input queue is closed")
	errWorkerExecInputOverflow = errors.New("worker exec input queue is full")
)

func newWorkerActiveExec(cancel context.CancelFunc, withInputs bool) *workerActiveExec {
	exec := &workerActiveExec{cancel: cancel}
	if withInputs {
		exec.inputs = make(chan client.ExecInput, 16)
		exec.done = make(chan struct{})
		exec.cond = sync.NewCond(&exec.mu)
		exec.queue = make([]workerQueuedExecInput, 0, workerExecInputQueueCapacity)
	}
	return exec
}

func (e *workerActiveExec) closeInputs() {
	if e == nil || e.inputs == nil {
		return
	}
	e.once.Do(func() {
		close(e.done)
		e.mu.Lock()
		e.closed = true
		if e.cond != nil {
			e.cond.Broadcast()
		}
		e.mu.Unlock()
	})
}

func (e *workerActiveExec) sendInput(input client.ExecInput) error {
	_, err := e.sendInputWithDelivery(input)
	return err
}

func (e *workerActiveExec) sendInputWithDelivery(input client.ExecInput) (<-chan struct{}, error) {
	if e == nil || e.inputs == nil {
		return nil, errWorkerExecInputsClosed
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, errWorkerExecInputsClosed
	}
	limit := workerExecControlQueueLimit
	if input.Kind == "stdin" {
		limit = workerExecStdinQueueLimit
	} else if input.Kind == "stdin_close" {
		limit = workerExecInputQueueCapacity
	}
	if len(e.queue) >= limit {
		return nil, errWorkerExecInputOverflow
	}
	if input.Kind == "stdin_close" {
		e.closed = true
	}
	delivered := make(chan struct{})
	e.queue = append(e.queue, workerQueuedExecInput{input: input, delivered: delivered})
	e.cond.Signal()
	return delivered, nil
}

func enqueueWorkerExecInput(exec *workerActiveExec, input client.ExecInput) error {
	err := exec.sendInput(input)
	if !errors.Is(err, errWorkerExecInputOverflow) {
		return err
	}
	if exec.cancel != nil {
		exec.cancel()
	}
	exec.closeInputs()
	return err
}

func acknowledgeWorkerExecInput(codec *vm.WorkerCodec, id uint64, exec *workerActiveExec, delivered <-chan struct{}) error {
	timer := time.NewTimer(workerExecInputDeliveryTimeout)
	defer timer.Stop()
	select {
	case <-delivered:
		return sendWorkerPayload(codec, id, vm.WorkerFrameExecInputAck, map[string]string{"status": "delivered"})
	case <-timer.C:
		if exec.cancel != nil {
			exec.cancel()
		}
		exec.closeInputs()
		return fmt.Errorf("worker exec input was not consumed within %s", workerExecInputDeliveryTimeout)
	}
}

func (e *workerActiveExec) forwardInputs() {
	defer close(e.inputs)
	for {
		e.mu.Lock()
		for len(e.queue) == 0 && !e.closed {
			e.cond.Wait()
		}
		if len(e.queue) == 0 && e.closed {
			e.mu.Unlock()
			return
		}
		queued := e.queue[0]
		copy(e.queue, e.queue[1:])
		e.queue[len(e.queue)-1] = workerQueuedExecInput{}
		e.queue = e.queue[:len(e.queue)-1]
		e.mu.Unlock()
		if queued.input.Kind == "stdin_close" {
			close(queued.delivered)
			return
		}
		select {
		case e.inputs <- queued.input:
			close(queued.delivered)
		case <-e.done:
			close(queued.delivered)
			return
		}
	}
}

func serveWorkerControl(codec *vm.WorkerCodec, srvState *server, opts ServerOptions) error {
	connectionCtx, cancelConnection := context.WithCancel(context.Background())
	defer cancelConnection()
	var activeMu sync.Mutex
	activeExecs := make(map[uint64]*workerActiveExec)
	operations := newWorkerOperationRegistry()
	defer operations.close()

	for {
		frame, err := codec.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		switch frame.Type {
		case vm.WorkerFrameStart:
			var req client.CreateInstanceRequest
			if !decodeWorkerRequest(codec, frame, &req) {
				continue
			}
			if !validateWorkerInstanceID(codec, frame, req.ID) {
				continue
			}
			if opts.NormalizeCreateRequest != nil {
				if err := opts.NormalizeCreateRequest(&req, srvState); err != nil {
					_ = sendWorkerError(codec, frame.ID, err)
					continue
				}
			}
			operations.start(frame.ID, workerVMKey(req.ID), func(ctx context.Context) {
				state, err := srvState.vms.StartStream(ctx, req, func(event client.BootEvent) error {
					return sendWorkerPayload(codec, frame.ID, vm.WorkerFrameEvent, event)
				})
				if err != nil {
					_ = sendWorkerError(codec, frame.ID, err)
					return
				}
				_ = sendWorkerPayload(codec, frame.ID, vm.WorkerFrameDone, vm.WorkerStartResponse{State: state})
			})
		case vm.WorkerFrameStartBlank:
			var req client.StartInstanceRequest
			if !decodeWorkerRequest(codec, frame, &req) {
				continue
			}
			if !validateWorkerInstanceID(codec, frame, req.ID) {
				continue
			}
			if opts.NormalizeStartRequest != nil {
				if err := opts.NormalizeStartRequest(&req, srvState); err != nil {
					_ = sendWorkerError(codec, frame.ID, err)
					continue
				}
			}
			operations.start(frame.ID, workerVMKey(req.ID), func(ctx context.Context) {
				state, err := srvState.vms.StartBlankStream(ctx, req, func(event client.BootEvent) error {
					return sendWorkerPayload(codec, frame.ID, vm.WorkerFrameEvent, event)
				})
				if err != nil {
					_ = sendWorkerError(codec, frame.ID, err)
					return
				}
				_ = sendWorkerPayload(codec, frame.ID, vm.WorkerFrameDone, vm.WorkerStartResponse{State: state})
			})
		case vm.WorkerFrameStatus:
			var req vm.WorkerStatusRequest
			if !decodeWorkerRequest(codec, frame, &req) || !validateWorkerInstanceID(codec, frame, req.ID) {
				continue
			}
			operations.start(frame.ID, "", func(context.Context) {
				_ = sendWorkerPayload(codec, frame.ID, vm.WorkerFrameDone, vm.WorkerStatusResponse{State: srvState.vms.StatusOf(req.ID)})
			})
		case vm.WorkerFrameStop:
			var req vm.WorkerStopRequest
			if !decodeWorkerRequest(codec, frame, &req) || !validateWorkerInstanceID(codec, frame, req.ID) {
				continue
			}
			operations.start(frame.ID, workerVMKey(req.ID), func(ctx context.Context) {
				if err := srvState.vms.ShutdownInstance(ctx, req.ID); err != nil {
					_ = sendWorkerError(codec, frame.ID, err)
					return
				}
				_ = sendWorkerPayload(codec, frame.ID, vm.WorkerFrameDone, vm.WorkerStatusResponse{State: srvState.vms.StatusOf(req.ID)})
			})
		case vm.WorkerFrameWait:
			var req vm.WorkerWaitRequest
			if !decodeWorkerRequest(codec, frame, &req) || !validateWorkerInstanceID(codec, frame, req.ID) {
				continue
			}
			operations.start(frame.ID, "", func(ctx context.Context) {
				state, completed := waitForWorkerState(ctx, func() client.InstanceState {
					return srvState.vms.StatusOf(req.ID)
				})
				if completed {
					_ = sendWorkerPayload(codec, frame.ID, vm.WorkerFrameDone, vm.WorkerStatusResponse{State: state})
				}
			})
		case vm.WorkerFrameFlush:
			var req vm.WorkerFlushRequest
			if !decodeWorkerRequest(codec, frame, &req) || !validateWorkerInstanceID(codec, frame, req.ID) {
				continue
			}
			operations.start(frame.ID, workerVMKey(req.ID), func(ctx context.Context) {
				if err := srvState.vms.FlushInstance(ctx, req.ID); err != nil {
					_ = sendWorkerError(codec, frame.ID, err)
					return
				}
				_ = sendWorkerPayload(codec, frame.ID, vm.WorkerFrameDone, map[string]string{"status": "flushed"})
			})
		case vm.WorkerFrameAddShare:
			var req vm.WorkerAddShareRequest
			if !decodeWorkerRequest(codec, frame, &req) || !validateWorkerInstanceID(codec, frame, req.ID) {
				continue
			}
			operations.start(frame.ID, workerVMKey(req.ID), func(ctx context.Context) {
				if err := srvState.vms.AddShareTo(ctx, req.ID, req.Share); err != nil {
					_ = sendWorkerError(codec, frame.ID, err)
					return
				}
				_ = sendWorkerPayload(codec, frame.ID, vm.WorkerFrameDone, map[string]string{"status": "mounted"})
			})
		case vm.WorkerFrameConsole:
			var req vm.WorkerConsoleRequest
			if !decodeWorkerRequest(codec, frame, &req) || !validateWorkerInstanceID(codec, frame, req.ID) {
				continue
			}
			operations.start(frame.ID, "", func(ctx context.Context) {
				history, err := srvState.vms.ConsoleHistory(ctx, req.ID)
				if err != nil {
					_ = sendWorkerError(codec, frame.ID, err)
					return
				}
				_ = sendWorkerPayload(codec, frame.ID, vm.WorkerFrameDone, vm.WorkerConsoleResponse{History: history})
			})
		case vm.WorkerFrameExec:
			if err := serveWorkerExec(connectionCtx, codec, srvState, frame, &activeMu, activeExecs); err != nil {
				_ = sendWorkerRequestError(codec, frame, err)
			}
		case vm.WorkerFrameExecInput:
			var req vm.WorkerExecInput
			if !decodeWorkerRequest(codec, frame, &req) {
				continue
			}
			activeMu.Lock()
			exec := activeExecs[frame.ID]
			activeMu.Unlock()
			if exec == nil || exec.inputs == nil {
				continue
			}
			if req.Closed {
				delivered, err := exec.sendInputWithDelivery(client.ExecInput{Kind: "stdin_close"})
				if err == nil {
					go func(id uint64, delivered <-chan struct{}) {
						if err := acknowledgeWorkerExecInput(codec, id, exec, delivered); err != nil {
							_ = sendWorkerError(codec, id, err)
						}
					}(frame.ID, delivered)
				}
				if err != nil {
					_ = sendWorkerError(codec, frame.ID, err)
				}
				continue
			}
			delivered, err := exec.sendInputWithDelivery(req.Input)
			if err == nil {
				go func(id uint64, delivered <-chan struct{}) {
					if err := acknowledgeWorkerExecInput(codec, id, exec, delivered); err != nil {
						_ = sendWorkerError(codec, id, err)
					}
				}(frame.ID, delivered)
			}
			if err != nil {
				_ = sendWorkerError(codec, frame.ID, err)
			}
		case vm.WorkerFrameCancel:
			var req vm.WorkerCancelRequest
			if !decodeWorkerRequest(codec, frame, &req) {
				continue
			}
			operations.cancel(frame.ID)
			activeMu.Lock()
			exec := activeExecs[frame.ID]
			activeMu.Unlock()
			if exec != nil {
				exec.cancel()
				exec.closeInputs()
			}
		default:
			_ = sendWorkerError(codec, frame.ID, fmt.Errorf("unsupported worker frame %q", frame.Type))
		}
	}
}

func waitForWorkerState(ctx context.Context, status func() client.InstanceState) (client.InstanceState, bool) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		state := status()
		if state.Status != "running" && state.Status != "starting" {
			return state, true
		}
		select {
		case <-ctx.Done():
			return client.InstanceState{}, false
		case <-ticker.C:
		}
	}
}

func serveWorkerExec(parent context.Context, codec *vm.WorkerCodec, srvState *server, frame vm.WorkerFrame, activeMu *sync.Mutex, activeExecs map[uint64]*workerActiveExec) error {
	var req vm.WorkerExecRequest
	if err := frame.DecodePayload(&req); err != nil {
		return err
	}
	if strings.TrimSpace(req.ID) == "" {
		return fmt.Errorf("worker instance id is required")
	}
	ctx, cancel := context.WithCancel(parent)
	exec := newWorkerActiveExec(cancel, req.InputStream)
	if req.InputStream {
		go exec.forwardInputs()
	}
	activeMu.Lock()
	activeExecs[frame.ID] = exec
	activeMu.Unlock()

	go func() {
		defer cancel()
		defer exec.closeInputs()
		defer func() {
			activeMu.Lock()
			delete(activeExecs, frame.ID)
			activeMu.Unlock()
		}()

		err := srvState.vms.StreamIn(ctx, req.ID, req.Request, exec.inputs, func(event client.ExecEvent) error {
			return sendWorkerPayload(codec, frame.ID, vm.WorkerFrameEvent, event)
		})
		if err != nil {
			_ = sendWorkerError(codec, frame.ID, err)
			return
		}
		_ = sendWorkerPayload(codec, frame.ID, vm.WorkerFrameDone, map[string]string{"status": "done"})
	}()
	return nil
}

func sendWorkerPayload(codec *vm.WorkerCodec, id uint64, frameType string, payload any) error {
	frame, err := vm.NewWorkerFrame(id, vm.WorkerServiceControl, frameType, payload)
	if err != nil {
		return err
	}
	return codec.Send(frame)
}

func sendWorkerError(codec *vm.WorkerCodec, id uint64, err error) error {
	return sendWorkerPayload(codec, id, vm.WorkerFrameError, vm.WorkerError{Error: err.Error()})
}

func decodeWorkerRequest(codec *vm.WorkerCodec, frame vm.WorkerFrame, dst any) bool {
	if err := frame.DecodePayload(dst); err != nil {
		_ = sendWorkerRequestError(codec, frame, fmt.Errorf("decode payload: %w", err))
		return false
	}
	return true
}

func validateWorkerInstanceID(codec *vm.WorkerCodec, frame vm.WorkerFrame, id string) bool {
	if strings.TrimSpace(id) != "" {
		return true
	}
	_ = sendWorkerRequestError(codec, frame, fmt.Errorf("worker instance id is required"))
	return false
}

func sendWorkerRequestError(codec *vm.WorkerCodec, frame vm.WorkerFrame, err error) error {
	return sendWorkerPayload(codec, frame.ID, vm.WorkerFrameError, vm.WorkerError{
		Error:       err.Error(),
		RequestID:   frame.ID,
		RequestType: frame.Type,
	})
}

type serverShutdown struct {
	once         sync.Once
	shutdownVMs  func(context.Context) error
	shutdownHTTP func(context.Context) error
	err          error
}

func newServerShutdown(srvState *server, httpServer *http.Server) *serverShutdown {
	shutdown := &serverShutdown{}
	if srvState != nil && srvState.vms != nil {
		shutdown.shutdownVMs = srvState.vms.ShutdownAll
	}
	if httpServer != nil {
		shutdown.shutdownHTTP = httpServer.Shutdown
	}
	return shutdown
}

func (s *serverShutdown) Shutdown() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		var errs []error
		if s.shutdownVMs != nil {
			if err := runShutdownStep(s.shutdownVMs); err != nil {
				errs = append(errs, fmt.Errorf("shutdown VMs: %w", err))
			}
		}
		if s.shutdownHTTP != nil {
			if err := runShutdownStep(s.shutdownHTTP); err != nil {
				errs = append(errs, fmt.Errorf("shutdown HTTP server: %w", err))
			}
		}
		s.err = errors.Join(errs...)
	})
}

func (s *serverShutdown) Err() error {
	if s == nil {
		return nil
	}
	s.Shutdown()
	return s.err
}

func runShutdownStep(step func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return step(ctx)
}

func daemonServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("serve daemon API: %w", err)
}

type apiRoute struct {
	Method string
	Path   string
}

type trackedServeMux struct {
	*http.ServeMux
	routes []apiRoute
}

func newTrackedServeMux() *trackedServeMux {
	return &trackedServeMux{ServeMux: http.NewServeMux()}
}

func (m *trackedServeMux) HandleFunc(pattern string, handler http.HandlerFunc) {
	m.record(pattern)
	m.ServeMux.HandleFunc(pattern, handler)
}

func (m *trackedServeMux) Handle(pattern string, handler http.Handler) {
	m.record(pattern)
	m.ServeMux.Handle(pattern, handler)
}

func (m *trackedServeMux) record(pattern string) {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok || strings.HasPrefix(path, "/debug/pprof") {
		return
	}
	m.routes = append(m.routes, apiRoute{Method: method, Path: path})
}

func newMux(srvState *server, watchdog *watchdogController, shutdown func(), opts ServerOptions) *http.ServeMux {
	mux, _ := newMuxWithRoutes(srvState, watchdog, shutdown, opts)
	return mux
}

func newMuxWithRoutes(srvState *server, watchdog *watchdogController, shutdown func(), opts ServerOptions) (*http.ServeMux, []apiRoute) {
	mux := newTrackedServeMux()
	if pprofEnabled() {
		runtime.SetBlockProfileRate(1)
		runtime.SetMutexProfileFraction(1)
		registerPprofHandlers(mux.ServeMux)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /capabilities", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, srvState.vms.Capabilities())
	})

	mux.HandleFunc("GET /debug/virtiofs", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		writeJSON(w, http.StatusOK, srvState.vms.VirtioFSStats(id))
	})

	mux.HandleFunc("GET /debug/exits", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, hvf.ExitTimingSnapshot())
	})

	mux.HandleFunc("POST /shutdown", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		go func() {
			time.Sleep(10 * time.Millisecond)
			if watchdog != nil {
				watchdog.Stop()
			}
			shutdown()
		}()
	})

	mux.HandleFunc("POST /watchdog", func(w http.ResponseWriter, r *http.Request) {
		if watchdog == nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("watchdog is unavailable"))
			return
		}
		var req watchdogRequest
		if err := decodeOptionalJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		timeout := 30 * time.Second
		if req.TimeoutSeconds > 0 {
			timeout = time.Duration(req.TimeoutSeconds * float64(time.Second))
		}
		if timeout <= 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("watchdog timeout must be positive"))
			return
		}
		watchdog.Create(timeout)
		writeJSON(w, http.StatusOK, map[string]any{
			"status":          "watching",
			"timeout_seconds": timeout.Seconds(),
		})
	})

	mux.HandleFunc("POST /watchdog/feed", func(w http.ResponseWriter, r *http.Request) {
		if watchdog == nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("watchdog is unavailable"))
			return
		}
		if !watchdog.Feed() {
			writeError(w, http.StatusConflict, fmt.Errorf("watchdog has not been created"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "fed"})
	})

	mux.HandleFunc("POST /watchdog/lease", func(w http.ResponseWriter, r *http.Request) {
		if watchdog == nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("watchdog is unavailable"))
			return
		}
		var req client.WatchdogLeaseRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		timeout, err := watchdogLeaseDuration(req.TimeoutSeconds)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		id, err := watchdog.CreateLease(timeout)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, client.WatchdogLeaseResponse{LeaseID: id, TimeoutSeconds: timeout.Seconds()})
	})

	mux.HandleFunc("POST /watchdog/lease/feed", func(w http.ResponseWriter, r *http.Request) {
		if watchdog == nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("watchdog is unavailable"))
			return
		}
		var req client.WatchdogLeaseRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if !watchdog.FeedLease(req.LeaseID) {
			writeError(w, http.StatusConflict, fmt.Errorf("watchdog lease is not active"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "fed"})
	})

	mux.HandleFunc("POST /watchdog/lease/release", func(w http.ResponseWriter, r *http.Request) {
		if watchdog == nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("watchdog is unavailable"))
			return
		}
		var req client.WatchdogLeaseRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if !watchdog.ReleaseLease(req.LeaseID) {
			writeError(w, http.StatusConflict, fmt.Errorf("watchdog lease is not active"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
	})

	mux.HandleFunc("GET /watchdog/activity", func(w http.ResponseWriter, r *http.Request) {
		if watchdog == nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("watchdog is unavailable"))
			return
		}
		writeJSON(w, http.StatusOK, watchdog.ActivityState())
	})

	mux.HandleFunc("GET /kernel", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, srvState.kernel.Status())
	})

	mux.HandleFunc("POST /kernel/download", func(w http.ResponseWriter, r *http.Request) {
		var req client.DownloadRequest
		if err := decodeOptionalJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if wantsProgressStream(r) {
			report := func(event client.ProgressEvent) {
				_ = writeProgressEvent(w, event)
			}
			if err := srvState.kernel.EnsureWithProgress(r.Context(), report); err != nil {
				_ = writeProgressEvent(w, client.ProgressEvent{Status: "error", Error: err.Error()})
			}
			return
		}
		if err := srvState.kernel.Ensure(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, srvState.kernel.Status())
	})

	mux.HandleFunc("GET /image", func(w http.ResponseWriter, r *http.Request) {
		images, err := srvState.images.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, images)
	})

	mux.HandleFunc("GET /image/{image}", func(w http.ResponseWriter, r *http.Request) {
		imageName := r.PathValue("image")
		if isBuiltInBSDImage(imageName) {
			writeJSON(w, http.StatusOK, client.ImageState{
				Name:       imageName,
				Source:     "builtin:" + strings.TrimPrefix(imageName, "@"),
				SourceKind: "builtin",
				Status:     "downloaded",
			})
			return
		}
		state, err := srvState.images.Get(imageName)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, state)
	})

	mux.HandleFunc("POST /image/{image}/metadata", func(w http.ResponseWriter, r *http.Request) {
		imageName := r.PathValue("image")
		if isBuiltInBSDImage(imageName) {
			writeJSON(w, http.StatusOK, client.ImageMetadataState{
				Name:       imageName,
				Status:     "prepared",
				SourceKind: "builtin",
				Env:        nil,
			})
			return
		}
		image, err := srvState.images.Open(imageName)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, client.ImageMetadataState{
			Name:         image.Name,
			Status:       "prepared",
			SourceKind:   image.SourceKind,
			Architecture: image.Architecture,
			Env:          append([]string(nil), image.Config.Env...),
		})
	})

	mux.HandleFunc("POST /image/{image}/qemu/download", func(w http.ResponseWriter, r *http.Request) {
		imageName := r.PathValue("image")
		if isBuiltInBSDImage(imageName) {
			writeJSON(w, http.StatusOK, client.EmulatorState{Status: "skipped", Required: false})
			return
		}
		image, err := srvState.images.Open(imageName)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if !vm.NeedsAMD64Emulation(image) {
			writeJSON(w, http.StatusOK, client.EmulatorState{Status: "skipped", Required: false})
			return
		}
		if wantsProgressStream(r) {
			report := func(event client.ProgressEvent) {
				_ = writeProgressEvent(w, event)
			}
			path, err := srvState.kernel.ExtractPackageFileWithProgress(
				r.Context(),
				"community",
				"qemu-x86_64",
				"usr/bin/qemu-x86_64",
				report,
			)
			if err != nil {
				_ = writeProgressEvent(w, client.ProgressEvent{Status: "error", Error: err.Error()})
				return
			}
			_ = writeProgressEvent(w, client.ProgressEvent{Status: "downloaded", Artifact: filepath.Base(path)})
			return
		}
		path, err := vm.PrepareAMD64Emulator(r.Context(), image, srvState.kernel.ExtractPackageFile)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, client.EmulatorState{
			Status:   "downloaded",
			Path:     path,
			Required: true,
		})
	})

	mux.HandleFunc("POST /image/{image}", func(w http.ResponseWriter, r *http.Request) {
		imageName := r.PathValue("image")
		var req client.PullImageRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		source, err := req.SourceString()
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if wantsProgressStream(r) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			enc := json.NewEncoder(w)
			flusher, _ := w.(http.Flusher)
			events := make(chan client.ProgressEvent, 128)
			go func() {
				defer close(events)
				_, err := srvState.images.Pull(r.Context(), imageName, source, oci.PullOptions{
					Architecture:    req.Architecture,
					Prefetch:        req.Prefetch,
					PrefetchWorkers: req.PrefetchWorkers,
					CVMFSMirrors:    cvmfsSourceMirrors(req.SourceRef),
					Report: func(event client.ProgressEvent) {
						if event.Artifact == "" {
							event.Artifact = imageName
						}
						select {
						case events <- event:
						case <-r.Context().Done():
						}
					},
				})
				if err != nil {
					select {
					case events <- client.ProgressEvent{Status: "error", Artifact: imageName, Error: err.Error()}:
					case <-r.Context().Done():
					}
				}
			}()
			for event := range events {
				if err := enc.Encode(event); err != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			return
		}
		state, err := srvState.images.Pull(r.Context(), imageName, source, oci.PullOptions{
			Architecture:    req.Architecture,
			Prefetch:        req.Prefetch,
			PrefetchWorkers: req.PrefetchWorkers,
			CVMFSMirrors:    cvmfsSourceMirrors(req.SourceRef),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, state)
	})

	mux.HandleFunc("DELETE /image/{image}", func(w http.ResponseWriter, r *http.Request) {
		imageName := r.PathValue("image")
		if err := srvState.images.Delete(imageName); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "image": imageName})
	})

	mux.HandleFunc("POST /cvmfs/list", func(w http.ResponseWriter, r *http.Request) {
		var req client.CVMFSListRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		cvmfsClient := intcvmfs.NewClient()
		cvmfsClient.Context = r.Context()
		cvmfsClient.CacheDir = cvmfsRequestCacheDir(req.CacheDir, srvState.cvmfsCacheDir)
		cvmfsClient.Mirrors = req.Mirrors
		if watchdog != nil {
			cvmfsClient.OnActivity = func(event intcvmfs.ActivityEvent) {
				watchdog.RecordCVMFSActivity(event.Bytes)
			}
		}
		target := cvmfsTarget(req.Mirror, req.Repo, req.Path)
		entries, err := cvmfsClient.ReadDir(target)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		resp := client.CVMFSListResponse{Entries: make([]client.CVMFSDirectoryEntry, 0, len(entries))}
		basePath := ensureAbsolutePath(req.Path)
		for _, entry := range entries {
			kind := "file"
			if entry.Mode.IsDir() {
				kind = "directory"
			} else if entry.Mode&fs.ModeSymlink != 0 {
				kind = "symlink"
			}
			resp.Entries = append(resp.Entries, client.CVMFSDirectoryEntry{
				Name: entry.Name,
				Path: pathJoin(basePath, entry.Name),
				Kind: kind,
				Size: entry.Size,
			})
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("POST /cvmfs/read", func(w http.ResponseWriter, r *http.Request) {
		var req client.CVMFSReadRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		cvmfsClient := intcvmfs.NewClient()
		cvmfsClient.Context = r.Context()
		cvmfsClient.CacheDir = cvmfsRequestCacheDir(req.CacheDir, srvState.cvmfsCacheDir)
		cvmfsClient.Mirrors = req.Mirrors
		if watchdog != nil {
			cvmfsClient.OnActivity = func(event intcvmfs.ActivityEvent) {
				watchdog.RecordCVMFSActivity(event.Bytes)
			}
		}
		target := cvmfsTarget(req.Mirror, req.Repo, req.Path)
		data, eof, err := cvmfsClient.ReadFileRange(target, req.Offset, req.Length)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, client.CVMFSReadResponse{
			Path:   ensureAbsolutePath(req.Path),
			Offset: req.Offset,
			Data:   data,
			EOF:    eof,
		})
	})

	mux.HandleFunc("GET /vm/supported", func(w http.ResponseWriter, r *http.Request) {
		err := vm.Supports()
		resp := client.VMSupportedResponse{Supported: err == nil}
		if err != nil {
			resp.Error = err.Error()
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("GET /vm/status", func(w http.ResponseWriter, r *http.Request) {
		if id := strings.TrimSpace(r.URL.Query().Get("id")); id != "" {
			writeJSON(w, http.StatusOK, srvState.vms.StatusOf(id))
			return
		}
		writeJSON(w, http.StatusOK, srvState.vms.Status())
	})
	mux.HandleFunc("GET /vm", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, srvState.vms.Statuses())
	})
	mux.HandleFunc("POST /vm/{id}/flush", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if err := srvState.vms.FlushInstance(r.Context(), id); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "flushed"})
	})
	mux.HandleFunc("POST /vm/{id}/save", func(w http.ResponseWriter, r *http.Request) {
		var req client.SaveImageRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("image name is required"))
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		requestedImage := strings.TrimSpace(req.Image)
		root, sourceImage, err := srvState.vms.SnapshotRootFS(r.Context(), id, requestedImage)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		var opts oci.SaveOptions
		if sourceImage == "" {
			sourceImage = requestedImage
		}
		if sourceImage != "" {
			opts.Source = "vm:" + id + " from " + sourceImage
			if image, err := srvState.images.Open(sourceImage); err == nil {
				opts.Architecture = image.Architecture
				opts.Config = image.Config
			}
		} else {
			opts.Source = "vm:" + id
		}
		state, err := srvState.images.SaveRootFS(r.Context(), name, root, opts)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, state)
	})
	mux.HandleFunc("POST /vm/start", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var req client.StartInstanceRequest
		if err := decodeOptionalJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if opts.NormalizeStartRequest != nil {
			if err := opts.NormalizeStartRequest(&req, srvState); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
		}
		bootTimeout := bootTimeoutFromRequest(req.TimeoutSeconds)
		bootCtx, cancel := context.WithTimeout(r.Context(), bootTimeout)
		defer cancel()
		bootEvents := newBootEventWriter(w)
		defer bootEvents.Close()
		timingLog("POST /vm/start decode took=%s", time.Since(start))
		var startImage *oci.Image
		builtInBSDImage := isBuiltInBSDImage(req.Image)
		if imageName := strings.TrimSpace(req.Image); imageName != "" {
			if builtInBSDImage {
				if wantsBootEventStream(r) {
					bootEvents.Write(client.BootEvent{Kind: "status", Message: fmt.Sprintf("validated image %s", imageName)})
				}
			} else if _, err := srvState.images.Get(imageName); err != nil {
				msg := fmt.Sprintf("image %q is not available", imageName)
				if wantsBootEventStream(r) {
					bootEvents.Write(client.BootEvent{Kind: "error", Error: msg})
					return
				}
				writeError(w, http.StatusBadRequest, fmt.Errorf("%s", msg))
				return
			} else {
				image, err := srvState.images.Open(imageName)
				if err != nil {
					msg := fmt.Sprintf("image %q is not available: %s", imageName, err)
					if wantsBootEventStream(r) {
						bootEvents.Write(client.BootEvent{Kind: "error", Error: msg})
						return
					}
					writeError(w, http.StatusBadRequest, fmt.Errorf("%s", msg))
					return
				}
				startImage = image
				if wantsBootEventStream(r) {
					bootEvents.Write(client.BootEvent{Kind: "status", Message: fmt.Sprintf("validated image %s", imageName)})
				}
			}
		}
		if err := vm.Supports(); err != nil {
			if wantsBootEventStream(r) {
				bootEvents.Write(client.BootEvent{Kind: "error", Error: err.Error()})
				return
			}
			writeError(w, http.StatusServiceUnavailable, err)
			return
		}
		if !builtInBSDImage && srvState.kernel.Status().Status != "downloaded" {
			if wantsBootEventStream(r) {
				bootEvents.Write(client.BootEvent{Kind: "status", Message: "ensuring kernel is available"})
			}
			if err := srvState.kernel.Ensure(bootCtx); err != nil {
				if wantsBootEventStream(r) {
					if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
						bootEvents.Write(client.BootEvent{Kind: "error", Error: fmt.Sprintf("vm boot timed out after %s", bootTimeout)})
						return
					}
					bootEvents.Write(client.BootEvent{Kind: "error", Error: err.Error()})
					return
				}
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
					writeError(w, http.StatusGatewayTimeout, fmt.Errorf("vm boot timed out after %s", bootTimeout))
					return
				}
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		timingLog("POST /vm/start kernel ensure/status took=%s", time.Since(start))
		if vm.NeedsAMD64Emulation(startImage) {
			if wantsBootEventStream(r) {
				bootEvents.Write(client.BootEvent{Kind: "status", Message: "preparing amd64 emulator"})
				_, err := srvState.kernel.ExtractPackageFileWithProgress(
					bootCtx,
					"community",
					"qemu-x86_64",
					"usr/bin/qemu-x86_64",
					func(event client.ProgressEvent) {
						_ = bootEvents.Write(client.BootEvent{Kind: "status", Message: bootProgressMessage("preparing amd64 emulator", event)})
					},
				)
				if err != nil {
					if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
						bootEvents.Write(client.BootEvent{Kind: "error", Error: fmt.Sprintf("vm boot timed out after %s", bootTimeout)})
						return
					}
					bootEvents.Write(client.BootEvent{Kind: "error", Error: err.Error()})
					return
				}
			} else if _, err := vm.PrepareAMD64Emulator(bootCtx, startImage, srvState.kernel.ExtractPackageFile); err != nil {
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
					writeError(w, http.StatusGatewayTimeout, fmt.Errorf("vm boot timed out after %s", bootTimeout))
					return
				}
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		if wantsBootEventStream(r) {
			writeStreamEvent := bootEvents.Write

			writeStreamEvent(client.BootEvent{Kind: "status", Message: "starting VM"})
			state, err := srvState.vms.StartBlankStream(bootCtx, req, func(event client.BootEvent) error {
				return writeStreamEvent(event)
			})
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
					_ = writeStreamEvent(client.BootEvent{Kind: "error", Error: fmt.Sprintf("vm boot timed out after %s", bootTimeout)})
					return
				}
				_ = writeStreamEvent(client.BootEvent{Kind: "error", Error: err.Error()})
				return
			}
			timingLog("POST /vm/start vms.StartBlankStream took=%s", time.Since(start))
			_ = writeStreamEvent(client.BootEvent{Kind: "ready", State: state})
			timingLog("POST /vm/start total=%s", time.Since(start))
			return
		}
		state, err := srvState.vms.StartBlank(bootCtx, req)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
				if req.Dmesg {
					writeError(w, http.StatusGatewayTimeout, fmt.Errorf("vm boot timed out after %s: %w", bootTimeout, err))
					return
				}
				writeError(w, http.StatusGatewayTimeout, fmt.Errorf("vm boot timed out after %s", bootTimeout))
				return
			}
			writeError(w, http.StatusBadRequest, err)
			return
		}
		timingLog("POST /vm/start vms.StartBlank took=%s", time.Since(start))
		writeJSON(w, http.StatusOK, state)
		timingLog("POST /vm/start total=%s", time.Since(start))
	})
	mux.HandleFunc("POST /vm", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var req client.CreateInstanceRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if opts.NormalizeCreateRequest != nil {
			if err := opts.NormalizeCreateRequest(&req, srvState); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
		}
		bootTimeout := bootTimeoutFromRequest(req.TimeoutSeconds)
		bootCtx, cancel := context.WithTimeout(r.Context(), bootTimeout)
		defer cancel()
		bootEvents := newBootEventWriter(w)
		defer bootEvents.Close()
		timingLog("POST /vm decode took=%s image=%q", time.Since(start), req.Image)
		builtInBSDImage := isBuiltInBSDImage(req.Image)
		if !builtInBSDImage {
			if _, err := srvState.images.Get(req.Image); err != nil {
				if wantsBootEventStream(r) {
					bootEvents.Write(client.BootEvent{Kind: "error", Error: fmt.Sprintf("image %q is not available", req.Image)})
					return
				}
				writeError(w, http.StatusBadRequest, fmt.Errorf("image %q is not available", req.Image))
				return
			}
			timingLog("POST /vm image lookup took=%s", time.Since(start))
		} else {
			timingLog("POST /vm builtin image lookup took=%s", time.Since(start))
		}
		if wantsBootEventStream(r) {
			bootEvents.Write(client.BootEvent{Kind: "status", Message: fmt.Sprintf("validated image %s", req.Image)})
		}
		if err := vm.Supports(); err != nil {
			if wantsBootEventStream(r) {
				bootEvents.Write(client.BootEvent{Kind: "error", Error: err.Error()})
				return
			}
			writeError(w, http.StatusServiceUnavailable, err)
			return
		}
		if !builtInBSDImage && srvState.kernel.Status().Status != "downloaded" {
			if wantsBootEventStream(r) {
				bootEvents.Write(client.BootEvent{Kind: "status", Message: "ensuring kernel is available"})
			}
			if err := srvState.kernel.Ensure(bootCtx); err != nil {
				if wantsBootEventStream(r) {
					if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
						bootEvents.Write(client.BootEvent{Kind: "error", Error: fmt.Sprintf("vm boot timed out after %s", bootTimeout)})
						return
					}
					bootEvents.Write(client.BootEvent{Kind: "error", Error: err.Error()})
					return
				}
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
					writeError(w, http.StatusGatewayTimeout, fmt.Errorf("vm boot timed out after %s", bootTimeout))
					return
				}
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		timingLog("POST /vm kernel ensure/status took=%s", time.Since(start))
		if wantsBootEventStream(r) {
			writeStreamEvent := bootEvents.Write

			writeStreamEvent(client.BootEvent{Kind: "status", Message: fmt.Sprintf("starting VM for %s", req.Image)})
			state, err := srvState.vms.StartStream(bootCtx, req, func(event client.BootEvent) error {
				return writeStreamEvent(event)
			})
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
					_ = writeStreamEvent(client.BootEvent{Kind: "error", Error: fmt.Sprintf("vm boot timed out after %s", bootTimeout)})
					return
				}
				_ = writeStreamEvent(client.BootEvent{Kind: "error", Error: err.Error()})
				return
			}
			timingLog("POST /vm vms.StartStream took=%s", time.Since(start))
			_ = writeStreamEvent(client.BootEvent{Kind: "ready", State: state})
			timingLog("POST /vm total=%s", time.Since(start))
			return
		}
		state, err := srvState.vms.Start(bootCtx, req)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(bootCtx.Err(), context.DeadlineExceeded) {
				if req.Dmesg {
					writeError(w, http.StatusGatewayTimeout, fmt.Errorf("vm boot timed out after %s: %w", bootTimeout, err))
					return
				}
				writeError(w, http.StatusGatewayTimeout, fmt.Errorf("vm boot timed out after %s", bootTimeout))
				return
			}
			writeError(w, http.StatusBadRequest, err)
			return
		}
		timingLog("POST /vm vms.Start took=%s", time.Since(start))
		writeJSON(w, http.StatusOK, state)
		timingLog("POST /vm total=%s", time.Since(start))
	})
	mux.HandleFunc("POST /vm/shutdown", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if err := srvState.vms.ShutdownInstance(r.Context(), id); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, srvState.vms.StatusOf(id))
	})
	mux.HandleFunc("GET /vm/console", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		history, err := srvState.vms.ConsoleHistory(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, client.ConsoleHistoryResponse{History: history})
	})
	mux.HandleFunc("POST /vm/forward", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		var req client.PortForward
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := srvState.vms.AddPortForwardTo(r.Context(), id, req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, req)
	})
	mux.HandleFunc("POST /vm/service-proxy-port", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		var req client.ServiceProxyPortRequest
		if err := decodeRequiredJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := srvState.vms.AllowServiceProxyPortTo(r.Context(), id, req.Port); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, req)
	})
	mux.HandleFunc("POST /vm/run", func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeRunRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if opts.NormalizeRunRequest != nil {
			if err := opts.NormalizeRunRequest(&req, srvState); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
		}
		builtInBSDImage := isBuiltInBSDImage(req.Image)
		if req.Image != "" && !builtInBSDImage {
			if _, err := srvState.images.Open(req.Image); err != nil {
				writeError(w, http.StatusBadRequest, fmt.Errorf("image %q is not available", req.Image))
				return
			}
		}
		if !builtInBSDImage && srvState.kernel.Status().Status != "downloaded" && (req.Image != "" || srvState.vms.Status().Status == "running") {
			if err := srvState.kernel.Ensure(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		runCtx, cancelRun := runRequestContext(r.Context(), req)
		defer cancelRun()
		if wantsExecEventStream(r) {
			writeRunEventStream(w, runCtx, srvState.vms, req)
			return
		}
		resp, err := srvState.vms.Run(runCtx, req)
		if err != nil {
			if req.TimeoutSeconds > 0 && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
				resp.ExitCode = 124
				resp.Output += fmt.Sprintf("\n[ccvm] command timed out after %.1fs\n", req.TimeoutSeconds)
				writeJSON(w, http.StatusOK, resp)
				return
			}
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})
	mux.Handle("GET /vm/run", websocket.Server{
		Handshake: validateWebSocketOrigin,
		Handler: func(ws *websocket.Conn) {
			ws.MaxPayloadBytes = maxWebSocketMessageBytes
			_ = ws.SetDeadline(time.Time{})
			serveRunWebSocket(ws, func(ctx context.Context, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
				return srvState.vms.Stream(ctx, req, inputs, onEvent)
			})
		},
	})
	mux.Handle("GET /vm/run/stream", websocket.Server{
		Handshake: validateWebSocketOrigin,
		Handler: func(ws *websocket.Conn) {
			ws.MaxPayloadBytes = maxWebSocketMessageBytes
			_ = ws.SetDeadline(time.Time{})
			serveRunRequestWebSocket(ws, srvState, opts.NormalizeRunRequest, func(ctx context.Context, req client.RunRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
				runCtx, cancel := runRequestContext(ctx, req)
				defer cancel()
				if err := srvState.vms.RunStream(runCtx, req, inputs, onEvent); err != nil {
					if req.TimeoutSeconds > 0 && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
						if eventErr := onEvent(client.ExecEvent{Kind: "stderr", Output: fmt.Sprintf("\n[ccvm] command timed out after %.1fs\n", req.TimeoutSeconds)}); eventErr != nil {
							return eventErr
						}
						return onEvent(client.ExecEvent{Kind: "exit", ExitCode: 124})
					}
					return err
				}
				return nil
			})
		},
	})
	return mux.ServeMux, append([]apiRoute(nil), mux.routes...)
}

func pprofEnabled() bool {
	return strings.TrimSpace(os.Getenv("CCX3_DEBUG_PPROF")) != "" ||
		strings.TrimSpace(os.Getenv("CCX3_PPROF")) != ""
}

func resolveCacheDir(arg string) (string, error) {
	if arg != "" {
		return arg, cachepath.EnsurePrivateRoot(arg)
	}
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	dir := filepath.Join(userCacheDir, "ccx3")
	return dir, cachepath.EnsurePrivateRoot(dir)
}

func registerPprofHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	mux.Handle("GET /debug/pprof/allocs", pprof.Handler("allocs"))
	mux.Handle("GET /debug/pprof/block", pprof.Handler("block"))
	mux.Handle("GET /debug/pprof/goroutine", pprof.Handler("goroutine"))
	mux.Handle("GET /debug/pprof/heap", pprof.Handler("heap"))
	mux.Handle("GET /debug/pprof/mutex", pprof.Handler("mutex"))
	mux.Handle("GET /debug/pprof/threadcreate", pprof.Handler("threadcreate"))
}

func watchdogLeaseDuration(seconds float64) (time.Duration, error) {
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0, fmt.Errorf("watchdog lease timeout must be a finite positive duration")
	}
	nanoseconds := seconds * float64(time.Second)
	if nanoseconds >= float64(uint64(1)<<63) {
		return 0, fmt.Errorf("watchdog lease timeout is too large")
	}
	timeout := time.Duration(nanoseconds)
	if timeout <= 0 {
		return 0, fmt.Errorf("watchdog lease timeout is below timer resolution")
	}
	return timeout, nil
}

type websocketOriginError struct {
	Origin string
	Host   string
}

func (e *websocketOriginError) Error() string {
	return fmt.Sprintf("websocket origin %q does not match request host %q", e.Origin, e.Host)
}

func validateWebSocketOrigin(_ *websocket.Config, r *http.Request) error {
	origins := r.Header.Values("Origin")
	if len(origins) == 0 {
		return nil
	}
	if len(origins) != 1 {
		return &websocketOriginError{Origin: strings.Join(origins, ", "), Host: r.Host}
	}
	origin, err := url.Parse(origins[0])
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		return &websocketOriginError{Origin: origins[0], Host: r.Host}
	}
	expectedScheme := "http"
	if r.TLS != nil {
		expectedScheme = "https"
	}
	if !strings.EqualFold(origin.Scheme, expectedScheme) {
		return &websocketOriginError{Origin: origins[0], Host: r.Host}
	}
	originAuthority, err := canonicalWebSocketAuthority(origin.Host, expectedScheme)
	if err != nil {
		return &websocketOriginError{Origin: origins[0], Host: r.Host}
	}
	hostAuthority, err := canonicalWebSocketAuthority(r.Host, expectedScheme)
	if err != nil || originAuthority != hostAuthority {
		return &websocketOriginError{Origin: origins[0], Host: r.Host}
	}
	return nil
}

func canonicalWebSocketAuthority(authority, scheme string) (string, error) {
	parsed, err := url.Parse("//" + authority)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" {
		return "", fmt.Errorf("invalid authority")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" {
		return "", fmt.Errorf("empty host")
	}
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	}
	port := parsed.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(host, port), nil
}

func sharedRuntimeRoot() string {
	if root := strings.TrimSpace(os.Getenv("CCX3_RUNTIME_SHARED_CACHE_DIR")); root != "" {
		return root
	}
	userCacheDir, err := os.UserCacheDir()
	if err != nil || userCacheDir == "" {
		return filepath.Join(os.TempDir(), "ccx3-runtime")
	}
	return filepath.Join(userCacheDir, "ccx3", "runtime")
}

func decodeRequiredJSON(r *http.Request, dst any) error {
	return decodeJSON(r, dst, true)
}

func decodeOptionalJSON(r *http.Request, dst any) error {
	return decodeJSON(r, dst, false)
}

func decodeJSON(r *http.Request, dst any, required bool) error {
	if r.Body == nil || r.ContentLength == 0 {
		if required {
			return fmt.Errorf("request body is required")
		}
		return nil
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		if !required && errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("decode request body: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode request body: multiple JSON values")
		}
		return fmt.Errorf("decode request body: trailing data: %w", err)
	}
	return nil
}

func decodeRunRequest(r *http.Request) (client.RunRequest, error) {
	var req client.RunRequest
	if err := decodeRequiredJSON(r, &req); err != nil {
		return req, err
	}
	return req, nil
}

func runRequestContext(parent context.Context, req client.RunRequest) (context.Context, context.CancelFunc) {
	if req.TimeoutSeconds <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, time.Duration(req.TimeoutSeconds*float64(time.Second)))
}

func serveRunWebSocket(ws *websocket.Conn, runner func(context.Context, client.ExecRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error) {
	defer ws.Close()

	var req client.ExecRequest
	if err := websocket.JSON.Receive(ws, &req); err != nil {
		_ = websocket.JSON.Send(ws, client.ExecEvent{Kind: "error", Error: fmt.Sprintf("decode exec request: %v", err)})
		return
	}

	inputs := make(chan client.ExecInput, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		defer close(inputs)
		defer cancel()
		for {
			var input client.ExecInput
			if err := websocket.JSON.Receive(ws, &input); err != nil {
				return
			}
			select {
			case inputs <- input:
			case <-ctx.Done():
				return
			}
		}
	}()

	err := runner(ctx, req, inputs, func(event client.ExecEvent) error {
		event = sanitizeExecEventForJSON(event)
		return websocket.JSON.Send(ws, event)
	})
	if err != nil {
		_ = websocket.JSON.Send(ws, client.ExecEvent{Kind: "error", Error: err.Error()})
	}
}

func serveRunRequestWebSocket(ws *websocket.Conn, runtime RuntimeView, normalize func(*client.RunRequest, RuntimeView) error, runner func(context.Context, client.RunRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error) {
	defer ws.Close()

	var req client.RunRequest
	if err := websocket.JSON.Receive(ws, &req); err != nil {
		_ = websocket.JSON.Send(ws, client.ExecEvent{Kind: "error", Error: fmt.Sprintf("decode run request: %v", err)})
		return
	}
	if normalize != nil {
		if err := normalize(&req, runtime); err != nil {
			_ = websocket.JSON.Send(ws, client.ExecEvent{Kind: "error", Error: err.Error()})
			return
		}
	}

	inputs := make(chan client.ExecInput, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		defer close(inputs)
		defer cancel()
		for {
			var input client.ExecInput
			if err := websocket.JSON.Receive(ws, &input); err != nil {
				return
			}
			select {
			case inputs <- input:
			case <-ctx.Done():
				return
			}
		}
	}()

	err := runner(ctx, req, inputs, func(event client.ExecEvent) error {
		event = sanitizeExecEventForJSON(event)
		return websocket.JSON.Send(ws, event)
	})
	if err != nil {
		_ = websocket.JSON.Send(ws, client.ExecEvent{Kind: "error", Error: err.Error()})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func wantsExecEventStream(r *http.Request) bool {
	if r.URL.Query().Get("stream") == "1" {
		return true
	}
	return r.Header.Get("Accept") == "application/x-ndjson"
}

func wantsBootEventStream(r *http.Request) bool {
	if r.URL.Query().Get("stream") == "1" {
		return true
	}
	return r.Header.Get("Accept") == "application/x-ndjson"
}

func wantsProgressStream(r *http.Request) bool {
	if r.URL.Query().Get("stream") == "1" {
		return true
	}
	return r.Header.Get("Accept") == "application/x-ndjson"
}

func writeExecEventStream(w http.ResponseWriter, resp client.ExecResponse) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	if resp.Output != "" {
		_ = enc.Encode(client.ExecEvent{Kind: "output", Output: resp.Output})
	}
	_ = enc.Encode(client.ExecEvent{Kind: "exit", ExitCode: resp.ExitCode})
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeRunEventStream(w http.ResponseWriter, ctx context.Context, manager *vm.Manager, req client.RunRequest) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	err := manager.RunStream(ctx, req, nil, func(event client.ExecEvent) error {
		event = sanitizeExecEventForJSON(event)
		if err := enc.Encode(event); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		if req.TimeoutSeconds > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			_ = enc.Encode(client.ExecEvent{Kind: "stderr", Output: fmt.Sprintf("\n[ccvm] command timed out after %.1fs\n", req.TimeoutSeconds)})
			_ = enc.Encode(client.ExecEvent{Kind: "exit", ExitCode: 124})
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		_ = enc.Encode(client.ExecEvent{Kind: "error", Error: err.Error()})
	}
	if flusher != nil {
		flusher.Flush()
	}
}

func sanitizeExecEventForJSON(event client.ExecEvent) client.ExecEvent {
	if len(event.Data) == 0 {
		return event
	}
	if utf8.Valid(event.Data) {
		event.Data = nil
		return event
	}
	if event.Output != "" {
		event.Output = ""
	}
	return event
}

type bootEventWriter struct {
	mu   sync.Mutex
	w    http.ResponseWriter
	open bool
}

func newBootEventWriter(w http.ResponseWriter) *bootEventWriter {
	return &bootEventWriter{w: w, open: true}
}

func (w *bootEventWriter) Close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.open = false
	w.mu.Unlock()
}

func (w *bootEventWriter) Write(event client.BootEvent) (err error) {
	if w == nil {
		return fmt.Errorf("boot event writer is unavailable")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.open {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("write boot event: %v", recovered)
		}
	}()
	w.w.Header().Set("Content-Type", "application/x-ndjson")
	if event.Kind == "" {
		event.Kind = "status"
	}
	if err := json.NewEncoder(w.w).Encode(event); err != nil {
		return err
	}
	if flusher, ok := w.w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func bootProgressMessage(prefix string, event client.ProgressEvent) string {
	parts := []string{prefix}
	if event.Artifact != "" {
		parts = append(parts, event.Artifact)
	}
	if event.Blob != "" {
		parts = append(parts, event.Blob)
	}
	if event.Status != "" {
		parts = append(parts, event.Status)
	}
	if event.BytesDownloaded > 0 || event.BytesTotal > 0 {
		if event.BytesTotal > 0 {
			parts = append(parts, fmt.Sprintf("%d/%d bytes", event.BytesDownloaded, event.BytesTotal))
		} else {
			parts = append(parts, fmt.Sprintf("%d bytes", event.BytesDownloaded))
		}
	}
	if event.FilesDownloaded > 0 || event.FilesTotal > 0 {
		if event.FilesTotal > 0 {
			parts = append(parts, fmt.Sprintf("%d/%d files", event.FilesDownloaded, event.FilesTotal))
		} else {
			parts = append(parts, fmt.Sprintf("%d files", event.FilesDownloaded))
		}
	}
	if event.Error != "" {
		parts = append(parts, event.Error)
	}
	return strings.Join(parts, ": ")
}

func writeProgressEvent(w http.ResponseWriter, event client.ProgressEvent) error {
	w.Header().Set("Content-Type", "application/x-ndjson")
	if _, ok := w.(http.Flusher); ok {
		w.WriteHeader(http.StatusOK)
	}
	if err := json.NewEncoder(w).Encode(event); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		status = http.StatusRequestEntityTooLarge
	}
	writeJSON(w, status, client.ErrorResponse{Error: err.Error()})
}

func cvmfsTarget(mirror, repo, innerPath string) string {
	repo = strings.TrimSpace(repo)
	pathValue := ensureAbsolutePath(innerPath)
	mirror = strings.TrimRight(strings.TrimSpace(mirror), "/")
	if mirror == "" {
		return fmt.Sprintf("cvmfs://%s%s", repo, pathValue)
	}
	mirror = ensureCVMFSMirrorPath(mirror)
	return fmt.Sprintf("%s/%s%s", mirror, repo, pathValue)
}

func cvmfsSourceMirrors(source *client.ImageSource) []string {
	if source == nil || strings.ToLower(strings.TrimSpace(source.Type)) != "cvmfs" {
		return nil
	}
	return source.Mirrors
}

func cvmfsRequestCacheDir(requested string, fallback string) string {
	if dir := strings.TrimSpace(requested); dir != "" {
		return dir
	}
	return fallback
}

func pathJoin(base, name string) string {
	base = ensureAbsolutePath(base)
	if base == "/" {
		return "/" + strings.TrimPrefix(name, "/")
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimPrefix(name, "/")
}

func ensureAbsolutePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if strings.HasPrefix(value, "/") {
		return value
	}
	return "/" + value
}

func ensureCVMFSMirrorPath(mirror string) string {
	mirror = strings.TrimRight(strings.TrimSpace(mirror), "/")
	u, err := url.Parse(mirror)
	if err != nil {
		if !strings.HasSuffix(mirror, "/cvmfs") {
			return mirror + "/cvmfs"
		}
		return mirror
	}
	if !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/cvmfs") {
		u.Path = strings.TrimRight(u.Path, "/") + "/cvmfs"
	}
	return strings.TrimRight(u.String(), "/")
}
