//go:build darwin

package virgl

import "testing"

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
DCL IN[0]
DCL OUT[0], COLOR
DCL SAMP[0]
DCL SVIEW[0], 2D, FLOAT
  0: TEX OUT[0], IN[0], SAMP[0], 2D
  1: END`

	_, vertexGLSL, err := translateTGSI(vertex)
	if err != nil {
		t.Fatal(err)
	}
	_, fragmentGLSL, err := translateTGSI(fragment)
	if err != nil {
		t.Fatal(err)
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
