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

type workerCall struct {
	frames chan WorkerFrame
	done   chan error
}

func newWorkerCall() *workerCall {
	return &workerCall{
		frames: make(chan WorkerFrame, 256),
		done:   make(chan error, 1),
	}
}

func (c *workerCall) finish(err error) {
	select {
	case c.done <- err:
	default:
	}
}

func DialWorker(ctx context.Context, socketPath string) (*Client, error) {
	var conn net.Conn
	var err error
	network, address := workerDialTarget(socketPath)
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
	worker := &Client{conn: conn, codec: NewWorkerCodec(conn), pending: map[uint64]*workerCall{}}
	frame, err := worker.codec.Receive()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read sidecar worker hello: %w", err)
	}
	if frame.Type != WorkerFrameHello {
		_ = conn.Close()
		return nil, fmt.Errorf("sidecar worker sent %q before hello", frame.Type)
	}
	go worker.receiveLoop()
	return worker, nil
}

func workerDialTarget(address string) (string, string) {
	if strings.HasPrefix(address, "tcp://") {
		return "tcp", strings.TrimPrefix(address, "tcp://")
	}
	return "unix", address
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
		call := c.pending[frame.ID]
		c.pendingMu.Unlock()
		if call == nil {
			continue
		}
		select {
		case call.frames <- frame:
		default:
			err := fmt.Errorf("%w for request %d", ErrWorkerCallOverflow, frame.ID)
			if c.failCall(frame.ID, call, err) {
				id := frame.ID
				go func() { _ = c.sendCancel(id) }()
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
			_ = c.sendCancel(requestID)
		case <-cancelDone:
		}
	}()
	defer close(cancelDone)

	for {
		got, err := c.nextFrame(ctx, call)
		if err != nil {
			return err
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
		got, err := c.nextFrame(ctx, call)
		if err != nil {
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
					return err
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
	case err := <-call.done:
		return WorkerFrame{}, err
	default:
	}
	select {
	case <-ctx.Done():
		return WorkerFrame{}, ctx.Err()
	case err := <-call.done:
		if ctx.Err() != nil {
			return WorkerFrame{}, ctx.Err()
		}
		return WorkerFrame{}, err
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
