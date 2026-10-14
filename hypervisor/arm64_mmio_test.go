package hypervisor

import "testing"

func TestARM64MMIOInstructionDecode(t *testing.T) {
	for _, tc := range []struct {
		word                uint32
		offset              int64
		mode                uint32
		size                int
		write, signed, wide bool
	}{
		{0xb8080d09, 128, 3, 4, true, false, false}, // original PCC GIC initialization
		{0xf81f8420, -8, 1, 8, true, false, true},
		{0x38c01c20, 1, 3, 1, false, true, false},
		{0xb85fc420, -4, 1, 4, false, false, false},
	} {
		i, err := DecodeARM64MMIOInstruction(tc.word)
		if err != nil || i.Offset != tc.offset || i.Mode != tc.mode || i.Size != tc.size || i.Write != tc.write || i.Signed != tc.signed || i.Wide != tc.wide {
			t.Fatalf("%#x: %+v %v", tc.word, i, err)
		}
	}
	for _, word := range []uint32{0xd503201f, 0xa9000020, 0x3c800020, 0xb8206820, 0xb8000c21, 0xb8000820} {
		if _, err := DecodeARM64MMIOInstruction(word); err == nil {
			t.Fatalf("accepted unsupported/unpredictable instruction %#x", word)
		}
	}
}
