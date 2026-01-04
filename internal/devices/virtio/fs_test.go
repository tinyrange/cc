package virtio

import (
	"encoding/binary"
	"testing"
)

type recordRenameBackend struct {
	emptyBackend
	called    bool
	oldParent uint64
	oldName   string
	newParent uint64
	newName   string
	flags     uint32
}

func (r *recordRenameBackend) Rename(oldParent uint64, oldName string, newParent uint64, newName string, flags uint32) int32 {
	r.called = true
	r.oldParent = oldParent
	r.oldName = oldName
	r.newParent = newParent
	r.newName = newName
	r.flags = flags
	return 0
}

func makeFuseReq(opcode uint32, nodeID uint64, payload []byte) []byte {
	req := make([]byte, fuseHdrInSize+len(payload))
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], opcode)
	binary.LittleEndian.PutUint64(req[8:16], 1) // unique
	binary.LittleEndian.PutUint64(req[16:24], nodeID)
	copy(req[fuseHdrInSize:], payload)
	return req
}

func TestDispatchFUSERename2ParsesNamesAndFlags(t *testing.T) {
	be := &recordRenameBackend{}
	fs := &FS{backend: be}

	oldParent := uint64(111)
	newParent := uint64(222)
	oldName := "from"
	newName := "to"
	flags := uint32(0)

	payload := make([]byte, 0, 16+len(oldName)+1+len(newName)+1)
	tmp := make([]byte, 16)
	binary.LittleEndian.PutUint64(tmp[0:8], newParent)
	binary.LittleEndian.PutUint32(tmp[8:12], flags)
	// tmp[12:16] padding
	payload = append(payload, tmp...)
	payload = append(payload, []byte(oldName)...)
	payload = append(payload, 0)
	payload = append(payload, []byte(newName)...)
	payload = append(payload, 0)

	req := makeFuseReq(FUSE_RENAME2, oldParent, payload)
	resp := make([]byte, 256)
	outLen, err := fs.dispatchFUSE(req, resp)
	if err != nil {
		t.Fatalf("dispatchFUSE: %v", err)
	}
	if outLen != fuseHdrOutSize {
		t.Fatalf("outLen=%d want %d", outLen, fuseHdrOutSize)
	}
	if got := int32(binary.LittleEndian.Uint32(resp[4:8])); got != 0 {
		t.Fatalf("resp errno=%d want 0", got)
	}
	if !be.called {
		t.Fatalf("backend Rename not called")
	}
	if be.oldParent != oldParent || be.newParent != newParent || be.oldName != oldName || be.newName != newName || be.flags != flags {
		t.Fatalf("Rename args mismatch: got oldParent=%d oldName=%q newParent=%d newName=%q flags=0x%x",
			be.oldParent, be.oldName, be.newParent, be.newName, be.flags)
	}
}

func TestDispatchFUSERenameDoesNotConsumeFlagsBytes(t *testing.T) {
	be := &recordRenameBackend{}
	fs := &FS{backend: be}

	oldParent := uint64(111)
	newParent := uint64(222)
	oldName := "from"
	newName := "to"

	payload := make([]byte, 0, 8+len(oldName)+1+len(newName)+1)
	tmp := make([]byte, 8)
	binary.LittleEndian.PutUint64(tmp[0:8], newParent)
	payload = append(payload, tmp...)
	payload = append(payload, []byte(oldName)...)
	payload = append(payload, 0)
	payload = append(payload, []byte(newName)...)
	payload = append(payload, 0)

	req := makeFuseReq(FUSE_RENAME, oldParent, payload)
	resp := make([]byte, 256)
	outLen, err := fs.dispatchFUSE(req, resp)
	if err != nil {
		t.Fatalf("dispatchFUSE: %v", err)
	}
	if outLen != fuseHdrOutSize {
		t.Fatalf("outLen=%d want %d", outLen, fuseHdrOutSize)
	}
	if got := int32(binary.LittleEndian.Uint32(resp[4:8])); got != 0 {
		t.Fatalf("resp errno=%d want 0", got)
	}
	if !be.called {
		t.Fatalf("backend Rename not called")
	}
	if be.flags != 0 {
		t.Fatalf("flags=0x%x want 0", be.flags)
	}
}
