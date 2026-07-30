//go:build darwin

package virgl

import (
	"errors"
	"fmt"
	"image"
	"math"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"j5.nz/cc/internal/virtio"
)

const (
	nsOpenGLPFAAccelerated       = 73
	nsOpenGLPFAColorSize         = 8
	nsOpenGLPFADepthSize         = 12
	nsOpenGLPFAStencilSize       = 13
	nsOpenGLPFAOpenGLProfile     = 99
	nsOpenGLProfileVersion41Core = 0x4100
)

type hostRequest struct {
	run  func() error
	done chan error
}

type darwinHost struct {
	requests                chan hostRequest
	stop                    chan struct{}
	done                    chan struct{}
	once                    sync.Once
	gl                      *hostGL
	vao                     uint32
	enabledVertexAttributes int
	blitReadFBO             uint32
	blitDrawFBO             uint32
	programs                map[string]hostProgram
	contexts                map[uint32]*hostContext
	resources               map[uint32]*hostResource
}

type hostResource struct {
	description virtio.GPUResource3D
	texture     uint32
	buffer      uint32
	framebuffer uint32
	depth       bool
	stencil     bool
}

type hostSurface struct {
	resourceID uint32
}

type hostSamplerView struct {
	resourceID uint32
	format     uint32
	firstLevel uint32
	lastLevel  uint32
	swizzle    [4]uint32
}

type hostSamplerState struct {
	state       uint32
	lodBias     float32
	minLOD      float32
	maxLOD      float32
	borderColor [4]float32
}

type hostBlendState struct {
	state         uint32
	renderTargets [8]uint32
}

type hostDepthStencilAlpha struct {
	state uint32
}

type hostVertexElement struct {
	offset      uint32
	bufferIndex uint32
	format      uint32
}

type hostVertexBuffer struct {
	stride     uint32
	offset     uint32
	resourceID uint32
}

type hostShader struct {
	stage  uint32
	source string
}

type hostProgram struct {
	id        uint32
	constants int32
}

type hostScissor struct {
	minX, minY uint32
	maxX, maxY uint32
}

type hostContext struct {
	blendStates         map[uint32]hostBlendState
	surfaces            map[uint32]hostSurface
	samplerViews        map[uint32]hostSamplerView
	samplerStates       map[uint32]hostSamplerState
	depthStencilAlpha   map[uint32]hostDepthStencilAlpha
	vertexElements      map[uint32][]hostVertexElement
	rasterizers         map[uint32]uint32
	shaders             map[uint32]hostShader
	boundShaders        [6]uint32
	boundVertexElements uint32
	boundBlend          uint32
	boundDSA            uint32
	boundRasterizer     uint32
	colorSurface        uint32
	blendColor          [4]float32
	scissors            [16]hostScissor
	vertexBuffers       [16]hostVertexBuffer
	indexBuffer         uint32
	indexSize           uint32
	indexOffset         uint32
	constants           [2][]float32
	boundSamplerViews   [6][16]uint32
	boundSamplerStates  [6][16]uint32
}

func NewHostRenderer() (virtio.GPURenderer, error) {
	host, err := newDarwinHost()
	if err != nil {
		return nil, err
	}
	return NewRenderer(host), nil
}

func newDarwinHost() (*darwinHost, error) {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_LAZY); err != nil {
		return nil, fmt.Errorf("load AppKit for VirGL: %w", err)
	}
	gl, err := loadHostGL()
	if err != nil {
		return nil, err
	}
	host := &darwinHost{
		requests:  make(chan hostRequest),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		gl:        gl,
		programs:  make(map[string]hostProgram),
		contexts:  make(map[uint32]*hostContext),
		resources: make(map[uint32]*hostResource),
	}
	ready := make(chan error, 1)
	go host.contextLoop(ready)
	if err := <-ready; err != nil {
		<-host.done
		return nil, err
	}
	return host, nil
}

func (h *darwinHost) contextLoop(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(h.done)

	alloc := objc.RegisterName("alloc")
	initSelector := objc.RegisterName("init")
	release := objc.RegisterName("release")
	initWithAttributes := objc.RegisterName("initWithAttributes:")
	initWithFormat := objc.RegisterName("initWithFormat:shareContext:")
	makeCurrent := objc.RegisterName("makeCurrentContext")
	clearCurrent := objc.RegisterName("clearCurrentContext")

	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(alloc)
	pool = pool.Send(initSelector)
	if pool == 0 {
		ready <- errors.New("create VirGL autorelease pool")
		return
	}
	defer pool.Send(release)

	attributes := []uint32{
		nsOpenGLPFAAccelerated,
		nsOpenGLPFAColorSize, 24,
		nsOpenGLPFADepthSize, 24,
		nsOpenGLPFAStencilSize, 8,
		nsOpenGLPFAOpenGLProfile, nsOpenGLProfileVersion41Core,
		0,
	}
	format := objc.ID(objc.GetClass("NSOpenGLPixelFormat")).Send(alloc)
	format = format.Send(initWithAttributes, unsafe.Pointer(&attributes[0]))
	if format == 0 {
		ready <- errors.New("create accelerated VirGL pixel format")
		return
	}
	defer format.Send(release)

	context := objc.ID(objc.GetClass("NSOpenGLContext")).Send(alloc)
	context = context.Send(initWithFormat, format, objc.ID(0))
	if context == 0 {
		ready <- errors.New("create VirGL OpenGL 4.1 context")
		return
	}
	defer context.Send(release)
	context.Send(makeCurrent)

	h.gl.genVertexArrays(1, &h.vao)
	h.gl.bindVertexArray(h.vao)
	h.gl.genFramebuffers(1, &h.blitReadFBO)
	h.gl.genFramebuffers(1, &h.blitDrawFBO)
	h.gl.pixelStorei(glPackAlignment, 1)
	h.gl.pixelStorei(glUnpackAlignment, 1)
	ready <- nil
	defer func() {
		h.releaseGLObjects()
		objc.ID(objc.GetClass("NSOpenGLContext")).Send(clearCurrent)
	}()

	for {
		select {
		case request := <-h.requests:
			context.Send(makeCurrent)
			request.done <- request.run()
		case <-h.stop:
			return
		}
	}
}

func (h *darwinHost) dispatch(run func() error) error {
	request := hostRequest{
		done: make(chan error, 1),
		run:  run,
	}
	select {
	case h.requests <- request:
	case <-h.done:
		return errors.New("VirGL OpenGL context is closed")
	}
	select {
	case err := <-request.done:
		return err
	case <-h.done:
		return errors.New("VirGL OpenGL context closed during submission")
	}
}

func (h *darwinHost) createContext(id uint32) error {
	return h.dispatch(func() error {
		if h.contexts[id] != nil {
			return fmt.Errorf("Darwin VirGL context %d already exists", id)
		}
		h.contexts[id] = newHostContext()
		return nil
	})
}

func (h *darwinHost) destroyContext(id uint32) error {
	return h.dispatch(func() error {
		if h.contexts[id] == nil {
			return fmt.Errorf("unknown Darwin VirGL context %d", id)
		}
		delete(h.contexts, id)
		return nil
	})
}

