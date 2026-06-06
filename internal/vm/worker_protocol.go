package vm

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"j5.nz/cc/client"
)

const WorkerProtocolVersion = 1

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
	WorkerFrameStop         = "stop"
	WorkerFrameWait         = "wait"
	WorkerFrameStatus       = "status"
	WorkerFrameExec         = "exec"
	WorkerFrameEvent        = "event"
	WorkerFramePacket       = "packet"
	WorkerFrameFilesystemOp = "fsop"
	WorkerFrameError        = "error"
)

type WorkerHello struct {
	Version      int                `json:"version"`
	WorkerID     string             `json:"worker_id,omitempty"`
	Backend      string             `json:"backend,omitempty"`
	Capabilities VMHostCapabilities `json:"capabilities"`
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
	mu   sync.Mutex
}

func NewWorkerCodec(conn io.ReadWriteCloser) *WorkerCodec {
	return &WorkerCodec{
		conn: conn,
		enc:  json.NewEncoder(conn),
		dec:  json.NewDecoder(conn),
	}
}

func (c *WorkerCodec) Send(frame WorkerFrame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enc.Encode(frame)
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
