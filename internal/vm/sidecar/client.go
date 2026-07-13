package sidecar

import (
	"context"
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

	idMu sync.Mutex
	next uint64

	pendingMu sync.Mutex
	pending   map[uint64]chan workerFrameResult
	closed    bool
	recvErr   error
}

type workerFrameResult struct {
	frame WorkerFrame
	err   error
}

func DialWorker(ctx context.Context, socketPath string) (*Client, error) {
	return dialWorker(ctx, socketPath, nil)
}

func DialWorkerTLS(ctx context.Context, endpoint, configPath string) (*Client, error) {
	security, err := LoadWorkerClientSecurity(configPath)
	if err != nil {
		return nil, err
	}
	return dialWorker(ctx, endpoint, security)
}

func dialWorker(ctx context.Context, socketPath string, security *WorkerTransportSecurity) (*Client, error) {
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
	conn, err := (&net.Dialer{}).DialContext(ctx, target.network, target.address)
	if err != nil {
		return nil, fmt.Errorf("dial sidecar worker control socket: %w", err)
	}
	if target.secure {
		conn, err = handshakeWorkerClient(ctx, conn, socketPath, security)
		if err != nil {
			return nil, err
		}
	}
	worker := &Client{conn: conn, codec: NewWorkerCodec(conn), pending: map[uint64]chan workerFrameResult{}}
	frame, err := worker.codec.Receive()
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
	if hello.Version != WorkerProtocolVersion {
		_ = conn.Close()
		return nil, fmt.Errorf("sidecar worker protocol version %d is not supported", hello.Version)
	}
	if target.secure && hello.WorkerID != security.Scope {
		_ = conn.Close()
		return nil, &WorkerSecurityError{
			Endpoint: socketPath,
			Reason:   WorkerSecurityPeerScopeMismatch,
			Detail:   "worker hello identity does not match the authenticated certificate scope",
		}
	}
	go worker.receiveLoop()
	return worker, nil
}

type workerDialEndpoint struct {
	network string
	address string
	secure  bool
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

func (c *Client) receiveLoop() {
	for {
		frame, err := c.codec.Receive()
		if err != nil {
			c.closePending(err)
			return
		}
		c.pendingMu.Lock()
		ch := c.pending[frame.ID]
		c.pendingMu.Unlock()
		if ch == nil {
			continue
		}
		ch <- workerFrameResult{frame: frame}
	}
}

func (c *Client) registerCall(id uint64) (chan workerFrameResult, error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if c.closed {
		if c.recvErr != nil {
			return nil, c.recvErr
		}
		return nil, fmt.Errorf("sidecar worker is closed")
	}
	ch := make(chan workerFrameResult, 256)
	c.pending[id] = ch
	return ch, nil
}

func (c *Client) unregisterCall(id uint64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
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
	c.pending = map[uint64]chan workerFrameResult{}
	c.pendingMu.Unlock()
	for _, ch := range pending {
		ch <- workerFrameResult{err: err}
		close(ch)
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
	requestID := c.nextID()
	frames, err := c.registerCall(requestID)
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
	if err := c.codec.Send(frame); err != nil {
		return err
	}

	var stopInputs chan struct{}
	if inputs != nil {
		stopInputs = make(chan struct{})
		defer close(stopInputs)
		go c.forwardExecInputs(requestID, inputs, stopInputs)
	}

	cancelDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = c.conn.SetReadDeadline(time.Now())
			_ = c.sendCancel(requestID)
		case <-cancelDone:
		}
	}()
	defer func() {
		close(cancelDone)
		_ = c.conn.SetReadDeadline(time.Time{})
	}()

	for {
		result, err := c.nextFrame(ctx, frames)
		if err != nil {
			return err
		}
		got := result.frame
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

func (c *Client) call(ctx context.Context, frameType string, payload any, onFrame func(WorkerFrame) error, out any) error {
	if c == nil || c.codec == nil {
		return fmt.Errorf("sidecar worker is not connected")
	}
	id := c.nextID()
	frames, err := c.registerCall(id)
	if err != nil {
		return err
	}
	defer c.unregisterCall(id)

	cancelDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = c.sendCancel(id)
		case <-cancelDone:
		}
	}()
	defer func() {
		close(cancelDone)
	}()
	frame, err := NewWorkerFrame(id, WorkerServiceControl, frameType, payload)
	if err != nil {
		return err
	}
	if err := c.codec.Send(frame); err != nil {
		return err
	}
	for {
		result, err := c.nextFrame(ctx, frames)
		if err != nil {
			return err
		}
		got := result.frame
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

func (c *Client) nextFrame(ctx context.Context, frames <-chan workerFrameResult) (workerFrameResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return workerFrameResult{}, ctx.Err()
	case result, ok := <-frames:
		if !ok {
			return workerFrameResult{}, fmt.Errorf("sidecar worker is closed")
		}
		if result.err != nil {
			if ctx.Err() != nil {
				return workerFrameResult{}, ctx.Err()
			}
			return workerFrameResult{}, result.err
		}
		return result, nil
	}
}

func (c *Client) nextID() uint64 {
	c.idMu.Lock()
	defer c.idMu.Unlock()
	c.next++
	return c.next
}

func (c *Client) forwardExecInputs(id uint64, inputs <-chan client.ExecInput, stop <-chan struct{}) {
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

func (c *Client) sendCancel(id uint64) error {
	frame, err := NewWorkerFrame(id, WorkerServiceControl, WorkerFrameCancel, WorkerCancelRequest{})
	if err != nil {
		return err
	}
	return c.codec.Send(frame)
}
