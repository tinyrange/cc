package virtio

import "encoding/binary"

// Virtio GPU Device ID
const gpuDeviceID = 16

// Virtio GPU feature bits
const (
	VIRTIO_GPU_F_VIRGL         = 0 // 3D mode (not used in 2D mode)
	VIRTIO_GPU_F_EDID          = 1 // EDID support
	VIRTIO_GPU_F_RESOURCE_UUID = 2 // Resource UUID support
	VIRTIO_GPU_F_RESOURCE_BLOB = 3 // Blob resources
	VIRTIO_GPU_F_CONTEXT_INIT  = 4 // Context initialization
)

// Virtio GPU command types
const (
	VIRTIO_GPU_CMD_GET_DISPLAY_INFO        = 0x0100
	VIRTIO_GPU_CMD_RESOURCE_CREATE_2D      = 0x0101
	VIRTIO_GPU_CMD_RESOURCE_UNREF          = 0x0102
	VIRTIO_GPU_CMD_SET_SCANOUT             = 0x0103
	VIRTIO_GPU_CMD_RESOURCE_FLUSH          = 0x0104
	VIRTIO_GPU_CMD_TRANSFER_TO_HOST_2D     = 0x0105
	VIRTIO_GPU_CMD_RESOURCE_ATTACH_BACKING = 0x0106
	VIRTIO_GPU_CMD_RESOURCE_DETACH_BACKING = 0x0107
	VIRTIO_GPU_CMD_GET_CAPSET_INFO         = 0x0108
	VIRTIO_GPU_CMD_GET_CAPSET              = 0x0109
	VIRTIO_GPU_CMD_GET_EDID                = 0x010a
	VIRTIO_GPU_CMD_RESOURCE_ASSIGN_UUID    = 0x010b
	VIRTIO_GPU_CMD_RESOURCE_CREATE_BLOB    = 0x010c
	VIRTIO_GPU_CMD_SET_SCANOUT_BLOB        = 0x010d

	// Cursor commands
	VIRTIO_GPU_CMD_UPDATE_CURSOR = 0x0300
	VIRTIO_GPU_CMD_MOVE_CURSOR   = 0x0301

	// Response types
	VIRTIO_GPU_RESP_OK_NODATA        = 0x1100
	VIRTIO_GPU_RESP_OK_DISPLAY_INFO  = 0x1101
	VIRTIO_GPU_RESP_OK_CAPSET_INFO   = 0x1102
	VIRTIO_GPU_RESP_OK_CAPSET        = 0x1103
	VIRTIO_GPU_RESP_OK_EDID          = 0x1104
	VIRTIO_GPU_RESP_OK_RESOURCE_UUID = 0x1105
	VIRTIO_GPU_RESP_OK_MAP_INFO      = 0x1106

	VIRTIO_GPU_RESP_ERR_UNSPEC              = 0x1200
	VIRTIO_GPU_RESP_ERR_OUT_OF_MEMORY       = 0x1201
	VIRTIO_GPU_RESP_ERR_INVALID_SCANOUT_ID  = 0x1202
	VIRTIO_GPU_RESP_ERR_INVALID_RESOURCE_ID = 0x1203
	VIRTIO_GPU_RESP_ERR_INVALID_CONTEXT_ID  = 0x1204
	VIRTIO_GPU_RESP_ERR_INVALID_PARAMETER   = 0x1205
)

// Virtio GPU formats
const (
	VIRTIO_GPU_FORMAT_B8G8R8A8_UNORM = 1
	VIRTIO_GPU_FORMAT_B8G8R8X8_UNORM = 2
	VIRTIO_GPU_FORMAT_A8R8G8B8_UNORM = 3
	VIRTIO_GPU_FORMAT_X8R8G8B8_UNORM = 4
	VIRTIO_GPU_FORMAT_R8G8B8A8_UNORM = 67
	VIRTIO_GPU_FORMAT_X8B8G8R8_UNORM = 68
	VIRTIO_GPU_FORMAT_A8B8G8R8_UNORM = 121
	VIRTIO_GPU_FORMAT_R8G8B8X8_UNORM = 134
)

// Maximum number of scanouts
const VIRTIO_GPU_MAX_SCANOUTS = 16

// virtioGPUCtrlHdr is the common header for all GPU commands
type virtioGPUCtrlHdr struct {
	Type    uint32
	Flags   uint32
	FenceID uint64
	CtxID   uint32
	RingIdx uint8
	Padding [3]uint8
}

const virtioGPUCtrlHdrSize = 24

func parseCtrlHdr(data []byte) virtioGPUCtrlHdr {
	return virtioGPUCtrlHdr{
		Type:    binary.LittleEndian.Uint32(data[0:4]),
		Flags:   binary.LittleEndian.Uint32(data[4:8]),
		FenceID: binary.LittleEndian.Uint64(data[8:16]),
		CtxID:   binary.LittleEndian.Uint32(data[16:20]),
		RingIdx: data[20],
	}
}

