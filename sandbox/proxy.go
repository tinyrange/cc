package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

const proxyHeaderLimit = 4096

type proxyRequest struct {
	Version int    `json:"version"`
	Host    string `json:"host"`
	Port    string `json:"port"`
}
type proxyResponse struct {
	Version int    `json:"version"`
	Error   string `json:"error,omitempty"`
}

// ProxyDialContext resolves the destination only inside the guest. The host
// independently dials this VM's proxy, never the requested host or host DNS.
func (s *Sandbox) ProxyDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, errors.New("sandbox proxy supports only TCP")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || host == "" {
		return nil, errors.New("invalid sandbox proxy destination")
	}
	req := proxyRequest{Version: 1, Host: host, Port: port}
	wire, err := json.Marshal(req)
	if err != nil || len(wire)+1 > proxyHeaderLimit {
		return nil, errors.New("sandbox proxy request exceeds limit")
	}
	ip := s.GuestIPv4()
	if ip == "" {
		return nil, errors.New("guest network unavailable")
	}
	conn, err := s.DialContext(ctx, "tcp", net.JoinHostPort(ip, "10780"))
	if err != nil {
		return nil, err
	}
	return proxyHandshake(ctx, conn, append(wire, '\n'))
}

func proxyHandshake(ctx context.Context, conn net.Conn, wire []byte) (net.Conn, error) {
	ok := false
	defer func() {
		if !ok {
			conn.Close()
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	if d, exists := ctx.Deadline(); exists && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { conn.Close(); close(stopped) })
	watchFinished := false
	defer func() {
		if !watchFinished && !stop() {
			<-stopped
		}
	}()
	for len(wire) > 0 {
		n, err := conn.Write(wire)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		if n == 0 {
			return nil, errors.New("short proxy write")
		}
		wire = wire[n:]
	}
	reader := bufio.NewReaderSize(conn, proxyHeaderLimit)
	line, err := reader.ReadSlice('\n')
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("sandbox proxy response: %w", err)
	}
	if len(line) > proxyHeaderLimit {
		return nil, errors.New("sandbox proxy response exceeds limit")
	}
	var response proxyResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return nil, fmt.Errorf("sandbox proxy response: %w", err)
	}
	if response.Version != 1 {
		return nil, errors.New("sandbox proxy protocol mismatch")
	}
	if response.Error != "" {
		return nil, fmt.Errorf("guest proxy: %s", response.Error)
	}
	watchFinished = true
	if !stop() {
		<-stopped
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	ok = true
	return &proxyConn{Conn: conn, reader: reader}, nil
}

type proxyConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *proxyConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *proxyConn) CloseWrite() error {
	if c, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return c.CloseWrite()
	}
	return errors.New("proxy connection does not support half-close")
}
