//go:build darwin

package virgl

import (
	"encoding/binary"
	"image"
	"math"
	"strings"
	"testing"

	"j5.nz/cc/internal/virtio"
)

func TestVertexTGSIRequestsPositionInvarianceForMultipassDepth(t *testing.T) {
	const vertex = `VERT
DCL IN[0]
DCL OUT[0], POSITION
  0: MOV OUT[0], IN[0]
  1: END`
	stage, glsl, err := translateTGSI(vertex)
	if err != nil {
		t.Fatal(err)
	}
	if stage != tgsiVertex {
		t.Fatalf("translated stage = %d, want vertex", stage)
	}
	lines := strings.Split(glsl, "\n")
	got := ""
	if len(lines) >= 2 {
		got = lines[1]
	}
	if got != "invariant gl_Position;" {
		t.Fatalf("vertex position qualifier = %q, want %q", got, "invariant gl_Position;")
	}
}

func TestFragmentedTGSIShaderIsReassembledBeforeTranslation(t *testing.T) {
	const source = "VERT\nDCL IN[0]\nDCL OUT[0], POSITION\n  0: MOV OUT[0], IN[0]\n  1: END\n"
	text := append([]byte(source), 0)
	for len(text)%4 != 0 {
		text = append(text, 0)
	}
	const split = 20
	first := []uint32{41, tgsiVertex, uint32(len(source) + 1), 32, 0}
	first = append(first, shaderTestWords(text[:split])...)
	continuation := []uint32{41, tgsiVertex, uint32(1<<31 | split), 32, 0}
	continuation = append(continuation, shaderTestWords(text[split:])...)

	context := newHostContext()
	if err := context.createShader(first); err != nil {
		t.Fatal(err)
	}
	if _, complete := context.shaders[41]; complete {
		t.Fatal("fragmented shader was translated before its continuation")
	}
	if err := context.createShader(continuation); err != nil {
		t.Fatal(err)
	}
	if _, complete := context.shaders[41]; !complete {
		t.Fatal("shader was not completed after its final continuation")
	}
	if _, pending := context.shaderAssemblies[41]; pending {
		t.Fatal("completed shader retained its assembly buffer")
	}
}

func shaderTestWords(data []byte) []uint32 {
	result := make([]uint32, len(data)/4)
	for index := range result {
		result[index] = binary.LittleEndian.Uint32(data[index*4:])
	}
	return result
}

