//go:build darwin

package virgl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"strings"

	"j5.nz/cc/internal/virtio"
)

// ReplayCapture executes a deterministic VirGL capture and writes the selected
// raw scanout checkpoint as a PNG. A frame value of zero selects the final
// checkpoint in the capture.
func ReplayCapture(capturePath, outputPath string, frame int) (int, error) {
	return replayCapture(capturePath, outputPath, frame, 0, 0, 0, 0, nil, nil)
}

// ReplayCaptureResource renders a specific active resource at a capture
// checkpoint. It is useful for comparing an application's render target with
// the compositor scanout when diagnosing presentation faults.
func ReplayCaptureResource(capturePath, outputPath string, frame int, resourceID uint32) (int, error) {
	return ReplayCaptureResourceLevel(capturePath, outputPath, frame, resourceID, 0)
}

// ReplayCaptureResourceLevel renders one mip level of a specific active
// texture resource at a capture checkpoint.
func ReplayCaptureResourceLevel(capturePath, outputPath string, frame int, resourceID, level uint32) (int, error) {
	if resourceID == 0 {
		return 0, errors.New("VirGL replay resource ID must be nonzero")
	}
	return replayCapture(capturePath, outputPath, frame, resourceID, level, 0, 0, nil, nil)
}

// ReplayCaptureResourceDraw renders a resource immediately after a selected
// draw in one captured frame. Draw numbers start at one within the frame.
func ReplayCaptureResourceDraw(capturePath, outputPath string, frame int, resourceID uint32, draw int) (int, error) {
	if resourceID == 0 {
		return 0, errors.New("VirGL replay resource ID must be nonzero")
	}
	if frame <= 0 || draw <= 0 {
		return 0, errors.New("VirGL replay frame and draw must be positive")
	}
	return replayCapture(capturePath, outputPath, frame, resourceID, 0, draw, 0, nil, nil)
}

// ReplayCaptureTraceResource writes the final scanout and reports draw state
// whenever the selected texture resource is bound during one captured frame.
func ReplayCaptureTraceResource(capturePath, outputPath string, frame int, resourceID uint32, output io.Writer) (int, error) {
	if frame <= 0 || resourceID == 0 {
		return 0, errors.New("VirGL replay trace frame and resource ID must be positive")
	}
	if output == nil {
		return 0, errors.New("VirGL replay trace output must be non-nil")
	}
	return replayCapture(capturePath, outputPath, frame, 0, 0, 0, resourceID, output, nil)
}

// ReplayCaptureTraceDraws writes the final scanout and reports the complete
// draw-state sequence for one captured frame.
func ReplayCaptureTraceDraws(capturePath, outputPath string, frame int, output io.Writer) (int, error) {
	if frame <= 0 {
		return 0, errors.New("VirGL replay trace frame must be positive")
	}
	if output == nil {
		return 0, errors.New("VirGL replay trace output must be non-nil")
	}
	return replayCapture(capturePath, outputPath, frame, 0, 0, 0, ^uint32(0), output, nil)
}

// FindCaptureProjectiveDraws reports draws whose first vertex attribute has a
// varying clip-space W component. Mesa's texture projection tests encode their
// perspective in vertex positions, so this locates the relevant checkpoints in
// a large capture without relying on test-order or frame-count assumptions.
func FindCaptureProjectiveDraws(capturePath string, output io.Writer) error {
	if output == nil {
		return errors.New("VirGL projective draw output must be non-nil")
	}
	_, err := replayCapture(capturePath, "", 0, 0, 0, 0, 0, nil, output)
	return err
}

// FindCaptureShaderText reports capture checkpoints whose shader command data
// contains text. It provides a cheap way to locate a workload in a large
// capture before replaying a specific checkpoint and inspecting its draw state.
func FindCaptureShaderText(capturePath, match string, output io.Writer) error {
	if match == "" {
		return errors.New("VirGL shader search text must be nonempty")
	}
	if output == nil {
		return errors.New("VirGL shader search output must be non-nil")
	}
	decoder, err := openCapture(capturePath)
	if err != nil {
		return fmt.Errorf("open VirGL capture: %w", err)
	}
	defer decoder.close()

	checkpoint := 0
	for {
		kind, payload, err := decoder.next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read VirGL capture record: %w", err)
		}
		switch kind {
		case captureScanout:
			checkpoint++
		case captureExecute:
			if len(payload) < 8 {
				return errors.New("truncated VirGL execute record")
			}
			contextID := binary.LittleEndian.Uint32(payload)
			commands, err := decodeCommands(payload[8:])
			if err != nil {
				return err
			}
			for _, item := range commands {
				if item.Opcode != 1 || item.Object != 4 || len(item.Payload) < 6 {
					continue
				}
				chunk := shaderBytes(item.Payload[5:])
				if !strings.Contains(string(chunk), match) {
					continue
				}
				fmt.Fprintf(output, "checkpoint=%d context=%d shader=%d stage=%d match=%q\n",
					checkpoint, contextID, item.Payload[0], item.Payload[1], match)
			}
		}
	}
}

