package virtio

import (
	"encoding/binary"
	"fmt"
)

// Vsock header size
const vsockHdrSize = 44

// Vsock packet types
const (
	VIRTIO_VSOCK_TYPE_STREAM = 1
)

// Vsock operations
const (
	VIRTIO_VSOCK_OP_INVALID        = 0
	VIRTIO_VSOCK_OP_REQUEST        = 1 // Connection request
	VIRTIO_VSOCK_OP_RESPONSE       = 2 // Connection accepted
	VIRTIO_VSOCK_OP_RST            = 3 // Reset/reject
	VIRTIO_VSOCK_OP_SHUTDOWN       = 4 // Graceful shutdown
	VIRTIO_VSOCK_OP_RW             = 5 // Data transfer
	VIRTIO_VSOCK_OP_CREDIT_UPDATE  = 6 // Flow control
	VIRTIO_VSOCK_OP_CREDIT_REQUEST = 7 // Request credit info
)

// Vsock shutdown flags
const (
	VIRTIO_VSOCK_SHUTDOWN_RCV  = 1
	VIRTIO_VSOCK_SHUTDOWN_SEND = 2
)

// Well-known CIDs
const (
	VSOCK_CID_HYPERVISOR = 0 // Reserved
	VSOCK_CID_LOCAL      = 1 // Local loopback
	VSOCK_CID_HOST       = 2 // Host CID
)

// vsockHeader represents the virtio-vsock packet header.
// Layout:
//
//	u64 src_cid
//	u64 dst_cid
//	u32 src_port
//	u32 dst_port
//	u32 len
//	u16 type
//	u16 op
//	u32 flags
//	u32 buf_alloc
//	u32 fwd_cnt
type vsockHeader struct {
	SrcCID   uint64
	DstCID   uint64
	SrcPort  uint32
	DstPort  uint32
	Len      uint32
	Type     uint16
	Op       uint16
	Flags    uint32
	BufAlloc uint32
	FwdCnt   uint32
}

// parseVsockHeader parses a vsock header from a byte slice.
func parseVsockHeader(data []byte) (vsockHeader, error) {
	if len(data) < vsockHdrSize {
		return vsockHeader{}, fmt.Errorf("vsock header too short: %d < %d", len(data), vsockHdrSize)
	}
	return vsockHeader{
		SrcCID:   binary.LittleEndian.Uint64(data[0:8]),
		DstCID:   binary.LittleEndian.Uint64(data[8:16]),
		SrcPort:  binary.LittleEndian.Uint32(data[16:20]),
		DstPort:  binary.LittleEndian.Uint32(data[20:24]),
		Len:      binary.LittleEndian.Uint32(data[24:28]),
		Type:     binary.LittleEndian.Uint16(data[28:30]),
		Op:       binary.LittleEndian.Uint16(data[30:32]),
		Flags:    binary.LittleEndian.Uint32(data[32:36]),
		BufAlloc: binary.LittleEndian.Uint32(data[36:40]),
		FwdCnt:   binary.LittleEndian.Uint32(data[40:44]),
	}, nil
}

// encodeVsockHeader encodes a vsock header into a byte slice.
func encodeVsockHeader(hdr vsockHeader) []byte {
	buf := make([]byte, vsockHdrSize)
	binary.LittleEndian.PutUint64(buf[0:8], hdr.SrcCID)
	binary.LittleEndian.PutUint64(buf[8:16], hdr.DstCID)
	binary.LittleEndian.PutUint32(buf[16:20], hdr.SrcPort)
	binary.LittleEndian.PutUint32(buf[20:24], hdr.DstPort)
	binary.LittleEndian.PutUint32(buf[24:28], hdr.Len)
	binary.LittleEndian.PutUint16(buf[28:30], hdr.Type)
	binary.LittleEndian.PutUint16(buf[30:32], hdr.Op)
	binary.LittleEndian.PutUint32(buf[32:36], hdr.Flags)
	binary.LittleEndian.PutUint32(buf[36:40], hdr.BufAlloc)
	binary.LittleEndian.PutUint32(buf[40:44], hdr.FwdCnt)
	return buf
}

// opString returns a human-readable string for a vsock operation.
func opString(op uint16) string {
	switch op {
	case VIRTIO_VSOCK_OP_INVALID:
		return "INVALID"
	case VIRTIO_VSOCK_OP_REQUEST:
		return "REQUEST"
	case VIRTIO_VSOCK_OP_RESPONSE:
		return "RESPONSE"
	case VIRTIO_VSOCK_OP_RST:
		return "RST"
	case VIRTIO_VSOCK_OP_SHUTDOWN:
		return "SHUTDOWN"
	case VIRTIO_VSOCK_OP_RW:
		return "RW"
	case VIRTIO_VSOCK_OP_CREDIT_UPDATE:
		return "CREDIT_UPDATE"
	case VIRTIO_VSOCK_OP_CREDIT_REQUEST:
		return "CREDIT_REQUEST"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", op)
	}
}
