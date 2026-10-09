package virtio

import (
	"encoding/binary"
	"reflect"
	"testing"
)

type inertFSBackend struct{}

func (inertFSBackend) Init() (uint32, uint32) { return 128 << 10, 0 }
func (inertFSBackend) GetAttr(uint64) (FuseAttr, int32) {
	return FuseAttr{}, -linuxENOENT
}
func (inertFSBackend) Lookup(uint64, string) (uint64, FuseAttr, int32) {
	return 0, FuseAttr{}, -linuxENOENT
}
func (inertFSBackend) Open(uint64, uint32) (uint64, int32) { return 0, -linuxENOENT }
func (inertFSBackend) Release(uint64, uint64)              {}
func (inertFSBackend) Read(uint64, uint64, uint64, uint32) ([]byte, int32) {
	return nil, -linuxENOENT
}
func (inertFSBackend) OpenDir(uint64, uint32) (uint64, int32) { return 0, -linuxENOENT }
func (inertFSBackend) ReadDir(uint64, uint64, uint64, uint32) ([]byte, int32) {
	return nil, -linuxENOENT
}
func (inertFSBackend) ReleaseDir(uint64, uint64) {}
func (inertFSBackend) Readlink(uint64) (string, int32) {
	return "", -linuxENOENT
}
func (inertFSBackend) StatFS(uint64) (uint64, uint64, uint64, uint64, uint64, uint64, uint64, uint64, int32) {
	return 0, 0, 0, 0, 0, 4096, 4096, 255, 0
}

func TestDecodeFUSERequest(t *testing.T) {
	raw := make([]byte, fuseInHeaderSize+7, fuseInHeaderSize+16)
	binary.LittleEndian.PutUint32(raw[0:4], uint32(len(raw)))
	binary.LittleEndian.PutUint32(raw[4:8], fuseWrite)
	binary.LittleEndian.PutUint64(raw[8:16], 0x1020304050607080)
	binary.LittleEndian.PutUint64(raw[16:24], 71)
	binary.LittleEndian.PutUint32(raw[24:28], 1000)
	binary.LittleEndian.PutUint32(raw[28:32], 1001)
	binary.LittleEndian.PutUint32(raw[32:36], 42)
	copy(raw[fuseInHeaderSize:], "payload")

	req, err := decodeFUSERequest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if req.opcode != fuseWrite || req.unique != 0x1020304050607080 || req.nodeID != 71 {
		t.Fatalf("decoded identity = opcode %d unique %#x node %d", req.opcode, req.unique, req.nodeID)
	}
	if req.callerUID != 1000 || req.callerGID != 1001 || req.callerPID != 42 {
		t.Fatalf("decoded caller = uid %d gid %d pid %d", req.callerUID, req.callerGID, req.callerPID)
	}
	if string(req.body) != "payload" {
		t.Fatalf("decoded body = %q", req.body)
	}
	if len(req.raw) != len(raw) {
		t.Fatalf("decoded request length = %d, want %d", len(req.raw), len(raw))
	}
}

func TestDecodeFUSERequestRejectsInvalidLengths(t *testing.T) {
	tests := []struct {
		name     string
		raw      []byte
		declared uint32
	}{
		{name: "short header", raw: make([]byte, fuseInHeaderSize-1)},
		{name: "short declared length", raw: make([]byte, fuseInHeaderSize), declared: fuseInHeaderSize - 1},
		{name: "truncated body", raw: make([]byte, fuseInHeaderSize), declared: fuseInHeaderSize + 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if len(test.raw) >= 4 {
				binary.LittleEndian.PutUint32(test.raw[:4], test.declared)
			}
			if _, err := decodeFUSERequest(test.raw); err == nil {
				t.Fatal("decode succeeded")
			}
		})
	}
}

func TestDecodeFUSERequestExcludesDescriptorPadding(t *testing.T) {
	raw := make([]byte, fuseInHeaderSize+8)
	binary.LittleEndian.PutUint32(raw[:4], fuseInHeaderSize)
	for index := fuseInHeaderSize; index < len(raw); index++ {
		raw[index] = 0xff
	}
	req, err := decodeFUSERequest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.raw) != fuseInHeaderSize || len(req.body) != 0 {
		t.Fatalf("decoded lengths = raw %d body %d", len(req.raw), len(req.body))
	}
}