func replayCapture(capturePath, outputPath string, frame int, resourceID, resourceLevel uint32, selectedDraw int, traceResourceID uint32, traceOutput, projectiveOutput io.Writer) (int, error) {
	if frame < 0 {
		return 0, errors.New("VirGL replay frame cannot be negative")
	}
	decoder, err := openCapture(capturePath)
	if err != nil {
		return 0, fmt.Errorf("open VirGL capture: %w", err)
	}
	defer decoder.close()
	host, err := newDarwinHost()
	if err != nil {
		return 0, err
	}
	defer host.close()

	resources := make(map[uint32]*resource)
	checkpoint := 0
	draw := 0
	projectiveDraw := 0
	traceStates := make(map[string]struct{})
	projectiveStates := make(map[string]struct{})
	isolatedDrawFrame := false
	var selectedResource *resource
	var selectedRect image.Rectangle
	for {
		kind, payload, err := decoder.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("read VirGL capture record: %w", err)
		}
		switch kind {
		case captureReset:
			if len(payload) != 0 {
				return 0, errors.New("invalid VirGL reset record")
			}
			if err := host.reset(); err != nil {
				return 0, err
			}
			resources = make(map[uint32]*resource)
		case captureCreateContext:
			values, err := fixedWords(payload, 1)
			if err != nil {
				return 0, err
			}
			if err := host.createContext(values[0]); err != nil {
				return 0, err
			}
		case captureDestroyContext:
			values, err := fixedWords(payload, 1)
			if err != nil {
				return 0, err
			}
			if err := host.destroyContext(values[0]); err != nil {
				return 0, err
			}
		case captureCreateResource:
			values, err := fixedWords(payload, 11)
			if err != nil {
				return 0, err
			}
			description := virtio.GPUResource3D{
				ID: values[0], Target: values[1], Format: values[2], Bind: values[3],
				Width: values[4], Height: values[5], Depth: values[6], ArraySize: values[7],
				LastLevel: values[8], Samples: values[9], Flags: values[10],
			}
			if err := host.createResource(description); err != nil {
				return 0, err
			}
			resources[description.ID] = &resource{description: description}
		case captureUnrefResource:
			values, err := fixedWords(payload, 1)
			if err != nil {
				return 0, err
			}
			if err := host.unrefResource(values[0]); err != nil {
				return 0, err
			}
			delete(resources, values[0])
		case captureTransferToHost:
			if err := replayTransfer(host, resources, payload); err != nil {
				return 0, err
			}
		case captureExecute:
			if len(payload) < 8 {
				return 0, errors.New("truncated VirGL execute record")
			}
			contextID := binary.LittleEndian.Uint32(payload)
			length := binary.LittleEndian.Uint32(payload[4:])
			if uint64(length) != uint64(len(payload)-8) {
				return 0, errors.New("invalid VirGL execute stream length")
			}
			commands, err := decodeCommands(payload[8:])
			if err != nil {
				return 0, err
			}
			if selectedDraw != 0 && checkpoint == frame-1 && !isolatedDrawFrame {
				if err := clearReplayDrawTarget(host, resources[resourceID]); err != nil {
					return 0, err
				}
				isolatedDrawFrame = true
			}
			traceFrame := traceOutput != nil && checkpoint == frame-1
			if (selectedDraw != 0 || traceFrame) && checkpoint == frame-1 {
				for _, item := range commands {
					if err := host.execute(contextID, []command{item}, resources); err != nil {
						return 0, err
					}
					if item.Opcode != 8 {
						continue
					}
					draw++
					if traceFrame {
						traceResourceDraw(traceOutput, host, contextID, draw, traceResourceID, traceStates, item)
					}
					if draw != selectedDraw {
						continue
					}
					selectedResource = resources[resourceID]
					if selectedResource == nil {
						return 0, fmt.Errorf("VirGL draw replay refers to unknown resource %d", resourceID)
					}
					selectedRect = image.Rect(0, 0, int(selectedResource.description.Width), int(selectedResource.description.Height))
					return frame, writeReplayPNG(host, selectedResource, selectedRect, 0, outputPath)
				}
			} else {
				for index, item := range commands {
					if item.Opcode == 8 && projectiveOutput != nil {
						// The search only needs command state and the latest vertex
						// bytes. Avoid compiling programs and rendering thousands of
						// unrelated draws while scanning a large capture.
						if err := host.dispatch(host.flushPendingBufferTransfers); err != nil {
							return 0, err
						}
						projectiveDraw++
						traceProjectiveDraw(projectiveOutput, host, contextID, checkpoint+1, projectiveDraw, projectiveStates)
						continue
					}
					if err := host.execute(contextID, []command{item}, resources); err != nil {
						return 0, err
					}
					var commandError uint32
					if err := host.dispatch(func() error {
						commandError = host.gl.getError()
						return nil
					}); err != nil {
						return 0, err
					}
					if commandError != 0 {
						return 0, fmt.Errorf("OpenGL error %#x after VirGL command %d/%d at checkpoint %d in context %d (command %d of %d)",
							commandError, item.Opcode, item.Object, checkpoint, contextID, index+1, len(commands))
					}
				}
			}
			var glError uint32
			if err := host.dispatch(func() error {
				glError = host.gl.getError()
				return nil
			}); err != nil {
				return 0, err
			}
			if glError != 0 {
				commandNames := make([]string, len(commands))
				for index, item := range commands {
					commandNames[index] = fmt.Sprintf("%d/%d", item.Opcode, item.Object)
				}
				return 0, fmt.Errorf("OpenGL error %#x after VirGL execute at checkpoint %d in context %d (commands %s)",
					glError, checkpoint, contextID, strings.Join(commandNames, ","))
			}
		case captureScanout:
			values, err := fixedWords(payload, 5)
			if err != nil {
				return 0, err
			}
			checkpoint++
			if frame == 0 || checkpoint == frame {
				selectedID := values[0]
				if resourceID != 0 {
					selectedID = resourceID
				}
				selectedResource = resources[selectedID]
				if selectedResource == nil {
					return 0, fmt.Errorf("VirGL checkpoint refers to unknown resource %d", selectedID)
				}
				if resourceID != 0 {
					if resourceLevel > selectedResource.description.LastLevel {
						return 0, fmt.Errorf("VirGL resource %d mip level %d exceeds last level %d",
							resourceID, resourceLevel, selectedResource.description.LastLevel)
					}
					width := selectedResource.description.Width >> resourceLevel
					height := selectedResource.description.Height >> resourceLevel
					if width == 0 {
						width = 1
					}
					if height == 0 {
						height = 1
					}
					selectedRect = image.Rect(0, 0, int(width), int(height))
				} else {
					selectedRect = image.Rect(int(int32(values[1])), int(int32(values[2])),
						int(int32(values[3])), int(int32(values[4])))
				}
			}
			if frame != 0 && checkpoint == frame {
				return checkpoint, writeReplayPNG(host, selectedResource, selectedRect, resourceLevel, outputPath)
			}
		default:
			return 0, fmt.Errorf("unknown VirGL capture record %d", kind)
		}
	}
	if selectedResource == nil {
		if projectiveOutput != nil {
			return checkpoint, nil
		}
		if selectedDraw != 0 {
			return 0, fmt.Errorf("VirGL frame %d has %d draws, requested %d", frame, draw, selectedDraw)
		}
		return 0, fmt.Errorf("VirGL capture has %d checkpoints, requested %d", checkpoint, frame)
	}
	if projectiveOutput != nil {
		return checkpoint, nil
	}
	return checkpoint, writeReplayPNG(host, selectedResource, selectedRect, resourceLevel, outputPath)
}

