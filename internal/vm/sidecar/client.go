package sidecar

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"j5.nz/cc/client"
)

type Client struct {
	conn  net.Conn
	codec *WorkerCodec
	hello WorkerHello

	idMu sync.Mutex
	next uint64

	pendingMu sync.Mutex
	pending   map[uint64]*workerCall
	closed    bool
	recvErr   error
}

// ErrWorkerCallOverflow reports that a caller stopped consuming frames quickly
// enough for its bounded delivery queue to fill.
var ErrWorkerCallOverflow = errors.New("sidecar worker call frame buffer overflow")

type TerminationUnconfirmedError struct {
	RequestID uint64
	Cause     error
}

func (e *TerminationUnconfirmedError) Error() string {
	return fmt.Sprintf("sidecar request %d termination is unconfirmed; worker connection was quarantined: %v", e.RequestID, e.Cause)
}

func (e *TerminationUnconfirmedError) Unwrap() error { return e.Cause }

type workerCall struct {
	frames        chan WorkerFrame
	done          chan struct{}
	inputAck      chan struct{}
	abort         sync.Once
	aborting      chan struct{}
	terminal      sync.Once
	terminalReady chan struct{}
	finishOnce    sync.Once
	errMu         sync.Mutex
	err           error
}

type WorkerRequirements struct {
	SupportsFSRPC bool
	SupportsL2    bool
}

type WorkerProtocolVersionError struct {
	Received  int
	Supported int
}

func (e *WorkerProtocolVersionError) Error() string {
	return fmt.Sprintf("unsupported sidecar worker protocol version %d (supported version: %d)", e.Received, e.Supported)
}

type MissingWorkerCapabilityError struct {
	Capability string
}

func (e *MissingWorkerCapabilityError) Error() string {
	return fmt.Sprintf("sidecar worker does not support required capability %q", e.Capability)
}

func newWorkerCall() *workerCall {
	return &workerCall{
		frames:        make(chan WorkerFrame, 256),
		done:          make(chan struct{}),
		inputAck:      make(chan struct{}, 1),
		aborting:      make(chan struct{}),
		terminalReady: make(chan struct{}),
	}
}

func (c *workerCall) finish(err error) {
	c.finishOnce.Do(func() {
		c.errMu.Lock()
		c.err = err
		c.errMu.Unlock()
		close(c.done)
	})
}

func (c *workerCall) result() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	return c.err
}

const (
	workerConnectTimeout        = 5 * time.Second
	workerRetryDelay            = 10 * time.Millisecond
	workerHelloTimeout          = 5 * time.Second
	workerCancelDeliveryTimeout = time.Second
)

func DialWorker(ctx context.Context, socketPath string) (*Client, error) {
	return dialWorker(ctx, socketPath, nil, WorkerRequirements{})
}

func DialWorkerWithRequirements(ctx context.Context, socketPath string, requirements WorkerRequirements) (*Client, error) {
	return dialWorker(ctx, socketPath, nil, requirements)
}

func DialWorkerTLS(ctx context.Context, endpoint, configPath string) (*Client, error) {
	return DialWorkerTLSWithRequirements(ctx, endpoint, configPath, WorkerRequirements{})
}

func DialWorkerTLSWithRequirements(ctx context.Context, endpoint, configPath string, requirements WorkerRequirements) (*Client, error) {
	security, err := LoadWorkerClientSecurity(configPath)
	if err != nil {
		return nil, err
	}
	return dialWorker(ctx, endpoint, security, requirements)
}

func dialWorker(ctx context.Context, socketPath string, security *WorkerTransportSecurity, requirements WorkerRequirements) (*Client, error) {
	target, err := workerDialTarget(socketPath)
	if err != nil {
		return nil, err
	}
	if target.secure && security == nil {
		return nil, &WorkerSecurityError{Endpoint: socketPath, Reason: WorkerSecurityTLSConfigRequired}
	}
	if !target.secure && security != nil {
		return nil, &WorkerSecurityError{Endpoint: socketPath, Reason: WorkerSecurityInvalidTLSConfig, Detail: "TLS configuration was provided for a Unix socket"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := dialWorkerTargetConnection(ctx, target, workerConnectTimeout, (&net.Dialer{}).DialContext)
	if err != nil {
		return nil, fmt.Errorf("dial sidecar worker control socket: %w", err)
	}
	if target.secure {
		conn, err = handshakeWorkerClient(ctx, conn, socketPath, security)
		if err != nil {
			return nil, err
		}
	}
	worker := &Client{conn: conn, codec: NewWorkerCodec(conn), pending: map[uint64]*workerCall{}}
	frame, err := receiveWorkerHello(ctx, conn, worker.codec, workerHelloTimeout)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read sidecar worker hello: %w", err)
	}
	if frame.Type != WorkerFrameHello {
		_ = conn.Close()
		return nil, fmt.Errorf("sidecar worker sent %q before hello", frame.Type)
	}
	var hello WorkerHello
	if err := frame.DecodePayload(&hello); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("decode sidecar worker hello: %w", err)
	}
	if err := validateWorkerHello(hello, requirements); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if target.secure && hello.WorkerID != security.Scope {
		_ = conn.Close()
		return nil, &WorkerSecurityError{
			Endpoint: socketPath,
			Reason:   WorkerSecurityPeerScopeMismatch,
			Detail:   "worker hello identity does not match the authenticated certificate scope",
		}
	}
	worker.hello = hello
	go worker.receiveLoop()
	return worker, nil
}

