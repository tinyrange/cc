package ipc

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// socketCounter provides unique socket paths when multiple helpers are spawned concurrently.
var socketCounter atomic.Uint64

// Client manages a connection to a cc-helper process.
type Client struct {
	conn       net.Conn
	cmd        *exec.Cmd
	mu         sync.Mutex
	closed     atomic.Bool
	reqID      atomic.Uint64
	socketPath string
}

// HelperNotFoundError is returned when the cc-helper binary cannot be found.
type HelperNotFoundError struct {
	SearchedPaths []string
}

func (e *HelperNotFoundError) Error() string {
	return fmt.Sprintf("cc-helper not found (searched: %v)", e.SearchedPaths)
}

// findHelper searches for the cc-helper binary.
func findHelper(libPath string) (string, []string) {
	var searched []string

	// 1. CC_HELPER_PATH environment variable
	if path := os.Getenv("CC_HELPER_PATH"); path != "" {
		searched = append(searched, path)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// 2. Adjacent to current executable (for static linking)
	if exePath, err := os.Executable(); err == nil {
		dir := filepath.Dir(exePath)
		path := filepath.Join(dir, "cc-helper")
		searched = append(searched, path)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// 3. Adjacent to libcc (same directory as library)
	if libPath != "" {
		dir := filepath.Dir(libPath)
		path := filepath.Join(dir, "cc-helper")
		searched = append(searched, path)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// 4. Platform-specific user directory
	switch runtime.GOOS {
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			path := filepath.Join(home, "Library", "Application Support", "cc", "bin", "cc-helper")
			searched = append(searched, path)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	case "linux":
		if home, err := os.UserHomeDir(); err == nil {
			path := filepath.Join(home, ".local", "share", "cc", "bin", "cc-helper")
			searched = append(searched, path)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	case "windows":
		if appData := os.Getenv("LOCALAPPDATA"); appData != "" {
			path := filepath.Join(appData, "cc", "bin", "cc-helper.exe")
			searched = append(searched, path)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	// 5. System PATH
	if path, err := exec.LookPath("cc-helper"); err == nil {
		return path, nil
	}
	searched = append(searched, "$PATH")

	return "", searched
}

// SpawnHelper starts a new cc-helper process and connects to it.
// libPath should be the path to libcc.so/.dylib for locating cc-helper.
func SpawnHelper(libPath string) (*Client, error) {
	helperPath, searched := findHelper(libPath)
	if helperPath == "" {
		return nil, &HelperNotFoundError{SearchedPaths: searched}
	}

	// Create a temporary socket path (counter ensures uniqueness under concurrent spawns)
	tmpDir := os.TempDir()
	socketPath := filepath.Join(tmpDir, fmt.Sprintf("cc-helper-%d-%d-%d.sock", os.Getpid(), time.Now().UnixNano(), socketCounter.Add(1)))

	// Start the helper process
	cmd := exec.Command(helperPath, "-socket", socketPath)
	cmd.Stderr = os.Stderr // Forward helper errors for debugging

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start cc-helper: %w", err)
	}

	// Wait for the socket to appear (with timeout)
	deadline := time.Now().Add(10 * time.Second)
	var conn net.Conn
	var lastErr error
	for time.Now().Before(deadline) {
		conn, lastErr = net.Dial("unix", socketPath)
		if lastErr == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if conn == nil {
		cmd.Process.Kill()
		cmd.Wait()
		os.Remove(socketPath)
		return nil, fmt.Errorf("failed to connect to cc-helper: %w", lastErr)
	}

	return &Client{
		conn:       conn,
		cmd:        cmd,
		socketPath: socketPath,
	}, nil
}

// ConnectTo connects to an existing cc-helper process at the given socket path.
func ConnectTo(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to cc-helper: %w", err)
	}
	return &Client{
		conn:       conn,
		socketPath: socketPath,
	}, nil
}

// Close shuts down the client connection and helper process.
func (c *Client) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}

	var errs []error

	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if c.cmd != nil && c.cmd.Process != nil {
		// Give the helper a chance to exit gracefully
		done := make(chan struct{})
		go func() {
			c.cmd.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Process exited
		case <-time.After(2 * time.Second):
			// Force kill
			c.cmd.Process.Kill()
			<-done
		}
	}

	// Clean up socket file
	if c.socketPath != "" {
		os.Remove(c.socketPath)
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// Call sends a request and waits for a response.
// This is a synchronous RPC call.
func (c *Client) Call(msgType uint16, payload []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed.Load() {
		return nil, fmt.Errorf("client closed")
	}

	// Write request
	if err := WriteHeader(c.conn, Header{Type: msgType, Length: uint32(len(payload))}); err != nil {
		return nil, fmt.Errorf("write header: %w", err)
	}
	if len(payload) > 0 {
		if _, err := c.conn.Write(payload); err != nil {
			return nil, fmt.Errorf("write payload: %w", err)
		}
	}

	// Read response header
	respHeader, err := ReadHeader(c.conn)
	if err != nil {
		return nil, fmt.Errorf("read response header: %w", err)
	}

	// Read response payload
	respPayload := make([]byte, respHeader.Length)
	if respHeader.Length > 0 {
		if _, err := io.ReadFull(c.conn, respPayload); err != nil {
			return nil, fmt.Errorf("read response payload: %w", err)
		}
	}

	// Check for error response
	if respHeader.Type == MsgError {
		dec := NewDecoder(respPayload)
		ipcErr, err := DecodeError(dec)
		if err != nil {
			return nil, fmt.Errorf("decode error response: %w", err)
		}
		if ipcErr != nil {
			return nil, ipcErr
		}
	}

	return respPayload, nil
}

// CallWithEncoder is a convenience method that uses an encoder for the request.
func (c *Client) CallWithEncoder(msgType uint16, encode func(*Encoder)) ([]byte, error) {
	enc := NewEncoder()
	encode(enc)
	return c.Call(msgType, enc.Bytes())
}

// IsAlive checks if the helper process is still running.
func (c *Client) IsAlive() bool {
	if c.closed.Load() {
		return false
	}
	if c.cmd == nil || c.cmd.Process == nil {
		return true // External process, assume alive
	}
	// Try to send a no-op probe
	c.conn.SetDeadline(time.Now().Add(100 * time.Millisecond))
	defer c.conn.SetDeadline(time.Time{})

	// Check if we can peek at the connection
	one := make([]byte, 1)
	c.conn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
	_, err := c.conn.Read(one)
	c.conn.SetReadDeadline(time.Time{})

	if err == io.EOF {
		return false
	}
	// Any other error (including timeout) means connection is still open
	return true
}
