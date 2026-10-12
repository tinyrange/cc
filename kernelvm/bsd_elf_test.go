package kernelvm

import (
	"encoding/binary"
	"testing"
)

func TestBSDKernelRejectsMalformedLoadRanges(t *testing.T) {
	b := make([]byte, 128)
	copy(b, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	le := binary.LittleEndian
	le.PutUint16(b[16:], 2)
	le.PutUint16(b[18:], 62)
	le.PutUint32(b[20:], 1)
	le.PutUint64(b[24:], 0xffffffff81001000)
	le.PutUint64(b[32:], 64)
	le.PutUint16(b[52:], 64)
	le.PutUint16(b[54:], 56)
	le.PutUint16(b[56:], 1)
	le.PutUint32(b[64:], 1)
	le.PutUint64(b[72:], 120)
	le.PutUint64(b[88:], 0x100000)
	le.PutUint64(b[96:], 8)
	le.PutUint64(b[104:], 8)
	if err := validateBSDKernel(b); err != nil {
		t.Fatal(err)
	}
	for _, field := range []int{72, 88, 96, 104} {
		malformed := append([]byte(nil), b...)
		le.PutUint64(malformed[field:], ^uint64(0))
		if err := validateBSDKernel(malformed); err == nil {
			t.Fatalf("accepted corrupt field %d", field)
		}
	}
}