func validateWorkerHello(hello WorkerHello, requirements WorkerRequirements) error {
	if hello.Version != WorkerProtocolVersion {
		return &WorkerProtocolVersionError{Received: hello.Version, Supported: WorkerProtocolVersion}
	}
	if requirements.SupportsFSRPC && !hello.Capabilities.SupportsFSRPC {
		return &MissingWorkerCapabilityError{Capability: "filesystem-rpc"}
	}
	if requirements.SupportsL2 && !hello.Capabilities.SupportsL2 {
		return &MissingWorkerCapabilityError{Capability: "l2-networking"}
	}
	return nil
}

func receiveWorkerHello(ctx context.Context, conn net.Conn, codec *WorkerCodec, timeout time.Duration) (WorkerFrame, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return WorkerFrame{}, err
	}

	cancelWatchDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-cancelWatchDone:
		}
	}()
	frame, err := codec.Receive()
	close(cancelWatchDone)
	if clearErr := conn.SetReadDeadline(time.Time{}); err == nil && clearErr != nil {
		err = clearErr
	}
	if ctx.Err() != nil {
		return WorkerFrame{}, ctx.Err()
	}
	return frame, err
}

type workerDialEndpoint struct {
	network string
	address string
	secure  bool
}

type workerDialContextFunc func(context.Context, string, string) (net.Conn, error)

func dialWorkerConnection(ctx context.Context, socketPath string, timeout time.Duration, dial workerDialContextFunc) (net.Conn, error) {
	target, err := workerDialTarget(socketPath)
	if err != nil {
		return nil, err
	}
	return dialWorkerTargetConnection(ctx, target, timeout, dial)
}

func dialWorkerTargetConnection(ctx context.Context, target workerDialEndpoint, timeout time.Duration, dial workerDialContextFunc) (net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	retry := time.NewTimer(workerRetryDelay)
	if !retry.Stop() {
		<-retry.C
	}
	defer retry.Stop()
	for {
		conn, err := dial(connectCtx, target.network, target.address)
		if err == nil {
			return conn, nil
		}
		if connectCtx.Err() != nil {
			return nil, connectCtx.Err()
		}
		retry.Reset(workerRetryDelay)
		select {
		case <-connectCtx.Done():
			return nil, connectCtx.Err()
		case <-retry.C:
		}
	}
}

func workerDialTarget(address string) (workerDialEndpoint, error) {
	if strings.HasPrefix(address, "tcp://") {
		return workerDialEndpoint{}, &WorkerSecurityError{
			Endpoint: address,
			Reason:   WorkerSecurityPlaintextTCPRejected,
		}
	}
	if strings.HasPrefix(address, WorkerTLSScheme) {
		target := strings.TrimPrefix(address, WorkerTLSScheme)
		if _, _, err := net.SplitHostPort(target); err != nil {
			return workerDialEndpoint{}, fmt.Errorf("parse worker TLS endpoint: %w", err)
		}
		return workerDialEndpoint{network: "tcp", address: target, secure: true}, nil
	}
	return workerDialEndpoint{network: "unix", address: address}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) Hello() WorkerHello {
	if c == nil {
		return WorkerHello{}
	}
	return c.hello
}

func (c *Client) receiveLoop() {
	for {
		frame, err := c.codec.Receive()
		if err != nil {
			c.closePending(err)
			return
		}
		c.pendingMu.Lock()
		call := c.pending[frame.ID]
		c.pendingMu.Unlock()
		if call == nil {
			continue
		}
		if frame.Type == WorkerFrameExecInputAck {
			select {
			case call.inputAck <- struct{}{}:
			default:
			}
			continue
		}
		select {
		case call.frames <- frame:
			if frame.Type == WorkerFrameDone || frame.Type == WorkerFrameError {
				call.terminal.Do(func() { close(call.terminalReady) })
			}
		default:
			err := fmt.Errorf("%w for request %d", ErrWorkerCallOverflow, frame.ID)
			id := frame.ID
			if call.beginAbort() {
				go c.finishAbortLiveCall(id, call, err)
			}
		}
	}
}

