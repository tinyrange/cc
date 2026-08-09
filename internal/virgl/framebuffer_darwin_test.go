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
		{name: "R8G8B8A8_USCALED", format: 72, components: 4, dataType: glUnsignedByte, normalized: false},
		{name: "R8G8B8A8_SNORM", format: 77, components: 4, dataType: glByte, normalized: true},
		{name: "R8G8B8A8_SSCALED", format: 85, components: 4, dataType: glByte, normalized: false},
		{name: "R32G32_FIXED", format: 88, components: 2, dataType: glFixed, normalized: false},
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

func TestFixedPointVertexFormatRendersThroughDarwinHost(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	output := virtio.GPUResource3D{ID: 1, Target: 2, Format: 67, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	positions := virtio.GPUResource3D{ID: 2, Target: 0, Format: 88, Width: 24, Height: 1, Depth: 1, ArraySize: 1}
	if err := host.createResource(output); err != nil {
		t.Fatal(err)
	}
	if err := host.createResource(positions); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, positions.Width)
	for index, value := range []int32{-65536, -65536, 3 * 65536, -65536, -65536, 3 * 65536} {
		binary.LittleEndian.PutUint32(data[index*4:], uint32(value))
	}
	if err := host.transferToHost(&resource{description: positions, data: data}, virtio.GPUTransfer3D{
		ResourceID: positions.ID,
		Box:        virtio.GPUBox{Width: positions.Width, Height: 1, Depth: 1},
	}); err != nil {
		t.Fatal(err)
	}

	vertex := `#version 150
in vec2 position;
void main() { gl_Position = vec4(position, 0.0, 1.0); }`
	fragment := `#version 150
out vec4 result;
void main() { result = vec4(1.0, 0.0, 0.0, 1.0); }`
	if err := host.dispatch(func() error {
		program, err := host.gl.compileProgram(vertex, fragment)
		if err != nil {
			return err
		}
		defer host.gl.deleteProgram(program)
		target := host.resources[output.ID]
		buffer := host.resources[positions.ID]
		host.publishBuffer(buffer)
		host.gl.bindFramebuffer(glFramebuffer, target.framebuffer)
		host.gl.viewport(0, 0, 1, 1)
		host.gl.useProgram(program)
		host.gl.bindVertexArray(host.vao)
		host.gl.bindBuffer(glArrayBuffer, buffer.buffer)
		host.gl.vertexAttribPtr(0, 2, glFixed, false, 8, 0)
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
	if got, want := pixels, []byte{0, 0, 255, 255}; string(got) != string(want) {
		t.Fatalf("fixed-point vertex draw BGRA = %v, want %v", got, want)
	}
}

func TestVertexElementPreservesInstanceDivisor(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	if err := host.execute(contextID, []command{{
		Opcode:  1,
		Object:  5,
		Payload: []uint32{27, 12, 3, 1, 67},
	}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := host.dispatch(func() error {
		elements := host.contexts[contextID].vertexElements[27]
		if len(elements) != 1 || elements[0].instanceDivisor != 3 {
			return fmt.Errorf("vertex element instance divisor = %+v, want 3", elements)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
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

func TestProgramCacheEvictsLeastRecentlyUsedProgram(t *testing.T) {
	var deleted []uint32
	host := &darwinHost{
		gl: &hostGL{
			deleteProgram: func(program uint32) {
				deleted = append(deleted, program)
			},
			useProgram: func(uint32) {},
		},
		programs: make(map[hostProgramKey]hostProgram),
	}
	contexts := []*hostContext{newHostContext(), newHostContext(), newHostContext()}
	for index, context := range contexts {
		key := hostProgramKey{context: context}
		host.programs[key] = hostProgram{id: uint32(index + 1), lastUsed: uint64(index + 1)}
	}
	host.currentProgram = 1

	host.evictPrograms(2)

	if len(host.programs) != 2 {
		t.Fatalf("program cache size = %d, want 2", len(host.programs))
	}
	if len(deleted) != 1 || deleted[0] != 2 {
		t.Fatalf("deleted programs = %v, want [2]", deleted)
	}
	if host.currentProgram != 1 {
		t.Fatalf("current program = %d, want 1", host.currentProgram)
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

func TestSparseVertexBufferSlotsRenderMixedAttributes(t *testing.T) {
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
	colors := virtio.GPUResource3D{ID: 3, Target: 0, Width: 48}
	for _, description := range []virtio.GPUResource3D{color, positions, colors} {
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
		{colors, floatBytes(1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1)},
	} {
		if err := host.transferToHost(&resource{description: upload.description, data: upload.data}, virtio.GPUTransfer3D{
			ResourceID: upload.description.ID,
			Box:        virtio.GPUBox{Width: upload.description.Width},
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Mesa consolidates adjacent fixed-point attributes into one float buffer.
	// A later integer attribute can therefore occupy slot 2 while slot 1 is
	// explicitly unbound in the VirGL vertex-buffer array.
	if err := host.execute(contextID, []command{{Opcode: 6, Payload: []uint32{
		8, 0, positions.ID,
		0, 0, 0,
		16, 0, colors.ID,
	}}}, nil); err != nil {
		t.Fatal(err)
	}

	context := host.contexts[contextID]
	context.surfaces[11] = hostSurface{resourceID: color.ID, resource: host.resources[color.ID]}
	context.colorSurfaces[0] = 11
	context.vertexElements[12] = []hostVertexElement{
		{bufferIndex: 0, format: 29},
		{bufferIndex: 2, format: 31},
	}
	context.boundVertexElements = 12
	context.shaders[20] = hostShader{stage: tgsiVertex, source: `#version 410 core
layout(location = 0) in vec2 position;
layout(location = 1) in vec4 vertexColor;
out vec4 varyingColor;
void main() {
	gl_Position = vec4(position, 0.0, 1.0);
	varyingColor = vertexColor;
}`}
	context.shaders[21] = hostShader{stage: tgsiFragment, source: `#version 410 core
in vec4 varyingColor;
layout(location = 0) out vec4 color;
void main() { color = varyingColor; }`}
	context.boundShaders[tgsiVertex] = 20
	context.boundShaders[tgsiFragment] = 21
	if err := host.dispatch(func() error {
		if err := host.bindContextFramebuffer(context); err != nil {
			return err
		}
		host.gl.viewport(0, 0, 1, 1)
		host.gl.clearColor(0, 0, 0, 1)
		host.gl.clear(glColorBufferBit)
		return host.draw(context, []uint32{0, 3, 4, 0})
	}); err != nil {
		t.Fatal(err)
	}
	pixels, _, err := host.readScanout(&resource{description: color}, image.Rect(0, 0, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pixels, []byte{0, 0, 255, 255}; string(got) != string(want) {
		t.Fatalf("sparse vertex-buffer draw BGRA = %v, want %v", got, want)
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
	blit[9], blit[10], blit[11] = 1, 1, 1
	blit[12] = first.ID
	blit[18], blit[19], blit[20] = 1, 1, 1
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

func TestFramebufferClearUpdatesEveryColorTarget(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	first := virtio.GPUResource3D{ID: 1, Target: 2, Format: 67, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	second := virtio.GPUResource3D{ID: 2, Target: 2, Format: 67, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	for _, description := range []virtio.GPUResource3D{first, second} {
		if err := host.createResource(description); err != nil {
			t.Fatal(err)
		}
	}
	if err := host.execute(contextID, []command{
		{Opcode: 1, Object: 8, Payload: []uint32{11, first.ID}},
		{Opcode: 1, Object: 8, Payload: []uint32{12, second.ID}},
		{Opcode: 5, Payload: []uint32{2, 0, 11, 12}},
		{Opcode: 7, Payload: []uint32{
			0x4, math.Float32bits(1), math.Float32bits(0.25), math.Float32bits(0.5), math.Float32bits(1),
		}},
	}, nil); err != nil {
		t.Fatal(err)
	}

	want := []byte{128, 64, 255, 255}
	for _, description := range []virtio.GPUResource3D{first, second} {
		pixels, _, err := host.readScanout(&resource{description: description}, image.Rect(0, 0, 1, 1))
		if err != nil {
			t.Fatal(err)
		}
		if string(pixels) != string(want) {
			t.Fatalf("color target %d BGRA = %v, want %v", description.ID, pixels, want)
		}
	}
}

func TestArrayTextureLayersCanBeRenderedAndReadBack(t *testing.T) {
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
		ID: 1, Target: 7, Format: 67,
		Width: 1, Height: 1, Depth: 1, ArraySize: 2,
	}
	if err := host.createResource(description); err != nil {
		t.Fatal(err)
	}
	clear := func(red, green float32) command {
		return command{Opcode: 7, Payload: []uint32{
			0x4, math.Float32bits(red), math.Float32bits(green), 0, math.Float32bits(1),
		}}
	}
	if err := host.execute(contextID, []command{
		{Opcode: 1, Object: 8, Payload: []uint32{11, description.ID, description.Format, 0, 0}},
		{Opcode: 1, Object: 8, Payload: []uint32{12, description.ID, description.Format, 0, 1 | 1<<16}},
		{Opcode: 5, Payload: []uint32{1, 0, 11}},
		clear(1, 0),
		{Opcode: 5, Payload: []uint32{1, 0, 12}},
		clear(0, 1),
	}, nil); err != nil {
		t.Fatal(err)
	}

	readback := &resource{description: description, data: make([]byte, 8)}
	if err := host.transferFromHost(readback, virtio.GPUTransfer3D{
		ResourceID:  description.ID,
		Stride:      4,
		LayerStride: 4,
		Box:         virtio.GPUBox{Width: 1, Height: 1, Depth: 2},
	}); err != nil {
		t.Fatal(err)
	}
	want := []byte{255, 0, 0, 255, 0, 255, 0, 255}
	if string(readback.data) != string(want) {
		t.Fatalf("array texture layer pixels = %v, want %v", readback.data, want)
	}
}

func TestUniformBufferSuppliesFragmentConstants(t *testing.T) {
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
	uniforms := virtio.GPUResource3D{ID: 3, Target: 0, Width: 16}
	for _, description := range []virtio.GPUResource3D{color, positions, uniforms} {
		if err := host.createResource(description); err != nil {
			t.Fatal(err)
		}
	}
	floatBytes := func(values ...float32) []byte {
		data := make([]byte, len(values)*4)
		for index, value := range values {
			binary.LittleEndian.PutUint32(data[index*4:], math.Float32bits(value))
		}
		return data
	}
	for _, upload := range []struct {
		description virtio.GPUResource3D
		data        []byte
	}{
		{positions, floatBytes(-1, -1, 3, -1, -1, 3)},
		{uniforms, floatBytes(1, 0.25, 0.5, 1)},
	} {
		if err := host.transferToHost(&resource{description: upload.description, data: upload.data}, virtio.GPUTransfer3D{
			ResourceID: upload.description.ID,
			Box:        virtio.GPUBox{Width: upload.description.Width},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := host.execute(contextID, []command{
		{Opcode: 6, Payload: []uint32{8, 0, positions.ID}},
		{Opcode: 27, Payload: []uint32{tgsiFragment, 1, 0, 16, uniforms.ID}},
	}, nil); err != nil {
		t.Fatal(err)
	}

	context := host.contexts[contextID]
	context.surfaces[11] = hostSurface{resourceID: color.ID, resource: host.resources[color.ID]}
	context.colorSurfaces[0] = 11
	context.vertexElements[12] = []hostVertexElement{{bufferIndex: 0, format: 29}}
	context.boundVertexElements = 12
	_, vertexSource, err := translateTGSI(`VERT
DCL IN[0]
DCL OUT[0], POSITION
  0: MOV OUT[0], IN[0]
  1: END`)
	if err != nil {
		t.Fatal(err)
	}
	_, fragmentSource, err := translateTGSI(`FRAG
DCL CONST[1][0]
DCL OUT[0], COLOR
  0: MOV OUT[0], CONST[1][0]
  1: END`)
	if err != nil {
		t.Fatal(err)
	}
	context.shaders[20] = hostShader{stage: tgsiVertex, source: vertexSource}
	context.shaders[21] = hostShader{stage: tgsiFragment, source: fragmentSource}
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
	want := []byte{128, 64, 255, 255}
	if string(pixels) != string(want) {
		t.Fatalf("uniform-buffer draw BGRA = %v, want %v", pixels, want)
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

func TestClearIgnoresBoundStencilWriteMask(t *testing.T) {
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
	stencilOnly := virtio.GPUResource3D{ID: 2, Target: 2, Format: 20, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	if err := host.createResource(color); err != nil {
		t.Fatal(err)
	}
	if err := host.createResource(stencilOnly); err != nil {
		t.Fatal(err)
	}

	// Stencil is enabled with an all-zero write mask. A Gallium clear must
	// still update every stencil bit and restore the mask afterwards.
	stencil := uint32(1 | (7 << 1) | (0xff << 13))
	if err := host.execute(contextID, []command{
		{Opcode: 1, Object: 8, Payload: []uint32{11, color.ID}},
		{Opcode: 1, Object: 8, Payload: []uint32{12, stencilOnly.ID}},
		{Opcode: 5, Payload: []uint32{1, 12, 11}},
		{Opcode: 1, Object: 3, Payload: []uint32{13, 0, stencil, 0, 0}},
		{Opcode: 2, Object: 3, Payload: []uint32{13}},
		{Opcode: 7, Payload: []uint32{0x2, 0, 0, 0, 0, 0, 0, 0x7b}},
	}, nil); err != nil {
		t.Fatal(err)
	}

	var raw [1]byte
	if err := host.dispatch(func() error {
		if err := host.bindContextFramebuffer(host.contexts[contextID]); err != nil {
			return err
		}
		host.gl.readPixels(0, 0, 1, 1, glStencilIndex, glUnsignedByte, glPointer(raw[:]))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if raw[0] != 0x7b {
		t.Fatalf("stencil after masked clear = %#x, want 0x7b", raw[0])
	}
}

func TestLogicalStencil8ControlsAColorDraw(t *testing.T) {
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
	stencil := virtio.GPUResource3D{ID: 2, Target: 2, Format: 20, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	positions := virtio.GPUResource3D{ID: 3, Target: 0, Width: 24}
	for _, description := range []virtio.GPUResource3D{color, stencil, positions} {
		if err := host.createResource(description); err != nil {
			t.Fatal(err)
		}
	}
	positionBytes := make([]byte, positions.Width)
	// Clockwise order exercises the implicit back-face state. With separate
	// back-face stencil disabled, Gallium requires the complete front state,
	// including its reference value, to apply to this draw.
	for index, value := range []float32{-1, -1, -1, 3, 3, -1} {
		binary.LittleEndian.PutUint32(positionBytes[index*4:], math.Float32bits(value))
	}
	if err := host.transferToHost(&resource{description: positions, data: positionBytes}, virtio.GPUTransfer3D{
		ResourceID: positions.ID,
		Box:        virtio.GPUBox{Width: positions.Width},
	}); err != nil {
		t.Fatal(err)
	}

	context := host.contexts[contextID]
	context.surfaces[11] = hostSurface{resourceID: color.ID, resource: host.resources[color.ID]}
	context.surfaces[12] = hostSurface{resourceID: stencil.ID, resource: host.resources[stencil.ID]}
	context.colorSurfaces[0] = 11
	context.depthSurface = 12
	context.vertexElements[13] = []hostVertexElement{{bufferIndex: 0, format: 29}}
	context.boundVertexElements = 13
	context.vertexBuffers[0] = hostVertexBuffer{stride: 8, resourceID: positions.ID, resource: host.resources[positions.ID]}
	context.shaders[20] = hostShader{stage: tgsiVertex, source: `#version 410 core
layout(location = 0) in vec2 position;
void main() { gl_Position = vec4(position, 0.0, 1.0); }`}
	context.shaders[21] = hostShader{stage: tgsiFragment, source: `#version 410 core
layout(location = 0) out vec4 color;
void main() { color = vec4(1.0, 0.0, 0.0, 1.0); }`}
	context.boundShaders[tgsiVertex] = 20
	context.boundShaders[tgsiFragment] = 21

	// Write 0x7b through the stencil z-pass operation, clear only color, then
	// require that exact stencil value for the visible draw. This exercises the
	// operation ordering used by dEQP's stencil-clear verification cases.
	writeStencilState := uint32(1 | (7 << 1) | (2 << 7) | (0xff << 13) | (0xff << 21))
	compareStencilState := uint32(1 | (2 << 1) | (0xff << 13) | (0xff << 21))
	context.depthStencilAlpha[14] = hostDepthStencilAlpha{stencil: [2]uint32{writeStencilState}}
	context.boundDSA = 14
	context.stencilRef = [2]uint8{0x7b, 0}
	if err := host.dispatch(func() error {
		if err := host.bindContextFramebuffer(context); err != nil {
			return err
		}
		host.gl.viewport(0, 0, 1, 1)
		host.gl.frontFace(glCCW)
		host.gl.clearColor(0, 0, 0, 1)
		host.gl.clearStencil(0)
		host.gl.stencilMaskSeparate(glFrontAndBack, 0xff)
		host.gl.clear(glColorBufferBit | glStencilBufferBit)
		host.applyDepthStencilAlpha(context, context.depthStencilAlpha[14])
		if err := host.draw(context, []uint32{0, 3, 4, 0}); err != nil {
			return err
		}
		host.gl.clear(glColorBufferBit)
		context.depthStencilAlpha[14] = hostDepthStencilAlpha{stencil: [2]uint32{compareStencilState}}
		context.stencilRef = [2]uint8{0x7b, 0x7b}
		host.applyDepthStencilAlpha(context, context.depthStencilAlpha[14])
		return host.draw(context, []uint32{0, 3, 4, 0})
	}); err != nil {
		t.Fatal(err)
	}

	pixels, _, err := host.readScanout(&resource{description: color}, image.Rect(0, 0, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pixels, []byte{0, 0, 255, 255}; string(got) != string(want) {
		t.Fatalf("stencil-tested color draw BGRA = %v, want %v", got, want)
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
		{Opcode: 1, Object: 2, Payload: []uint32{13, 1 << 14, 0, 0, 0, 0, 0, 0, 0}},
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
		{Opcode: 1, Object: 2, Payload: []uint32{12, 2 << 8, 0, 0, 0, 0, 0, 0, 0}},
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
	context.colorSurfaces[0] = 11
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
	constant := virtio.GPUResource3D{ID: 3, Target: 0, Width: 8}
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
		{constant, floatBytes(0, 1)},
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
	context.colorSurfaces[0] = 11
	context.vertexElements[12] = []hostVertexElement{
		{bufferIndex: 0, format: 29},
		{bufferIndex: 1, offset: 4, format: 28},
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

func TestPointSpriteCoordinateModeControlsVerticalOrigin(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	color := virtio.GPUResource3D{ID: 1, Target: 2, Format: 67, Width: 2, Height: 2, Depth: 1, ArraySize: 1}
	position := virtio.GPUResource3D{ID: 2, Target: 0, Width: 8}
	for _, description := range []virtio.GPUResource3D{color, position} {
		if err := host.createResource(description); err != nil {
			t.Fatal(err)
		}
	}
	positionBytes := make([]byte, 8)
	if err := host.transferToHost(&resource{description: position, data: positionBytes}, virtio.GPUTransfer3D{
		ResourceID: position.ID,
		Box:        virtio.GPUBox{Width: position.Width},
	}); err != nil {
		t.Fatal(err)
	}

	context := host.contexts[contextID]
	context.surfaces[11] = hostSurface{resourceID: color.ID, resource: host.resources[color.ID]}
	context.colorSurfaces[0] = 11
	context.vertexElements[12] = []hostVertexElement{{bufferIndex: 0, format: 29}}
	context.boundVertexElements = 12
	context.vertexBuffers[0] = hostVertexBuffer{stride: 8, resourceID: position.ID, resource: host.resources[position.ID]}
	context.shaders[20] = hostShader{stage: tgsiVertex, source: `#version 410 core
layout(location = 0) in vec2 position;
void main() {
	gl_Position = vec4(position, 0.0, 1.0);
	gl_PointSize = 2.0;
}`}
	context.shaders[21] = hostShader{stage: tgsiFragment, source: `#version 410 core
layout(location = 0) out vec4 color;
void main() { color = vec4(0.0, gl_PointCoord.y, 0.0, 1.0); }`}
	context.boundShaders[tgsiVertex] = 20
	context.boundShaders[tgsiFragment] = 21

	render := func(state uint32) [16]byte {
		t.Helper()
		var pixels [16]byte
		if err := host.dispatch(func() error {
			rasterizer := hostRasterizer{state: state}
			context.rasterizers[13] = rasterizer
			context.boundRasterizer = 13
			host.applyRasterizer(context, rasterizer)
			host.gl.viewport(0, 0, 2, 2)
			host.gl.clearColor(0, 0, 0, 1)
			host.gl.clear(glColorBufferBit)
			if err := host.draw(context, []uint32{0, 1, 0, 0}); err != nil {
				return err
			}
			host.gl.readPixels(0, 0, 2, 2, glRGBA, glUnsignedByte, glPointer(pixels[:]))
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return pixels
	}

	const pointQuadAndPerVertexSize = uint32(1<<7 | 1<<24)
	lowerLeft := render(pointQuadAndPerVertexSize)
	if bottom, top := lowerLeft[1], lowerLeft[9]; bottom >= top {
		t.Fatalf("lower-left point coordinates have bottom/top green %d/%d, want bottom < top", bottom, top)
	}
	upperLeft := render(pointQuadAndPerVertexSize | 1<<6)
	if bottom, top := upperLeft[1], upperLeft[9]; bottom <= top {
		t.Fatalf("upper-left point coordinates have bottom/top green %d/%d, want bottom > top", bottom, top)
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
	context.colorSurfaces[0] = 11
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
		host.gl.bindSampler(0, context.samplerStates[21].id)
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

func TestIndependentSamplerStatesCanShareOneTexture(t *testing.T) {
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
	texture := virtio.GPUResource3D{ID: 2, Target: 2, Format: 67, Width: 2, Height: 1, Depth: 1, ArraySize: 1}
	if err := host.createResource(output); err != nil {
		t.Fatal(err)
	}
	if err := host.createResource(texture); err != nil {
		t.Fatal(err)
	}
	if err := host.transferToHost(&resource{
		description: texture,
		data: []byte{
			255, 0, 0, 255,
			0, 0, 255, 255,
		},
	}, virtio.GPUTransfer3D{
		ResourceID: texture.ID,
		Box:        virtio.GPUBox{Width: 2, Height: 1, Depth: 1},
	}); err != nil {
		t.Fatal(err)
	}

	statePayload := func(handle, state uint32) []uint32 {
		return []uint32{handle, state, 0, 0, math.Float32bits(1000), 0, 0, 0, 0}
	}
	if err := host.execute(contextID, []command{
		// PIPE_TEX_WRAP_CLAMP_TO_EDGE for S on sampler 10; repeat on 11.
		{Opcode: 1, Object: 7, Payload: statePayload(10, 2)},
		{Opcode: 1, Object: 7, Payload: statePayload(11, 0)},
	}, nil); err != nil {
		t.Fatal(err)
	}

	vertex := `#version 150
void main() {
	vec2 position = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
	gl_Position = vec4(position * 2.0 - 1.0, 0.0, 1.0);
}`
	fragment := `#version 150
uniform sampler2D clamped;
uniform sampler2D repeated;
out vec4 result;
void main() {
	result = gl_FragCoord.x < 1.0 ? texture(clamped, vec2(-0.25, 0.5))
	                                  : texture(repeated, vec2(-0.25, 0.5));
}`
	if err := host.dispatch(func() error {
		program, err := host.gl.compileProgram(vertex, fragment)
		if err != nil {
			return err
		}
		defer host.gl.deleteProgram(program)
		source := host.resources[texture.ID]
		for unit, sampler := range []uint32{10, 11} {
			host.gl.activeTexture(glTexture0 + uint32(unit))
			host.gl.bindTexture(glTexture2D, source.texture)
			host.gl.bindSampler(uint32(unit), host.contexts[contextID].samplerStates[sampler].id)
		}
		host.gl.bindFramebuffer(glFramebuffer, host.resources[output.ID].framebuffer)
		host.gl.viewport(0, 0, 2, 1)
		host.gl.useProgram(program)
		host.gl.uniform1i(uniformLocation(host.gl, program, "clamped"), 0)
		host.gl.uniform1i(uniformLocation(host.gl, program, "repeated"), 1)
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
	if got, want := pixels, []byte{0, 0, 255, 255, 255, 0, 0, 255}; string(got) != string(want) {
		t.Fatalf("independent sampler pixels BGRA = %v, want %v", got, want)
	}
}

func TestGalliumMipFilterNoneDoesNotSelectMipLevels(t *testing.T) {
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
	texture := virtio.GPUResource3D{ID: 2, Target: 2, Format: 67, Width: 2, Height: 2, Depth: 1, ArraySize: 1, LastLevel: 1}
	if err := host.createResource(output); err != nil {
		t.Fatal(err)
	}
	if err := host.createResource(texture); err != nil {
		t.Fatal(err)
	}
	level0 := []byte{
		255, 0, 0, 255, 255, 0, 0, 255,
		255, 0, 0, 255, 255, 0, 0, 255,
	}
	if err := host.transferToHost(&resource{description: texture, data: level0}, virtio.GPUTransfer3D{
		ResourceID: texture.ID,
		Box:        virtio.GPUBox{Width: 2, Height: 2, Depth: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := host.transferToHost(&resource{description: texture, data: []byte{0, 0, 255, 255}}, virtio.GPUTransfer3D{
		ResourceID: texture.ID,
		Level:      1,
		Box:        virtio.GPUBox{Width: 1, Height: 1, Depth: 1},
	}); err != nil {
		t.Fatal(err)
	}

	statePayload := func(handle, state uint32) []uint32 {
		return []uint32{handle, state, 0, 0, math.Float32bits(1000), 0, 0, 0, 0}
	}
	const clampToEdge = uint32(2 | (2 << 3))
	if err := host.execute(contextID, []command{
		// Gallium values: NONE=2 and NEAREST=0.
		{Opcode: 1, Object: 7, Payload: statePayload(10, clampToEdge|(2<<11))},
		{Opcode: 1, Object: 7, Payload: statePayload(11, clampToEdge)},
	}, nil); err != nil {
		t.Fatal(err)
	}

	vertex := `#version 150
void main() {
	vec2 position = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
	gl_Position = vec4(position * 2.0 - 1.0, 0.0, 1.0);
}`
	fragment := `#version 150
uniform sampler2D withoutMips;
uniform sampler2D withMips;
out vec4 result;
void main() {
	result = gl_FragCoord.x < 1.0 ? textureLod(withoutMips, vec2(0.5), 1.0)
	                                  : textureLod(withMips, vec2(0.5), 1.0);
}`
	if err := host.dispatch(func() error {
		program, err := host.gl.compileProgram(vertex, fragment)
		if err != nil {
			return err
		}
		defer host.gl.deleteProgram(program)
		source := host.resources[texture.ID]
		for unit, sampler := range []uint32{10, 11} {
			host.gl.activeTexture(glTexture0 + uint32(unit))
			host.gl.bindTexture(glTexture2D, source.texture)
			host.gl.bindSampler(uint32(unit), host.contexts[contextID].samplerStates[sampler].id)
		}
		host.gl.bindFramebuffer(glFramebuffer, host.resources[output.ID].framebuffer)
		host.gl.viewport(0, 0, 2, 1)
		host.gl.useProgram(program)
		host.gl.uniform1i(uniformLocation(host.gl, program, "withoutMips"), 0)
		host.gl.uniform1i(uniformLocation(host.gl, program, "withMips"), 1)
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
	if got, want := pixels, []byte{0, 0, 255, 255, 255, 0, 0, 255}; string(got) != string(want) {
		t.Fatalf("mip filter pixels BGRA = %v, want %v", got, want)
	}
}

func TestCubeMapTransfersAndSamplingRenderEveryFace(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	output := virtio.GPUResource3D{ID: 1, Target: 2, Format: 67, Width: 6, Height: 1, Depth: 1, ArraySize: 1}
	cube := virtio.GPUResource3D{ID: 2, Target: 4, Format: 67, Width: 1, Height: 1, Depth: 1, ArraySize: 6}
	staging := virtio.GPUResource3D{ID: 3, Target: 2, Format: 67, Width: 1, Height: 1, Depth: 1, ArraySize: 1}
	if err := host.createResource(output); err != nil {
		t.Fatal(err)
	}
	if err := host.createResource(cube); err != nil {
		t.Fatal(err)
	}
	if err := host.createResource(staging); err != nil {
		t.Fatal(err)
	}

	faces := [6][4]byte{
		{255, 0, 0, 255},
		{0, 255, 0, 255},
		{0, 0, 255, 255},
		{255, 255, 0, 255},
		{255, 0, 255, 255},
		{0, 255, 255, 255},
	}
	for face, pixel := range faces {
		if face == 0 {
			continue
		}
		if err := host.transferToHost(&resource{
			description: cube,
			data:        pixel[:],
		}, virtio.GPUTransfer3D{
			ResourceID: cube.ID,
			Box:        virtio.GPUBox{Z: uint32(face), Width: 1, Height: 1, Depth: 1},
		}); err != nil {
			t.Fatalf("upload cube face %d: %v", face, err)
		}
	}
	if err := host.transferToHost(&resource{description: staging, data: faces[0][:]}, virtio.GPUTransfer3D{
		ResourceID: staging.ID,
		Box:        virtio.GPUBox{Width: 1, Height: 1, Depth: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := host.execute(contextID, []command{{
		Opcode: 17,
		Payload: []uint32{
			cube.ID, 0, 0, 0, 0,
			staging.ID, 0, 0, 0, 0,
			1, 1, 1,
		},
	}}, nil); err != nil {
		t.Fatalf("copy staging texture into cube face: %v", err)
	}

	identitySwizzle := uint32(0 | (1 << 3) | (2 << 6) | (3 << 9))
	if err := host.execute(contextID, []command{
		{Opcode: 1, Object: 8, Payload: []uint32{11, output.ID}},
		{Opcode: 5, Payload: []uint32{1, 0, 11}},
		{Opcode: 1, Object: 6, Payload: []uint32{20, cube.ID, 67, 0, 0, identitySwizzle}},
		{Opcode: 1, Object: 7, Payload: []uint32{
			21, 0,
			math.Float32bits(0), math.Float32bits(0), math.Float32bits(0),
			math.Float32bits(0), math.Float32bits(0), math.Float32bits(0), math.Float32bits(0),
		}},
	}, nil); err != nil {
		t.Fatal(err)
	}

	vertex := `#version 150
void main() {
	vec2 position = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
	gl_Position = vec4(position * 2.0 - 1.0, 0.0, 1.0);
}`
	fragment := `#version 150
uniform samplerCube source;
out vec4 result;
void main() {
	float x = gl_FragCoord.x;
	vec3 direction = x < 1.0 ? vec3(1.0, 0.0, 0.0) :
	                 x < 2.0 ? vec3(-1.0, 0.0, 0.0) :
	                 x < 3.0 ? vec3(0.0, 1.0, 0.0) :
	                 x < 4.0 ? vec3(0.0, -1.0, 0.0) :
	                 x < 5.0 ? vec3(0.0, 0.0, 1.0) : vec3(0.0, 0.0, -1.0);
	result = texture(source, direction);
}`
	if err := host.dispatch(func() error {
		program, err := host.gl.compileProgram(vertex, fragment)
		if err != nil {
			return err
		}
		defer host.gl.deleteProgram(program)
		resource := host.resources[cube.ID]
		context := host.contexts[contextID]
		host.gl.activeTexture(glTexture0)
		host.gl.bindTexture(resource.textureTarget, resource.texture)
		if err := host.applySamplerView(context.samplerViews[20]); err != nil {
			return err
		}
		host.gl.bindSampler(0, context.samplerStates[21].id)
		if err := host.bindContextFramebuffer(context); err != nil {
			return err
		}
		host.gl.viewport(0, 0, 6, 1)
		host.gl.useProgram(program)
		host.gl.uniform1i(uniformLocation(host.gl, program, "source"), 0)
		host.gl.bindVertexArray(host.vao)
		host.gl.drawArrays(glTriangles, 0, 3)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	pixels, _, err := host.readScanout(&resource{description: output}, image.Rect(0, 0, 6, 1))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0, 0, 255, 255,
		0, 255, 0, 255,
		255, 0, 0, 255,
		0, 255, 255, 255,
		255, 0, 255, 255,
		255, 255, 0, 255,
	}
	if string(pixels) != string(want) {
		t.Fatalf("sampled cube-map pixels BGRA = %v, want %v", pixels, want)
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

func TestTransferFromHostReturnsTextureRowsToGuestBacking(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	renderer := NewRenderer(host)
	defer renderer.Close()

	description := virtio.GPUResource3D{
		ID: 1, Target: 2, Format: 67,
		Width: 4, Height: 2, Depth: 1, ArraySize: 1,
	}
	if err := renderer.CreateResource(description); err != nil {
		t.Fatal(err)
	}
	pixels := []byte{
		1, 2, 3, 255, 11, 12, 13, 255, 21, 22, 23, 255, 31, 32, 33, 255,
		41, 42, 43, 255, 51, 52, 53, 255, 61, 62, 63, 255, 71, 72, 73, 255,
	}
	if err := host.dispatch(func() error {
		host.gl.bindTexture(glTexture2D, host.resources[description.ID].texture)
		host.gl.texSubImage2D(glTexture2D, 0, 0, 0, 4, 2, glRGBA, glUnsignedByte, glPointer(pixels))
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	backing := transferBacking(make([]byte, 36))
	for index := range backing {
		backing[index] = 0xee
	}
	if err := renderer.TransferFromHost(virtio.GPUTransfer3D{
		ResourceID: description.ID,
		Box:        virtio.GPUBox{X: 1, Width: 2, Height: 2, Depth: 1},
		Offset:     4,
		Stride:     16,
		Backing:    backing,
	}); err != nil {
		t.Fatal(err)
	}

	want := transferBacking(make([]byte, len(backing)))
	for index := range want {
		want[index] = 0xee
	}
	copy(want[4:12], pixels[4:12])
	copy(want[20:28], pixels[20:28])
	if string(backing) != string(want) {
		t.Fatalf("guest backing after texture readback = %v, want %v", backing, want)
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
	blit[9], blit[10], blit[11] = 1, 1, 1
	blit[12], blit[13] = description.ID, 0
	blit[18], blit[19], blit[20] = 2, 2, 1
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

func TestBlitPopulatesRequestedCubeFaceAndMipLevel(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	src := virtio.GPUResource3D{ID: 1, Target: 2, Format: 67, Width: 2, Height: 2, Depth: 1, ArraySize: 1}
	dst := virtio.GPUResource3D{ID: 2, Target: 4, Format: 67, Width: 2, Height: 2, Depth: 1, ArraySize: 6, LastLevel: 1}
	if err := host.createResource(src); err != nil {
		t.Fatal(err)
	}
	if err := host.createResource(dst); err != nil {
		t.Fatal(err)
	}
	want := []byte{19, 83, 211, 255}
	pixels := make([]byte, 2*2*4)
	for offset := 0; offset < len(pixels); offset += 4 {
		copy(pixels[offset:], want)
	}
	if err := host.transferToHost(&resource{description: src, data: pixels}, virtio.GPUTransfer3D{
		ResourceID: src.ID,
		Box:        virtio.GPUBox{Width: 2, Height: 2, Depth: 1},
	}); err != nil {
		t.Fatal(err)
	}

	blit := make([]uint32, 21)
	blit[0] = 0xf | (1 << 8)
	blit[3], blit[4], blit[8] = dst.ID, 1, 3
	blit[9], blit[10], blit[11] = 1, 1, 1
	blit[12] = src.ID
	blit[18], blit[19], blit[20] = 2, 2, 1
	if err := host.execute(contextID, []command{{Opcode: 16, Payload: blit}}, nil); err != nil {
		t.Fatal(err)
	}

	got := make([]byte, 4)
	if err := host.dispatch(func() error {
		resource := host.resources[dst.ID]
		host.gl.bindFramebuffer(glReadFramebuffer, host.blitReadFBO)
		host.gl.framebufferTexture(glReadFramebuffer, glColorAttachment0, glTextureCubeMapPositiveX+3, resource.texture, 1)
		if status := host.gl.checkFramebuffer(glReadFramebuffer); status != glFramebufferComplete {
			return fmt.Errorf("cube mip framebuffer status %#x", status)
		}
		host.gl.readPixels(0, 0, 1, 1, glRGBA, glUnsignedByte, glPointer(got))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("cube face mip pixel RGBA = %v, want %v", got, want)
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

func TestResourceCopyRegionCopiesMultipleArrayLayers(t *testing.T) {
	host, err := newDarwinHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()

	const contextID = 1
	if err := host.createContext(contextID); err != nil {
		t.Fatal(err)
	}
	src := virtio.GPUResource3D{ID: 1, Target: 7, Format: 67, Width: 1, Height: 1, Depth: 1, ArraySize: 3}
	dst := virtio.GPUResource3D{ID: 2, Target: 7, Format: 67, Width: 1, Height: 1, Depth: 1, ArraySize: 3}
	for _, description := range []virtio.GPUResource3D{src, dst} {
		if err := host.createResource(description); err != nil {
			t.Fatal(err)
		}
	}
	want := []byte{19, 83, 211, 255, 7, 131, 41, 255}
	if err := host.transferToHost(&resource{description: src, data: want}, virtio.GPUTransfer3D{
		ResourceID:  src.ID,
		Stride:      4,
		LayerStride: 4,
		Box:         virtio.GPUBox{Z: 1, Width: 1, Height: 1, Depth: 2},
	}); err != nil {
		t.Fatal(err)
	}
	copyRegion := []uint32{
		dst.ID, 0, 0, 0, 0,
		src.ID, 0, 0, 0, 1,
		1, 1, 2,
	}
	if err := host.execute(contextID, []command{{Opcode: 17, Payload: copyRegion}}, nil); err != nil {
		t.Fatal(err)
	}

	readback := &resource{description: dst, data: make([]byte, len(want))}
	if err := host.transferFromHost(readback, virtio.GPUTransfer3D{
		ResourceID:  dst.ID,
		Stride:      4,
		LayerStride: 4,
		Box:         virtio.GPUBox{Width: 1, Height: 1, Depth: 2},
	}); err != nil {
		t.Fatal(err)
	}
	if string(readback.data) != string(want) {
		t.Fatalf("copied array texture layers = %v, want %v", readback.data, want)
	}
}
