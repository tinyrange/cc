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
	return replayCapture(capturePath, outputPath, frame, 0, 0, 0, 0, nil)
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
	return replayCapture(capturePath, outputPath, frame, resourceID, level, 0, 0, nil)
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
	return replayCapture(capturePath, outputPath, frame, resourceID, 0, draw, 0, nil)
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
	return replayCapture(capturePath, outputPath, frame, 0, 0, 0, resourceID, output)
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
	return replayCapture(capturePath, outputPath, frame, 0, 0, 0, ^uint32(0), output)
}

func replayCapture(capturePath, outputPath string, frame int, resourceID, resourceLevel uint32, selectedDraw int, traceResourceID uint32, traceOutput io.Writer) (int, error) {
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
	traceStates := make(map[string]struct{})
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
			} else if err := host.execute(contextID, commands, resources); err != nil {
				return 0, err
			}
			var glError uint32
			if err := host.dispatch(func() error {
				glError = host.gl.getError()
				return nil
			}); err != nil {
				return 0, err
			}
			if glError != 0 {
				return 0, fmt.Errorf("OpenGL error %#x after VirGL execute", glError)
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
		if selectedDraw != 0 {
			return 0, fmt.Errorf("VirGL frame %d has %d draws, requested %d", frame, draw, selectedDraw)
		}
		return 0, fmt.Errorf("VirGL capture has %d checkpoints, requested %d", checkpoint, frame)
	}
	return checkpoint, writeReplayPNG(host, selectedResource, selectedRect, resourceLevel, outputPath)
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
	if surface, ok := context.surfaces[context.colorSurface]; ok {
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
	fmt.Fprintf(output, "draw=%d context=%d subcontext=%d framebuffer=%d framebuffer_resource=%d depth_surface=%d depth_resource=%d depth_format=%d resource=%d fragment_slots=%v vertex_elements=%d shaders=%d/%d rasterizer=%d dsa=%d\n",
		draw, contextID, root.activeSubcontext, context.colorSurface, colorResource,
		context.depthSurface, depthResource, depthFormat, resourceID, slots,
		context.boundVertexElements, vertexHandle, fragmentHandle, context.boundRasterizer, context.boundDSA)
	fmt.Fprintf(output, "  draw_payload=%v index_resource=%d index_size=%d index_offset=%d\n",
		command.Payload, context.indexBuffer, context.indexSize, context.indexOffset)
	fmt.Fprintf(output, "  rasterizer_state=%#x dsa_state=%#x\n",
		context.rasterizers[context.boundRasterizer], context.depthStencilAlpha[context.boundDSA].state)
	fmt.Fprintf(output, "  vertex_constants=%v fragment_constants=%v\n",
		context.constants[tgsiVertex], context.constants[tgsiFragment])
	if _, ok := seen[stateKey]; ok {
		return
	}
	seen[stateKey] = struct{}{}
	for index, element := range context.vertexElements[context.boundVertexElements] {
		binding := context.vertexBuffers[element.bufferIndex]
		fmt.Fprintf(output, "  attribute=%d format=%d element_offset=%d buffer_slot=%d resource=%d stride=%d buffer_offset=%d\n",
			index, element.format, element.offset, element.bufferIndex,
			binding.resourceID, binding.stride, binding.offset)
	}
	for _, slot := range slots {
		viewHandle := context.boundSamplerViews[tgsiFragment][slot]
		view := context.samplerViews[viewHandle]
		stateHandle := context.boundSamplerStates[tgsiFragment][slot]
		fmt.Fprintf(output, "  sampler_slot=%d view=%d resource=%d format=%d levels=%d..%d state=%d\n",
			slot, viewHandle, view.resourceID, view.format, view.firstLevel, view.lastLevel, stateHandle)
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
	if level == 0 {
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
		host.gl.framebufferTexture(glReadFramebuffer, glColorAttachment0, glTexture2D, hostResource.texture, int32(level))
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