func traceProjectiveDraw(output io.Writer, host *darwinHost, contextID uint32, checkpoint, draw int, seen map[string]struct{}) {
	root := host.contexts[contextID]
	if root == nil {
		return
	}
	context := root.selectedContext()
	elements := context.vertexElements[context.boundVertexElements]
	if len(elements) == 0 {
		return
	}
	element := elements[0]
	binding := context.vertexBuffers[element.bufferIndex]
	if binding.resource == nil || binding.stride == 0 {
		return
	}
	var w [4]float32
	for vertex := uint32(0); vertex < uint32(len(w)); vertex++ {
		value, err := constantVertexAttribute(binding.resource.bufferBytes,
			binding.offset+element.offset+vertex*binding.stride, element.format)
		if err != nil {
			return
		}
		w[vertex] = value[3]
	}
	minW, maxW := w[0], w[0]
	for _, value := range w[1:] {
		if value < minW {
			minW = value
		}
		if value > maxW {
			maxW = value
		}
	}
	if maxW-minW < 1e-6 {
		return
	}
	key := fmt.Sprintf("%d/%d/%d/%d/%d/%v", checkpoint, contextID, root.activeSubcontext,
		binding.resourceID, binding.offset+element.offset, w)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	colorResource := uint32(0)
	if surface, ok := context.surfaces[context.firstColorSurface()]; ok {
		colorResource = surface.resourceID
	}
	rasterizer := context.rasterizers[context.boundRasterizer]
	fmt.Fprintf(output, "checkpoint=%d draw=%d context=%d subcontext=%d framebuffer_resource=%d viewport_adjust_y=%g rasterizer_state=%#x position_w=%v\n",
		checkpoint, draw, contextID, root.activeSubcontext, colorResource,
		context.viewport.adjustY, rasterizer.state, w)
}