func (h *virtioGPUCtrlHdr) encode(data []byte) {
	binary.LittleEndian.PutUint32(data[0:4], h.Type)
	binary.LittleEndian.PutUint32(data[4:8], h.Flags)
	binary.LittleEndian.PutUint64(data[8:16], h.FenceID)
	binary.LittleEndian.PutUint32(data[16:20], h.CtxID)
	data[20] = h.RingIdx
	data[21] = 0
	data[22] = 0
	data[23] = 0
}

// virtioGPURect represents a rectangle
type virtioGPURect struct {
	X      uint32
	Y      uint32
	Width  uint32
	Height uint32
}

func parseRect(data []byte) virtioGPURect {
	return virtioGPURect{
		X:      binary.LittleEndian.Uint32(data[0:4]),
		Y:      binary.LittleEndian.Uint32(data[4:8]),
		Width:  binary.LittleEndian.Uint32(data[8:12]),
		Height: binary.LittleEndian.Uint32(data[12:16]),
	}
}

func (r *virtioGPURect) encode(data []byte) {
	binary.LittleEndian.PutUint32(data[0:4], r.X)
	binary.LittleEndian.PutUint32(data[4:8], r.Y)
	binary.LittleEndian.PutUint32(data[8:12], r.Width)
	binary.LittleEndian.PutUint32(data[12:16], r.Height)
}

// virtioGPUDisplayOne represents one display's information
type virtioGPUDisplayOne struct {
	R       virtioGPURect
	Enabled uint32
	Flags   uint32
}

const virtioGPUDisplayOneSize = 24

func (d *virtioGPUDisplayOne) encode(data []byte) {
	d.R.encode(data[0:16])
	binary.LittleEndian.PutUint32(data[16:20], d.Enabled)
	binary.LittleEndian.PutUint32(data[20:24], d.Flags)
}

// virtioGPURespDisplayInfo is the response to GET_DISPLAY_INFO
type virtioGPURespDisplayInfo struct {
	Hdr    virtioGPUCtrlHdr
	PModes [VIRTIO_GPU_MAX_SCANOUTS]virtioGPUDisplayOne
}

const virtioGPURespDisplayInfoSize = virtioGPUCtrlHdrSize + VIRTIO_GPU_MAX_SCANOUTS*virtioGPUDisplayOneSize

func (r *virtioGPURespDisplayInfo) encode(data []byte) {
	r.Hdr.encode(data[0:virtioGPUCtrlHdrSize])
	offset := virtioGPUCtrlHdrSize
	for i := 0; i < VIRTIO_GPU_MAX_SCANOUTS; i++ {
		r.PModes[i].encode(data[offset : offset+virtioGPUDisplayOneSize])
		offset += virtioGPUDisplayOneSize
	}
}

// virtioGPUResourceCreate2D is the request to create a 2D resource
type virtioGPUResourceCreate2D struct {
	Hdr        virtioGPUCtrlHdr
	ResourceID uint32
	Format     uint32
	Width      uint32
	Height     uint32
}

func parseResourceCreate2D(data []byte) virtioGPUResourceCreate2D {
	return virtioGPUResourceCreate2D{
		Hdr:        parseCtrlHdr(data[0:virtioGPUCtrlHdrSize]),
		ResourceID: binary.LittleEndian.Uint32(data[24:28]),
		Format:     binary.LittleEndian.Uint32(data[28:32]),
		Width:      binary.LittleEndian.Uint32(data[32:36]),
		Height:     binary.LittleEndian.Uint32(data[36:40]),
	}
}

// virtioGPUResourceUnref is the request to unref a resource
type virtioGPUResourceUnref struct {
	Hdr        virtioGPUCtrlHdr
	ResourceID uint32
	Padding    uint32
}

func parseResourceUnref(data []byte) virtioGPUResourceUnref {
	return virtioGPUResourceUnref{
		Hdr:        parseCtrlHdr(data[0:virtioGPUCtrlHdrSize]),
		ResourceID: binary.LittleEndian.Uint32(data[24:28]),
	}
}

// virtioGPUSetScanout is the request to set scanout
type virtioGPUSetScanout struct {
	Hdr        virtioGPUCtrlHdr
	R          virtioGPURect
	ScanoutID  uint32
	ResourceID uint32
}

func parseSetScanout(data []byte) virtioGPUSetScanout {
	return virtioGPUSetScanout{
		Hdr:        parseCtrlHdr(data[0:virtioGPUCtrlHdrSize]),
		R:          parseRect(data[24:40]),
		ScanoutID:  binary.LittleEndian.Uint32(data[40:44]),
		ResourceID: binary.LittleEndian.Uint32(data[44:48]),
	}
}

// virtioGPUResourceFlush is the request to flush a resource
type virtioGPUResourceFlush struct {
	Hdr        virtioGPUCtrlHdr
	R          virtioGPURect
	ResourceID uint32
	Padding    uint32
}

