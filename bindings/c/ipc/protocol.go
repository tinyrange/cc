// Package ipc provides the inter-process communication protocol for the
// cc-helper out-of-process architecture. This allows the C bindings library
// (libcc) to communicate with codesigned helper processes that run VMs.
package ipc

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
)

// Message types for the IPC protocol.
// Organized by category with a prefix byte.
const (
	// Instance lifecycle (0x01xx)
	MsgInstanceNew        uint16 = 0x0100
	MsgInstanceClose      uint16 = 0x0101
	MsgInstanceWait       uint16 = 0x0102
	MsgInstanceID         uint16 = 0x0103
	MsgInstanceIsRunning  uint16 = 0x0104
	MsgInstanceSetConsole uint16 = 0x0105
	MsgInstanceSetNetwork uint16 = 0x0106
	MsgInstanceExec       uint16 = 0x0107

	// Filesystem operations (0x02xx)
	MsgFsOpen      uint16 = 0x0200
	MsgFsCreate    uint16 = 0x0201
	MsgFsOpenFile  uint16 = 0x0202
	MsgFsReadFile  uint16 = 0x0203
	MsgFsWriteFile uint16 = 0x0204
	MsgFsStat      uint16 = 0x0205
	MsgFsLstat     uint16 = 0x0206
	MsgFsRemove    uint16 = 0x0207
	MsgFsRemoveAll uint16 = 0x0208
	MsgFsMkdir     uint16 = 0x0209
	MsgFsMkdirAll  uint16 = 0x020A
	MsgFsRename    uint16 = 0x020B
	MsgFsSymlink   uint16 = 0x020C
	MsgFsReadlink  uint16 = 0x020D
	MsgFsReadDir   uint16 = 0x020E
	MsgFsChmod     uint16 = 0x020F
	MsgFsChown     uint16 = 0x0210
	MsgFsChtimes   uint16 = 0x0211
	MsgFsSnapshot  uint16 = 0x0212

	// File operations (0x03xx)
	MsgFileClose    uint16 = 0x0300
	MsgFileRead     uint16 = 0x0301
	MsgFileWrite    uint16 = 0x0302
	MsgFileSeek     uint16 = 0x0303
	MsgFileSync     uint16 = 0x0304
	MsgFileTruncate uint16 = 0x0305
	MsgFileStat     uint16 = 0x0306
	MsgFileName     uint16 = 0x0307

	// Command operations (0x04xx)
	MsgCmdNew            uint16 = 0x0400
	MsgCmdEntrypoint     uint16 = 0x0401
	MsgCmdFree           uint16 = 0x0402
	MsgCmdSetDir         uint16 = 0x0403
	MsgCmdSetEnv         uint16 = 0x0404
	MsgCmdGetEnv         uint16 = 0x0405
	MsgCmdEnviron        uint16 = 0x0406
	MsgCmdStart          uint16 = 0x0407
	MsgCmdWait           uint16 = 0x0408
	MsgCmdRun            uint16 = 0x0409
	MsgCmdOutput         uint16 = 0x040A
	MsgCmdCombinedOutput uint16 = 0x040B
	MsgCmdExitCode       uint16 = 0x040C
	MsgCmdKill           uint16 = 0x040D

	// Network operations (0x05xx)
	MsgNetListen      uint16 = 0x0500
	MsgListenerAccept uint16 = 0x0501
	MsgListenerClose  uint16 = 0x0502
	MsgListenerAddr   uint16 = 0x0503
	MsgConnRead       uint16 = 0x0504
	MsgConnWrite      uint16 = 0x0505
	MsgConnClose      uint16 = 0x0506
	MsgConnLocalAddr  uint16 = 0x0507
	MsgConnRemoteAddr uint16 = 0x0508

	// Snapshot operations (0x06xx)
	MsgSnapshotCacheKey uint16 = 0x0600
	MsgSnapshotParent   uint16 = 0x0601
	MsgSnapshotClose    uint16 = 0x0602
	MsgSnapshotAsSource uint16 = 0x0603

	// Response types (0xFFxx)
	MsgResponse uint16 = 0xFF00
	MsgError    uint16 = 0xFF01
)

// Wire format:
// [2 bytes: msg_type (big endian)]
// [4 bytes: payload_len (big endian)]
// [payload_len bytes: payload]

// Header represents a message header.
type Header struct {
	Type   uint16
	Length uint32
}

// HeaderSize is the size of the header in bytes.
const HeaderSize = 6

