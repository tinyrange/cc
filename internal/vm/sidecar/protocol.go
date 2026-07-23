package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"j5.nz/cc/client"
)

const WorkerProtocolVersion = 2

const (
	WorkerServiceControl   = "control"
	WorkerServiceExec      = "exec"
	WorkerServiceConsole   = "console"
	WorkerServiceVirtioFS  = "virtio-fs"
	WorkerServiceVirtioNet = "virtio-net"
)

const (
	WorkerFrameHello        = "hello"
	WorkerFrameStart        = "start"
	WorkerFrameStartBlank   = "start_blank"
	WorkerFrameStop         = "stop"
	WorkerFrameWait         = "wait"
	WorkerFrameStatus       = "status"
	WorkerFrameExec         = "exec"
	WorkerFrameAddShare     = "add_share"
	WorkerFrameExecInput    = "exec_input"
	WorkerFrameExecInputAck = "exec_input_ack"
	WorkerFrameCancel       = "cancel"
	WorkerFrameFlush        = "flush"
	WorkerFrameConsole      = "console"
	WorkerFrameDone         = "done"
	WorkerFrameEvent        = "event"
	WorkerFramePacket       = "packet"
	WorkerFrameFilesystemOp = "fsop"
	WorkerFrameError        = "error"
)

type WorkerHello struct {
	Version      int              `json:"version"`
	WorkerID     string           `json:"worker_id,omitempty"`
	Backend      string           `json:"backend,omitempty"`
	Capabilities HostCapabilities `json:"capabilities"`
}

type HostCapabilities struct {
	Backend         string
	MaxVMs          int
	Locality        string
	SupportsFSRPC   bool
	SupportsL2      bool
	SupportsDisplay bool
}

type WorkerStartRequest struct {
	Version           int                           `json:"version"`
	WorkerID          string                        `json:"worker_id"`
	VMID              string                        `json:"vm_id"`
	CacheRoot         string                        `json:"cache_root"`
	CoordinatorSocket string                        `json:"coordinator_socket"`
	AuthToken         string                        `json:"auth_token"`
	Create            *client.CreateInstanceRequest `json:"create,omitempty"`
	Blank             *client.StartInstanceRequest  `json:"blank,omitempty"`
}

type WorkerStartResponse struct {
	State client.InstanceState `json:"state"`
}

type WorkerStatusRequest struct {
	ID string `json:"id,omitempty"`
}

type WorkerStatusResponse struct {
	State client.InstanceState `json:"state"`
}

type WorkerStopRequest struct {
	ID string `json:"id,omitempty"`
}

type WorkerWaitRequest struct {
	ID string `json:"id,omitempty"`
}

type WorkerFlushRequest struct {
	ID string `json:"id,omitempty"`
}

type WorkerAddShareRequest struct {
	ID    string            `json:"id,omitempty"`
	Share client.ShareMount `json:"share"`
}

type WorkerConsoleRequest struct {
	ID string `json:"id,omitempty"`
}

type WorkerConsoleResponse struct {
	History string `json:"history"`
}

type WorkerExecRequest struct {
	ID          string             `json:"id,omitempty"`
	Request     client.ExecRequest `json:"request"`
	InputStream bool               `json:"input_stream,omitempty"`
}

type WorkerExecInput struct {
	Input  client.ExecInput `json:"input,omitempty"`
	Closed bool             `json:"closed,omitempty"`
}

type WorkerCancelRequest struct {
	ID string `json:"id,omitempty"`
}

type WorkerError struct {
	Error       string `json:"error"`
	RequestID   uint64 `json:"request_id,omitempty"`
	RequestType string `json:"request_type,omitempty"`
}

func (r WorkerStartRequest) Validate() error {
	if r.Version != WorkerProtocolVersion {
		return fmt.Errorf("unsupported worker protocol version %d", r.Version)
	}
	if r.WorkerID == "" {
		return fmt.Errorf("worker id is required")
	}
	if r.VMID == "" {
		return fmt.Errorf("VM id is required")
	}
	if r.CacheRoot == "" {
		return fmt.Errorf("cache root is required")
	}
	if r.CoordinatorSocket == "" {
		return fmt.Errorf("coordinator socket is required")
	}
	if r.AuthToken == "" {
		return fmt.Errorf("auth token is required")
	}
	if (r.Create == nil) == (r.Blank == nil) {
		return fmt.Errorf("exactly one start request is required")
	}
	return nil
}

type WorkerFrame struct {
	ID      uint64          `json:"id,omitempty"`
	Service string          `json:"service"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func NewWorkerFrame(id uint64, service string, frameType string, payload any) (WorkerFrame, error) {
	frame := WorkerFrame{ID: id, Service: service, Type: frameType}
	if payload == nil {
		return frame, nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return WorkerFrame{}, err
	}
	frame.Payload = raw
	return frame, nil
}

func (f WorkerFrame) DecodePayload(dst any) error {
	if len(f.Payload) == 0 {
		return fmt.Errorf("worker frame has no payload")
	}
	return json.Unmarshal(f.Payload, dst)
}

type WorkerCodec struct {
	conn io.ReadWriteCloser
	enc  *json.Encoder
	dec  *json.Decoder
	send chan struct{}
}

type WorkerStreamWriteError struct {
	Err error
}

func (e *WorkerStreamWriteError) Error() string {
	return fmt.Sprintf("sidecar worker stream write failed and the connection was poisoned: %v", e.Err)
}

func (e *WorkerStreamWriteError) Unwrap() error { return e.Err }

func NewWorkerCodec(conn io.ReadWriteCloser) *WorkerCodec {
	return &WorkerCodec{
		conn: conn,
		enc:  json.NewEncoder(conn),
		dec:  json.NewDecoder(conn),
		send: make(chan struct{}, 1),
	}
}

func (c *WorkerCodec) Send(frame WorkerFrame) error {
	return c.SendContext(context.Background(), frame)
}

func (c *WorkerCodec) SendContext(ctx context.Context, frame WorkerFrame) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case c.send <- struct{}{}:
		defer func() { <-c.send }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if deadline, ok := ctx.Deadline(); ok {
		if conn, ok := c.conn.(interface{ SetWriteDeadline(time.Time) error }); ok {
			if err := conn.SetWriteDeadline(deadline); err != nil {
				return err
			}
			defer conn.SetWriteDeadline(time.Time{})
		}
	}
	if err := c.enc.Encode(frame); err != nil {
		_ = c.conn.Close()
		return &WorkerStreamWriteError{Err: err}
	}
	return nil
}

func (c *WorkerCodec) Receive() (WorkerFrame, error) {
	var frame WorkerFrame
	if err := c.dec.Decode(&frame); err != nil {
		return WorkerFrame{}, err
	}
	return frame, nil
}

func (c *WorkerCodec) Close() error {
	return c.conn.Close()
}