func TestKMSCubeTGSICompilesInDarwinHostContext(t *testing.T) {
	const vertex = `VERT
DCL IN[0]
DCL IN[1]
DCL IN[2]
DCL OUT[0], POSITION
DCL OUT[1], GENERIC[9]
DCL CONST[0..10]
DCL TEMP[0..16]
IMM[0] UINT32 {1073741824, 1101004800, 0, 1065353216}
DCL TEMP[17..20]
  0: MUL TEMP[0], CONST[5], IN[0].yyyy
  1: MAD TEMP[1], CONST[4], IN[0].xxxx, TEMP[0]
  2: MAD TEMP[2], CONST[6], IN[0].zzzz, TEMP[1]
  3: MAD OUT[0], CONST[7], IN[0].wwww, TEMP[2]
  4: MUL TEMP[3], CONST[1], IN[0].yyyy
  5: MAD TEMP[4], CONST[0], IN[0].xxxx, TEMP[3]
  6: MAD TEMP[5], CONST[2], IN[0].zzzz, TEMP[4]
  7: MAD TEMP[6], CONST[3], IN[0].wwww, TEMP[5]
  8: DIV TEMP[7].xyz, TEMP[6].xyzz, TEMP[6].wwwx
  9: ADD TEMP[8].xyz, IMM[0].xxyx, -TEMP[7].xyzx
 10: MUL TEMP[9].xyz, CONST[9].xyzz, IN[1].yyyx
 11: MAD TEMP[10].xyz, CONST[8].xyzx, IN[1].xxxx, TEMP[9].xyzx
 12: MAD TEMP[11].xyz, CONST[10].xyzx, IN[1].zzzx, TEMP[10].xyzx
 13: DP3 TEMP[12].x, TEMP[8].xyzx, TEMP[8].xyzx
 14: RSQ TEMP[13].x, TEMP[12].xxxx
 15: MUL TEMP[14].xyz, TEMP[8].xyzz, TEMP[13].xxxx
 16: DP3 TEMP[15].x, TEMP[11].xyzx, TEMP[14].xyzx
 17: MAX TEMP[16].x, IMM[0].zzzz, TEMP[15].xxxx
 18: MUL OUT[1].xyz, TEMP[16].xxxx, IN[2].xyzz
 19: MOV OUT[1].w, IMM[0].wwww
 20: END`
	const fragment = `FRAG
PROPERTY FS_COLOR0_WRITES_ALL_CBUFS 1
DCL IN[0], GENERIC[9], PERSPECTIVE
DCL OUT[0], COLOR
DCL TEMP[0..3]
  0: MOV OUT[0], IN[0]
  1: END`

	vertexStage, vertexGLSL, err := translateTGSI(vertex)
	if err != nil {
		t.Fatal(err)
	}
	fragmentStage, fragmentGLSL, err := translateTGSI(fragment)
	if err != nil {
		t.Fatal(err)
	}
	if vertexStage != tgsiVertex || fragmentStage != tgsiFragment {
		t.Fatalf("translated stages = %d/%d", vertexStage, fragmentStage)
	}

	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()
	if err := host.dispatch(func() error {
		program, err := host.gl.compileProgram(vertexGLSL, fragmentGLSL)
		if err == nil {
			host.gl.deleteProgram(program)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTexturedIndexedSceneTGSICompilesAndLinks(t *testing.T) {
	const vertex = `VERT
DCL IN[0]
DCL IN[1]
DCL OUT[0], POSITION
DCL OUT[1]
  0: MOV OUT[0], IN[0]
  1: MOV OUT[1], IN[1]
  2: END`
	const fragment = `FRAG
DCL IN[0], GENERIC[9], PERSPECTIVE
DCL IN[1], GENERIC[10], PERSPECTIVE
DCL OUT[0], COLOR
DCL SAMP[0]
DCL SVIEW[0], 2D, FLOAT
DCL TEMP[0]
  0: TEX TEMP[0], IN[0], SAMP[0], 2D
  1: ADD OUT[0], TEMP[0], IN[1]
  2: END`

	_, vertexGLSL, err := translateTGSI(vertex)
	if err != nil {
		t.Fatal(err)
	}
	_, fragmentGLSL, err := translateTGSI(fragment)
	if err != nil {
		t.Fatal(err)
	}
	vertexGLSL = linkTGSIInterfaces(vertexGLSL, fragmentGLSL)
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()
	if err := host.dispatch(func() error {
		program, err := host.gl.compileProgram(vertexGLSL, fragmentGLSL)
		if err == nil {
			host.gl.deleteProgram(program)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMaskedTGSIfaceBuiltinDoesNotShiftGenericVaryings(t *testing.T) {
	const vertex = `VERT
DCL IN[0]
DCL OUT[0], POSITION
DCL OUT[1].xy, GENERIC[9]
IMM[0] FLT32 {0.5, 0.0, 0.0, 1.0}
  0: MOV OUT[0], IN[0]
  1: MOV OUT[1].xy, IMM[0].xyxx
  2: END`
	const fragment = `FRAG
DCL IN[0].x, FACE, CONSTANT
DCL IN[1].xy, GENERIC[9], PERSPECTIVE
DCL OUT[0], COLOR
DCL TEMP[0..1]
IMM[0] FLT32 {0.0, 0.0, 0.0, 0.0}
IMM[1] FLT32 {1.0, 0.0, 0.0, 1.0}
IMM[2] FLT32 {0.0, 0.0, 1.0, 1.0}
  0: SGE TEMP[0], IN[0], IMM[0]
  1: UCMP TEMP[1], TEMP[0], IMM[1], IMM[2]
  2: MOV TEMP[1].y, IN[1].x
  3: MOV OUT[0], TEMP[1]
  4: END`
	_, vertexGLSL, err := translateTGSI(vertex)
	if err != nil {
		t.Fatal(err)
	}
	_, fragmentGLSL, err := translateTGSI(fragment)
	if err != nil {
		t.Fatal(err)
	}
	vertexGLSL = linkTGSIInterfaces(vertexGLSL, fragmentGLSL)

	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()
	output := virtio.GPUResource3D{ID: 1, Target: 2, Format: 67, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	positions := virtio.GPUResource3D{ID: 2, Target: 0, Width: 24}
	if err := host.createResource(output); err != nil {
		t.Fatal(err)
	}
	if err := host.createResource(positions); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, positions.Width)
	for index, value := range []float32{-1, -1, 3, -1, -1, 3} {
		binary.LittleEndian.PutUint32(data[index*4:], math.Float32bits(value))
	}

	if err := host.dispatch(func() error {
		program, err := host.gl.compileProgram(vertexGLSL, fragmentGLSL)
		if err != nil {
			return err
		}
		defer host.gl.deleteProgram(program)
		target := host.resources[output.ID]
		host.gl.bindFramebuffer(glFramebuffer, target.framebuffer)
		host.gl.viewport(0, 0, 1, 1)
		host.gl.useProgram(program)
		host.gl.uniform1f(uniformLocation(host.gl, program, "uWinsysAdjustY"), 1)
		host.gl.bindVertexArray(host.vao)
		host.gl.bindBuffer(glArrayBuffer, host.resources[positions.ID].buffer)
		host.gl.bufferSubData(glArrayBuffer, 0, len(data), glPointer(data))
		host.gl.vertexAttribPtr(0, 2, glFloat, false, 8, 0)
		host.gl.enableVertexAttrib(0)
		host.gl.drawArrays(glTriangles, 0, 3)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	pixels, _, err := host.readScanout(&resource{description: output}, image.Rect(0, 0, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pixels, []byte{0, 128, 255, 255}; string(got) != string(want) {
		t.Fatalf("masked FACE declaration with generic varying pixel BGRA = %v, want %v", got, want)
	}
}

func TestGlmarkArithmeticTGSICompilesInDarwinHostContext(t *testing.T) {
	const fragment = `FRAG
DCL OUT[0], COLOR
DCL IN[0], POSITION
DCL TEMP[0..7]
DCL CONST[0..7]
DCL ADDR[0]
DCL SAMP[0]
DCL SVIEW[0], SHADOW2D, FLOAT
IMM[0] FLT32 {0.25, 0.5, 1.0, 2.0}
IMM[1] UINT32 {0, 1, 2, 3}
  0: MOV TEMP[0], IMM[0]
  1: RCP TEMP[1], TEMP[0]
  2: POW TEMP[2], TEMP[0], IMM[0].wwww
  3: FLR TEMP[3], TEMP[1]
  4: FRC TEMP[4], TEMP[1]
  5: FSLT TEMP[5], TEMP[0], TEMP[1]
  6: FSGE TEMP[6], TEMP[1], TEMP[0]
 7: FSEQ TEMP[7], TEMP[0], TEMP[0]
  8: FSNE TEMP[7], TEMP[0], TEMP[1]
 9: SSG TEMP[7], TEMP[0]
10: NOT TEMP[1], IMM[1]
 11: F2I TEMP[1], TEMP[0]
 12: USNE TEMP[1], IMM[1], TEMP[1]
 13: EX2 TEMP[7], TEMP[0]
 14: SIN TEMP[7], TEMP[0]
 15: LRP TEMP[6], TEMP[0], TEMP[2], TEMP[4]
 16: AND TEMP[7], IMM[0], IMM[0]
 17: DIV_SAT TEMP[6], TEMP[2], TEMP[1]
 18: UCMP TEMP[7], IMM[0], TEMP[6], IN[0]
 19: ARL ADDR[0].x, IMM[0]
 20: MOV TEMP[0], CONST[ADDR[0].x+1]
 21: ISGE TEMP[1], IMM[1], IMM[1]
 22: USEQ TEMP[1], IMM[1], IMM[1]
 23: UADD TEMP[1], IMM[1], IMM[1]
 24: UMUL TEMP[1], TEMP[1], IMM[1]
 25: SHL TEMP[1], TEMP[1], IMM[1]
 26: USHR TEMP[1], TEMP[1], IMM[1]
 27: ISHR TEMP[1], TEMP[1], IMM[1]
 28: OR TEMP[1], TEMP[1], IMM[1]
 29: XOR TEMP[1], TEMP[1], IMM[1]
 30: SGE TEMP[1], TEMP[0], TEMP[1]
 31: DP2 TEMP[1], TEMP[0], TEMP[1]
 32: LG2 TEMP[1], TEMP[0]
 33: BGNLOOP :39
 34: UIF IMM[0] :37
 35: BRK
 36: ELSE :38
 37: CONT
 38: ENDIF
 39: ENDLOOP :33
 40: KILL_IF TEMP[0]
 41: ADD OUT[0], TEMP[6], TEMP[7]
 42: TEX TEMP[1], TEMP[0], SAMP[0], SHADOW2D
 43: COS TEMP[1], TEMP[0]
 44: DDX TEMP[1], TEMP[0]
 45: DDY TEMP[1], TEMP[0]
 46: F2U TEMP[1], TEMP[0]
 47: I2F TEMP[1], TEMP[0]
 48: U2F TEMP[1], TEMP[0]
 49: END`
	_, fragmentGLSL, err := translateTGSI(fragment)
	if err != nil {
		t.Fatal(err)
	}
	const vertexGLSL = `#version 410 core
void main() {
	vec2 position = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
	gl_Position = vec4(position * 2.0 - 1.0, 0.0, 1.0);
}`
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()
	if err := host.dispatch(func() error {
		program, err := host.gl.compileProgram(vertexGLSL, fragmentGLSL)
		if err == nil {
			host.gl.deleteProgram(program)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
