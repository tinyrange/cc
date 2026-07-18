//go:build linux && (amd64 || arm64)

package kvm

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const bsdControlReconnectWait = 5 * time.Second

// reconnectingBSDControl keeps the managed-session writer pointed at the
// current guest TCP connection. BSD init reconnects after a transport loss,
// so a transient idle-channel failure does not strand an otherwise live VM.
type reconnectingBSDControl struct {
	mu        sync.Mutex
	conn      net.Conn
	available chan struct{}
	closed    bool
}

func newReconnectingBSDControl() *reconnectingBSDControl {
	return &reconnectingBSDControl{available: make(chan struct{})}
}

func (c *reconnectingBSDControl) setConnection(conn net.Conn) bool {
	if conn == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		_ = conn.Close()
		return false
	}
	if c.conn != nil && c.conn != conn {
		_ = c.conn.Close()
	}
	c.conn = conn
	if c.available != nil {
		close(c.available)
		c.available = nil
	}
	return true
}

func (c *reconnectingBSDControl) clearConnection(conn net.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != conn {
		return
	}
	c.conn = nil
	if !c.closed && c.available == nil {
		c.available = make(chan struct{})
	}
}

func (c *reconnectingBSDControl) connection() (net.Conn, error) {
	timer := time.NewTimer(bsdControlReconnectWait)
	defer timer.Stop()
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, net.ErrClosed
		}
		if c.conn != nil {
			conn := c.conn
			c.mu.Unlock()
			return conn, nil
		}
		available := c.available
		c.mu.Unlock()
		select {
		case <-available:
		case <-timer.C:
			return nil, fmt.Errorf("guest control connection unavailable after %s", bsdControlReconnectWait)
		}
	}
}

func (c *reconnectingBSDControl) Read(data []byte) (int, error) {
	conn, err := c.connection()
	if err != nil {
		return 0, err
	}
	n, err := conn.Read(data)
	if err != nil {
		c.clearConnection(conn)
	}
	return n, err
}

func (c *reconnectingBSDControl) Write(data []byte) (int, error) {
	conn, err := c.connection()
	if err != nil {
		return 0, err
	}
	n, err := conn.Write(data)
	if err != nil {
		c.clearConnection(conn)
	}
	return n, err
}

func (c *reconnectingBSDControl) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	conn := c.conn
	c.conn = nil
	if c.available != nil {
		close(c.available)
		c.available = nil
	}
	c.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func acceptBSDControlConnections(listener net.Listener, transcript io.Writer) (*reconnectingBSDControl, <-chan struct{}, <-chan error) {
	control := newReconnectingBSDControl()
	connected := make(chan struct{})
	acceptErr := make(chan error, 1)
	go func() {
		var first sync.Once
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case acceptErr <- err:
				default:
				}
				return
			}
			if !control.setConnection(conn) {
				return
			}
			first.Do(func() { close(connected) })
			go func(conn net.Conn) {
				_, _ = io.Copy(transcript, conn)
				control.clearConnection(conn)
				_ = conn.Close()
			}(conn)
		}
	}()
	return control, connected, acceptErr
}