func clearReplayDrawTarget(host *darwinHost, resource *resource) error {
	if resource == nil {
		return errors.New("VirGL draw replay target is unavailable at frame start")
	}
	return host.dispatch(func() error {
		target := host.resources[resource.description.ID]
		if target == nil || target.framebuffer == 0 {
			return fmt.Errorf("VirGL draw replay target resource %d is not renderable", resource.description.ID)
		}
		host.gl.bindFramebuffer(glFramebuffer, target.framebuffer)
		host.gl.disable(glScissorTest)
		host.gl.colorMask(true, true, true, true)
		host.gl.clearColor(1, 0, 1, 1)
		// Isolate color contributions without changing the depth/stencil history
		// carried into this frame. Some applications intentionally preserve depth
		// between swap checkpoints, and clearing it here changes multipass results.
		host.gl.clear(glColorBufferBit)
		host.framebufferBindingValid = false
		host.activeContext = nil
		return nil
	})
}

func traceResourceDraw(output io.Writer, host *darwinHost, contextID uint32, draw int, resourceID uint32, seen map[string]struct{}, command command) {
	root := host.contexts[contextID]
	if root == nil {
		return
	}
	context := root.selectedContext()
	traceAll := resourceID == ^uint32(0)
	var slots []int
	for slot, handle := range context.boundSamplerViews[tgsiFragment] {
		if view, ok := context.samplerViews[handle]; ok && (traceAll || view.resourceID == resourceID) {
			slots = append(slots, slot)
		}
	}
	if !traceAll && len(slots) == 0 {
		return
	}
	colorResource := uint32(0)
	if surface, ok := context.surfaces[context.firstColorSurface()]; ok {
		colorResource = surface.resourceID
	}
	depthResource, depthFormat := uint32(0), uint32(0)
	if surface, ok := context.surfaces[context.depthSurface]; ok {
		depthResource = surface.resourceID
		if surface.resource != nil {
			depthFormat = surface.resource.description.Format
		}
	}
	vertexHandle := context.boundShaders[tgsiVertex]
	fragmentHandle := context.boundShaders[tgsiFragment]
	stateKey := fmt.Sprintf("%p/%d/%d/%d/%v", context, context.boundVertexElements, vertexHandle, fragmentHandle, slots)
	fmt.Fprintf(output, "draw=%d context=%d subcontext=%d framebuffers=%v framebuffer_resource=%d depth_surface=%d depth_resource=%d depth_format=%d resource=%d fragment_slots=%v vertex_elements=%d shaders=%d/%d rasterizer=%d dsa=%d\n",
		draw, contextID, root.activeSubcontext, context.colorSurfaces, colorResource,
		context.depthSurface, depthResource, depthFormat, resourceID, slots,
		context.boundVertexElements, vertexHandle, fragmentHandle, context.boundRasterizer, context.boundDSA)
	fmt.Fprintf(output, "  draw_payload=%v index_resource=%d index_size=%d index_offset=%d\n",
		command.Payload, context.indexBuffer, context.indexSize, context.indexOffset)
	dsa := context.depthStencilAlpha[context.boundDSA]
	rasterizer := context.rasterizers[context.boundRasterizer]
	fmt.Fprintf(output, "  rasterizer_state=%#x point_size=%g sprite_coordinates=%#x dsa_state=%#x stencil=%#x/%#x stencil_ref=%v\n",
		rasterizer.state, rasterizer.pointSize, rasterizer.spriteCoordinateEnable,
		dsa.state, dsa.stencil[0], dsa.stencil[1], context.stencilRef)
	fmt.Fprintf(output, "  vertex_constants=%v fragment_constants=%v\n",
		context.constants[tgsiVertex], context.constants[tgsiFragment])
	if _, ok := seen[stateKey]; ok {
		return
	}
	seen[stateKey] = struct{}{}
	for index, element := range context.vertexElements[context.boundVertexElements] {
		binding := context.vertexBuffers[element.bufferIndex]
		fmt.Fprintf(output, "  attribute=%d format=%d element_offset=%d buffer_slot=%d resource=%d stride=%d buffer_offset=%d",
			index, element.format, element.offset, element.bufferIndex,
			binding.resourceID, binding.stride, binding.offset)
		if binding.resource != nil {
			value, err := constantVertexAttribute(binding.resource.bufferBytes, binding.offset+element.offset, element.format)
			if err != nil {
				fmt.Fprintf(output, " first_error=%q", err)
			} else {
				label := "first"
				if binding.stride == 0 {
					label = "constant"
				}
				fmt.Fprintf(output, " %s=%v", label, value)
				if binding.stride != 0 {
					for vertex := uint32(1); vertex < 4; vertex++ {
						next, nextErr := constantVertexAttribute(binding.resource.bufferBytes,
							binding.offset+element.offset+vertex*binding.stride, element.format)
						if nextErr != nil {
							break
						}
						fmt.Fprintf(output, " first%d=%v", vertex+1, next)
					}
				}
			}
		}
		fmt.Fprintln(output)
	}
	for _, slot := range slots {
		viewHandle := context.boundSamplerViews[tgsiFragment][slot]
		view := context.samplerViews[viewHandle]
		stateHandle := context.boundSamplerStates[tgsiFragment][slot]
		state := context.samplerStates[stateHandle]
		description := view.resource.description
		fmt.Fprintf(output, "  sampler_slot=%d view=%d resource=%d target=%d size=%dx%d last_level=%d flags=%#x format=%d levels=%d..%d state=%d bits=%#x lod=%g..%g bias=%g\n",
			slot, viewHandle, view.resourceID, description.Target, description.Width, description.Height,
			description.LastLevel, description.Flags, view.format, view.firstLevel, view.lastLevel,
			stateHandle, state.state, state.minLOD, state.maxLOD, state.lodBias)
	}
	for slot, viewHandle := range context.boundSamplerViews[tgsiVertex] {
		view, ok := context.samplerViews[viewHandle]
		if !ok {
			continue
		}
		stateHandle := context.boundSamplerStates[tgsiVertex][slot]
		state := context.samplerStates[stateHandle]
		description := view.resource.description
		fmt.Fprintf(output, "  vertex_sampler_slot=%d view=%d resource=%d target=%d size=%dx%d last_level=%d flags=%#x format=%d levels=%d..%d state=%d bits=%#x lod=%g..%g bias=%g\n",
			slot, viewHandle, view.resourceID, description.Target, description.Width, description.Height,
			description.LastLevel, description.Flags, view.format, view.firstLevel, view.lastLevel,
			stateHandle, state.state, state.minLOD, state.maxLOD, state.lodBias)
	}
	writeShader := func(label string, shader hostShader) {
		fmt.Fprintf(output, "  %s_shader:\n", label)
		for _, line := range strings.Split(shader.source, "\n") {
			fmt.Fprintf(output, "    %s\n", line)
		}
	}
	writeShader("vertex", context.shaders[vertexHandle])
	writeShader("fragment", context.shaders[fragmentHandle])
}

