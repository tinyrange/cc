package helper

import (
	"bytes"
	"io"
	"testing"

	"github.com/tinyrange/cc/internal/ipc"
)

type testWriteCloser struct {
	buf    bytes.Buffer
	closed bool
}

func (w *testWriteCloser) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *testWriteCloser) Close() error {
	w.closed = true
	return nil
}

func decodeSuccess(t *testing.T, payload []byte) *ipc.Decoder {
	t.Helper()

	dec := ipc.NewDecoder(payload)
	ipcErr, err := ipc.DecodeError(dec)
	if err != nil {
		t.Fatalf("DecodeError: %v", err)
	}
	if ipcErr != nil {
		t.Fatalf("unexpected IPC error: %v", ipcErr)
	}
	return dec
}

func TestHandleConnReadFromPipe(t *testing.T) {
	h := NewHelper()
	handle := h.newHandle()
	h.pipeRds[handle] = io.NopCloser(bytes.NewReader([]byte("hello from pipe")))

	req := ipc.NewEncoder()
	req.Uint64(handle)
	req.Uint32(128)

	resp, err := h.handleConnRead(ipc.NewDecoder(req.Bytes()))
	if err != nil {
		t.Fatalf("handleConnRead: %v", err)
	}

	dec := decodeSuccess(t, resp)
	data, err := dec.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if got, want := string(data), "hello from pipe"; got != want {
		t.Fatalf("read mismatch: got %q, want %q", got, want)
	}
}

func TestHandleConnWriteAndClosePipe(t *testing.T) {
	h := NewHelper()
	handle := h.newHandle()
	w := &testWriteCloser{}
	h.pipeWrs[handle] = w

	reqWrite := ipc.NewEncoder()
	reqWrite.Uint64(handle)
	reqWrite.WriteBytes([]byte("stdin payload"))

	resp, err := h.handleConnWrite(ipc.NewDecoder(reqWrite.Bytes()))
	if err != nil {
		t.Fatalf("handleConnWrite: %v", err)
	}

	dec := decodeSuccess(t, resp)
	n, err := dec.Uint32()
	if err != nil {
		t.Fatalf("Uint32: %v", err)
	}
	if got, want := int(n), len("stdin payload"); got != want {
		t.Fatalf("write size mismatch: got %d, want %d", got, want)
	}
	if got, want := w.buf.String(), "stdin payload"; got != want {
		t.Fatalf("write payload mismatch: got %q, want %q", got, want)
	}

	reqClose := ipc.NewEncoder()
	reqClose.Uint64(handle)

	resp, err = h.handleConnClose(ipc.NewDecoder(reqClose.Bytes()))
	if err != nil {
		t.Fatalf("handleConnClose: %v", err)
	}
	_ = decodeSuccess(t, resp)

	if !w.closed {
		t.Fatal("expected pipe writer to be closed")
	}
	if _, ok := h.pipeWrs[handle]; ok {
		t.Fatal("expected pipe writer handle to be removed")
	}
}

func TestHandleConnAddrForPipe(t *testing.T) {
	h := NewHelper()
	handle := h.newHandle()
	h.pipeRds[handle] = io.NopCloser(bytes.NewReader(nil))

	req := ipc.NewEncoder()
	req.Uint64(handle)

	localResp, err := h.handleConnLocalAddr(ipc.NewDecoder(req.Bytes()))
	if err != nil {
		t.Fatalf("handleConnLocalAddr: %v", err)
	}
	localDec := decodeSuccess(t, localResp)
	localAddr, err := localDec.String()
	if err != nil {
		t.Fatalf("String(local): %v", err)
	}
	if localAddr != "pipe" {
		t.Fatalf("local addr mismatch: got %q, want %q", localAddr, "pipe")
	}

	remoteResp, err := h.handleConnRemoteAddr(ipc.NewDecoder(req.Bytes()))
	if err != nil {
		t.Fatalf("handleConnRemoteAddr: %v", err)
	}
	remoteDec := decodeSuccess(t, remoteResp)
	remoteAddr, err := remoteDec.String()
	if err != nil {
		t.Fatalf("String(remote): %v", err)
	}
	if remoteAddr != "pipe" {
		t.Fatalf("remote addr mismatch: got %q, want %q", remoteAddr, "pipe")
	}
}
