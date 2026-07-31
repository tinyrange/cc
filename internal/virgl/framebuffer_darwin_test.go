//go:build darwin

package virgl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
	"testing"

	"j5.nz/cc/internal/virtio"
)

func TestOpenArenaVertexFormats(t *testing.T) {
	for _, test := range []struct {
		name       string
		format     uint32
		components int32
		dataType   uint32
		normalized bool
	}{
		{name: "R16G16B16A16_UNORM", format: 51, components: 4, dataType: glUnsignedShort, normalized: true},
		{name: "R16G16_SSCALED", format: 61, components: 2, dataType: glShort, normalized: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			components, dataType, normalized, ok := vertexFormat(test.format)
			if !ok {
				t.Fatalf("format %d is unsupported", test.format)
			}
			if components != test.components || dataType != test.dataType || normalized != test.normalized {
				t.Fatalf(
					"mapping = (%d, %#x, normalized=%t), want (%d, %#x, normalized=%t)",
					components, dataType, normalized, test.components, test.dataType, test.normalized,
				)
			}
		})
	}
}

func TestDestroyObjectDoesNotDeleteOtherObjectTypesWithSameHandle(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	const handle = 42
	if err := host.execute(contextID, []command{
		{Opcode: 1, Object: 1, Payload: append([]uint32{handle}, make([]uint32, 10)...)},
		{Opcode: 1, Object: 3, Payload: []uint32{handle, 0, 0, 0, 0}},
		{Opcode: 3, Object: 1, Payload: []uint32{handle}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := host.dispatch(func() error {
		if _, ok := host.contexts[contextID].depthStencilAlpha[handle]; !ok {
			return fmt.Errorf("destroying blend object %d also deleted DSA object %d", handle, handle)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSurfaceKeepsResourceAliveAfterGuestUnref(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	color := virtio.GPUResource3D{ID: 55, Target: 2, Format: 1, Width: 4, Height: 4, Depth: 1, ArraySize: 1}
	if err := host.createResource(color); err != nil {
		t.Fatal(err)
	}
	if err := host.execute(contextID, []command{
		{Opcode: 1, Object: 8, Payload: []uint32{5, color.ID}},
		{Opcode: 5, Payload: []uint32{1, 0, 5}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := host.unrefResource(color.ID); err != nil {
		t.Fatal(err)
	}
	if err := host.execute(contextID, []command{{
		Opcode: 7,
		Payload: []uint32{
			0x4,
			math.Float32bits(1),
			math.Float32bits(0),
			math.Float32bits(0),
			math.Float32bits(1),
		},
	}}, nil); err != nil {
		t.Fatalf("clear through surface after guest resource unref: %v", err)
	}
	if err := host.execute(contextID, []command{{Opcode: 3, Object: 8, Payload: []uint32{5}}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := host.dispatch(func() error {
		if len(host.allResources) != 0 {
			return fmt.Errorf("%d host resources remain after final surface reference was destroyed", len(host.allResources))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUnrefCommitsQueuedBufferTransferForRetainedBinding(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	description := virtio.GPUResource3D{ID: 55, Target: 0, Width: 8}
	if err := host.createResource(description); err != nil {
		t.Fatal(err)
	}
	if err := host.execute(contextID, []command{{
		Opcode:  6,
		Payload: []uint32{4, 0, description.ID},
	}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := host.queueBufferTransfer(&resource{
		description: description,
		data:        []byte{10, 20, 30, 40},
	}, virtio.GPUTransfer3D{
		ResourceID: description.ID,
		Box:        virtio.GPUBox{X: 2, Width: 4},
	}); err != nil {
		t.Fatal(err)
	}
	if err := host.unrefResource(description.ID); err != nil {
		t.Fatal(err)
	}
	if err := host.dispatch(func() error {
		retained := host.contexts[contextID].vertexBuffers[0].resource
		if retained == nil {
			return errors.New("vertex binding did not retain buffer")
		}
		if got, want := retained.bufferBytes, []byte{0, 0, 10, 20, 30, 40, 0, 0}; string(got) != string(want) {
			return fmt.Errorf("retained buffer bytes = %v, want %v", got, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSubcontextsKeepIndependentFramebufferState(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	first := virtio.GPUResource3D{ID: 1, Target: 2, Format: 1, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	second := virtio.GPUResource3D{ID: 2, Target: 2, Format: 1, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	for _, description := range []virtio.GPUResource3D{first, second} {
		if err := host.createResource(description); err != nil {
			t.Fatal(err)
		}
	}
	createSurface := func(resourceID uint32) command {
		return command{Opcode: 1, Object: 8, Payload: []uint32{11, resourceID}}
	}
	setFramebuffer := command{Opcode: 5, Payload: []uint32{1, 0, 11}}
	clear := func(red, green, blue float32) command {
		return command{Opcode: 7, Payload: []uint32{
			0x4,
			math.Float32bits(red),
			math.Float32bits(green),
			math.Float32bits(blue),
			math.Float32bits(1),
		}}
	}
	if err := host.execute(contextID, []command{
		createSurface(first.ID),
		setFramebuffer,
		{Opcode: 29, Payload: []uint32{7}},
		{Opcode: 28, Payload: []uint32{7}},
		createSurface(second.ID),
		setFramebuffer,
		clear(0, 0, 1),
		{Opcode: 28, Payload: []uint32{0}},
		clear(1, 0, 0),
	}, nil); err != nil {
		t.Fatal(err)
	}

	firstPixels, _, err := host.readScanout(&resource{description: first}, image.Rect(0, 0, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	secondPixels, _, err := host.readScanout(&resource{description: second}, image.Rect(0, 0, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := firstPixels, []byte{0, 0, 255, 255}; string(got) != string(want) {
		t.Fatalf("default subcontext framebuffer BGRA = %v, want %v", got, want)
	}
	if got, want := secondPixels, []byte{255, 0, 0, 255}; string(got) != string(want) {
		t.Fatalf("created subcontext framebuffer BGRA = %v, want %v", got, want)
	}
}

func TestClearTargetsBoundVirGLFramebufferAfterBlit(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}

	first := virtio.GPUResource3D{ID: 1, Target: 2, Format: 1, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	second := virtio.GPUResource3D{ID: 2, Target: 2, Format: 1, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	if err := host.createResource(first); err != nil {
		t.Fatal(err)
	}
	if err := host.createResource(second); err != nil {
		t.Fatal(err)
	}

	createSurface := func(handle, resourceID uint32) command {
		return command{Opcode: 1, Object: 8, Payload: []uint32{handle, resourceID}}
	}
	setFramebuffer := func(surface uint32) command {
		return command{Opcode: 5, Payload: []uint32{1, 0, surface}}
	}
	clear := func(red, green, blue float32) command {
		return command{Opcode: 7, Payload: []uint32{
			0x4,
			math.Float32bits(red),
			math.Float32bits(green),
			math.Float32bits(blue),
			math.Float32bits(1),
		}}
	}
	if err := host.execute(contextID, []command{
		createSurface(11, first.ID),
		createSurface(12, second.ID),
		setFramebuffer(11),
		clear(1, 0, 0),
	}, nil); err != nil {
		t.Fatal(err)
	}

	blit := make([]uint32, 21)
	blit[0] = 0xf
	blit[3] = second.ID
	blit[9], blit[10] = 1, 1
	blit[12] = first.ID
	blit[18], blit[19] = 1, 1
	if err := host.execute(contextID, []command{
		{Opcode: 16, Payload: blit},
		clear(0, 0, 1),
	}, nil); err != nil {
		t.Fatal(err)
	}

	firstPixels, _, err := host.readScanout(&resource{description: first}, image.Rect(0, 0, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	secondPixels, _, err := host.readScanout(&resource{description: second}, image.Rect(0, 0, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := firstPixels, []byte{255, 0, 0, 255}; string(got) != string(want) {
		t.Fatalf("active framebuffer pixel BGRA = %v, want %v", got, want)
	}
	if got, want := secondPixels, []byte{0, 0, 255, 255}; string(got) != string(want) {
		t.Fatalf("blit destination pixel BGRA = %v, want %v", got, want)
	}
}

func TestClearIgnoresBoundDepthWriteMask(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	color := virtio.GPUResource3D{ID: 1, Target: 2, Format: 1, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	depth := virtio.GPUResource3D{ID: 2, Target: 2, Format: 21, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	if err := host.createResource(color); err != nil {
		t.Fatal(err)
	}
	if err := host.createResource(depth); err != nil {
		t.Fatal(err)
	}

	clearDepth := func(value float64) command {
		bits := math.Float64bits(value)
		return command{Opcode: 7, Payload: []uint32{
			0x1, 0, 0, 0, 0, uint32(bits), uint32(bits >> 32),
		}}
	}
	if err := host.execute(contextID, []command{
		{Opcode: 1, Object: 8, Payload: []uint32{11, color.ID}},
		{Opcode: 1, Object: 8, Payload: []uint32{12, depth.ID}},
		{Opcode: 5, Payload: []uint32{1, 12, 11}},
		{Opcode: 1, Object: 3, Payload: []uint32{13, 0x2, 0, 0, 0}},
		{Opcode: 2, Object: 3, Payload: []uint32{13}},
		clearDepth(0.75),
		{Opcode: 1, Object: 3, Payload: []uint32{14, 0, 0, 0, 0}},
		{Opcode: 2, Object: 3, Payload: []uint32{14}},
		clearDepth(0.25),
	}, nil); err != nil {
		t.Fatal(err)
	}

	var raw [4]byte
	if err := host.dispatch(func() error {
		if err := host.bindContextFramebuffer(host.contexts[contextID]); err != nil {
			return err
		}
		host.gl.readPixels(0, 0, 1, 1, glDepthComponent, glFloat, glPointer(raw[:]))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got := math.Float32frombits(binary.LittleEndian.Uint32(raw[:]))
	if math.Abs(float64(got-0.25)) > 0.001 {
		t.Fatalf("depth after masked clear = %g, want 0.25", got)
	}
}

func TestBlendAndScissorAffectRenderedPixels(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	color := virtio.GPUResource3D{ID: 1, Target: 2, Format: 1, Width: 2, Height: 1, Depth: 1, ArraySize: 1}
	if err := host.createResource(color); err != nil {
		t.Fatal(err)
	}
	clear := func(red, green, blue float32) command {
		return command{Opcode: 7, Payload: []uint32{
			0x4,
			math.Float32bits(red),
			math.Float32bits(green),
			math.Float32bits(blue),
			math.Float32bits(1),
		}}
	}
	if err := host.execute(contextID, []command{
		{Opcode: 1, Object: 8, Payload: []uint32{11, color.ID}},
		{Opcode: 5, Payload: []uint32{1, 0, 11}},
		{Opcode: 4, Payload: []uint32{
			0,
			math.Float32bits(1), math.Float32bits(0.5), math.Float32bits(1),
			math.Float32bits(1), math.Float32bits(0.5), math.Float32bits(0),
		}},
		clear(0, 0, 1),
	}, nil); err != nil {
		t.Fatal(err)
	}

	drawColor := func(red, green, blue, alpha float32) {
		t.Helper()
		vertex := `#version 150
void main() {
	vec2 position = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
	gl_Position = vec4(position * 2.0 - 1.0, 0.0, 1.0);
}`
		fragment := fmt.Sprintf(`#version 150
out vec4 result;
void main() { result = vec4(%g, %g, %g, %g); }`, red, green, blue, alpha)
		if err := host.dispatch(func() error {
			program, err := host.gl.compileProgram(vertex, fragment)
			if err != nil {
				return err
			}
			defer host.gl.deleteProgram(program)
			if err := host.bindContextFramebuffer(host.contexts[contextID]); err != nil {
				return err
			}
			host.gl.useProgram(program)
			host.gl.bindVertexArray(host.vao)
			host.gl.drawArrays(glTriangles, 0, 3)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Source-alpha red over blue must produce 25% red and 75% blue.
	renderTarget := uint32(1 | (3 << 4) | (0x13 << 9) | (1 << 17) | (0x11 << 22) | (0xf << 27))
	blendPayload := []uint32{12, 0, 0, renderTarget, 0, 0, 0, 0, 0, 0, 0}
	if err := host.execute(contextID, []command{
		{Opcode: 1, Object: 1, Payload: blendPayload},
		{Opcode: 2, Object: 1, Payload: []uint32{12}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	drawColor(1, 0, 0, 0.25)
	pixels, _, err := host.readScanout(&resource{description: color}, image.Rect(0, 0, 2, 1))
	if err != nil {
		t.Fatal(err)
	}
	for pixel := 0; pixel < 2; pixel++ {
		got := pixels[pixel*4 : pixel*4+4]
		want := []byte{191, 0, 64, 64}
		for channel := range want {
			if difference := int(got[channel]) - int(want[channel]); difference < -1 || difference > 1 {
				t.Fatalf("blended pixel %d BGRA = %v, want approximately %v", pixel, got, want)
			}
		}
	}

	// With rasterizer scissoring enabled, only the left pixel is replaced.
	if err := host.execute(contextID, []command{
		{Opcode: 2, Object: 1, Payload: []uint32{0}},
		{Opcode: 1, Object: 2, Payload: []uint32{13, 1 << 14}},
		{Opcode: 15, Payload: []uint32{0, 0, 1 | (1 << 16)}},
		{Opcode: 2, Object: 2, Payload: []uint32{13}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	drawColor(0, 1, 0, 1)
	pixels, _, err = host.readScanout(&resource{description: color}, image.Rect(0, 0, 2, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pixels[:4], []byte{0, 255, 0, 255}; string(got) != string(want) {
		t.Fatalf("scissored left pixel BGRA = %v, want %v", got, want)
	}
	wantRight := []byte{191, 0, 64, 64}
	for channel := range wantRight {
		if difference := int(pixels[4+channel]) - int(wantRight[channel]); difference < -1 || difference > 1 {
			t.Fatalf("scissored right pixel BGRA = %v, want approximately %v", pixels[4:8], wantRight)
		}
	}
}

func TestRasterizerWindingAccountsForHostFramebufferOrigin(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	color := virtio.GPUResource3D{ID: 1, Target: 2, Format: 67, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	if err := host.createResource(color); err != nil {
		t.Fatal(err)
	}
	if err := host.execute(contextID, []command{
		{Opcode: 1, Object: 8, Payload: []uint32{11, color.ID}},
		{Opcode: 5, Payload: []uint32{1, 0, 11}},
		{Opcode: 4, Payload: []uint32{
			0,
			math.Float32bits(0.5), math.Float32bits(0.5), math.Float32bits(1),
			math.Float32bits(0.5), math.Float32bits(0.5), math.Float32bits(0),
		}},
		// Cull back faces with Gallium's clockwise front face.
		{Opcode: 1, Object: 2, Payload: []uint32{12, 2 << 8}},
		{Opcode: 2, Object: 2, Payload: []uint32{12}},
		{Opcode: 7, Payload: []uint32{0x4, 0, 0, 0, math.Float32bits(1)}},
	}, nil); err != nil {
		t.Fatal(err)
	}

	vertex := `#version 150
void main() {
	vec2 position = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
	gl_Position = vec4(position * 2.0 - 1.0, 0.0, 1.0);
}`
	fragment := `#version 150
out vec4 result;
void main() { result = vec4(1.0, 0.0, 0.0, 1.0); }`
	if err := host.dispatch(func() error {
		program, err := host.gl.compileProgram(vertex, fragment)
		if err != nil {
			return err
		}
		defer host.gl.deleteProgram(program)
		if err := host.bindContextFramebuffer(host.contexts[contextID]); err != nil {
			return err
		}
		host.gl.useProgram(program)
		host.gl.bindVertexArray(host.vao)
		host.gl.drawArrays(glTriangles, 0, 3)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	pixels, _, err := host.readScanout(&resource{description: color}, image.Rect(0, 0, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pixels, []byte{0, 0, 255, 255}; string(got) != string(want) {
		t.Fatalf("front-facing Gallium triangle BGRA = %v, want %v", got, want)
	}
}

func TestDrawDisablesRetiredVertexAttributes(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	color := virtio.GPUResource3D{ID: 1, Target: 2, Format: 67, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	positions := virtio.GPUResource3D{ID: 2, Target: 0, Width: 32}
	coordinates := virtio.GPUResource3D{ID: 3, Target: 0, Width: 32}
	for _, resource := range []virtio.GPUResource3D{color, positions, coordinates} {
		if err := host.createResource(resource); err != nil {
			t.Fatal(err)
		}
	}
	floatBytes := func(values ...float32) []byte {
		result := make([]byte, len(values)*4)
		for index, value := range values {
			binary.LittleEndian.PutUint32(result[index*4:], math.Float32bits(value))
		}
		return result
	}
	if err := host.dispatch(func() error {
		for id, data := range map[uint32][]byte{
			positions.ID:   floatBytes(-1, -1, 1, -1, -1, 1, 1, 1),
			coordinates.ID: floatBytes(0, 0, 1, 0, 0, 1, 1, 1),
		} {
			host.gl.bindBuffer(glArrayBuffer, host.resources[id].buffer)
			host.gl.bufferSubData(glArrayBuffer, 0, len(data), glPointer(data))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	context := host.contexts[contextID]
	context.surfaces[11] = hostSurface{resourceID: color.ID, resource: host.resources[color.ID]}
	context.colorSurface = 11
	context.vertexElements[12] = []hostVertexElement{
		{bufferIndex: 0, format: 29},
		{bufferIndex: 1, format: 29},
	}
	context.boundVertexElements = 12
	context.vertexBuffers[0] = hostVertexBuffer{stride: 8, resourceID: positions.ID, resource: host.resources[positions.ID]}
	context.vertexBuffers[1] = hostVertexBuffer{stride: 8, resourceID: coordinates.ID, resource: host.resources[coordinates.ID]}
	context.shaders[20] = hostShader{stage: tgsiVertex, source: `#version 410 core
layout(location = 0) in vec2 position;
layout(location = 1) in vec2 coordinate;
out vec2 varyingCoordinate;
void main() {
	gl_Position = vec4(position, 0.0, 1.0);
	varyingCoordinate = coordinate;
}`}
	context.shaders[21] = hostShader{stage: tgsiFragment, source: `#version 410 core
in vec2 varyingCoordinate;
layout(location = 0) out vec4 color;
void main() { color = vec4(1.0, varyingCoordinate.x * 0.0, 0.0, 1.0); }`}
	context.boundShaders[tgsiVertex] = 20
	context.boundShaders[tgsiFragment] = 21

	if err := host.dispatch(func() error {
		if err := host.bindContextFramebuffer(context); err != nil {
			return err
		}
		host.gl.viewport(0, 0, 1, 1)
		host.gl.clearColor(0, 0, 0, 1)
		host.gl.clear(glColorBufferBit)

		// Model the preceding draw having one more array than this draw. Once
		// Mesa retires that buffer, leaving attribute 2 enabled makes macOS
		// reject the otherwise valid draw with GL_INVALID_OPERATION.
		var retired uint32
		host.gl.genBuffers(1, &retired)
		host.gl.bindVertexArray(host.vao)
		host.gl.bindBuffer(glArrayBuffer, retired)
		host.gl.bufferData(glArrayBuffer, 4, 0, glStaticDraw)
		host.gl.vertexAttribPtr(2, 1, glFloat, false, 4, 0)
		host.gl.enableVertexAttrib(2)
		host.gl.deleteBuffers(1, &retired)
		host.enabledVertexAttributes = 0x7

		return host.draw(context, []uint32{0, 4, 5, 0})
	}); err != nil {
		t.Fatal(err)
	}
	pixels, _, err := host.readScanout(&resource{description: color}, image.Rect(0, 0, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pixels, []byte{0, 0, 255, 255}; string(got) != string(want) {
		t.Fatalf("draw after retiring an unused vertex attribute BGRA = %v, want %v", got, want)
	}
}

func TestZeroStrideVertexBufferSuppliesAConstantAttribute(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	color := virtio.GPUResource3D{ID: 1, Target: 2, Format: 67, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	positions := virtio.GPUResource3D{ID: 2, Target: 0, Width: 24}
	constant := virtio.GPUResource3D{ID: 3, Target: 0, Width: 4}
	for _, description := range []virtio.GPUResource3D{color, positions, constant} {
		if err := host.createResource(description); err != nil {
			t.Fatal(err)
		}
	}
	floatBytes := func(values ...float32) []byte {
		result := make([]byte, len(values)*4)
		for index, value := range values {
			binary.LittleEndian.PutUint32(result[index*4:], math.Float32bits(value))
		}
		return result
	}
	for _, upload := range []struct {
		description virtio.GPUResource3D
		data        []byte
	}{
		{positions, floatBytes(-1, -1, 3, -1, -1, 3)},
		{constant, floatBytes(1)},
	} {
		if err := host.transferToHost(&resource{description: upload.description, data: upload.data}, virtio.GPUTransfer3D{
			ResourceID: upload.description.ID,
			Box:        virtio.GPUBox{Width: upload.description.Width},
		}); err != nil {
			t.Fatal(err)
		}
	}

	context := host.contexts[contextID]
	context.surfaces[11] = hostSurface{resourceID: color.ID, resource: host.resources[color.ID]}
	context.colorSurface = 11
	context.vertexElements[12] = []hostVertexElement{
		{bufferIndex: 0, format: 29},
		{bufferIndex: 1, format: 28},
	}
	context.boundVertexElements = 12
	context.vertexBuffers[0] = hostVertexBuffer{stride: 8, resourceID: positions.ID, resource: host.resources[positions.ID]}
	context.vertexBuffers[1] = hostVertexBuffer{stride: 0, resourceID: constant.ID, resource: host.resources[constant.ID]}
	context.shaders[20] = hostShader{stage: tgsiVertex, source: `#version 410 core
layout(location = 0) in vec2 position;
layout(location = 1) in float constantValue;
out vec4 value;
void main() {
	gl_Position = vec4(position, 0.0, 1.0);
	value = vec4(constantValue);
}`}
	context.shaders[21] = hostShader{stage: tgsiFragment, source: `#version 410 core
in vec4 value;
layout(location = 0) out vec4 color;
void main() { color = vec4(value.x, 0.0, 0.0, 1.0); }`}
	context.boundShaders[tgsiVertex] = 20
	context.boundShaders[tgsiFragment] = 21

	if err := host.dispatch(func() error {
		host.gl.viewport(0, 0, 1, 1)
		return host.draw(context, []uint32{0, 3, 4, 0})
	}); err != nil {
		t.Fatal(err)
	}
	pixels, _, err := host.readScanout(&resource{description: color}, image.Rect(0, 0, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pixels, []byte{0, 0, 255, 255}; string(got) != string(want) {
		t.Fatalf("zero-stride constant attribute BGRA = %v, want %v", got, want)
	}
}

func TestIndexedDrawAppliesBaseVertex(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	color := virtio.GPUResource3D{ID: 1, Target: 2, Format: 67, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	positions := virtio.GPUResource3D{ID: 2, Target: 0, Width: 48}
	indices := virtio.GPUResource3D{ID: 3, Target: 0, Width: 6}
	for _, resource := range []virtio.GPUResource3D{color, positions, indices} {
		if err := host.createResource(resource); err != nil {
			t.Fatal(err)
		}
	}
	floatBytes := func(values ...float32) []byte {
		result := make([]byte, len(values)*4)
		for index, value := range values {
			binary.LittleEndian.PutUint32(result[index*4:], math.Float32bits(value))
		}
		return result
	}
	indexBytes := make([]byte, 6)
	binary.LittleEndian.PutUint16(indexBytes[0:], 0)
	binary.LittleEndian.PutUint16(indexBytes[2:], 1)
	binary.LittleEndian.PutUint16(indexBytes[4:], 2)
	if err := host.dispatch(func() error {
		for id, data := range map[uint32][]byte{
			positions.ID: floatBytes(
				2, 2, 2, 2, 2, 2,
				-1, -1, 3, -1, -1, 3,
			),
			indices.ID: indexBytes,
		} {
			host.gl.bindBuffer(glArrayBuffer, host.resources[id].buffer)
			host.gl.bufferSubData(glArrayBuffer, 0, len(data), glPointer(data))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	context := host.contexts[contextID]
	context.surfaces[11] = hostSurface{resourceID: color.ID, resource: host.resources[color.ID]}
	context.colorSurface = 11
	context.vertexElements[12] = []hostVertexElement{{bufferIndex: 0, format: 29}}
	context.boundVertexElements = 12
	context.vertexBuffers[0] = hostVertexBuffer{stride: 8, resourceID: positions.ID, resource: host.resources[positions.ID]}
	context.indexBuffer = indices.ID
	context.indexResource = host.resources[indices.ID]
	context.indexSize = 2
	context.shaders[20] = hostShader{stage: tgsiVertex, source: `#version 410 core
layout(location = 0) in vec2 position;
void main() { gl_Position = vec4(position, 0.0, 1.0); }`}
	context.shaders[21] = hostShader{stage: tgsiFragment, source: `#version 410 core
layout(location = 0) out vec4 color;
void main() { color = vec4(1.0, 0.0, 0.0, 1.0); }`}
	context.boundShaders[tgsiVertex] = 20
	context.boundShaders[tgsiFragment] = 21

	if err := host.dispatch(func() error {
		if err := host.bindContextFramebuffer(context); err != nil {
			return err
		}
		host.gl.viewport(0, 0, 1, 1)
		host.gl.clearColor(0, 0, 0, 1)
		host.gl.clear(glColorBufferBit)
		return host.draw(context, []uint32{0, 3, 4, 1, 1, 3, 0, 0, 0, 0, 2, 0})
	}); err != nil {
		t.Fatal(err)
	}
	pixels, _, err := host.readScanout(&resource{description: color}, image.Rect(0, 0, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pixels, []byte{0, 0, 255, 255}; string(got) != string(want) {
		t.Fatalf("indexed draw with base vertex BGRA = %v, want %v", got, want)
	}
}

func TestSamplerViewAndStateAffectRenderedPixels(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	output := virtio.GPUResource3D{ID: 1, Target: 2, Format: 67, Width: 2, Height: 1, Depth: 1, ArraySize: 1}
	texture := virtio.GPUResource3D{ID: 2, Target: 2, Format: 67, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	if err := host.createResource(output); err != nil {
		t.Fatal(err)
	}
	if err := host.createResource(texture); err != nil {
		t.Fatal(err)
	}

	identitySwizzle := uint32(0 | (1 << 3) | (2 << 6) | (3 << 9))
	samplerBits := uint32(3 | (3 << 3) | (2 << 11))
	if err := host.execute(contextID, []command{
		{Opcode: 1, Object: 8, Payload: []uint32{11, output.ID}},
		{Opcode: 5, Payload: []uint32{1, 0, 11}},
		{Opcode: 4, Payload: []uint32{
			0,
			math.Float32bits(1), math.Float32bits(0.5), math.Float32bits(1),
			math.Float32bits(1), math.Float32bits(0.5), math.Float32bits(0),
		}},
		{Opcode: 1, Object: 6, Payload: []uint32{20, texture.ID, 67, 0, 0, identitySwizzle}},
		{Opcode: 1, Object: 7, Payload: []uint32{
			21, samplerBits,
			math.Float32bits(0), math.Float32bits(0), math.Float32bits(0),
			math.Float32bits(0.25), math.Float32bits(0.5), math.Float32bits(0.75), math.Float32bits(1),
		}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := host.transferToHost(&resource{
		description: texture,
		data:        []byte{90, 91, 92, 10, 20, 30, 40},
	}, virtio.GPUTransfer3D{
		ResourceID: texture.ID,
		Offset:     3,
		Box:        virtio.GPUBox{Width: 1, Height: 1, Depth: 1},
	}); err != nil {
		t.Fatal(err)
	}

	vertex := `#version 150
void main() {
	vec2 position = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
	gl_Position = vec4(position * 2.0 - 1.0, 0.0, 1.0);
}`
	fragment := `#version 150
uniform sampler2D source;
out vec4 result;
void main() {
	vec2 coordinate = gl_FragCoord.x < 1.0 ? vec2(0.5) : vec2(-1.0, 0.5);
	result = texture(source, coordinate);
}`
	if err := host.dispatch(func() error {
		program, err := host.gl.compileProgram(vertex, fragment)
		if err != nil {
			return err
		}
		defer host.gl.deleteProgram(program)
		source := host.resources[texture.ID]
		host.gl.activeTexture(glTexture0)
		host.gl.bindTexture(glTexture2D, source.texture)
		context := host.contexts[contextID]
		if err := host.applySamplerView(context.samplerViews[20]); err != nil {
			return err
		}
		host.applySamplerState(context.samplerStates[21])
		if err := host.bindContextFramebuffer(context); err != nil {
			return err
		}
		host.gl.useProgram(program)
		host.gl.uniform1i(uniformLocation(host.gl, program, "source"), 0)
		host.gl.bindVertexArray(host.vao)
		host.gl.drawArrays(glTriangles, 0, 3)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	pixels, _, err := host.readScanout(&resource{description: output}, image.Rect(0, 0, 2, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pixels[:4], []byte{30, 20, 10, 40}; string(got) != string(want) {
		t.Fatalf("sampled texture pixel BGRA = %v, want %v", got, want)
	}
	wantBorder := []byte{191, 128, 64, 255}
	for channel := range wantBorder {
		if difference := int(pixels[4+channel]) - int(wantBorder[channel]); difference < -1 || difference > 1 {
			t.Fatalf("sampler border pixel BGRA = %v, want approximately %v", pixels[4:8], wantBorder)
		}
	}
}

func TestBufferTransfersHonorInlineAndBackingOffsets(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	description := virtio.GPUResource3D{ID: 1, Target: 0, Format: 64, Width: 16}
	if err := host.createResource(description); err != nil {
		t.Fatal(err)
	}
	resource := &resource{
		description: description,
		data:        []byte{90, 91, 92, 93, 10, 20, 30, 40, 94},
	}
	if err := host.transferToHost(resource, virtio.GPUTransfer3D{
		ResourceID: description.ID,
		Offset:     4,
		Box:        virtio.GPUBox{X: 6, Width: 4},
	}); err != nil {
		t.Fatal(err)
	}
	if err := host.createContext(1); err != nil {
		t.Fatal(err)
	}
	if err := host.execute(1, []command{{
		Opcode: 9,
		Payload: []uint32{
			description.ID, 0, 0, 0, 0,
			1, 0, 0, 4, 1, 1,
			80<<24 | 70<<16 | 60<<8 | 50,
		},
	}}, nil); err != nil {
		t.Fatal(err)
	}
	resource.data = []byte{99, 98}
	if err := host.transferToHost(resource, virtio.GPUTransfer3D{
		ResourceID: description.ID,
		Box:        virtio.GPUBox{X: 12, Width: 2},
	}); err != nil {
		t.Fatal(err)
	}

	got := make([]byte, description.Width)
	if err := host.dispatch(func() error {
		hostResource := host.resources[description.ID]
		host.publishBuffer(hostResource)
		host.gl.bindBuffer(glArrayBuffer, hostResource.buffer)
		host.gl.getBufferSubData(glArrayBuffer, 0, len(got), glPointer(got))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := make([]byte, description.Width)
	copy(want[1:5], []byte{50, 60, 70, 80})
	copy(want[6:10], []byte{10, 20, 30, 40})
	copy(want[12:14], []byte{99, 98})
	if string(got) != string(want) {
		t.Fatalf("transferred buffer bytes = %v, want %v", got, want)
	}
}

func TestZeroStridePartialTextureTransferUsesFullMipWidth(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	description := virtio.GPUResource3D{
		ID: 1, Target: 2, Format: 67,
		Width: 4, Height: 2, Depth: 1, ArraySize: 1,
	}
	if err := host.createResource(description); err != nil {
		t.Fatal(err)
	}
	red := []byte{255, 0, 0, 255}
	green := []byte{0, 255, 0, 255}
	blue := []byte{0, 0, 255, 255}
	white := []byte{255, 255, 255, 255}
	data := make([]byte, 24)
	copy(data[0:4], red)
	copy(data[4:8], green)
	copy(data[16:20], blue)
	copy(data[20:24], white)
	if err := host.transferToHost(&resource{description: description, data: data}, virtio.GPUTransfer3D{
		ResourceID: description.ID,
		Box:        virtio.GPUBox{X: 1, Width: 2, Height: 2, Depth: 1},
	}); err != nil {
		t.Fatal(err)
	}

	pixels, _, err := host.readScanout(&resource{description: description}, image.Rect(0, 0, 4, 2))
	if err != nil {
		t.Fatal(err)
	}
	// readScanout returns BGRA rows from the top of the image. Both transferred
	// rows must retain their own colors instead of consuming the padding between
	// full-width rows.
	got := append([]byte(nil), pixels[4:12]...)
	got = append(got, pixels[20:28]...)
	want := []byte{
		255, 0, 0, 255, 255, 255, 255, 255,
		0, 0, 255, 255, 0, 255, 0, 255,
	}
	if string(got) != string(want) {
		t.Fatalf("zero-stride partial texture pixels BGRA = %v, want %v", got, want)
	}
}

func TestBlitTargetsRequestedMipLevels(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	description := virtio.GPUResource3D{
		ID: 1, Target: 2, Format: 67,
		Width: 2, Height: 2, Depth: 1, ArraySize: 1, LastLevel: 1,
	}
	if err := host.createResource(description); err != nil {
		t.Fatal(err)
	}
	levelZero := []byte{
		255, 0, 0, 255, 255, 0, 0, 255,
		255, 0, 0, 255, 255, 0, 0, 255,
	}
	if err := host.transferToHost(&resource{
		description: description,
		data:        levelZero,
	}, virtio.GPUTransfer3D{
		ResourceID: description.ID,
		Box:        virtio.GPUBox{Width: 2, Height: 2, Depth: 1},
	}); err != nil {
		t.Fatal(err)
	}

	blit := make([]uint32, 21)
	blit[0] = 0xf | (1 << 8)
	blit[3], blit[4] = description.ID, 1
	blit[9], blit[10] = 1, 1
	blit[12], blit[13] = description.ID, 0
	blit[18], blit[19] = 2, 2
	if err := host.execute(contextID, []command{{Opcode: 16, Payload: blit}}, nil); err != nil {
		t.Fatal(err)
	}

	got := make([]byte, 4)
	if err := host.dispatch(func() error {
		resource := host.resources[description.ID]
		host.gl.bindFramebuffer(glReadFramebuffer, host.blitReadFBO)
		host.gl.framebufferTexture(glReadFramebuffer, glColorAttachment0, glTexture2D, resource.texture, 1)
		if status := host.gl.checkFramebuffer(glReadFramebuffer); status != glFramebufferComplete {
			return fmt.Errorf("mip framebuffer status %#x", status)
		}
		host.gl.readPixels(0, 0, 1, 1, glRGBA, glUnsignedByte, glPointer(got))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := []byte{255, 0, 0, 255}; string(got) != string(want) {
		t.Fatalf("mip level 1 pixel RGBA = %v, want %v", got, want)
	}
}

func TestResourceCopyRegionPopulatesRequestedMipLevel(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	src := virtio.GPUResource3D{ID: 1, Target: 2, Format: 67, Width: 2, Height: 2, Depth: 1, ArraySize: 1, LastLevel: 1}
	dst := virtio.GPUResource3D{ID: 2, Target: 2, Format: 67, Width: 2, Height: 2, Depth: 1, ArraySize: 1, LastLevel: 1}
	if err := host.createResource(src); err != nil {
		t.Fatal(err)
	}
	if err := host.createResource(dst); err != nil {
		t.Fatal(err)
	}
	want := []byte{19, 83, 211, 255}
	if err := host.dispatch(func() error {
		host.gl.bindTexture(glTexture2D, host.resources[src.ID].texture)
		host.gl.texSubImage2D(glTexture2D, 1, 0, 0, 1, 1, glRGBA, glUnsignedByte, glPointer(want))
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	copyRegion := []uint32{
		dst.ID, 1, 0, 0, 0,
		src.ID, 1, 0, 0, 0,
		1, 1, 1,
	}
	if err := host.execute(contextID, []command{{Opcode: 17, Payload: copyRegion}}, nil); err != nil {
		t.Fatal(err)
	}

	got := make([]byte, 4)
	if err := host.dispatch(func() error {
		host.gl.bindFramebuffer(glFramebuffer, host.blitReadFBO)
		host.gl.framebufferTexture(glFramebuffer, glColorAttachment0, glTexture2D, host.resources[dst.ID].texture, 1)
		if status := host.gl.checkFramebuffer(glFramebuffer); status != glFramebufferComplete {
			return fmt.Errorf("destination mip framebuffer status %#x", status)
		}
		host.gl.readPixels(0, 0, 1, 1, glRGBA, glUnsignedByte, glPointer(got))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("copied mip pixel RGBA = %v, want %v", got, want)
	}
}