func replayTransfer(host hostBackend, resources map[uint32]*resource, payload []byte) error {
	if len(payload) < 56 {
		return errors.New("truncated VirGL transfer record")
	}
	reader := bytes.NewReader(payload)
	values := make([]uint32, 8)
	if err := binary.Read(reader, binary.LittleEndian, values); err != nil {
		return err
	}
	var offset uint64
	if err := binary.Read(reader, binary.LittleEndian, &offset); err != nil {
		return err
	}
	trailer := make([]uint32, 4)
	if err := binary.Read(reader, binary.LittleEndian, trailer); err != nil {
		return err
	}
	if uint64(trailer[3]) != uint64(reader.Len()) {
		return errors.New("invalid VirGL transfer data length")
	}
	data := make([]byte, reader.Len())
	if _, err := io.ReadFull(reader, data); err != nil {
		return err
	}
	resource := resources[values[1]]
	if resource == nil {
		return fmt.Errorf("VirGL transfer refers to unknown resource %d", values[1])
	}
	resource.data = data
	transfer := virtio.GPUTransfer3D{
		ContextID: values[0], ResourceID: values[1],
		Box: virtio.GPUBox{
			X: values[2], Y: values[3], Z: values[4],
			Width: values[5], Height: values[6], Depth: values[7],
		},
		Offset: offset, Level: trailer[0], Stride: trailer[1], LayerStride: trailer[2],
	}
	// Preserve the live renderer's deferred buffer-upload path. Replaying every
	// transfer synchronously can conceal ordering bugs that only affect queued
	// vertex, index, and uniform-buffer writes.
	if resource.description.Target == 0 {
		if queue, ok := host.(bufferTransferQueuer); ok {
			return queue.queueBufferTransfer(resource, transfer)
		}
	}
	return host.transferToHost(resource, transfer)
}

