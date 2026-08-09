package virgl

import (
	"strings"
	"testing"
)

func TestTranslateTGSIRejectsUnsupportedOpcode(t *testing.T) {
	_, _, err := translateTGSI("VERT\nDCL OUT[0], POSITION\n 0: EXPLODE OUT[0]\n 1: END\n")
	if err == nil {
		t.Fatal("unsupported TGSI opcode was accepted")
	}
}

func TestTranslateTGSIUsesHostInstancingSystemValues(t *testing.T) {
	shader := `VERT
DCL OUT[0], POSITION
DCL OUT[1], GENERIC[0]
DCL SV[0], INSTANCEID
DCL SV[1], VERTEXID
0: MOV OUT[0], SV[1]
1: MOV OUT[1], SV[0]
2: END
`
	_, glsl, err := translateTGSI(shader)
	if err != nil {
		t.Fatal(err)
	}
	for _, builtin := range []string{"ivec4(gl_InstanceID)", "ivec4(gl_VertexID)"} {
		if !strings.Contains(glsl, builtin) {
			t.Fatalf("translated shader does not use %s:\n%s", builtin, glsl)
		}
	}
}