// ReadHeader reads a message header from the reader.
func ReadHeader(r io.Reader) (Header, error) {
	var buf [HeaderSize]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return Header{}, err
	}
	return Header{
		Type:   binary.BigEndian.Uint16(buf[0:2]),
		Length: binary.BigEndian.Uint32(buf[2:6]),
	}, nil
}

// WriteHeader writes a message header to the writer.
func WriteHeader(w io.Writer, h Header) error {
	var buf [HeaderSize]byte
	binary.BigEndian.PutUint16(buf[0:2], h.Type)
	binary.BigEndian.PutUint32(buf[2:6], h.Length)
	_, err := w.Write(buf[:])
	return err
}

// Encoder writes IPC messages.
type Encoder struct {
	buf []byte
}

// NewEncoder creates a new encoder.
func NewEncoder() *Encoder {
	return &Encoder{buf: make([]byte, 0, 4096)}
}

// Reset clears the buffer for reuse.
func (e *Encoder) Reset() {
	e.buf = e.buf[:0]
}

// Bytes returns the encoded bytes.
func (e *Encoder) Bytes() []byte {
	return e.buf
}

// Uint8 appends a uint8.
func (e *Encoder) Uint8(v uint8) {
	e.buf = append(e.buf, v)
}

// Uint16 appends a uint16 (big endian).
func (e *Encoder) Uint16(v uint16) {
	e.buf = append(e.buf, byte(v>>8), byte(v))
}

