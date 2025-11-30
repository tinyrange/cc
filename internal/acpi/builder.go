package acpi

import (
	"bytes"
	"encoding/binary"
)

type tableWriter struct {
	buf  bytes.Buffer
	base uint64
	oem  OEMInfo
}

func newTableWriter(base uint64, oem OEMInfo) *tableWriter {
	return &tableWriter{base: base, oem: oem}
}

type tableParams struct {
	Signature  [4]byte
	Revision   uint8
	OEMTableID [8]byte
	Body       []byte
}

func (w *tableWriter) Append(params tableParams) uint64 {
	start := w.buf.Len()
	w.buf.Grow(36 + len(params.Body))

	header := make([]byte, 36)
	copy(header[:4], params.Signature[:])
	copy(header[10:16], w.oem.OEMID[:])

	tableID := params.OEMTableID
	if tableID == ([8]byte{}) {
		tableID = w.oem.OEMTableID
	}
	copy(header[16:24], tableID[:])

	binary.LittleEndian.PutUint32(header[24:28], w.oem.OEMRevision)
	binary.LittleEndian.PutUint32(header[28:32], binary.LittleEndian.Uint32(w.oem.CreatorID[:]))
	binary.LittleEndian.PutUint32(header[32:36], w.oem.CreatorRevision)
	header[8] = params.Revision

	w.buf.Write(header)
	if len(params.Body) > 0 {
		w.buf.Write(params.Body)
	}

	tableBytes := w.buf.Bytes()[start:]
	binary.LittleEndian.PutUint32(tableBytes[4:8], uint32(len(tableBytes)))
	tableBytes[9] = checksum(tableBytes)

	if pad := len(tableBytes) % 8; pad != 0 {
		padding := make([]byte, 8-pad)
		w.buf.Write(padding)
	}

	return w.base + uint64(start)
}

func (w *tableWriter) Bytes() []byte {
	return w.buf.Bytes()
}

func checksum(b []byte) byte {
	var sum uint8
	for _, v := range b {
		sum += v
	}
	return byte(0 - sum)
}

func sig(name string) [4]byte {
	var out [4]byte
	copy(out[:], []byte(name))
	return out
}

func tableID(name string) [8]byte {
	var out [8]byte
	copy(out[:], []byte(name))
	return out
}
