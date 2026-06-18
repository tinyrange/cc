package nfs

import (
	"encoding/binary"
	"fmt"
	"io"
)

type xdrReader struct {
	data []byte
	off  int
}

func newXDRReader(data []byte) *xdrReader {
	return &xdrReader{data: data}
}

func (r *xdrReader) remaining() int {
	return len(r.data) - r.off
}

func (r *xdrReader) Uint32() (uint32, error) {
	if r.remaining() < 4 {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint32(r.data[r.off : r.off+4])
	r.off += 4
	return v, nil
}

func (r *xdrReader) Uint64() (uint64, error) {
	hi, err := r.Uint32()
	if err != nil {
		return 0, err
	}
	lo, err := r.Uint32()
	if err != nil {
		return 0, err
	}
	return uint64(hi)<<32 | uint64(lo), nil
}

func (r *xdrReader) Opaque(max uint32) ([]byte, error) {
	n, err := r.Uint32()
	if err != nil {
		return nil, err
	}
	if n > max {
		return nil, fmt.Errorf("opaque length %d exceeds max %d", n, max)
	}
	pad := xdrPad(int(n))
	if r.remaining() < int(n)+pad {
		return nil, io.ErrUnexpectedEOF
	}
	out := append([]byte(nil), r.data[r.off:r.off+int(n)]...)
	r.off += int(n) + pad
	return out, nil
}

func (r *xdrReader) String(max uint32) (string, error) {
	data, err := r.Opaque(max)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (r *xdrReader) SkipAuth() error {
	flavor, err := r.Uint32()
	if err != nil {
		return err
	}
	body, err := r.Opaque(4096)
	if err != nil {
		return err
	}
	_ = flavor
	_ = body
	return nil
}

type xdrWriter struct {
	data []byte
}

func (w *xdrWriter) Bytes() []byte {
	return w.data
}

func (w *xdrWriter) Uint32(v uint32) {
	w.data = binary.BigEndian.AppendUint32(w.data, v)
}

func (w *xdrWriter) Uint64(v uint64) {
	w.Uint32(uint32(v >> 32))
	w.Uint32(uint32(v))
}

func (w *xdrWriter) Bool(v bool) {
	if v {
		w.Uint32(1)
		return
	}
	w.Uint32(0)
}

func (w *xdrWriter) Opaque(data []byte) {
	w.Uint32(uint32(len(data)))
	w.FixedOpaque(data)
}

func (w *xdrWriter) FixedOpaque(data []byte) {
	w.data = append(w.data, data...)
	for range xdrPad(len(data)) {
		w.data = append(w.data, 0)
	}
}

func (w *xdrWriter) String(value string) {
	w.Opaque([]byte(value))
}

func xdrPad(n int) int {
	return (4 - (n & 3)) & 3
}