// Uint32 appends a uint32 (big endian).
func (e *Encoder) Uint32(v uint32) {
	e.buf = append(e.buf,
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// Uint64 appends a uint64 (big endian).
func (e *Encoder) Uint64(v uint64) {
	e.buf = append(e.buf,
		byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// Int32 appends an int32 (big endian).
func (e *Encoder) Int32(v int32) {
	e.Uint32(uint32(v))
}

// Int64 appends an int64 (big endian).
func (e *Encoder) Int64(v int64) {
	e.Uint64(uint64(v))
}

// Bool appends a bool (1 byte).
func (e *Encoder) Bool(v bool) {
	if v {
		e.buf = append(e.buf, 1)
	} else {
		e.buf = append(e.buf, 0)
	}
}

// String appends a length-prefixed string (4 bytes length + data).
func (e *Encoder) String(s string) {
	e.Uint32(uint32(len(s)))
	e.buf = append(e.buf, s...)
}

// WriteBytes appends a length-prefixed byte slice (4 bytes length + data).
func (e *Encoder) WriteBytes(b []byte) {
	e.Uint32(uint32(len(b)))
	e.buf = append(e.buf, b...)
}

// StringSlice appends a string slice (4 bytes count + strings).
func (e *Encoder) StringSlice(ss []string) {
	e.Uint32(uint32(len(ss)))
	for _, s := range ss {
		e.String(s)
	}
}

// Decoder reads IPC messages.
type Decoder struct {
	buf []byte
	pos int
}

// NewDecoder creates a new decoder for the given bytes.
func NewDecoder(buf []byte) *Decoder {
	return &Decoder{buf: buf}
}

// Remaining returns the number of unread bytes.
func (d *Decoder) Remaining() int {
	return len(d.buf) - d.pos
}

// Uint8 reads a uint8.
func (d *Decoder) Uint8() (uint8, error) {
	if d.pos >= len(d.buf) {
		return 0, io.ErrUnexpectedEOF
	}
	v := d.buf[d.pos]
	d.pos++
	return v, nil
}

// Uint16 reads a uint16 (big endian).
func (d *Decoder) Uint16() (uint16, error) {
	if d.pos+2 > len(d.buf) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint16(d.buf[d.pos:])
	d.pos += 2
	return v, nil
}

// Uint32 reads a uint32 (big endian).
func (d *Decoder) Uint32() (uint32, error) {
	if d.pos+4 > len(d.buf) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint32(d.buf[d.pos:])
	d.pos += 4
	return v, nil
}

// Uint64 reads a uint64 (big endian).
func (d *Decoder) Uint64() (uint64, error) {
	if d.pos+8 > len(d.buf) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint64(d.buf[d.pos:])
	d.pos += 8
	return v, nil
}

// Int32 reads an int32 (big endian).
func (d *Decoder) Int32() (int32, error) {
	v, err := d.Uint32()
	return int32(v), err
}

// Int64 reads an int64 (big endian).
func (d *Decoder) Int64() (int64, error) {
	v, err := d.Uint64()
	return int64(v), err
}

// Bool reads a bool (1 byte).
func (d *Decoder) Bool() (bool, error) {
	v, err := d.Uint8()
	return v != 0, err
}

// String reads a length-prefixed string.
func (d *Decoder) String() (string, error) {
	length, err := d.Uint32()
	if err != nil {
		return "", err
	}
	if d.pos+int(length) > len(d.buf) {
		return "", io.ErrUnexpectedEOF
	}
	s := string(d.buf[d.pos : d.pos+int(length)])
	d.pos += int(length)
	return s, nil
}

// Bytes reads a length-prefixed byte slice.
func (d *Decoder) Bytes() ([]byte, error) {
	length, err := d.Uint32()
	if err != nil {
		return nil, err
	}
	if d.pos+int(length) > len(d.buf) {
		return nil, io.ErrUnexpectedEOF
	}
	b := make([]byte, length)
	copy(b, d.buf[d.pos:d.pos+int(length)])
	d.pos += int(length)
	return b, nil
}

// StringSlice reads a string slice.
func (d *Decoder) StringSlice() ([]string, error) {
	count, err := d.Uint32()
	if err != nil {
		return nil, err
	}
	ss := make([]string, count)
	for i := range ss {
		ss[i], err = d.String()
		if err != nil {
			return nil, err
		}
	}
	return ss, nil
}

// Error codes for IPC errors (matches C error codes).
const (
	ErrCodeOK                    = 0
	ErrCodeInvalidHandle         = 1
	ErrCodeInvalidArgument       = 2
	ErrCodeNotRunning            = 3
	ErrCodeAlreadyClosed         = 4
	ErrCodeTimeout               = 5
	ErrCodeHypervisorUnavailable = 6
	ErrCodeIO                    = 7
	ErrCodeNetwork               = 8
	ErrCodeCancelled             = 9
	ErrCodeUnknown               = 99
)

// IPCError represents an error in the IPC protocol.
type IPCError struct {
	Code    uint8
	Message string
	Op      string
	Path    string
}

func (e *IPCError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s: %s: %s", e.Op, e.Path, e.Message)
	}
	if e.Op != "" {
		return fmt.Sprintf("%s: %s", e.Op, e.Message)
	}
	return e.Message
}

// EncodeError encodes an error response.
func EncodeError(enc *Encoder, code uint8, message, op, path string) {
	enc.Uint8(code)
	enc.String(message)
	enc.String(op)
	enc.String(path)
}

// DecodeError decodes an error response.
func DecodeError(dec *Decoder) (*IPCError, error) {
	code, err := dec.Uint8()
	if err != nil {
		return nil, err
	}
	if code == ErrCodeOK {
		return nil, nil
	}
	message, err := dec.String()
	if err != nil {
		return nil, err
	}
	op, err := dec.String()
	if err != nil {
		return nil, err
	}
	path, err := dec.String()
	if err != nil {
		return nil, err
	}
	return &IPCError{Code: code, Message: message, Op: op, Path: path}, nil
}

// FileInfo holds file metadata for IPC transfer.
type FileInfo struct {
	Name      string
	Size      int64
	Mode      fs.FileMode
	ModTime   int64 // Unix timestamp
	IsDir     bool
	IsSymlink bool
}

// EncodeFileInfo encodes file info.
func EncodeFileInfo(enc *Encoder, fi FileInfo) {
	enc.String(fi.Name)
	enc.Int64(fi.Size)
	enc.Uint32(uint32(fi.Mode))
	enc.Int64(fi.ModTime)
	enc.Bool(fi.IsDir)
	enc.Bool(fi.IsSymlink)
}

// DecodeFileInfo decodes file info.
func DecodeFileInfo(dec *Decoder) (FileInfo, error) {
	var fi FileInfo
	var err error
	fi.Name, err = dec.String()
	if err != nil {
		return fi, err
	}
	fi.Size, err = dec.Int64()
	if err != nil {
		return fi, err
	}
	mode, err := dec.Uint32()
	if err != nil {
		return fi, err
	}
	fi.Mode = fs.FileMode(mode)
	fi.ModTime, err = dec.Int64()
	if err != nil {
		return fi, err
	}
	fi.IsDir, err = dec.Bool()
	if err != nil {
		return fi, err
	}
	fi.IsSymlink, err = dec.Bool()
	if err != nil {
		return fi, err
	}
	return fi, nil
}

// DirEntry holds directory entry data for IPC transfer.
type DirEntry struct {
	Name  string
	IsDir bool
	Mode  fs.FileMode
}

// EncodeDirEntry encodes a directory entry.
func EncodeDirEntry(enc *Encoder, de DirEntry) {
	enc.String(de.Name)
	enc.Bool(de.IsDir)
	enc.Uint32(uint32(de.Mode))
}

// DecodeDirEntry decodes a directory entry.
func DecodeDirEntry(dec *Decoder) (DirEntry, error) {
	var de DirEntry
	var err error
	de.Name, err = dec.String()
	if err != nil {
		return de, err
	}
	de.IsDir, err = dec.Bool()
	if err != nil {
		return de, err
	}
	mode, err := dec.Uint32()
	if err != nil {
		return de, err
	}
	de.Mode = fs.FileMode(mode)
	return de, nil
}

// MountConfig holds mount configuration for IPC transfer.
type MountConfig struct {
	Tag      string
	HostPath string
	Writable bool
}

// EncodeMountConfig encodes mount configuration.
func EncodeMountConfig(enc *Encoder, mc MountConfig) {
	enc.String(mc.Tag)
	enc.String(mc.HostPath)
	enc.Bool(mc.Writable)
}

// DecodeMountConfig decodes mount configuration.
func DecodeMountConfig(dec *Decoder) (MountConfig, error) {
	var mc MountConfig
	var err error
	mc.Tag, err = dec.String()
	if err != nil {
		return mc, err
	}
	mc.HostPath, err = dec.String()
	if err != nil {
		return mc, err
	}
	mc.Writable, err = dec.Bool()
	if err != nil {
		return mc, err
	}
	return mc, nil
}

// InstanceOptions holds instance options for IPC transfer.
type InstanceOptions struct {
	MemoryMB    uint64
	CPUs        int
	TimeoutSecs float64
	User        string
	EnableDmesg bool
	Mounts      []MountConfig
}

// EncodeInstanceOptions encodes instance options.
func EncodeInstanceOptions(enc *Encoder, opts InstanceOptions) {
	enc.Uint64(opts.MemoryMB)
	enc.Int32(int32(opts.CPUs))
	enc.Uint64(uint64(opts.TimeoutSecs * 1e9)) // nanoseconds
	enc.String(opts.User)
	enc.Bool(opts.EnableDmesg)
	enc.Uint32(uint32(len(opts.Mounts)))
	for _, m := range opts.Mounts {
		EncodeMountConfig(enc, m)
	}
}

// DecodeInstanceOptions decodes instance options.
func DecodeInstanceOptions(dec *Decoder) (InstanceOptions, error) {
	var opts InstanceOptions
	var err error
	opts.MemoryMB, err = dec.Uint64()
	if err != nil {
		return opts, err
	}
	cpus, err := dec.Int32()
	if err != nil {
		return opts, err
	}
	opts.CPUs = int(cpus)
	nanos, err := dec.Uint64()
	if err != nil {
		return opts, err
	}
	opts.TimeoutSecs = float64(nanos) / 1e9
	opts.User, err = dec.String()
	if err != nil {
		return opts, err
	}
	opts.EnableDmesg, err = dec.Bool()
	if err != nil {
		return opts, err
	}
	count, err := dec.Uint32()
	if err != nil {
		return opts, err
	}
	opts.Mounts = make([]MountConfig, count)
	for i := range opts.Mounts {
		opts.Mounts[i], err = DecodeMountConfig(dec)
		if err != nil {
			return opts, err
		}
	}
	return opts, nil
}

// SnapshotOptions holds snapshot options for IPC transfer.
type SnapshotOptions struct {
	Excludes []string
	CacheDir string
}

// EncodeSnapshotOptions encodes snapshot options.
func EncodeSnapshotOptions(enc *Encoder, opts SnapshotOptions) {
	enc.StringSlice(opts.Excludes)
	enc.String(opts.CacheDir)
}

// DecodeSnapshotOptions decodes snapshot options.
func DecodeSnapshotOptions(dec *Decoder) (SnapshotOptions, error) {
	var opts SnapshotOptions
	var err error
	opts.Excludes, err = dec.StringSlice()
	if err != nil {
		return opts, err
	}
	opts.CacheDir, err = dec.String()
	if err != nil {
		return opts, err
	}
	return opts, nil
}