func (h *darwinHost) createResource(description virtio.GPUResource3D) error {
	return h.dispatch(func() error {
		hostResource := &hostResource{description: description}
		switch description.Target {
		case 0:
			h.gl.genBuffers(1, &hostResource.buffer)
			h.gl.bindBuffer(glArrayBuffer, hostResource.buffer)
			h.gl.bufferData(glArrayBuffer, int(description.Width), 0, glStaticDraw)
		case 2:
			maxDimension := description.Width
			if description.Height > maxDimension {
				maxDimension = description.Height
			}
			maxLevel := uint32(0)
			for dimension := maxDimension; dimension > 1; dimension >>= 1 {
				maxLevel++
			}
			if description.LastLevel > maxLevel {
				return fmt.Errorf("VirGL resource %d last mip level %d exceeds %d for %dx%d texture",
					description.ID, description.LastLevel, maxLevel, description.Width, description.Height)
			}
			h.gl.genTextures(1, &hostResource.texture)
			h.gl.bindTexture(glTexture2D, hostResource.texture)
			h.gl.texParameteri(glTexture2D, glTextureMinFilter, glLinear)
			h.gl.texParameteri(glTexture2D, glTextureMagFilter, glLinear)
			h.gl.texParameteri(glTexture2D, glTextureBaseLevel, 0)
			h.gl.texParameteri(glTexture2D, glTextureMaxLevel, int32(description.LastLevel))
			allocateLevels := func(internalFormat int32, format, dataType uint32) {
				for level := uint32(0); level <= description.LastLevel; level++ {
					width, height := description.Width>>level, description.Height>>level
					if width == 0 {
						width = 1
					}
					if height == 0 {
						height = 1
					}
					h.gl.texImage2D(glTexture2D, int32(level), internalFormat, int32(width), int32(height), 0, format, dataType, 0)
				}
			}
			switch description.Format {
			case 16, 18, 21:
				hostResource.depth = true
				allocateLevels(glDepthComponent24, glDepthComponent, glUnsignedInt)
			case 19:
				hostResource.depth = true
				hostResource.stencil = true
				allocateLevels(glDepth24Stencil8, glDepthStencil, glUnsignedInt248)
			default:
				allocateLevels(glRGBA8, textureTransferFormat(description.Format), glUnsignedByte)
				h.gl.genFramebuffers(1, &hostResource.framebuffer)
				h.gl.bindFramebuffer(glFramebuffer, hostResource.framebuffer)
				h.gl.framebufferTexture(glFramebuffer, glColorAttachment0, glTexture2D, hostResource.texture, 0)
				if status := h.gl.checkFramebuffer(glFramebuffer); status != glFramebufferComplete {
					return fmt.Errorf("VirGL resource %d framebuffer status %#x", description.ID, status)
				}
			}
		default:
			return fmt.Errorf("VirGL resource %d target %d is not supported by the Darwin backend", description.ID, description.Target)
		}
		h.resources[description.ID] = hostResource
		return nil
	})
}

func (h *darwinHost) unrefResource(id uint32) error {
	return h.dispatch(func() error {
		resource := h.resources[id]
		if resource == nil {
			return fmt.Errorf("unknown Darwin VirGL resource %d", id)
		}
		h.deleteResource(resource)
		delete(h.resources, id)
		return nil
	})
}

func (h *darwinHost) transferToHost(resource *resource, transfer virtio.GPUTransfer3D) error {
	return h.dispatch(func() error {
		hostResource := h.resources[resource.description.ID]
		if hostResource == nil {
			return fmt.Errorf("unknown Darwin VirGL resource %d", resource.description.ID)
		}
		return h.uploadResource(hostResource, resource.description, resource.data, transfer)
	})
}

func (h *darwinHost) uploadResource(hostResource *hostResource, description virtio.GPUResource3D, data []byte, transfer virtio.GPUTransfer3D) error {
	switch {
	case hostResource.buffer != 0:
		box := transfer.Box
		if box.X > description.Width || box.Width > description.Width-box.X {
			return fmt.Errorf("buffer transfer range %d..%d exceeds resource size %d",
				box.X, uint64(box.X)+uint64(box.Width), description.Width)
		}
		end := transfer.Offset + uint64(box.Width)
		if end < transfer.Offset || end > uint64(len(data)) {
			return fmt.Errorf("buffer transfer backing range %d..%d exceeds %d bytes",
				transfer.Offset, end, len(data))
		}
		h.gl.bindBuffer(glArrayBuffer, hostResource.buffer)
		bytes := data[int(transfer.Offset):int(end)]
		h.gl.bufferSubData(glArrayBuffer, int(box.X), len(bytes), glPointer(bytes))
	case hostResource.texture != 0:
		box := transfer.Box
		if box.Z != 0 || box.Depth > 1 {
			return errors.New("Darwin VirGL supports only single-layer 2D texture transfers")
		}
		levelWidth, levelHeight := description.Width>>transfer.Level, description.Height>>transfer.Level
		if levelWidth == 0 {
			levelWidth = 1
		}
		if levelHeight == 0 {
			levelHeight = 1
		}
		if transfer.Level > description.LastLevel ||
			box.X > levelWidth || box.Width > levelWidth-box.X ||
			box.Y > levelHeight || box.Height > levelHeight-box.Y {
			return fmt.Errorf("texture transfer box exceeds mip level %d dimensions %dx%d",
				transfer.Level, levelWidth, levelHeight)
		}
		const bytesPerPixel = uint64(4)
		rowBytes := uint64(box.Width) * bytesPerPixel
		stride := uint64(transfer.Stride)
		if stride == 0 {
			stride = rowBytes
		}
		if stride < rowBytes {
			return fmt.Errorf("texture transfer stride %d is smaller than row size %d", stride, rowBytes)
		}
		required := rowBytes
		if box.Height > 1 {
			required += uint64(box.Height-1) * stride
		}
		end := transfer.Offset + required
		if end < transfer.Offset || end > uint64(len(data)) {
			return fmt.Errorf("texture transfer backing range %d..%d exceeds %d bytes",
				transfer.Offset, end, len(data))
		}
		bytes := data[int(transfer.Offset):int(end)]
		if stride != rowBytes {
			packed := make([]byte, int(rowBytes)*int(box.Height))
			for row := uint32(0); row < box.Height; row++ {
				sourceOffset := int(uint64(row) * stride)
				copy(packed[int(row)*int(rowBytes):int(row+1)*int(rowBytes)],
					bytes[sourceOffset:sourceOffset+int(rowBytes)])
			}
			bytes = packed
		}
		h.gl.bindTexture(glTexture2D, hostResource.texture)
		format, dataType := textureTransferFormat(description.Format), uint32(glUnsignedByte)
		if hostResource.depth {
			format, dataType = glDepthComponent, glUnsignedInt
		}
		if hostResource.stencil {
			format, dataType = glDepthStencil, glUnsignedInt248
		}
		h.gl.texSubImage2D(glTexture2D, int32(transfer.Level), int32(box.X), int32(box.Y),
			int32(box.Width), int32(box.Height), format, dataType, glPointer(bytes))
	}
	return nil
}

func (h *darwinHost) transferFromHost(_ *resource, _ virtio.GPUTransfer3D) error {
	return nil
}