func parseResourceFlush(data []byte) virtioGPUResourceFlush {
	return virtioGPUResourceFlush{
		Hdr:        parseCtrlHdr(data[0:virtioGPUCtrlHdrSize]),
		R:          parseRect(data[24:40]),
		ResourceID: binary.LittleEndian.Uint32(data[40:44]),
	}
}

// virtioGPUTransferToHost2D is the request to transfer data to host
type virtioGPUTransferToHost2D struct {
	Hdr        virtioGPUCtrlHdr
	R          virtioGPURect
	Offset     uint64
	ResourceID uint32
	Padding    uint32
}

func parseTransferToHost2D(data []byte) virtioGPUTransferToHost2D {
	return virtioGPUTransferToHost2D{
		Hdr:        parseCtrlHdr(data[0:virtioGPUCtrlHdrSize]),
		R:          parseRect(data[24:40]),
		Offset:     binary.LittleEndian.Uint64(data[40:48]),
		ResourceID: binary.LittleEndian.Uint32(data[48:52]),
	}
}

// virtioGPUMemEntry represents a memory entry for backing store
type virtioGPUMemEntry struct {
	Addr    uint64
	Length  uint32
	Padding uint32
}

const virtioGPUMemEntrySize = 16

func parseMemEntry(data []byte) virtioGPUMemEntry {
	return virtioGPUMemEntry{
		Addr:   binary.LittleEndian.Uint64(data[0:8]),
		Length: binary.LittleEndian.Uint32(data[8:12]),
	}
}

// virtioGPUResourceAttachBacking is the request to attach backing store
type virtioGPUResourceAttachBacking struct {
	Hdr        virtioGPUCtrlHdr
	ResourceID uint32
	NrEntries  uint32
}

func parseResourceAttachBacking(data []byte) virtioGPUResourceAttachBacking {
	return virtioGPUResourceAttachBacking{
		Hdr:        parseCtrlHdr(data[0:virtioGPUCtrlHdrSize]),
		ResourceID: binary.LittleEndian.Uint32(data[24:28]),
		NrEntries:  binary.LittleEndian.Uint32(data[28:32]),
	}
}

// virtioGPUResourceDetachBacking is the request to detach backing store
type virtioGPUResourceDetachBacking struct {
	Hdr        virtioGPUCtrlHdr
	ResourceID uint32
	Padding    uint32
}

func parseResourceDetachBacking(data []byte) virtioGPUResourceDetachBacking {
	return virtioGPUResourceDetachBacking{
		Hdr:        parseCtrlHdr(data[0:virtioGPUCtrlHdrSize]),
		ResourceID: binary.LittleEndian.Uint32(data[24:28]),
	}
}

// virtioGPUCursorPos is the cursor position
type virtioGPUCursorPos struct {
	ScanoutID uint32
	X         uint32
	Y         uint32
	Padding   uint32
}

// virtioGPUUpdateCursor is the request to update cursor
type virtioGPUUpdateCursor struct {
	Hdr        virtioGPUCtrlHdr
	Pos        virtioGPUCursorPos
	ResourceID uint32
	HotX       uint32
	HotY       uint32
	Padding    uint32
}

func parseUpdateCursor(data []byte) virtioGPUUpdateCursor {
	return virtioGPUUpdateCursor{
		Hdr: parseCtrlHdr(data[0:virtioGPUCtrlHdrSize]),
		Pos: virtioGPUCursorPos{
			ScanoutID: binary.LittleEndian.Uint32(data[24:28]),
			X:         binary.LittleEndian.Uint32(data[28:32]),
			Y:         binary.LittleEndian.Uint32(data[32:36]),
		},
		ResourceID: binary.LittleEndian.Uint32(data[40:44]),
		HotX:       binary.LittleEndian.Uint32(data[44:48]),
		HotY:       binary.LittleEndian.Uint32(data[48:52]),
	}
}

// gpuResource2D represents a 2D resource in the host
type gpuResource2D struct {
	id      uint32
	format  uint32
	width   uint32
	height  uint32
	backing []virtioGPUMemEntry
	pixels  []byte // Host-side pixel buffer
}

// bytesPerPixel returns the bytes per pixel for a given format
func bytesPerPixel(format uint32) uint32 {
	switch format {
	case VIRTIO_GPU_FORMAT_B8G8R8A8_UNORM,
		VIRTIO_GPU_FORMAT_B8G8R8X8_UNORM,
		VIRTIO_GPU_FORMAT_A8R8G8B8_UNORM,
		VIRTIO_GPU_FORMAT_X8R8G8B8_UNORM,
		VIRTIO_GPU_FORMAT_R8G8B8A8_UNORM,
		VIRTIO_GPU_FORMAT_X8B8G8R8_UNORM,
		VIRTIO_GPU_FORMAT_A8B8G8R8_UNORM,
		VIRTIO_GPU_FORMAT_R8G8B8X8_UNORM:
		return 4
	default:
		return 4 // Default to 32-bit
	}
}