func (c *Client) registerCall(id uint64) (*workerCall, error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if c.closed {
		if c.recvErr != nil {
			return nil, c.recvErr
		}
		return nil, fmt.Errorf("sidecar worker is closed")
	}
	call := newWorkerCall()
	c.pending[id] = call
	return call, nil
}

func (c *Client) unregisterCall(id uint64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

func (c *Client) failCall(id uint64, call *workerCall, err error) bool {
	c.pendingMu.Lock()
	if c.pending[id] != call {
		c.pendingMu.Unlock()
		return false
	}
	delete(c.pending, id)
	c.pendingMu.Unlock()
	call.finish(err)
	return true
}

func (c *Client) closePending(err error) {
	if err == nil {
		err = fmt.Errorf("sidecar worker is closed")
	}
	c.pendingMu.Lock()
	if c.closed {
		c.pendingMu.Unlock()
		return
	}
	c.closed = true
	c.recvErr = err
	pending := c.pending
	c.pending = map[uint64]*workerCall{}
	c.pendingMu.Unlock()
	for _, call := range pending {
		call.finish(err)
	}
}

func (c *Client) Start(ctx context.Context, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (client.InstanceState, error) {
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

func (c *Client) StartBlank(ctx context.Context, req client.StartInstanceRequest, onEvent func(client.BootEvent) error) (client.InstanceState, error) {
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

func (c *Client) Status(ctx context.Context, id string) (client.InstanceState, error) {
	var resp WorkerStatusResponse
	err := c.call(ctx, WorkerFrameStatus, WorkerStatusRequest{ID: id}, nil, &resp)
	return resp.State, err
}

func (c *Client) Stop(ctx context.Context, id string) error {
	var resp WorkerStatusResponse
	return c.call(ctx, WorkerFrameStop, WorkerStopRequest{ID: id}, nil, &resp)
}

func (c *Client) Wait(ctx context.Context, id string) error {
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

func (c *Client) Flush(ctx context.Context, id string) error {
	var resp map[string]string
	return c.call(ctx, WorkerFrameFlush, WorkerFlushRequest{ID: id}, nil, &resp)
}

func (c *Client) AddShare(ctx context.Context, id string, share client.ShareMount) error {
	var resp map[string]string
	return c.call(ctx, WorkerFrameAddShare, WorkerAddShareRequest{ID: id, Share: share}, nil, &resp)
}

func (c *Client) ConsoleHistory(ctx context.Context, id string) (string, error) {
	var resp WorkerConsoleResponse
	err := c.call(ctx, WorkerFrameConsole, WorkerConsoleRequest{ID: id}, nil, &resp)
	return resp.History, err
}

func (c *Client) Exec(ctx context.Context, id string, req client.ExecRequest) ([]client.ExecEvent, error) {
	var events []client.ExecEvent
	err := c.ExecStream(ctx, id, req, nil, func(event client.ExecEvent) error {
		events = append(events, event)
		return nil
	})
	return events, err
}

func (c *Client) ExecStream(ctx context.Context, id string, req client.ExecRequest, inputs <-chan client.ExecInput, onEvent func(client.ExecEvent) error) error {
	if c == nil || c.codec == nil {
		return fmt.Errorf("sidecar worker is not connected")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestID := c.nextID()
	call, err := c.registerCall(requestID)
	if err != nil {
		return err
	}
	defer c.unregisterCall(requestID)

	frame, err := NewWorkerFrame(requestID, WorkerServiceControl, WorkerFrameExec, WorkerExecRequest{
		ID:          id,
		Request:     req,
		InputStream: inputs != nil,
	})
	if err != nil {
		return err
	}
	if err := c.codec.SendContext(ctx, frame); err != nil {
		return err
	}

	var stopInputs context.CancelFunc
	if inputs != nil {
		var inputCtx context.Context
		inputCtx, stopInputs = context.WithCancel(ctx)
		defer stopInputs()
		go c.forwardExecInputs(inputCtx, requestID, call, inputs)
	}

	for {
		got, err := c.nextFrame(ctx, call)
		if err != nil {
			if ctx.Err() != nil {
				return c.abortRequest(requestID, ctx.Err())
			}
			return err
		}
		switch got.Type {
		case WorkerFrameError:
			var workerErr WorkerError
			if err := got.DecodePayload(&workerErr); err != nil {
				return err
			}
			return fmt.Errorf("%s", workerErr.Error)
		case WorkerFrameDone:
			return nil
		case WorkerFrameEvent:
			var event client.ExecEvent
			if err := got.DecodePayload(&event); err != nil {
				return c.abortRequest(requestID, err)
			}
			if onEvent != nil {
				if err := onEvent(event); err != nil {
					return c.abortRequest(requestID, err)
				}
			}
		}
	}
}

func (c *Client) call(ctx context.Context, frameType string, payload any, onFrame func(WorkerFrame) error, out any) error {
	if c == nil || c.codec == nil {
		return fmt.Errorf("sidecar worker is not connected")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	id := c.nextID()
	call, err := c.registerCall(id)
	if err != nil {
		return err
	}
	defer c.unregisterCall(id)

	frame, err := NewWorkerFrame(id, WorkerServiceControl, frameType, payload)
	if err != nil {
		return err
	}
	if err := c.codec.SendContext(ctx, frame); err != nil {
		return err
	}
	for {
		got, err := c.nextFrame(ctx, call)
		if err != nil {
			if ctx.Err() != nil {
				return c.abortRequest(id, ctx.Err())
			}
			return err
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
					return c.abortRequest(id, err)
				}
			}
		}
	}
}

func (c *Client) nextFrame(ctx context.Context, call *workerCall) (WorkerFrame, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-call.aborting:
		<-call.done
		return WorkerFrame{}, call.result()
	default:
	}
	select {
	case <-call.terminalReady:
		return <-call.frames, nil
	default:
	}
	select {
	case <-call.done:
		return WorkerFrame{}, call.result()
	default:
	}
	select {
	case <-call.aborting:
		<-call.done
		return WorkerFrame{}, call.result()
	case <-call.terminalReady:
		return <-call.frames, nil
	case <-ctx.Done():
		select {
		case <-call.terminalReady:
			return <-call.frames, nil
		default:
		}
		return WorkerFrame{}, ctx.Err()
	case <-call.done:
		return WorkerFrame{}, call.result()
	case frame := <-call.frames:
		return frame, nil
	}
}

func (c *Client) nextID() uint64 {
	c.idMu.Lock()
	defer c.idMu.Unlock()
	c.next++
	return c.next
}

func (c *Client) forwardExecInputs(ctx context.Context, id uint64, call *workerCall, inputs <-chan client.ExecInput) {
	for {
		select {
		case <-ctx.Done():
			return
		case input, ok := <-inputs:
			if !ok {
				frame, err := NewWorkerFrame(id, WorkerServiceControl, WorkerFrameExecInput, WorkerExecInput{Closed: true})
				if err != nil {
					c.abortLiveCall(id, call, err)
					return
				}
				if err := c.codec.SendContext(ctx, frame); err != nil {
					if ctx.Err() == nil {
						c.abortLiveCall(id, call, err)
					}
					return
				}
				c.waitExecInputAck(ctx, call)
				return
			}
			frame, err := NewWorkerFrame(id, WorkerServiceControl, WorkerFrameExecInput, WorkerExecInput{Input: input})
			if err != nil {
				c.abortLiveCall(id, call, err)
				return
			}
			if err := c.codec.SendContext(ctx, frame); err != nil {
				if ctx.Err() == nil {
					c.abortLiveCall(id, call, err)
				}
				return
			}
			if !c.waitExecInputAck(ctx, call) {
				return
			}
		}
	}
}

func (c *Client) waitExecInputAck(ctx context.Context, call *workerCall) bool {
	select {
	case <-call.inputAck:
		return true
	case <-ctx.Done():
		return false
	case <-call.done:
		return false
	}
}

func (c *Client) sendCancel(id uint64) error {
	ctx, cancel := context.WithTimeout(context.Background(), workerCancelDeliveryTimeout)
	defer cancel()
	return c.sendCancelContext(ctx, id)
}

func (c *Client) sendCancelContext(ctx context.Context, id uint64) error {
	frame, err := NewWorkerFrame(id, WorkerServiceControl, WorkerFrameCancel, WorkerCancelRequest{})
	if err != nil {
		return err
	}
	return c.codec.SendContext(ctx, frame)
}

func (c *Client) quarantineCancellation(id uint64, cause error) error {
	err := &TerminationUnconfirmedError{RequestID: id, Cause: cause}
	c.closePending(err)
	_ = c.conn.Close()
	return err
}

// abortRequest is the single ownership transition for every local request
// failure. Once cancel delivery is uncertain, the worker may still be mutating
// a VM and the multiplexed stream is no longer safe to reuse.
func (c *Client) abortRequest(id uint64, cause error) error {
	if err := c.sendCancel(id); err != nil {
		return errors.Join(cause, c.quarantineCancellation(id, err))
	}
	return cause
}

func (c *Client) abortLiveCall(id uint64, call *workerCall, cause error) {
	if call.beginAbort() {
		c.finishAbortLiveCall(id, call, cause)
	}
}

func (c *workerCall) beginAbort() bool {
	started := false
	c.abort.Do(func() {
		close(c.aborting)
		started = true
	})
	return started
}

func (c *Client) finishAbortLiveCall(id uint64, call *workerCall, cause error) {
	err := c.abortRequest(id, cause)
	c.failCall(id, call, err)
}