func TestFUSEDispatcherEnforcesAndReleasesPOSIXLocks(t *testing.T) {
	device := NewFS(0, 0, 0, "locks", inertFSBackend{})
	dispatchLock := func(opcode uint32, owner uint64, lockType uint32) fsReply {
		raw := make([]byte, fuseInHeaderSize+fuseLKInSize)
		binary.LittleEndian.PutUint32(raw[0:4], uint32(len(raw)))
		binary.LittleEndian.PutUint32(raw[4:8], opcode)
		binary.LittleEndian.PutUint64(raw[8:16], uint64(opcode)+owner)
		binary.LittleEndian.PutUint64(raw[16:24], 9)
		binary.LittleEndian.PutUint64(raw[40:48], 1)
		binary.LittleEndian.PutUint64(raw[48:56], owner)
		binary.LittleEndian.PutUint64(raw[64:72], ^uint64(0))
		binary.LittleEndian.PutUint32(raw[72:76], lockType)
		binary.LittleEndian.PutUint32(raw[76:80], uint32(owner))
		result, err := device.dispatcher.Dispatch(raw)
		if err != nil {
			t.Fatalf("dispatch %s: %v", fuseOpcodeName(opcode), err)
		}
		return result.reply
	}

	if reply := dispatchLock(fuseSetLK, 100, linuxFWrLck); reply.errno != 0 {
		t.Fatalf("first SETLK errno = %d", reply.errno)
	}
	get := dispatchLock(fuseGetLK, 200, linuxFRdLck)
	if get.errno != 0 || len(get.extra) != fuseLKOutSize || binary.LittleEndian.Uint32(get.extra[16:20]) != linuxFWrLck || binary.LittleEndian.Uint32(get.extra[20:24]) == 0 {
		t.Fatalf("GETLK reply = errno %d extra %x", get.errno, get.extra)
	}
	if reply := dispatchLock(fuseSetLK, 200, linuxFRdLck); reply.errno != -linuxEAGAIN {
		t.Fatalf("conflicting SETLK errno = %d", reply.errno)
	}

	flush := make([]byte, fuseInHeaderSize+24)
	binary.LittleEndian.PutUint32(flush[0:4], uint32(len(flush)))
	binary.LittleEndian.PutUint32(flush[4:8], fuseFlush)
	binary.LittleEndian.PutUint64(flush[8:16], 77)
	binary.LittleEndian.PutUint64(flush[16:24], 9)
	binary.LittleEndian.PutUint32(flush[48:52], fuseFlushLockOwner)
	binary.LittleEndian.PutUint64(flush[56:64], 100)
	if result, err := device.dispatcher.Dispatch(flush); err != nil || result.reply.errno != 0 {
		t.Fatalf("FLUSH lock owner = reply %+v, err %v", result.reply, err)
	}
	if reply := dispatchLock(fuseSetLK, 200, linuxFRdLck); reply.errno != 0 {
		t.Fatalf("SETLK after FLUSH errno = %d", reply.errno)
	}
	release := make([]byte, fuseInHeaderSize+24)
	binary.LittleEndian.PutUint32(release[0:4], uint32(len(release)))
	binary.LittleEndian.PutUint32(release[4:8], fuseRelease)
	binary.LittleEndian.PutUint64(release[8:16], 78)
	binary.LittleEndian.PutUint64(release[16:24], 9)
	binary.LittleEndian.PutUint32(release[52:56], fuseReleaseFlockUnlock)
	binary.LittleEndian.PutUint64(release[56:64], 200)
	if result, err := device.dispatcher.Dispatch(release); err != nil || result.reply.errno != 0 {
		t.Fatalf("RELEASE flock owner = reply %+v, err %v", result.reply, err)
	}
	if reply := dispatchLock(fuseSetLK, 300, linuxFWrLck); reply.errno != 0 {
		t.Fatalf("SETLK after RELEASE errno = %d", reply.errno)
	}
}