func fixedWords(payload []byte, count int) ([]uint32, error) {
	if len(payload) != count*4 {
		return nil, fmt.Errorf("VirGL capture record has %d bytes, want %d", len(payload), count*4)
	}
	result := make([]uint32, count)
	for index := range result {
		result[index] = binary.LittleEndian.Uint32(payload[index*4:])
	}
	return result, nil
}

func writeReplayPNG(host *darwinHost, resource *resource, rect image.Rectangle, level uint32, path string) error {
	var pixels []byte
	var stride int
	var err error
	hostResource := host.resources[resource.description.ID]
	if level == 0 && hostResource != nil && hostResource.framebuffer != 0 {
		pixels, stride, err = host.readScanout(resource, rect)
	} else {
		pixels, stride, err = readReplayTextureLevel(host, resource, level)
	}
	if err != nil {
		return err
	}
	output := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for y := 0; y < rect.Dy(); y++ {
		for x := 0; x < rect.Dx(); x++ {
			source := y*stride + x*4
			destination := y*output.Stride + x*4
			output.Pix[destination+0] = pixels[source+2]
			output.Pix[destination+1] = pixels[source+1]
			output.Pix[destination+2] = pixels[source+0]
			output.Pix[destination+3] = 0xff
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(file, output); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func readReplayTextureLevel(host *darwinHost, resource *resource, level uint32) ([]byte, int, error) {
	width, height := resource.description.Width>>level, resource.description.Height>>level
	if width == 0 {
		width = 1
	}
	if height == 0 {
		height = 1
	}
	stride := int(width) * 4
	result := make([]byte, stride*int(height))
	err := host.dispatch(func() error {
		hostResource := host.resources[resource.description.ID]
		if hostResource == nil || hostResource.texture == 0 || hostResource.depth {
			return fmt.Errorf("VirGL resource %d is not a color texture", resource.description.ID)
		}
		host.framebufferBindingValid = false
		host.gl.bindFramebuffer(glReadFramebuffer, host.blitReadFBO)
		imageTarget := hostResource.textureTarget
		if resource.description.Target == 4 {
			imageTarget = glTextureCubeMapPositiveX
		}
		host.gl.framebufferTexture(glReadFramebuffer, glColorAttachment0, imageTarget, hostResource.texture, int32(level))
		if status := host.gl.checkFramebuffer(glReadFramebuffer); status != glFramebufferComplete {
			return fmt.Errorf("VirGL resource %d mip level %d framebuffer status %#x",
				resource.description.ID, level, status)
		}
		host.gl.finish()
		raw := make([]byte, len(result))
		host.gl.readPixels(0, 0, int32(width), int32(height), glBGRA, glUnsignedByte, glPointer(raw))
		for y := 0; y < int(height); y++ {
			copy(result[y*stride:(y+1)*stride], raw[(int(height)-1-y)*stride:(int(height)-y)*stride])
		}
		return nil
	})
	return result, stride, err
}