func (h *darwinHost) execute(contextID uint32, commands []command, _ map[uint32]*resource) error {
	return h.dispatch(func() error {
		context := h.contexts[contextID]
		if context == nil {
			return fmt.Errorf("unknown Darwin VirGL context %d", contextID)
		}
		for _, command := range commands {
			if err := h.executeCommand(context, command); err != nil {
				return fmt.Errorf("VirGL opcode %d object %d: %w", command.Opcode, command.Object, err)
			}
		}
		return nil
	})
}

func (h *darwinHost) readScanout(resource *resource, rect image.Rectangle) ([]byte, int, error) {
	var result []byte
	err := h.dispatch(func() error {
		width := int(resource.description.Width)
		height := int(resource.description.Height)
		rect = rect.Intersect(image.Rect(0, 0, width, height))
		if rect.Empty() {
			return errors.New("VirGL scanout rectangle is empty")
		}
		hostResource := h.resources[resource.description.ID]
		if hostResource == nil || hostResource.framebuffer == 0 {
			return fmt.Errorf("VirGL resource %d is not renderable", resource.description.ID)
		}
		raw := make([]byte, rect.Dx()*rect.Dy()*4)
		h.gl.bindFramebuffer(glFramebuffer, hostResource.framebuffer)
		h.gl.finish()
		glY := height - rect.Max.Y
		h.gl.readPixels(int32(rect.Min.X), int32(glY), int32(rect.Dx()), int32(rect.Dy()), glBGRA, glUnsignedByte, glPointer(raw))
		result = make([]byte, len(raw))
		rowBytes := rect.Dx() * 4
		for y := 0; y < rect.Dy(); y++ {
			copy(result[y*rowBytes:(y+1)*rowBytes], raw[(rect.Dy()-1-y)*rowBytes:(rect.Dy()-y)*rowBytes])
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return result, rect.Dx() * 4, nil
}

func (h *darwinHost) reset() error {
	return h.dispatch(func() error {
		for _, resource := range h.resources {
			h.deleteResource(resource)
		}
		h.resources = make(map[uint32]*hostResource)
		h.contexts = make(map[uint32]*hostContext)
		return nil
	})
}

func newHostContext() *hostContext {
	return &hostContext{
		blendStates:       make(map[uint32]hostBlendState),
		surfaces:          make(map[uint32]hostSurface),
		samplerViews:      make(map[uint32]hostSamplerView),
		samplerStates:     make(map[uint32]hostSamplerState),
		depthStencilAlpha: make(map[uint32]hostDepthStencilAlpha),
		vertexElements:    make(map[uint32][]hostVertexElement),
		rasterizers:       make(map[uint32]uint32),
		shaders:           make(map[uint32]hostShader),
	}
}

func (h *darwinHost) executeCommand(context *hostContext, command command) error {
	payload := command.Payload
	switch command.Opcode {
	case 1: // VIRGL_CCMD_CREATE_OBJECT
		if len(payload) == 0 {
			return errors.New("missing object handle")
		}
		handle := payload[0]
		switch command.Object {
		case 1: // VIRGL_OBJECT_BLEND
			if len(payload) != 11 {
				return errors.New("invalid blend payload")
			}
			state := hostBlendState{state: payload[1]}
			copy(state.renderTargets[:], payload[3:])
			context.blendStates[handle] = state
		case 2: // VIRGL_OBJECT_RASTERIZER
			if len(payload) < 2 {
				return errors.New("truncated rasterizer")
			}
			context.rasterizers[handle] = payload[1]
		case 3: // VIRGL_OBJECT_DSA
			if len(payload) != 5 {
				return errors.New("invalid depth/stencil/alpha payload")
			}
			context.depthStencilAlpha[handle] = hostDepthStencilAlpha{state: payload[1]}
		case 4: // VIRGL_OBJECT_SHADER
			if len(payload) < 5 {
				return errors.New("truncated shader")
			}
			stage, source, err := translateTGSI(shaderText(payload))
			if err != nil {
				return err
			}
			if stage != payload[1] {
				return fmt.Errorf("shader payload type %d disagrees with TGSI stage %d", payload[1], stage)
			}
			context.shaders[handle] = hostShader{stage: stage, source: source}
		case 5: // VIRGL_OBJECT_VERTEX_ELEMENTS
			if (len(payload)-1)%4 != 0 {
				return errors.New("invalid vertex element payload")
			}
			elements := make([]hostVertexElement, 0, (len(payload)-1)/4)
			for index := 1; index < len(payload); index += 4 {
				elements = append(elements, hostVertexElement{
					offset:      payload[index],
					bufferIndex: payload[index+2],
					format:      payload[index+3],
				})
			}
			context.vertexElements[handle] = elements
		case 6: // VIRGL_OBJECT_SAMPLER_VIEW
			if len(payload) != 6 {
				return errors.New("invalid sampler view payload")
			}
			resource := h.resources[payload[1]]
			if resource == nil {
				return fmt.Errorf("sampler view refers to unknown resource %d", payload[1])
			}
			firstLevel, lastLevel := payload[4]&0xff, (payload[4]>>8)&0xff
			if firstLevel > lastLevel || lastLevel > resource.description.LastLevel {
				return fmt.Errorf("sampler view mip range %d..%d exceeds resource last level %d",
					firstLevel, lastLevel, resource.description.LastLevel)
			}
			context.samplerViews[handle] = hostSamplerView{
				resourceID: payload[1],
				format:     payload[2] & 0x00ffffff,
				firstLevel: firstLevel,
				lastLevel:  lastLevel,
				swizzle: [4]uint32{
					payload[5] & 7,
					(payload[5] >> 3) & 7,
					(payload[5] >> 6) & 7,
					(payload[5] >> 9) & 7,
				},
			}
		case 7: // VIRGL_OBJECT_SAMPLER_STATE
			if len(payload) != 9 {
				return errors.New("invalid sampler state payload")
			}
			state := hostSamplerState{
				state:   payload[1],
				lodBias: math.Float32frombits(payload[2]),
				minLOD:  math.Float32frombits(payload[3]),
				maxLOD:  math.Float32frombits(payload[4]),
			}
			for index := range state.borderColor {
				state.borderColor[index] = math.Float32frombits(payload[5+index])
			}
			context.samplerStates[handle] = state
		case 8: // VIRGL_OBJECT_SURFACE
			if len(payload) < 2 {
				return errors.New("surface has no resource")
			}
			if h.resources[payload[1]] == nil {
				return fmt.Errorf("surface refers to unknown resource %d", payload[1])
			}
			context.surfaces[handle] = hostSurface{resourceID: payload[1]}
		default:
			return fmt.Errorf("unsupported object type %d", command.Object)
		}
	case 2: // VIRGL_CCMD_BIND_OBJECT
		if len(payload) != 1 {
			return errors.New("invalid bind payload")
		}
		switch command.Object {
		case 1:
			context.boundBlend = payload[0]
			if payload[0] == 0 {
				h.applyDefaultBlend()
				break
			}
			state, ok := context.blendStates[payload[0]]
			if !ok {
				return fmt.Errorf("unknown blend state %d", payload[0])
			}
			if err := h.applyBlend(state); err != nil {
				return err
			}
		case 2:
			context.boundRasterizer = payload[0]
			if payload[0] == 0 {
				h.gl.disable(glCullFace)
				h.gl.disable(glScissorTest)
				break
			}
			state, ok := context.rasterizers[payload[0]]
			if !ok {
				return fmt.Errorf("unknown rasterizer %d", payload[0])
			}
			h.applyRasterizer(context, state)
		case 5:
			if payload[0] == 0 {
				context.boundVertexElements = 0
				break
			}
			if context.vertexElements[payload[0]] == nil {
				return fmt.Errorf("unknown vertex elements %d", payload[0])
			}
			context.boundVertexElements = payload[0]
		case 3:
			context.boundDSA = payload[0]
			if payload[0] == 0 {
				h.gl.disable(glDepthTest)
				h.gl.depthMask(true)
				break
			}
			state, ok := context.depthStencilAlpha[payload[0]]
			if !ok {
				return fmt.Errorf("unknown depth/stencil/alpha state %d", payload[0])
			}
			h.applyDepthStencilAlpha(state)
		case 4, 6, 7, 8:
			// Other fixed-function state is represented by core GL defaults
			// for this first accelerated path.
		}
	case 3: // VIRGL_CCMD_DESTROY_OBJECT
		if len(payload) != 1 {
			return errors.New("invalid destroy payload")
		}
		delete(context.vertexElements, payload[0])
		delete(context.blendStates, payload[0])
		delete(context.rasterizers, payload[0])
		delete(context.shaders, payload[0])
		delete(context.surfaces, payload[0])
		delete(context.samplerViews, payload[0])
		delete(context.samplerStates, payload[0])
		delete(context.depthStencilAlpha, payload[0])
	case 4: // VIRGL_CCMD_SET_VIEWPORT_STATE
		if len(payload) < 7 {
			return errors.New("truncated viewport")
		}
		scaleX := float64(math.Float32frombits(payload[1]))
		scaleY := float64(math.Float32frombits(payload[2]))
		translateX := float64(math.Float32frombits(payload[4]))
		translateY := float64(math.Float32frombits(payload[5]))
		minX := math.Min(translateX-scaleX, translateX+scaleX)
		minY := math.Min(translateY-scaleY, translateY+scaleY)
		h.gl.viewport(int32(math.Round(minX)), int32(math.Round(minY)),
			int32(math.Round(math.Abs(scaleX*2))), int32(math.Round(math.Abs(scaleY*2))))
	case 5: // VIRGL_CCMD_SET_FRAMEBUFFER_STATE
		if len(payload) < 2 {
			return errors.New("truncated framebuffer state")
		}
		if payload[0] == 0 {
			context.colorSurface = 0
			h.gl.bindFramebuffer(glFramebuffer, 0)
			break
		}
		if len(payload) < 3 {
			return errors.New("framebuffer has no color surface")
		}
		surface := context.surfaces[payload[2]]
		resource := h.resources[surface.resourceID]
		if resource == nil || resource.framebuffer == 0 {
			return fmt.Errorf("unknown color surface %d", payload[2])
		}
		context.colorSurface = payload[2]
		h.gl.bindFramebuffer(glFramebuffer, resource.framebuffer)
		h.gl.framebufferTexture(glFramebuffer, glDepthAttachment, glTexture2D, 0, 0)
		h.gl.framebufferTexture(glFramebuffer, glDepthStencilAttachment, glTexture2D, 0, 0)
		if payload[1] != 0 {
			depthSurface, ok := context.surfaces[payload[1]]
			if !ok {
				return fmt.Errorf("unknown depth surface %d", payload[1])
			}
			depthResource := h.resources[depthSurface.resourceID]
			if depthResource == nil || !depthResource.depth {
				return fmt.Errorf("surface %d is not a depth resource", payload[1])
			}
			attachment := uint32(glDepthAttachment)
			if depthResource.stencil {
				attachment = glDepthStencilAttachment
			}
			h.gl.framebufferTexture(glFramebuffer, attachment, glTexture2D, depthResource.texture, 0)
		}
		if status := h.gl.checkFramebuffer(glFramebuffer); status != glFramebufferComplete {
			return fmt.Errorf("VirGL framebuffer status %#x", status)
		}
	case 6: // VIRGL_CCMD_SET_VERTEX_BUFFERS
		if len(payload) < 3 || len(payload)%3 != 0 {
			return errors.New("invalid vertex buffer state")
		}
		for index := 0; index < len(payload)/3; index++ {
			context.vertexBuffers[index] = hostVertexBuffer{
				stride:     payload[index*3],
				offset:     payload[index*3+1],
				resourceID: payload[index*3+2],
			}
		}
	case 7: // VIRGL_CCMD_CLEAR
		if len(payload) < 5 {
			return errors.New("truncated clear")
		}
		if err := h.bindContextFramebuffer(context); err != nil {
			return err
		}
		h.gl.clearColor(
			math.Float32frombits(payload[1]),
			math.Float32frombits(payload[2]),
			math.Float32frombits(payload[3]),
			math.Float32frombits(payload[4]),
		)
		var mask uint32
		if payload[0]&0x3fc != 0 {
			mask |= glColorBufferBit
			// Gallium full clears ignore blend color masks.
			h.gl.colorMask(true, true, true, true)
		}
		if payload[0]&0x1 != 0 {
			mask |= glDepthBufferBit
			// Gallium's full clear operation ignores the currently bound
			// depth write mask. OpenGL's glClear does not.
			h.gl.depthMask(true)
			if len(payload) >= 7 {
				h.gl.clearDepth(math.Float64frombits(uint64(payload[5]) | uint64(payload[6])<<32))
			}
		}
		if payload[0]&0x2 != 0 {
			mask |= glStencilBufferBit
			if len(payload) >= 8 {
				h.gl.clearStencil(int32(payload[7]))
			}
		}
		// Gallium full clears are not restricted by rasterizer scissoring.
		scissorEnabled := context.boundRasterizer != 0 &&
			context.rasterizers[context.boundRasterizer]&(1<<14) != 0
		if scissorEnabled {
			h.gl.disable(glScissorTest)
		}
		h.gl.clear(mask)
		if mask&glDepthBufferBit != 0 {
			h.restoreDepthWriteMask(context)
		}
		if mask&glColorBufferBit != 0 {
			h.restoreColorMask(context)
		}
		if scissorEnabled {
			h.gl.enable(glScissorTest)
		}
	case 8: // VIRGL_CCMD_DRAW_VBO
		if len(payload) < 12 {
			return errors.New("truncated draw")
		}
		return h.draw(context, payload)
	case 9: // VIRGL_CCMD_RESOURCE_INLINE_WRITE
		if len(payload) < 11 {
			return errors.New("truncated inline resource write")
		}
		resource := h.resources[payload[0]]
		if resource == nil {
			return fmt.Errorf("inline write refers to unknown resource %d", payload[0])
		}
		data := make([]byte, 0, (len(payload)-11)*4)
		for _, word := range payload[11:] {
			data = append(data, byte(word), byte(word>>8), byte(word>>16), byte(word>>24))
		}
		return h.uploadResource(resource, resource.description, data, virtio.GPUTransfer3D{
			ResourceID:  payload[0],
			Level:       payload[1],
			Stride:      payload[3],
			LayerStride: payload[4],
			Box: virtio.GPUBox{
				X: payload[5], Y: payload[6], Z: payload[7],
				Width: payload[8], Height: payload[9], Depth: payload[10],
			},
		})
	case 10: // VIRGL_CCMD_SET_SAMPLER_VIEWS
		if len(payload) < 2 || payload[0] >= 6 || payload[1] >= 16 || len(payload)-2 > 16-int(payload[1]) {
			return errors.New("invalid sampler view binding")
		}
		stage, start := payload[0], payload[1]
		for slot := int(start); slot < len(context.boundSamplerViews[stage]); slot++ {
			context.boundSamplerViews[stage][slot] = 0
		}
		for index, handle := range payload[2:] {
			if handle != 0 {
				if _, ok := context.samplerViews[handle]; !ok {
					return fmt.Errorf("unknown sampler view %d", handle)
				}
			}
			context.boundSamplerViews[stage][int(start)+index] = handle
		}
	case 11: // VIRGL_CCMD_SET_INDEX_BUFFER
		if len(payload) != 1 && len(payload) != 3 {
			return errors.New("invalid index buffer state")
		}
		context.indexBuffer = payload[0]
		context.indexSize = 0
		context.indexOffset = 0
		if payload[0] != 0 {
			if h.resources[payload[0]] == nil {
				return fmt.Errorf("unknown index buffer %d", payload[0])
			}
			context.indexSize = payload[1]
			context.indexOffset = payload[2]
		}
	case 12: // VIRGL_CCMD_SET_CONSTANT_BUFFER
		if len(payload) < 2 {
			return errors.New("truncated constant buffer")
		}
		stage := payload[0]
		if stage > tgsiFragment {
			return fmt.Errorf("constant buffer stage %d is unsupported", stage)
		}
		context.constants[stage] = make([]float32, len(payload)-2)
		for index, bits := range payload[2:] {
			context.constants[stage][index] = math.Float32frombits(bits)
		}
	case 14: // VIRGL_CCMD_SET_BLEND_COLOR
		if len(payload) != 4 {
			return errors.New("invalid blend color")
		}
		for index, bits := range payload {
			context.blendColor[index] = math.Float32frombits(bits)
		}
		h.gl.blendColor(context.blendColor[0], context.blendColor[1], context.blendColor[2], context.blendColor[3])
	case 15: // VIRGL_CCMD_SET_SCISSOR_STATE
		if len(payload) < 1 || (len(payload)-1)%2 != 0 {
			return errors.New("invalid scissor state")
		}
		start, count := payload[0], (len(payload)-1)/2
		if start >= uint32(len(context.scissors)) || int(start)+count > len(context.scissors) {
			return errors.New("scissor slots are out of range")
		}
		for index := 0; index < count; index++ {
			minXY, maxXY := payload[1+index*2], payload[2+index*2]
			context.scissors[int(start)+index] = hostScissor{
				minX: minXY & 0xffff,
				minY: minXY >> 16,
				maxX: maxXY & 0xffff,
				maxY: maxXY >> 16,
			}
		}
		if start == 0 && context.boundRasterizer != 0 &&
			context.rasterizers[context.boundRasterizer]&(1<<14) != 0 {
			h.applyScissor(context.scissors[0])
		}
	case 16: // VIRGL_CCMD_BLIT
		return h.blit(context, payload)
	case 17: // VIRGL_CCMD_RESOURCE_COPY_REGION
		return h.copyResourceRegion(context, payload)
	case 28: // VIRGL_CCMD_SET_SUB_CTX
	case 29: // VIRGL_CCMD_CREATE_SUB_CTX
	case 30: // VIRGL_CCMD_DESTROY_SUB_CTX
	case 31: // VIRGL_CCMD_BIND_SHADER
		if len(payload) != 2 || payload[1] >= uint32(len(context.boundShaders)) {
			return errors.New("invalid shader binding")
		}
		if payload[0] == 0 {
			context.boundShaders[payload[1]] = 0
			break
		}
		if payload[1] > tgsiFragment {
			return fmt.Errorf("shader stage %d is unsupported", payload[1])
		}
		shader := context.shaders[payload[0]]
		if shader.stage != payload[1] {
			return fmt.Errorf("shader %d is not stage %d", payload[0], payload[1])
		}
		context.boundShaders[payload[1]] = payload[0]
	case 18: // VIRGL_CCMD_BIND_SAMPLER_STATES
		if len(payload) < 2 || payload[0] >= 6 || payload[1] >= 16 || len(payload)-2 > 16-int(payload[1]) {
			return errors.New("invalid sampler state binding")
		}
		stage, start := payload[0], payload[1]
		for slot := int(start); slot < len(context.boundSamplerStates[stage]); slot++ {
			context.boundSamplerStates[stage][slot] = 0
		}
		for index, handle := range payload[2:] {
			if handle != 0 {
				if _, ok := context.samplerStates[handle]; !ok {
					return fmt.Errorf("unknown sampler state %d", handle)
				}
			}
			context.boundSamplerStates[stage][int(start)+index] = handle
		}
	case 13, 22, 23, 24, 32:
		// Empty index state, stencil reference, clip/sample state, and
		// tessellation state do not require additional GL calls yet.
	default:
		return fmt.Errorf("command is not implemented")
	}
	return nil
}

func shaderText(payload []uint32) string {
	bytes := make([]byte, 0, (len(payload)-5)*4)
	for _, word := range payload[5:] {
		bytes = append(bytes, byte(word), byte(word>>8), byte(word>>16), byte(word>>24))
	}
	return strings.TrimRight(string(bytes), "\x00")
}

func (h *darwinHost) draw(context *hostContext, payload []uint32) error {
	start, count, mode, indexed := payload[0], payload[1], payload[2], payload[3] != 0
	surface := context.surfaces[context.colorSurface]
	target := h.resources[surface.resourceID]
	if target == nil || target.framebuffer == 0 {
		return errors.New("draw has no color framebuffer")
	}
	elements := context.vertexElements[context.boundVertexElements]
	if len(elements) == 0 {
		return errors.New("draw has no vertex elements")
	}
	h.gl.bindFramebuffer(glFramebuffer, target.framebuffer)
	h.gl.bindVertexArray(h.vao)
	for index, element := range elements {
		if element.bufferIndex >= uint32(len(context.vertexBuffers)) {
			return fmt.Errorf("vertex element %d uses invalid buffer index %d", index, element.bufferIndex)
		}
		binding := context.vertexBuffers[element.bufferIndex]
		buffer := h.resources[binding.resourceID]
		if buffer == nil || buffer.buffer == 0 {
			return fmt.Errorf("vertex element %d refers to unknown buffer %d", index, binding.resourceID)
		}
		components, dataType, normalized, ok := vertexFormat(element.format)
		if !ok {
			return fmt.Errorf("vertex element %d uses unsupported format %d", index, element.format)
		}
		h.gl.bindBuffer(glArrayBuffer, buffer.buffer)
		h.gl.vertexAttribPtr(uint32(index), components, dataType, normalized, int32(binding.stride),
			uintptr(binding.offset+element.offset))
		h.gl.enableVertexAttrib(uint32(index))
	}
	for index := len(elements); index < h.enabledVertexAttributes; index++ {
		h.gl.disableVertexAttrib(uint32(index))
	}
	h.enabledVertexAttributes = len(elements)

	program, err := h.programFor(context)
	if err != nil {
		return err
	}
	h.gl.useProgram(program.id)
	vertexConstants := context.constants[tgsiVertex]
	if program.constants >= 0 && len(vertexConstants) != 0 {
		h.gl.uniform4fv(program.constants, int32(len(vertexConstants)/4), &vertexConstants[0])
	}
	for slot, viewHandle := range context.boundSamplerViews[tgsiFragment] {
		if viewHandle == 0 {
			continue
		}
		view := context.samplerViews[viewHandle]
		texture := h.resources[view.resourceID]
		if texture == nil || texture.texture == 0 {
			return fmt.Errorf("sampler view %d has no texture", viewHandle)
		}
		h.gl.activeTexture(glTexture0 + uint32(slot))
		h.gl.bindTexture(glTexture2D, texture.texture)
		if err := h.applySamplerView(view); err != nil {
			return fmt.Errorf("sampler view %d: %w", viewHandle, err)
		}
		if stateHandle := context.boundSamplerStates[tgsiFragment][slot]; stateHandle != 0 {
			h.applySamplerState(context.samplerStates[stateHandle])
		}
		location := uniformLocation(h.gl, program.id, fmt.Sprintf("sampler%d", slot))
		if location >= 0 {
			h.gl.uniform1i(location, int32(slot))
		}
	}

	var glMode uint32
	switch mode {
	case 4:
		glMode = glTriangles
	case 5:
		glMode = glTriangleStrip
	default:
		return fmt.Errorf("unsupported primitive mode %d", mode)
	}
	if indexed {
		buffer := h.resources[context.indexBuffer]
		if buffer == nil || buffer.buffer == 0 {
			return fmt.Errorf("draw refers to unknown index buffer %d", context.indexBuffer)
		}
		var indexType uint32
		switch context.indexSize {
		case 1:
			indexType = glUnsignedByte
		case 2:
			indexType = glUnsignedShort
		case 4:
			indexType = glUnsignedInt
		default:
			return fmt.Errorf("unsupported index size %d", context.indexSize)
		}
		h.gl.bindBuffer(glElementArrayBuffer, buffer.buffer)
		primitiveRestart := payload[7] != 0
		if primitiveRestart {
			h.gl.enable(glPrimitiveRestart)
			h.gl.primitiveRestartIndex(payload[8])
		}
		offset := uintptr(context.indexOffset + start*context.indexSize)
		indexBias := int32(0)
		if len(payload) > 5 {
			indexBias = int32(payload[5])
		}
		if indexBias != 0 {
			h.gl.drawElementsBaseVertex(glMode, int32(count), indexType, offset, indexBias)
		} else {
			h.gl.drawElements(glMode, int32(count), indexType, offset)
		}
		if primitiveRestart {
			h.gl.disable(glPrimitiveRestart)
		}
	} else {
		h.gl.drawArrays(glMode, int32(start), int32(count))
	}
	return nil
}

func (h *darwinHost) bindContextFramebuffer(context *hostContext) error {
	if context.colorSurface == 0 {
		h.gl.bindFramebuffer(glFramebuffer, 0)
		return nil
	}
	surface, ok := context.surfaces[context.colorSurface]
	if !ok {
		return fmt.Errorf("unknown bound color surface %d", context.colorSurface)
	}
	target := h.resources[surface.resourceID]
	if target == nil || target.framebuffer == 0 {
		return fmt.Errorf("bound color surface %d has no framebuffer", context.colorSurface)
	}
	h.gl.bindFramebuffer(glFramebuffer, target.framebuffer)
	return nil
}

func (h *darwinHost) restoreDepthWriteMask(context *hostContext) {
	if context.boundDSA == 0 {
		h.gl.depthMask(true)
		return
	}
	h.gl.depthMask(context.depthStencilAlpha[context.boundDSA].state&(1<<1) != 0)
}

func (h *darwinHost) programFor(context *hostContext) (hostProgram, error) {
	vertex := context.shaders[context.boundShaders[tgsiVertex]]
	fragment := context.shaders[context.boundShaders[tgsiFragment]]
	if vertex.source == "" || fragment.source == "" {
		return hostProgram{}, errors.New("draw has incomplete shader state")
	}
	key := vertex.source + "\x00" + fragment.source
	if program, ok := h.programs[key]; ok {
		return program, nil
	}
	id, err := h.gl.compileProgram(vertex.source, fragment.source)
	if err != nil {
		return hostProgram{}, err
	}
	program := hostProgram{
		id:        id,
		constants: uniformLocation(h.gl, id, "uConstants[0]"),
	}
	h.programs[key] = program
	return program, nil
}

func (h *darwinHost) applyRasterizer(context *hostContext, state uint32) {
	switch (state >> 8) & 0x3 {
	case 0:
		h.gl.disable(glCullFace)
	case 1:
		h.gl.enable(glCullFace)
		h.gl.cullFace(glFront)
	case 2:
		h.gl.enable(glCullFace)
		h.gl.cullFace(glBack)
	case 3:
		h.gl.enable(glCullFace)
		h.gl.cullFace(glFrontAndBack)
	}
	// Gallium's winding reaches the host framebuffer unchanged. Scanout
	// orientation is handled when the resource is presented, after rasterizing.
	if state&(1<<15) != 0 {
		h.gl.frontFace(glCCW)
	} else {
		h.gl.frontFace(glCW)
	}
	if state&(1<<14) != 0 {
		h.gl.enable(glScissorTest)
		h.applyScissor(context.scissors[0])
	} else {
		h.gl.disable(glScissorTest)
	}
}

func (h *darwinHost) applyDepthStencilAlpha(state hostDepthStencilAlpha) {
	if state.state&1 == 0 {
		h.gl.disable(glDepthTest)
	} else {
		h.gl.enable(glDepthTest)
		functions := [...]uint32{glNever, glLess, glEqual, glLEqual, glGreater, glNotEqual, glGEqual, glAlways}
		h.gl.depthFunc(functions[(state.state>>2)&7])
	}
	h.gl.depthMask(state.state&(1<<1) != 0)
}

func (h *darwinHost) applySamplerState(state hostSamplerState) {
	wrap := func(value uint32) int32 {
		switch value {
		case 0:
			return glRepeat
		case 3:
			return glClampToBorder
		case 4:
			return glMirroredRepeat
		default:
			return glClampToEdge
		}
	}
	h.gl.texParameteri(glTexture2D, glTextureWrapS, wrap(state.state&7))
	h.gl.texParameteri(glTexture2D, glTextureWrapT, wrap((state.state>>3)&7))
	linearMin := state.state&(1<<9) != 0
	var minFilter int32
	switch (state.state >> 11) & 3 {
	case 0:
		if linearMin {
			minFilter = glLinearMipmapNearest
		} else {
			minFilter = glNearestMipmapNearest
		}
	case 1:
		if linearMin {
			minFilter = glLinearMipmapLinear
		} else {
			minFilter = glNearestMipmapLinear
		}
	default:
		if linearMin {
			minFilter = glLinear
		} else {
			minFilter = glNearest
		}
	}
	magFilter := int32(glNearest)
	if state.state&(1<<13) != 0 {
		magFilter = glLinear
	}
	h.gl.texParameteri(glTexture2D, glTextureMinFilter, minFilter)
	h.gl.texParameteri(glTexture2D, glTextureMagFilter, magFilter)
	h.gl.texParameterf(glTexture2D, glTextureLODBias, state.lodBias)
	h.gl.texParameterf(glTexture2D, glTextureMinLOD, state.minLOD)
	h.gl.texParameterf(glTexture2D, glTextureMaxLOD, state.maxLOD)
	h.gl.texParameterfv(glTexture2D, glTextureBorderColor, &state.borderColor[0])
	if state.state&(1<<15) != 0 {
		h.gl.texParameteri(glTexture2D, glTextureCompareMode, glCompareRefToTexture)
		functions := [...]int32{glNever, glLess, glEqual, glLEqual, glGreater, glNotEqual, glGEqual, glAlways}
		h.gl.texParameteri(glTexture2D, glTextureCompareFunc, functions[(state.state>>16)&7])
	} else {
		h.gl.texParameteri(glTexture2D, glTextureCompareMode, glNone)
	}
}

func (h *darwinHost) applySamplerView(view hostSamplerView) error {
	h.gl.texParameteri(glTexture2D, glTextureBaseLevel, int32(view.firstLevel))
	h.gl.texParameteri(glTexture2D, glTextureMaxLevel, int32(view.lastLevel))
	swizzles := [...]int32{glRed, glGreen, glBlue, glAlpha, glZero, glOne}
	parameters := [...]uint32{glTextureSwizzleR, glTextureSwizzleG, glTextureSwizzleB, glTextureSwizzleA}
	for index, value := range view.swizzle {
		if value >= uint32(len(swizzles)) {
			return fmt.Errorf("invalid component swizzle %d", value)
		}
		h.gl.texParameteri(glTexture2D, parameters[index], swizzles[value])
	}
	return nil
}

func (h *darwinHost) applyDefaultBlend() {
	h.gl.disable(glBlend)
	h.gl.colorMask(true, true, true, true)
}

func (h *darwinHost) applyBlend(state hostBlendState) error {
	target := state.renderTargets[0]
	mask := (target >> 27) & 0xf
	h.gl.colorMask(mask&1 != 0, mask&2 != 0, mask&4 != 0, mask&8 != 0)
	if target&1 == 0 {
		h.gl.disable(glBlend)
		return nil
	}
	factor := func(value uint32) (uint32, bool) {
		switch value {
		case 1:
			return glOne, true
		case 2:
			return glSrcColor, true
		case 3:
			return glSrcAlpha, true
		case 4:
			return glDstAlpha, true
		case 5:
			return glDstColor, true
		case 6:
			return glSrcAlphaSaturate, true
		case 7:
			return glConstantColor, true
		case 8:
			return glConstantAlpha, true
		case 9:
			return glSrc1Color, true
		case 10:
			return glSrc1Alpha, true
		case 0x11:
			return glZero, true
		case 0x12:
			return glOneMinusSrcColor, true
		case 0x13:
			return glOneMinusSrcAlpha, true
		case 0x14:
			return glOneMinusDstAlpha, true
		case 0x15:
			return glOneMinusDstColor, true
		case 0x17:
			return glOneMinusConstantColor, true
		case 0x18:
			return glOneMinusConstantAlpha, true
		case 0x19:
			return glOneMinusSrc1Color, true
		case 0x1a:
			return glOneMinusSrc1Alpha, true
		default:
			return 0, false
		}
	}
	equation := func(value uint32) (uint32, bool) {
		equations := [...]uint32{glFuncAdd, glFuncSubtract, glFuncReverseSubtract, glMin, glMax}
		if value >= uint32(len(equations)) {
			return 0, false
		}
		return equations[value], true
	}
	rgbSource, ok := factor((target >> 4) & 0x1f)
	if !ok {
		return fmt.Errorf("unsupported RGB source blend factor %d", (target>>4)&0x1f)
	}
	rgbDestination, ok := factor((target >> 9) & 0x1f)
	if !ok {
		return fmt.Errorf("unsupported RGB destination blend factor %d", (target>>9)&0x1f)
	}
	alphaSource, ok := factor((target >> 17) & 0x1f)
	if !ok {
		return fmt.Errorf("unsupported alpha source blend factor %d", (target>>17)&0x1f)
	}
	alphaDestination, ok := factor((target >> 22) & 0x1f)
	if !ok {
		return fmt.Errorf("unsupported alpha destination blend factor %d", (target>>22)&0x1f)
	}
	rgbEquation, ok := equation((target >> 1) & 7)
	if !ok {
		return fmt.Errorf("unsupported RGB blend equation %d", (target>>1)&7)
	}
	alphaEquation, ok := equation((target >> 14) & 7)
	if !ok {
		return fmt.Errorf("unsupported alpha blend equation %d", (target>>14)&7)
	}
	h.gl.blendFuncSeparate(rgbSource, rgbDestination, alphaSource, alphaDestination)
	h.gl.blendEquationSeparate(rgbEquation, alphaEquation)
	h.gl.enable(glBlend)
	return nil
}

func (h *darwinHost) restoreColorMask(context *hostContext) {
	if context.boundBlend == 0 {
		h.gl.colorMask(true, true, true, true)
		return
	}
	target := context.blendStates[context.boundBlend].renderTargets[0]
	mask := (target >> 27) & 0xf
	h.gl.colorMask(mask&1 != 0, mask&2 != 0, mask&4 != 0, mask&8 != 0)
}

func (h *darwinHost) applyScissor(scissor hostScissor) {
	width, height := int32(scissor.maxX-scissor.minX), int32(scissor.maxY-scissor.minY)
	if scissor.maxX < scissor.minX {
		width = 0
	}
	if scissor.maxY < scissor.minY {
		height = 0
	}
	h.gl.scissor(int32(scissor.minX), int32(scissor.minY), width, height)
}

func (h *darwinHost) blit(context *hostContext, payload []uint32) error {
	if len(payload) != 21 {
		return errors.New("invalid blit payload")
	}
	dst, src := h.resources[payload[3]], h.resources[payload[12]]
	if dst == nil || dst.texture == 0 || src == nil || src.texture == 0 {
		return errors.New("blit requires texture resources")
	}
	dstLevel, srcLevel := payload[4], payload[13]
	if dstLevel > dst.description.LastLevel || srcLevel > src.description.LastLevel {
		return fmt.Errorf("blit mip levels source %d/%d destination %d/%d are out of range",
			srcLevel, src.description.LastLevel, dstLevel, dst.description.LastLevel)
	}
	h.gl.bindFramebuffer(glReadFramebuffer, h.blitReadFBO)
	h.gl.framebufferTexture(glReadFramebuffer, glColorAttachment0, glTexture2D, src.texture, int32(srcLevel))
	if status := h.gl.checkFramebuffer(glReadFramebuffer); status != glFramebufferComplete {
		return fmt.Errorf("VirGL blit source framebuffer status %#x", status)
	}
	h.gl.bindFramebuffer(glDrawFramebuffer, h.blitDrawFBO)
	h.gl.framebufferTexture(glDrawFramebuffer, glColorAttachment0, glTexture2D, dst.texture, int32(dstLevel))
	if status := h.gl.checkFramebuffer(glDrawFramebuffer); status != glFramebufferComplete {
		return fmt.Errorf("VirGL blit destination framebuffer status %#x", status)
	}
	mask := uint32(0)
	if payload[0]&0xf != 0 {
		mask |= glColorBufferBit
	}
	if payload[0]&0x10 != 0 {
		mask |= glDepthBufferBit
	}
	if payload[0]&0x20 != 0 {
		mask |= glStencilBufferBit
	}
	filter := uint32(glNearest)
	if (payload[0]>>8)&3 != 0 {
		filter = glLinear
	}
	h.gl.blitFramebuffer(
		int32(payload[15]), int32(payload[16]), int32(payload[15]+payload[18]), int32(payload[16]+payload[19]),
		int32(payload[6]), int32(payload[7]), int32(payload[6]+payload[9]), int32(payload[7]+payload[10]),
		mask, filter,
	)
	return h.bindContextFramebuffer(context)
}

func (h *darwinHost) copyResourceRegion(context *hostContext, payload []uint32) error {
	if len(payload) != 13 {
		return errors.New("invalid resource copy region payload")
	}
	dst, src := h.resources[payload[0]], h.resources[payload[5]]
	if dst == nil || src == nil {
		return errors.New("resource copy region refers to an unknown resource")
	}
	dstLevel, srcLevel := payload[1], payload[6]
	if dstLevel > dst.description.LastLevel || srcLevel > src.description.LastLevel {
		return fmt.Errorf("resource copy mip levels source %d/%d destination %d/%d are out of range",
			srcLevel, src.description.LastLevel, dstLevel, dst.description.LastLevel)
	}
	dstX, dstY, dstZ := payload[2], payload[3], payload[4]
	srcX, srcY, srcZ := payload[7], payload[8], payload[9]
	width, height, depth := payload[10], payload[11], payload[12]

	if src.buffer != 0 || dst.buffer != 0 {
		if src.buffer == 0 || dst.buffer == 0 || srcLevel != 0 || dstLevel != 0 ||
			srcY != 0 || srcZ != 0 || dstY != 0 || dstZ != 0 || height != 1 || depth != 1 {
			return errors.New("buffer copy region must describe a one-dimensional buffer range")
		}
		if uint64(srcX)+uint64(width) > uint64(src.description.Width) ||
			uint64(dstX)+uint64(width) > uint64(dst.description.Width) {
			return errors.New("buffer copy region is out of bounds")
		}
		bytes := make([]byte, int(width))
		if len(bytes) == 0 {
			return nil
		}
		h.gl.bindBuffer(glArrayBuffer, src.buffer)
		h.gl.getBufferSubData(glArrayBuffer, int(srcX), len(bytes), glPointer(bytes))
		h.gl.bindBuffer(glArrayBuffer, dst.buffer)
		h.gl.bufferSubData(glArrayBuffer, int(dstX), len(bytes), glPointer(bytes))
		return nil
	}
	if src.texture == 0 || dst.texture == 0 {
		return errors.New("resource copy region requires matching buffer or texture resources")
	}
	if srcZ != 0 || dstZ != 0 || depth != 1 {
		return errors.New("Darwin resource copy region supports one 2D texture layer")
	}
	levelDimension := func(value, level uint32) uint32 {
		value >>= level
		if value == 0 {
			return 1
		}
		return value
	}
	srcWidth, srcHeight := levelDimension(src.description.Width, srcLevel), levelDimension(src.description.Height, srcLevel)
	dstWidth, dstHeight := levelDimension(dst.description.Width, dstLevel), levelDimension(dst.description.Height, dstLevel)
	if uint64(srcX)+uint64(width) > uint64(srcWidth) ||
		uint64(srcY)+uint64(height) > uint64(srcHeight) ||
		uint64(dstX)+uint64(width) > uint64(dstWidth) ||
		uint64(dstY)+uint64(height) > uint64(dstHeight) {
		return errors.New("texture copy region is out of bounds")
	}
	if src.depth != dst.depth || src.stencil != dst.stencil {
		return errors.New("resource copy region requires compatible texture formats")
	}

	attachment := uint32(glColorAttachment0)
	mask := uint32(glColorBufferBit)
	if src.depth {
		attachment = glDepthAttachment
		mask = glDepthBufferBit
		if src.stencil {
			attachment = glDepthStencilAttachment
			mask |= glStencilBufferBit
		}
	}
	h.gl.bindFramebuffer(glReadFramebuffer, h.blitReadFBO)
	h.gl.framebufferTexture(glReadFramebuffer, glColorAttachment0, glTexture2D, 0, 0)
	h.gl.framebufferTexture(glReadFramebuffer, glDepthAttachment, glTexture2D, 0, 0)
	h.gl.framebufferTexture(glReadFramebuffer, glDepthStencilAttachment, glTexture2D, 0, 0)
	h.gl.framebufferTexture(glReadFramebuffer, attachment, glTexture2D, src.texture, int32(srcLevel))
	if status := h.gl.checkFramebuffer(glReadFramebuffer); status != glFramebufferComplete {
		return fmt.Errorf("VirGL copy source framebuffer status %#x", status)
	}
	h.gl.bindFramebuffer(glDrawFramebuffer, h.blitDrawFBO)
	h.gl.framebufferTexture(glDrawFramebuffer, glColorAttachment0, glTexture2D, 0, 0)
	h.gl.framebufferTexture(glDrawFramebuffer, glDepthAttachment, glTexture2D, 0, 0)
	h.gl.framebufferTexture(glDrawFramebuffer, glDepthStencilAttachment, glTexture2D, 0, 0)
	h.gl.framebufferTexture(glDrawFramebuffer, attachment, glTexture2D, dst.texture, int32(dstLevel))
	if status := h.gl.checkFramebuffer(glDrawFramebuffer); status != glFramebufferComplete {
		return fmt.Errorf("VirGL copy destination framebuffer status %#x", status)
	}
	h.gl.blitFramebuffer(
		int32(srcX), int32(srcY), int32(srcX+width), int32(srcY+height),
		int32(dstX), int32(dstY), int32(dstX+width), int32(dstY+height),
		mask, glNearest,
	)
	return h.bindContextFramebuffer(context)
}

func textureTransferFormat(format uint32) uint32 {
	switch format {
	case 1, 2:
		return glBGRA
	default:
		return glRGBA
	}
}

func vertexFormat(format uint32) (components int32, dataType uint32, normalized bool, ok bool) {
	switch {
	case format >= 28 && format <= 31:
		return int32(format - 27), glFloat, false, true
	case format >= 64 && format <= 67:
		return int32(format - 63), glUnsignedByte, true, true
	default:
		return 0, 0, false, false
	}
}

func (h *darwinHost) deleteResource(resource *hostResource) {
	if resource.framebuffer != 0 {
		h.gl.deleteFramebuffers(1, &resource.framebuffer)
	}
	if resource.texture != 0 {
		h.gl.deleteTextures(1, &resource.texture)
	}
	if resource.buffer != 0 {
		h.gl.deleteBuffers(1, &resource.buffer)
	}
}

func (h *darwinHost) releaseGLObjects() {
	for _, resource := range h.resources {
		h.deleteResource(resource)
	}
	if h.vao != 0 {
		h.gl.deleteVertexArrays(1, &h.vao)
	}
	if h.blitReadFBO != 0 {
		h.gl.deleteFramebuffers(1, &h.blitReadFBO)
	}
	if h.blitDrawFBO != 0 {
		h.gl.deleteFramebuffers(1, &h.blitDrawFBO)
	}
	for _, program := range h.programs {
		h.gl.deleteProgram(program.id)
	}
}

func (h *darwinHost) close() error {
	h.once.Do(func() {
		close(h.stop)
		<-h.done
	})
	return nil
}