func FuzzDecodeFUSERequest(f *testing.F) {
	valid := make([]byte, fuseInHeaderSize+4)
	binary.LittleEndian.PutUint32(valid[:4], uint32(len(valid)))
	binary.LittleEndian.PutUint32(valid[4:8], fuseGetAttr)
	f.Add(valid)
	f.Add([]byte(nil))
	f.Add(make([]byte, fuseInHeaderSize))

	f.Fuzz(func(t *testing.T, raw []byte) {
		req, err := decodeFUSERequest(raw)
		if err != nil {
			return
		}
		if len(req.raw) < fuseInHeaderSize {
			t.Fatalf("decoded short request: %d", len(req.raw))
		}
		declared := binary.LittleEndian.Uint32(req.raw[:4])
		if int(declared) != len(req.raw) {
			t.Fatalf("decoded length = %d, header = %d", len(req.raw), declared)
		}
		if len(req.body) != len(req.raw)-fuseInHeaderSize {
			t.Fatalf("decoded body length = %d, raw = %d", len(req.body), len(req.raw))
		}
	})
}

func FuzzFUSEDispatcher(f *testing.F) {
	seed := make([]byte, fuseInHeaderSize+128)
	binary.LittleEndian.PutUint32(seed[:4], uint32(len(seed)))
	binary.LittleEndian.PutUint32(seed[4:8], fuseWrite)
	f.Add(seed)

	device := NewFS(0, 0, 0, "fuzz", inertFSBackend{})
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = device.dispatcher.Dispatch(raw)
	})
}

func BenchmarkDecodeFUSERequest(b *testing.B) {
	raw := make([]byte, fuseInHeaderSize+24)
	binary.LittleEndian.PutUint32(raw[:4], uint32(len(raw)))
	binary.LittleEndian.PutUint32(raw[4:8], fuseRead)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := decodeFUSERequest(raw); err != nil {
			b.Fatal(err)
		}
	}
}

// RPC backends conventionally return positive errno. The wire must never expose
// a positive out_header.error: VFS can interpret it as success without an inode.
type positiveErrnoFSBackend struct{ inertFSBackend }

func (positiveErrnoFSBackend) Lookup(uint64, string) (uint64, FuseAttr, int32) {
	return 0, FuseAttr{}, linuxENOENT
}
func (positiveErrnoFSBackend) Create(uint64, string, uint32, uint32, uint32, uint32) (uint64, uint64, FuseAttr, int32) {
	return 0, 0, FuseAttr{}, linuxEACCES
}

func TestFUSEDispatcherNormalizesPositiveBackendErrno(t *testing.T) {
	device := NewFS(0, 0, 0, "errno", positiveErrnoFSBackend{})
	for _, tc := range []struct {
		opcode uint32
		size   int
		want   int32
	}{
		{fuseLookup, 0, -linuxENOENT}, {fuseCreate, 16, -linuxEACCES},
	} {
		raw := make([]byte, fuseInHeaderSize+tc.size+5)
		binary.LittleEndian.PutUint32(raw[0:4], uint32(len(raw)))
		binary.LittleEndian.PutUint32(raw[4:8], tc.opcode)
		binary.LittleEndian.PutUint64(raw[8:16], 77)
		binary.LittleEndian.PutUint64(raw[16:24], 1)
		copy(raw[fuseInHeaderSize+tc.size:], "live")
		result, err := device.dispatcher.Dispatch(raw)
		if err != nil {
			t.Fatal(err)
		}
		wire := result.reply.Bytes()
		if got := int32(binary.LittleEndian.Uint32(wire[4:8])); got != tc.want {
			t.Fatalf("opcode %d error=%d, want %d", tc.opcode, got, tc.want)
		}
		if len(wire) != fuseOutHeaderSize {
			t.Fatalf("error reply included success payload: %x", wire)
		}
	}
	for _, errno := range []int32{linuxEACCES, -linuxEACCES} {
		wire := fuseReply(78, errno, []byte("must not appear in error reply")).Bytes()
		if len(wire) != fuseOutHeaderSize || int32(binary.LittleEndian.Uint32(wire[4:8])) != -linuxEACCES {
			t.Fatalf("malformed error reply: %x", wire)
		}
	}
}

type disappearingReadDirFSBackend struct {
	inertFSBackend
	missingErrno int32
	calls        []uint64
}

