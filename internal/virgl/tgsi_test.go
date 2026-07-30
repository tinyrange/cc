package virgl

import "testing"

func TestTranslateTGSIRejectsUnsupportedOpcode(t *testing.T) {
	_, _, err := translateTGSI("VERT\nDCL OUT[0], POSITION\n 0: EXPLODE OUT[0]\n 1: END\n")
	if err == nil {
		t.Fatal("unsupported TGSI opcode was accepted")
	}
}
