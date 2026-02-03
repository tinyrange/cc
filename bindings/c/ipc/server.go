package ipc

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
)

// Handler is a function that handles a request and returns a response.
type Handler func(msgType uint16, payload []byte) ([]byte, error)

// Server accepts connections from libcc clients.
type Server struct {
	listener   net.Listener
	socketPath string
	handler    Handler
	closed     atomic.Bool
	wg         sync.WaitGroup
	conns      map[net.Conn]struct{}
	connsMu    sync.Mutex
}

// NewServer creates a new IPC server listening on the given Unix socket path.
func NewServer(socketPath string, handler Handler) (*Server, error) {
	// Remove any existing socket file
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", socketPath, err)
	}

	return &Server{
		listener:   listener,
		socketPath: socketPath,
		handler:    handler,
		conns:      make(map[net.Conn]struct{}),
	}, nil
}

// SocketPath returns the path to the Unix socket.
func (s *Server) SocketPath() string {
	return s.socketPath
}

// Serve accepts connections and handles requests.
// This blocks until Close is called.
func (s *Server) Serve() error {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.closed.Load() {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}

		s.connsMu.Lock()
		s.conns[conn] = struct{}{}
		s.connsMu.Unlock()

		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

// ServeOne accepts a single connection and handles requests on it.
// This is useful for the one-process-per-instance model.
// Returns when the connection is closed.
func (s *Server) ServeOne() error {
	conn, err := s.listener.Accept()
	if err != nil {
		if s.closed.Load() {
			return nil
		}
		return fmt.Errorf("accept: %w", err)
	}

	s.connsMu.Lock()
	s.conns[conn] = struct{}{}
	s.connsMu.Unlock()

	s.wg.Add(1)
	s.handleConn(conn)
	return nil
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		conn.Close()
		s.connsMu.Lock()
		delete(s.conns, conn)
		s.connsMu.Unlock()
	}()

	for {
		if s.closed.Load() {
			return
		}

		// Read request header
		header, err := ReadHeader(conn)
		if err != nil {
			if err == io.EOF || s.closed.Load() {
				return
			}
			s.sendError(conn, ErrCodeIO, fmt.Sprintf("read header: %v", err), "", "")
			return
		}

		// Read request payload
		payload := make([]byte, header.Length)
		if header.Length > 0 {
			if _, err := io.ReadFull(conn, payload); err != nil {
				s.sendError(conn, ErrCodeIO, fmt.Sprintf("read payload: %v", err), "", "")
				return
			}
		}

		// Handle the request
		resp, err := s.handler(header.Type, payload)
		if err != nil {
			// Convert error to IPC error
			s.sendErrorFromGoError(conn, err)
			continue
		}

		// Send response
		if err := WriteHeader(conn, Header{Type: MsgResponse, Length: uint32(len(resp))}); err != nil {
			return
		}
		if len(resp) > 0 {
			if _, err := conn.Write(resp); err != nil {
				return
			}
		}
	}
}

func (s *Server) sendError(conn net.Conn, code uint8, message, op, path string) {
	enc := NewEncoder()
	EncodeError(enc, code, message, op, path)
	WriteHeader(conn, Header{Type: MsgError, Length: uint32(len(enc.Bytes()))})
	conn.Write(enc.Bytes())
}

func (s *Server) sendErrorFromGoError(conn net.Conn, err error) {
	if ipcErr, ok := err.(*IPCError); ok {
		s.sendError(conn, ipcErr.Code, ipcErr.Message, ipcErr.Op, ipcErr.Path)
		return
	}
	s.sendError(conn, ErrCodeUnknown, err.Error(), "", "")
}

// Close shuts down the server.
func (s *Server) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Close listener first to stop accepting new connections
	if s.listener != nil {
		s.listener.Close()
	}

	// Close all active connections
	s.connsMu.Lock()
	for conn := range s.conns {
		conn.Close()
	}
	s.connsMu.Unlock()

	// Wait for handlers to finish
	s.wg.Wait()

	// Clean up socket file
	if s.socketPath != "" {
		os.Remove(s.socketPath)
	}

	return nil
}

// Mux is a message type multiplexer for the server.
type Mux struct {
	handlers map[uint16]MuxHandler
	mu       sync.RWMutex
}

// MuxHandler handles a specific message type.
type MuxHandler func(dec *Decoder) ([]byte, error)

// NewMux creates a new message multiplexer.
func NewMux() *Mux {
	return &Mux{
		handlers: make(map[uint16]MuxHandler),
	}
}

// Handle registers a handler for a message type.
func (m *Mux) Handle(msgType uint16, handler MuxHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[msgType] = handler
}

// Handler returns a Handler function for use with Server.
func (m *Mux) Handler() Handler {
	return func(msgType uint16, payload []byte) ([]byte, error) {
		m.mu.RLock()
		handler, ok := m.handlers[msgType]
		m.mu.RUnlock()

		if !ok {
			return nil, &IPCError{
				Code:    ErrCodeInvalidArgument,
				Message: fmt.Sprintf("unknown message type: 0x%04x", msgType),
			}
		}

		dec := NewDecoder(payload)
		return handler(dec)
	}
}

// ResponseBuilder helps build response payloads.
type ResponseBuilder struct {
	enc *Encoder
}

// NewResponseBuilder creates a new response builder.
func NewResponseBuilder() *ResponseBuilder {
	return &ResponseBuilder{enc: NewEncoder()}
}

// Success marks the response as successful (error code 0).
func (r *ResponseBuilder) Success() *ResponseBuilder {
	r.enc.Uint8(ErrCodeOK)
	return r
}

// Uint8 appends a uint8.
func (r *ResponseBuilder) Uint8(v uint8) *ResponseBuilder {
	r.enc.Uint8(v)
	return r
}

// Uint16 appends a uint16.
func (r *ResponseBuilder) Uint16(v uint16) *ResponseBuilder {
	r.enc.Uint16(v)
	return r
}

// Uint32 appends a uint32.
func (r *ResponseBuilder) Uint32(v uint32) *ResponseBuilder {
	r.enc.Uint32(v)
	return r
}

// Uint64 appends a uint64.
func (r *ResponseBuilder) Uint64(v uint64) *ResponseBuilder {
	r.enc.Uint64(v)
	return r
}

// Int32 appends an int32.
func (r *ResponseBuilder) Int32(v int32) *ResponseBuilder {
	r.enc.Int32(v)
	return r
}

// Int64 appends an int64.
func (r *ResponseBuilder) Int64(v int64) *ResponseBuilder {
	r.enc.Int64(v)
	return r
}

// Bool appends a bool.
func (r *ResponseBuilder) Bool(v bool) *ResponseBuilder {
	r.enc.Bool(v)
	return r
}

// String appends a string.
func (r *ResponseBuilder) String(s string) *ResponseBuilder {
	r.enc.String(s)
	return r
}

// Bytes appends bytes.
func (r *ResponseBuilder) Bytes(b []byte) *ResponseBuilder {
	r.enc.WriteBytes(b)
	return r
}

// FileInfo appends file info.
func (r *ResponseBuilder) FileInfo(fi FileInfo) *ResponseBuilder {
	EncodeFileInfo(r.enc, fi)
	return r
}

// Build returns the encoded response bytes.
func (r *ResponseBuilder) Build() []byte {
	return r.enc.Bytes()
}