func disappearingReadDirEntry(node, cookie uint64, name string) []byte {
	entry := make([]byte, align8(fuseDirentBaseSize+len(name)))
	binary.LittleEndian.PutUint64(entry[0:8], node)
	binary.LittleEndian.PutUint64(entry[8:16], cookie)
	binary.LittleEndian.PutUint32(entry[16:20], uint32(len(name)))
	binary.LittleEndian.PutUint32(entry[20:24], 8)
	copy(entry[24:], name)
	return entry
}

func (b *disappearingReadDirFSBackend) ReadDir(_, _ uint64, off uint64, _ uint32) ([]byte, int32) {
	b.calls = append(b.calls, off)
	switch off {
	case 0:
		return disappearingReadDirEntry(2, 7, "gone-page"), 0
	case 7:
		page := disappearingReadDirEntry(3, 19, "gone-entry")
		return append(page, disappearingReadDirEntry(4, 42, "alive-a")...), 0
	case 42:
		return disappearingReadDirEntry(5, 61, "alive-b"), 0
	case 61:
		return nil, 0
	default:
		return nil, -linuxEINVAL
	}
}
func (b *disappearingReadDirFSBackend) GetAttr(node uint64) (FuseAttr, int32) {
	if node == 2 || node == 3 {
		return FuseAttr{}, b.missingErrno
	}
	return FuseAttr{Ino: node, Mode: 0100644, NLink: 1}, 0
}

func TestReadDirPlusSkipsVanishedEntriesWithEitherErrnoSign(t *testing.T) {
	for _, tc := range []struct {
		name  string
		errno int32
	}{{"positive", linuxENOENT}, {"negative", -linuxENOENT}} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &disappearingReadDirFSBackend{missingErrno: tc.errno}
			device := NewFS(0, 0, 0, "vanishing", backend)
			off := uint64(0)
			for i, name := range []string{"alive-a", "alive-b", ""} {
				req := make([]byte, fuseInHeaderSize+24)
				binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
				binary.LittleEndian.PutUint32(req[4:8], fuseReadDirPlus)
				binary.LittleEndian.PutUint64(req[8:16], uint64(i+1))
				binary.LittleEndian.PutUint64(req[16:24], 1)
				binary.LittleEndian.PutUint64(req[40:48], 9)
				binary.LittleEndian.PutUint64(req[48:56], off)
				binary.LittleEndian.PutUint32(req[56:60], 4096)
				reply, err := dispatchFUSEForTest(device, req)
				if err != nil || reply.errno != 0 {
					t.Fatalf("page %d: error=%v errno=%d", i, err, reply.errno)
				}
				if name == "" {
					if len(reply.extra) != 0 {
						t.Fatal("expected real EOF")
					}
					continue
				}
				if len(reply.extra) < fuseDirentPlusBaseSize {
					t.Fatalf("false EOF after vanished entry at cookie %d", off)
				}
				data := reply.extra[fuseEntryOutSize:]
				n := int(binary.LittleEndian.Uint32(data[16:20]))
				if len(data) < 24+n || string(data[24:24+n]) != name {
					t.Fatalf("wrong surviving entry: %x", data)
				}
				if len(reply.extra) != align8(fuseDirentPlusBaseSize+n) {
					t.Fatal("vanished entry leaked into result")
				}
				node := uint64(i + 4)
				if binary.LittleEndian.Uint64(reply.extra[0:8]) != node || binary.LittleEndian.Uint64(reply.extra[40:48]) != node || binary.LittleEndian.Uint64(data[0:8]) != node {
					t.Fatal("surviving node identity changed")
				}
				off = binary.LittleEndian.Uint64(data[8:16])
			}
			if !reflect.DeepEqual(backend.calls, []uint64{0, 7, 42, 61}) {
				t.Fatalf("directory cookies skipped or reset: %v", backend.calls)
			}
		})
	}
}

func TestReadDirPlusDoesNotHideOtherBackendErrors(t *testing.T) {
	for _, errno := range []int32{linuxEIO, -linuxEIO} {
		backend := &disappearingReadDirFSBackend{missingErrno: errno}
		device := NewFS(0, 0, 0, "io-error", backend)
		data, got := device.dispatcher.(*fuseServer).readDirPlus(1, 9, 0, 4096)
		if got != errno || len(data) != 0 {
			t.Fatalf("non-ENOENT error became EOF/success: errno=%d data=%x", got, data)
		}
	}
}
