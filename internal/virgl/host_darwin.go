//go:build darwin

package virgl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
	"math/bits"
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
	maxHostPrograms              = 128
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
	enabledVertexAttributes uint32
	blitReadFBO             uint32
	blitDrawFBO             uint32
	depthOnlyFBO            uint32
	framebufferBindingValid bool
	boundFramebuffer        uint32
	boundColorAttachments   [8]hostFramebufferAttachment
	boundDepthTexture       uint32
	boundDepthAttachment    uint32
	boundDepthSurface       hostFramebufferAttachment
	currentProgram          uint32
	programs                map[hostProgramKey]hostProgram
	programUseSequence      uint64
	contexts                map[uint32]*hostContext
	activeContext           *hostContext
	resources               map[uint32]*hostResource
	allResources            map[*hostResource]struct{}
	sharedPresentation      bool
	nativeFrames            [3]hostNativeFrame
	pendingBufferTransfers  []hostBufferTransfer
}

type hostBufferTransfer struct {
	resource    *hostResource
	description virtio.GPUResource3D
	data        []byte
	transfer    virtio.GPUTransfer3D
}

type hostNativeFrame struct {
	texture       uint32
	width         int
	height        int
	producerFence uintptr
	consumerFence uintptr
	inUse         bool
}

type hostResource struct {
	description           virtio.GPUResource3D
	texture               uint32
	textureTarget         uint32
	buffer                uint32
	bufferBytes           []byte
	bufferDirty           bool
	bufferDirtyStart      uint32
	bufferDirtyEnd        uint32
	framebuffer           uint32
	depth                 bool
	stencil               bool
	packedStencil         bool
	references            int
	samplerViewConfigured bool
	appliedSamplerView    hostSamplerView
}

type hostSurface struct {
	resourceID uint32
	resource   *hostResource
	format     uint32
	level      uint32
	firstLayer uint32
	lastLayer  uint32
}

type hostFramebufferAttachment struct {
	texture uint32
	target  uint32
	level   uint32
	layer   uint32
}

type hostSamplerView struct {
	resourceID uint32
	resource   *hostResource
	format     uint32
	firstLevel uint32
	lastLevel  uint32
	firstLayer uint32
	lastLayer  uint32
	swizzle    [4]uint32
}

type hostSamplerState struct {
	id          uint32
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
	state   uint32
	stencil [2]uint32
}

type hostVertexElement struct {
	offset          uint32
	instanceDivisor uint32
	bufferIndex     uint32
	format          uint32
}

type hostVertexBuffer struct {
	stride     uint32
	offset     uint32
	resourceID uint32
	resource   *hostResource
}

type hostShader struct {
	stage      uint32
	source     string
	generation uint64
}

type hostShaderAssembly struct {
	stage      uint32
	numTokens  uint32
	totalBytes uint32
	nextOffset uint32
	text       []byte
}

type hostProgram struct {
	id                    uint32
	lastUsed              uint64
	constants             [2][16]int32
	winsysAdjustY         int32
	samplers              [2][16]int32
	explicitLODCrossovers [2][16]int32
}

type hostUniformBuffer struct {
	resourceID uint32
	resource   *hostResource
	offset     uint32
	length     uint32
}

type hostProgramKey struct {
	context                      *hostContext
	vertexHandle, fragmentHandle uint32
	vertexGeneration             uint64
	fragmentGeneration           uint64
	pointSpriteCoordinates       uint32
}

type hostRasterizer struct {
	state                  uint32
	pointSize              float32
	spriteCoordinateEnable uint32
	offsetUnits            float32
	offsetScale            float32
}

type hostScissor struct {
	minX, minY uint32
	maxX, maxY uint32
}

type hostViewport struct {
	x, y          int32
	width, height int32
	adjustY       float32
	near, far     float64
}

type hostContext struct {
	subcontexts          map[uint32]*hostContext
	activeSubcontext     uint32
	blendStates          map[uint32]hostBlendState
	surfaces             map[uint32]hostSurface
	samplerViews         map[uint32]hostSamplerView
	samplerStates        map[uint32]hostSamplerState
	depthStencilAlpha    map[uint32]hostDepthStencilAlpha
	vertexElements       map[uint32][]hostVertexElement
	rasterizers          map[uint32]hostRasterizer
	shaders              map[uint32]hostShader
	shaderAssemblies     map[uint32]*hostShaderAssembly
	boundShaders         [6]uint32
	boundVertexElements  uint32
	boundBlend           uint32
	boundDSA             uint32
	boundRasterizer      uint32
	colorSurfaces        [8]uint32
	depthSurface         uint32
	blendColor           [4]float32
	stencilRef           [2]uint8
	scissors             [16]hostScissor
	viewport             hostViewport
	viewportSet          bool
	vertexBuffers        [16]hostVertexBuffer
	indexBuffer          uint32
	indexResource        *hostResource
	indexSize            uint32
	indexOffset          uint32
	constants            [2][]float32
	uniformBuffers       [2][16]hostUniformBuffer
	boundSamplerViews    [6][16]uint32
	boundSamplerStates   [6][16]uint32
	nextShaderGeneration uint64
}

func NewHostRenderer() (virtio.GPURenderer, error) {
	return NewHostRendererWithShareGroup(0, 0)
}

func NewHostRendererWithShareGroup(shareContext, sharePixelFormat uintptr) (virtio.GPURenderer, error) {
	host, err := newDarwinHost(shareContext, sharePixelFormat)
	if err != nil {
		return nil, err
	}
	backend, err := captureHostFromEnvironment(host)
	if err != nil {
		_ = host.close()
		return nil, err
	}
	return NewRenderer(backend), nil
}

func newDarwinHost(shareGroup ...uintptr) (*darwinHost, error) {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_LAZY); err != nil {
		return nil, fmt.Errorf("load AppKit for VirGL: %w", err)
	}
	gl, err := loadHostGL()
	if err != nil {
		return nil, err
	}
	host := &darwinHost{
		requests:     make(chan hostRequest),
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
		gl:           gl,
		programs:     make(map[hostProgramKey]hostProgram),
		contexts:     make(map[uint32]*hostContext),
		resources:    make(map[uint32]*hostResource),
		allResources: make(map[*hostResource]struct{}),
	}
	var shareContext uintptr
	var sharePixelFormat uintptr
	if len(shareGroup) != 0 {
		shareContext = shareGroup[0]
	}
	if len(shareGroup) > 1 {
		sharePixelFormat = shareGroup[1]
	}
	host.sharedPresentation = shareContext != 0 && sharePixelFormat != 0
	ready := make(chan error, 1)
	go host.contextLoop(shareContext, sharePixelFormat, ready)
	if err := <-ready; err != nil {
		<-host.done
		return nil, err
	}
	return host, nil
}

func (h *darwinHost) contextLoop(shareContext, sharePixelFormat uintptr, ready chan<- error) {
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
	format := objc.ID(sharePixelFormat)
	if format == 0 {
		format = objc.ID(objc.GetClass("NSOpenGLPixelFormat")).Send(alloc)
		format = format.Send(initWithAttributes, unsafe.Pointer(&attributes[0]))
		if format == 0 {
			ready <- errors.New("create accelerated VirGL pixel format")
			return
		}
		defer format.Send(release)
	}

	context := objc.ID(objc.GetClass("NSOpenGLContext")).Send(alloc)
	context = context.Send(initWithFormat, format, objc.ID(shareContext))
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
	h.gl.genFramebuffers(1, &h.depthOnlyFBO)
	h.gl.pixelStorei(glPackAlignment, 1)
	h.gl.pixelStorei(glUnpackAlignment, 1)
	h.gl.enable(glProgramPointSize)
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
		context := h.contexts[id]
		if context == nil {
			return fmt.Errorf("unknown Darwin VirGL context %d", id)
		}
		h.releaseContextResources(context)
		if h.activeContext == context {
			h.activeContext = nil
		}
		delete(h.contexts, id)
		return nil
	})
}

func (h *darwinHost) createResource(description virtio.GPUResource3D) error {
	return h.dispatch(func() error {
		if h.resources[description.ID] != nil {
			return fmt.Errorf("Darwin VirGL resource %d already exists", description.ID)
		}
		hostResource := &hostResource{description: description, references: 1}
		switch description.Target {
		case 0:
			if uint64(int(description.Width)) != uint64(description.Width) {
				return fmt.Errorf("VirGL buffer resource %d is too large", description.ID)
			}
			hostResource.bufferBytes = make([]byte, int(description.Width))
			h.gl.genBuffers(1, &hostResource.buffer)
			h.gl.bindBuffer(glArrayBuffer, hostResource.buffer)
			h.gl.bufferData(glArrayBuffer, int(description.Width), 0, glStreamDraw)
		case 2, 4, 7:
			if description.Width > capsetMaxTexture2D || description.Height > capsetMaxTexture2D {
				return fmt.Errorf("VirGL texture resource %d dimensions %dx%d exceed %d",
					description.ID, description.Width, description.Height, capsetMaxTexture2D)
			}
			if description.Target == 4 && (description.Width != description.Height ||
				(description.ArraySize != 1 && description.ArraySize != 6)) {
				return fmt.Errorf("VirGL cube resource %d must be square with one cube, got %dx%d array size %d",
					description.ID, description.Width, description.Height, description.ArraySize)
			}
			if description.Target == 7 && (description.ArraySize == 0 || description.ArraySize > 256) {
				return fmt.Errorf("VirGL 2D array resource %d has invalid layer count %d", description.ID, description.ArraySize)
			}
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
			hostResource.textureTarget = glTexture2D
			if description.Target == 4 {
				hostResource.textureTarget = glTextureCubeMap
			} else if description.Target == 7 {
				hostResource.textureTarget = glTexture2DArray
			}
			h.gl.bindTexture(hostResource.textureTarget, hostResource.texture)
			h.gl.texParameteri(hostResource.textureTarget, glTextureMinFilter, glLinear)
			h.gl.texParameteri(hostResource.textureTarget, glTextureMagFilter, glLinear)
			h.gl.texParameteri(hostResource.textureTarget, glTextureBaseLevel, 0)
			h.gl.texParameteri(hostResource.textureTarget, glTextureMaxLevel, int32(description.LastLevel))
			allocateLevels := func(internalFormat int32, format, dataType uint32) {
				for level := uint32(0); level <= description.LastLevel; level++ {
					width, height := description.Width>>level, description.Height>>level
					if width == 0 {
						width = 1
					}
					if height == 0 {
						height = 1
					}
					if description.Target == 4 {
						for face := uint32(0); face < 6; face++ {
							h.gl.texImage2D(glTextureCubeMapPositiveX+face, int32(level), internalFormat, int32(width), int32(height), 0, format, dataType, 0)
						}
					} else if description.Target == 7 {
						h.gl.texImage3D(glTexture2DArray, int32(level), internalFormat, int32(width), int32(height), int32(description.ArraySize), 0, format, dataType, 0)
					} else {
						h.gl.texImage2D(glTexture2D, int32(level), internalFormat, int32(width), int32(height), 0, format, dataType, 0)
					}
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
			case 20:
				hostResource.stencil = true
				// macOS's frozen OpenGL 4.1 profile cannot allocate the later
				// GL_STENCIL_INDEX8 texture storage. Back a logical S8 Gallium
				// resource with packed D24S8 and expose only its stencil aspect.
				hostResource.packedStencil = true
				allocateLevels(glDepth24Stencil8, glDepthStencil, glUnsignedInt248)
			default:
				allocateLevels(glRGBA8, textureTransferFormat(description.Format), glUnsignedByte)
				if !hostResource.depth && !hostResource.stencil {
					h.gl.genFramebuffers(1, &hostResource.framebuffer)
					if description.Target == 2 {
						h.framebufferBindingValid = false
						h.gl.bindFramebuffer(glFramebuffer, hostResource.framebuffer)
						h.gl.framebufferTexture(glFramebuffer, glColorAttachment0, glTexture2D, hostResource.texture, 0)
						if status := h.gl.checkFramebuffer(glFramebuffer); status != glFramebufferComplete {
							return fmt.Errorf("VirGL resource %d framebuffer status %#x", description.ID, status)
						}
					}
				}
			}
		default:
			return fmt.Errorf("VirGL resource %d target %d is not supported by the Darwin backend", description.ID, description.Target)
		}
		h.resources[description.ID] = hostResource
		h.allResources[hostResource] = struct{}{}
		return nil
	})
}

func (h *darwinHost) unrefResource(id uint32) error {
	return h.dispatch(func() error {
		resource := h.resources[id]
		if resource == nil {
			return fmt.Errorf("unknown Darwin VirGL resource %d", id)
		}
		// A guest handle may be released while a vertex buffer, index buffer,
		// sampler view, or surface still retains the host resource. Preserve
		// transfer ordering by committing queued writes before removing the
		// guest-visible handle; dropping them leaves retained bindings with
		// stale contents.
		if err := h.flushPendingBufferTransfers(); err != nil {
			return err
		}
		delete(h.resources, id)
		h.releaseResource(resource)
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

func (h *darwinHost) queueBufferTransfer(resource *resource, transfer virtio.GPUTransfer3D) error {
	hostResource := h.resources[resource.description.ID]
	if hostResource == nil || hostResource.buffer == 0 {
		return fmt.Errorf("unknown Darwin VirGL buffer resource %d", resource.description.ID)
	}
	box := transfer.Box
	if box.X > resource.description.Width || box.Width > resource.description.Width-box.X {
		return fmt.Errorf("buffer transfer range %d..%d exceeds resource size %d",
			box.X, uint64(box.X)+uint64(box.Width), resource.description.Width)
	}
	end := transfer.Offset + uint64(box.Width)
	if end < transfer.Offset || end > uint64(len(resource.data)) {
		return fmt.Errorf("buffer transfer backing range %d..%d exceeds %d bytes",
			transfer.Offset, end, len(resource.data))
	}
	h.pendingBufferTransfers = append(h.pendingBufferTransfers, hostBufferTransfer{
		resource:    hostResource,
		description: resource.description,
		data:        resource.data,
		transfer:    transfer,
	})
	return nil
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
		bytes := data[int(transfer.Offset):int(end)]
		copy(hostResource.bufferBytes[int(box.X):int(box.X+box.Width)], bytes)
		h.markBufferDirty(hostResource, box.X, box.Width)
	case hostResource.texture != 0:
		box := transfer.Box
		validLayerRange := box.Depth > 0
		switch description.Target {
		case 2:
			validLayerRange = box.Depth == 1 && box.Z == 0
		case 4:
			validLayerRange = box.Depth == 1 && box.Z < 6
		case 7:
			validLayerRange = box.Z <= description.ArraySize && box.Depth <= description.ArraySize-box.Z
		}
		if !validLayerRange {
			return fmt.Errorf("Darwin VirGL texture transfer targets invalid layer %d depth %d for target %d", box.Z, box.Depth, description.Target)
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
		bytesPerPixel := textureFormatBytes(description.Format)
		rowBytes := uint64(box.Width) * bytesPerPixel
		stride := uint64(transfer.Stride)
		if stride == 0 {
			stride = uint64(levelWidth) * bytesPerPixel
		}
		if stride < rowBytes {
			return fmt.Errorf("texture transfer stride %d is smaller than row size %d", stride, rowBytes)
		}
		layerSpan := rowBytes
		if box.Height > 1 {
			layerSpan += uint64(box.Height-1) * stride
		}
		layerStride := uint64(transfer.LayerStride)
		if layerStride == 0 {
			layerStride = stride * uint64(levelHeight)
		}
		if box.Depth > 1 && layerStride < layerSpan {
			return fmt.Errorf("texture transfer layer stride %d is smaller than layer size %d", layerStride, layerSpan)
		}
		required := layerSpan + uint64(box.Depth-1)*layerStride
		end := transfer.Offset + required
		if end < transfer.Offset || end > uint64(len(data)) {
			return fmt.Errorf("texture transfer backing range %d..%d exceeds %d bytes",
				transfer.Offset, end, len(data))
		}
		bytes := data[int(transfer.Offset):int(end)]
		packedLayer := rowBytes * uint64(box.Height)
		if stride != rowBytes || layerStride != packedLayer {
			packed := make([]byte, int(packedLayer)*int(box.Depth))
			for layer := uint32(0); layer < box.Depth; layer++ {
				for row := uint32(0); row < box.Height; row++ {
					sourceOffset := int(uint64(layer)*layerStride + uint64(row)*stride)
					destinationOffset := int(uint64(layer)*packedLayer + uint64(row)*rowBytes)
					copy(packed[destinationOffset:destinationOffset+int(rowBytes)],
						bytes[sourceOffset:sourceOffset+int(rowBytes)])
				}
			}
			bytes = packed
		}
		h.gl.bindTexture(hostResource.textureTarget, hostResource.texture)
		format, dataType := textureTransferFormat(description.Format), uint32(glUnsignedByte)
		if hostResource.depth {
			format, dataType = glDepthComponent, glUnsignedInt
			if description.Format == 16 {
				dataType = glUnsignedShort
			}
		}
		if hostResource.depth && hostResource.stencil {
			format, dataType = glDepthStencil, glUnsignedInt248
		} else if hostResource.stencil {
			if hostResource.packedStencil {
				packed := make([]byte, len(bytes)*4)
				for index, value := range bytes {
					binary.LittleEndian.PutUint32(packed[index*4:], 0xffffff00|uint32(value))
				}
				bytes = packed
				format, dataType = glDepthStencil, glUnsignedInt248
			} else {
				format, dataType = glStencilIndex, glUnsignedByte
			}
		}
		imageTarget := hostResource.textureTarget
		if description.Target == 4 {
			imageTarget = glTextureCubeMapPositiveX + box.Z
		}
		if description.Target == 7 {
			h.gl.texSubImage3D(imageTarget, int32(transfer.Level), int32(box.X), int32(box.Y), int32(box.Z),
				int32(box.Width), int32(box.Height), int32(box.Depth), format, dataType, glPointer(bytes))
		} else {
			h.gl.texSubImage2D(imageTarget, int32(transfer.Level), int32(box.X), int32(box.Y),
				int32(box.Width), int32(box.Height), format, dataType, glPointer(bytes))
		}
	}
	return nil
}

func (h *darwinHost) transferFromHost(resource *resource, transfer virtio.GPUTransfer3D) error {
	return h.dispatch(func() error {
		if err := h.flushPendingBufferTransfers(); err != nil {
			return err
		}
		hostResource := h.resources[resource.description.ID]
		if hostResource == nil {
			return fmt.Errorf("unknown Darwin VirGL resource %d", resource.description.ID)
		}

		switch {
		case hostResource.buffer != 0:
			box := transfer.Box
			if box.X > resource.description.Width || box.Width > resource.description.Width-box.X {
				return fmt.Errorf("buffer transfer range %d..%d exceeds resource size %d",
					box.X, uint64(box.X)+uint64(box.Width), resource.description.Width)
			}
			end := transfer.Offset + uint64(box.Width)
			if end < transfer.Offset || end > uint64(len(resource.data)) {
				return fmt.Errorf("buffer transfer backing range %d..%d exceeds %d bytes",
					transfer.Offset, end, len(resource.data))
			}
			if box.Width == 0 {
				return nil
			}
			h.publishBuffer(hostResource)
			bytes := resource.data[int(transfer.Offset):int(end)]
			h.gl.bindBuffer(glArrayBuffer, hostResource.buffer)
			h.gl.getBufferSubData(glArrayBuffer, int(box.X), len(bytes), glPointer(bytes))
			copy(hostResource.bufferBytes[int(box.X):int(box.X+box.Width)], bytes)
			return nil

		case hostResource.texture != 0:
			box := transfer.Box
			validLayerRange := box.Depth > 0
			switch resource.description.Target {
			case 2:
				validLayerRange = box.Depth == 1 && box.Z == 0
			case 4:
				validLayerRange = box.Depth == 1 && box.Z < 6
			case 7:
				validLayerRange = box.Z <= resource.description.ArraySize && box.Depth <= resource.description.ArraySize-box.Z
			}
			if !validLayerRange {
				return fmt.Errorf("Darwin VirGL texture transfer targets invalid layer %d depth %d for target %d", box.Z, box.Depth, resource.description.Target)
			}
			levelWidth, levelHeight := resource.description.Width>>transfer.Level, resource.description.Height>>transfer.Level
			if levelWidth == 0 {
				levelWidth = 1
			}
			if levelHeight == 0 {
				levelHeight = 1
			}
			if transfer.Level > resource.description.LastLevel ||
				box.X > levelWidth || box.Width > levelWidth-box.X ||
				box.Y > levelHeight || box.Height > levelHeight-box.Y {
				return fmt.Errorf("texture transfer box exceeds mip level %d dimensions %dx%d",
					transfer.Level, levelWidth, levelHeight)
			}
			bytesPerPixel := textureFormatBytes(resource.description.Format)
			rowBytes := uint64(box.Width) * bytesPerPixel
			stride := uint64(transfer.Stride)
			if stride == 0 {
				stride = uint64(levelWidth) * bytesPerPixel
			}
			if stride < rowBytes {
				return fmt.Errorf("texture transfer stride %d is smaller than row size %d", stride, rowBytes)
			}
			layerSpan := rowBytes
			if box.Height > 1 {
				layerSpan += uint64(box.Height-1) * stride
			}
			layerStride := uint64(transfer.LayerStride)
			if layerStride == 0 {
				layerStride = stride * uint64(levelHeight)
			}
			if box.Depth > 1 && layerStride < layerSpan {
				return fmt.Errorf("texture transfer layer stride %d is smaller than layer size %d", layerStride, layerSpan)
			}
			required := layerSpan + uint64(box.Depth-1)*layerStride
			end := transfer.Offset + required
			if end < transfer.Offset || end > uint64(len(resource.data)) {
				return fmt.Errorf("texture transfer backing range %d..%d exceeds %d bytes",
					transfer.Offset, end, len(resource.data))
			}
			if box.Width == 0 || box.Height == 0 {
				return nil
			}

			packedLayer := rowBytes * uint64(box.Height)
			packed := make([]byte, int(packedLayer)*int(box.Depth))
			attachment := uint32(glColorAttachment0)
			format, dataType := textureTransferFormat(resource.description.Format), uint32(glUnsignedByte)
			if hostResource.depth {
				attachment = glDepthAttachment
				format, dataType = glDepthComponent, glUnsignedInt
				if resource.description.Format == 16 {
					dataType = glUnsignedShort
				}
			}
			if hostResource.depth && hostResource.stencil {
				attachment = glDepthStencilAttachment
				format, dataType = glDepthStencil, glUnsignedInt248
			} else if hostResource.stencil {
				if hostResource.packedStencil {
					attachment = glDepthStencilAttachment
				} else {
					attachment = glStencilAttachment
				}
				format, dataType = glStencilIndex, glUnsignedByte
			}

			h.framebufferBindingValid = false
			for layer := uint32(0); layer < box.Depth; layer++ {
				h.gl.bindFramebuffer(glReadFramebuffer, h.blitReadFBO)
				h.gl.framebufferTexture(glReadFramebuffer, glColorAttachment0, glTexture2D, 0, 0)
				h.gl.framebufferTexture(glReadFramebuffer, glDepthAttachment, glTexture2D, 0, 0)
				h.gl.framebufferTexture(glReadFramebuffer, glStencilAttachment, glTexture2D, 0, 0)
				h.gl.framebufferTexture(glReadFramebuffer, glDepthStencilAttachment, glTexture2D, 0, 0)
				if resource.description.Target == 7 {
					h.gl.framebufferTextureLayer(glReadFramebuffer, attachment, hostResource.texture, int32(transfer.Level), int32(box.Z+layer))
				} else {
					imageTarget := hostResource.textureTarget
					if resource.description.Target == 4 {
						imageTarget = glTextureCubeMapPositiveX + box.Z
					}
					h.gl.framebufferTexture(glReadFramebuffer, attachment, imageTarget, hostResource.texture, int32(transfer.Level))
				}
				if status := h.gl.checkFramebuffer(glReadFramebuffer); status != glFramebufferComplete {
					return fmt.Errorf("VirGL transfer source framebuffer status %#x", status)
				}
				h.gl.finish()
				destination := uint64(layer) * packedLayer
				h.gl.readPixels(int32(box.X), int32(box.Y), int32(box.Width), int32(box.Height),
					format, dataType, glPointer(packed[int(destination):]))
			}
			for layer := uint32(0); layer < box.Depth; layer++ {
				for row := uint32(0); row < box.Height; row++ {
					source := int(uint64(layer)*packedLayer + uint64(row)*rowBytes)
					destination := int(transfer.Offset + uint64(layer)*layerStride + uint64(row)*stride)
					copy(resource.data[destination:destination+int(rowBytes)], packed[source:source+int(rowBytes)])
				}
			}
			return nil
		default:
			return fmt.Errorf("Darwin VirGL resource %d has no host storage", resource.description.ID)
		}
	})
}

func (h *darwinHost) execute(contextID uint32, commands []command, _ map[uint32]*resource) error {
	return h.dispatch(func() error {
		if err := h.flushPendingBufferTransfers(); err != nil {
			return err
		}
		root := h.contexts[contextID]
		if root == nil {
			return fmt.Errorf("unknown Darwin VirGL context %d", contextID)
		}
		for _, command := range commands {
			if command.Opcode >= 28 && command.Opcode <= 30 {
				if err := h.executeSubcontextCommand(root, command); err != nil {
					return fmt.Errorf("VirGL opcode %d object %d: %w", command.Opcode, command.Object, err)
				}
				continue
			}
			context := root.selectedContext()
			if err := h.activateContext(context); err != nil {
				return err
			}
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
		h.framebufferBindingValid = false
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

func (h *darwinHost) nativeScanout(resource *resource, rect image.Rectangle) (virtio.GPUNativeFrame, bool, error) {
	if !h.sharedPresentation {
		return virtio.GPUNativeFrame{}, false, nil
	}
	var frame virtio.GPUNativeFrame
	var slot *hostNativeFrame
	err := h.dispatch(func() error {
		width := int(resource.description.Width)
		height := int(resource.description.Height)
		rect = rect.Intersect(image.Rect(0, 0, width, height))
		if rect.Empty() {
			return errors.New("VirGL native scanout rectangle is empty")
		}
		hostResource := h.resources[resource.description.ID]
		if hostResource == nil || hostResource.texture == 0 || hostResource.depth {
			return fmt.Errorf("VirGL resource %d is not a color texture", resource.description.ID)
		}
		for index := range h.nativeFrames {
			if !h.nativeFrames[index].inUse {
				slot = &h.nativeFrames[index]
				break
			}
		}
		if slot == nil {
			return nil
		}
		if slot.consumerFence != 0 {
			h.gl.waitSync(slot.consumerFence, 0, glTimeoutIgnored)
			h.gl.deleteSync(slot.consumerFence)
			slot.consumerFence = 0
		}
		if slot.texture == 0 || slot.width != rect.Dx() || slot.height != rect.Dy() {
			if slot.texture != 0 {
				h.gl.deleteTextures(1, &slot.texture)
			}
			h.gl.genTextures(1, &slot.texture)
			h.gl.bindTexture(glTexture2D, slot.texture)
			h.gl.texParameteri(glTexture2D, glTextureMinFilter, glNearest)
			h.gl.texParameteri(glTexture2D, glTextureMagFilter, glNearest)
			h.gl.texParameteri(glTexture2D, glTextureBaseLevel, 0)
			h.gl.texParameteri(glTexture2D, glTextureMaxLevel, 0)
			h.gl.texImage2D(glTexture2D, 0, glRGBA8, int32(rect.Dx()), int32(rect.Dy()), 0, glRGBA, glUnsignedByte, 0)
			slot.width, slot.height = rect.Dx(), rect.Dy()
		}
		h.framebufferBindingValid = false
		h.gl.bindFramebuffer(glReadFramebuffer, h.blitReadFBO)
		h.gl.framebufferTexture(glReadFramebuffer, glColorAttachment0, glTexture2D, hostResource.texture, 0)
		if status := h.gl.checkFramebuffer(glReadFramebuffer); status != glFramebufferComplete {
			return fmt.Errorf("VirGL native source framebuffer status %#x", status)
		}
		h.gl.bindFramebuffer(glDrawFramebuffer, h.blitDrawFBO)
		h.gl.framebufferTexture(glDrawFramebuffer, glColorAttachment0, glTexture2D, slot.texture, 0)
		if status := h.gl.checkFramebuffer(glDrawFramebuffer); status != glFramebufferComplete {
			return fmt.Errorf("VirGL native destination framebuffer status %#x", status)
		}
		h.gl.blitFramebuffer(
			int32(rect.Min.X), int32(height-rect.Min.Y), int32(rect.Max.X), int32(height-rect.Max.Y),
			0, 0, int32(rect.Dx()), int32(rect.Dy()),
			glColorBufferBit, glNearest,
		)
		slot.producerFence = h.gl.fenceSync(glSyncGPUCommandsComplete, 0)
		if slot.producerFence == 0 {
			return errors.New("create VirGL native frame fence")
		}
		h.gl.flush()
		slot.inUse = true
		frame = virtio.GPUNativeFrame{
			Width: rect.Dx(), Height: rect.Dy(), Damage: image.Rect(0, 0, rect.Dx(), rect.Dy()),
			Texture: slot.texture, ProducerFence: slot.producerFence,
		}
		return nil
	})
	if err != nil {
		return virtio.GPUNativeFrame{}, false, err
	}
	if slot == nil {
		return virtio.GPUNativeFrame{}, false, nil
	}
	var once sync.Once
	frame.ReleaseFrame = func(consumerFence uintptr) {
		once.Do(func() {
			_ = h.dispatch(func() error {
				if slot.producerFence != 0 {
					h.gl.deleteSync(slot.producerFence)
					slot.producerFence = 0
				}
				if slot.consumerFence != 0 {
					h.gl.deleteSync(slot.consumerFence)
				}
				slot.consumerFence = consumerFence
				slot.inUse = false
				return nil
			})
		})
	}
	return frame, true, nil
}

func (h *darwinHost) reset() error {
	return h.dispatch(func() error {
		h.pendingBufferTransfers = nil
		h.releaseNativeFrames()
		for _, resource := range h.resources {
			h.deleteResource(resource)
		}
		h.resources = make(map[uint32]*hostResource)
		h.contexts = make(map[uint32]*hostContext)
		h.activeContext = nil
		return nil
	})
}

func (h *darwinHost) flushPendingBufferTransfers() error {
	pending := h.pendingBufferTransfers
	h.pendingBufferTransfers = nil
	for _, transfer := range pending {
		if err := h.uploadResource(transfer.resource, transfer.description, transfer.data, transfer.transfer); err != nil {
			return err
		}
	}
	return nil
}

func newHostContext() *hostContext {
	return &hostContext{
		subcontexts:       make(map[uint32]*hostContext),
		blendStates:       make(map[uint32]hostBlendState),
		surfaces:          make(map[uint32]hostSurface),
		samplerViews:      make(map[uint32]hostSamplerView),
		samplerStates:     make(map[uint32]hostSamplerState),
		depthStencilAlpha: make(map[uint32]hostDepthStencilAlpha),
		vertexElements:    make(map[uint32][]hostVertexElement),
		rasterizers:       make(map[uint32]hostRasterizer),
		shaders:           make(map[uint32]hostShader),
		shaderAssemblies:  make(map[uint32]*hostShaderAssembly),
	}
}

func (c *hostContext) selectedContext() *hostContext {
	if c.activeSubcontext == 0 {
		return c
	}
	return c.subcontexts[c.activeSubcontext]
}

func (h *darwinHost) executeSubcontextCommand(root *hostContext, command command) error {
	if len(command.Payload) != 1 {
		return errors.New("invalid subcontext payload")
	}
	id := command.Payload[0]
	switch command.Opcode {
	case 28: // VIRGL_CCMD_SET_SUB_CTX
		if id != 0 && root.subcontexts[id] == nil {
			return fmt.Errorf("unknown VirGL subcontext %d", id)
		}
		root.activeSubcontext = id
		return h.activateContext(root.selectedContext())
	case 29: // VIRGL_CCMD_CREATE_SUB_CTX
		if id == 0 || root.subcontexts[id] != nil {
			return fmt.Errorf("VirGL subcontext %d already exists", id)
		}
		root.subcontexts[id] = newHostContext()
	case 30: // VIRGL_CCMD_DESTROY_SUB_CTX
		if id == 0 {
			return nil
		}
		context := root.subcontexts[id]
		if context == nil {
			return nil
		}
		if root.activeSubcontext == id {
			root.activeSubcontext = 0
		}
		if h.activeContext == context {
			h.activeContext = nil
		}
		h.releaseContextResources(context)
		delete(root.subcontexts, id)
	}
	return nil
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
			if len(payload) != 9 {
				return errors.New("truncated rasterizer")
			}
			context.rasterizers[handle] = hostRasterizer{
				state:                  payload[1],
				pointSize:              math.Float32frombits(payload[2]),
				spriteCoordinateEnable: payload[3],
				offsetUnits:            math.Float32frombits(payload[6]),
				offsetScale:            math.Float32frombits(payload[7]),
			}
		case 3: // VIRGL_OBJECT_DSA
			if len(payload) != 5 {
				return errors.New("invalid depth/stencil/alpha payload")
			}
			context.depthStencilAlpha[handle] = hostDepthStencilAlpha{
				state: payload[1], stencil: [2]uint32{payload[2], payload[3]},
			}
		case 4: // VIRGL_OBJECT_SHADER
			generation := context.nextShaderGeneration
			if err := context.createShader(payload); err != nil {
				return err
			}
			if context.nextShaderGeneration != generation {
				h.deleteProgramsForShader(context, handle)
			}
			return nil
		case 5: // VIRGL_OBJECT_VERTEX_ELEMENTS
			if (len(payload)-1)%4 != 0 {
				return errors.New("invalid vertex element payload")
			}
			elements := make([]hostVertexElement, 0, (len(payload)-1)/4)
			for index := 1; index < len(payload); index += 4 {
				elements = append(elements, hostVertexElement{
					offset:          payload[index],
					instanceDivisor: payload[index+1],
					bufferIndex:     payload[index+2],
					format:          payload[index+3],
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
			firstLayer, lastLayer := payload[3]&0xffff, payload[3]>>16
			firstLevel, lastLevel := payload[4]&0xff, (payload[4]>>8)&0xff
			if firstLevel > lastLevel || lastLevel > resource.description.LastLevel {
				return fmt.Errorf("sampler view mip range %d..%d exceeds resource last level %d",
					firstLevel, lastLevel, resource.description.LastLevel)
			}
			if err := validateTextureLayers(resource.description, firstLayer, lastLayer); err != nil {
				return fmt.Errorf("sampler view %d: %w", handle, err)
			}
			if previous, ok := context.samplerViews[handle]; ok {
				h.releaseResource(previous.resource)
			}
			h.retainResource(resource)
			context.samplerViews[handle] = hostSamplerView{
				resourceID: payload[1],
				resource:   resource,
				format:     payload[2] & 0x00ffffff,
				firstLevel: firstLevel,
				lastLevel:  lastLevel,
				firstLayer: firstLayer,
				lastLayer:  lastLayer,
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
			h.gl.genSamplers(1, &state.id)
			h.applySamplerState(state)
			if previous, ok := context.samplerStates[handle]; ok {
				h.gl.deleteSamplers(1, &previous.id)
			}
			context.samplerStates[handle] = state
		case 8: // VIRGL_OBJECT_SURFACE
			if len(payload) != 2 && len(payload) != 5 {
				return errors.New("invalid surface payload")
			}
			if h.resources[payload[1]] == nil {
				return fmt.Errorf("surface refers to unknown resource %d", payload[1])
			}
			if previous, ok := context.surfaces[handle]; ok {
				h.releaseResource(previous.resource)
			}
			resource := h.resources[payload[1]]
			format, level, firstLayer, lastLayer := resource.description.Format, uint32(0), uint32(0), uint32(0)
			if len(payload) == 5 {
				format = payload[2]
				level = payload[3]
				firstLayer, lastLayer = payload[4]&0xffff, payload[4]>>16
			}
			if level > resource.description.LastLevel {
				return fmt.Errorf("surface mip level %d exceeds resource last level %d", level, resource.description.LastLevel)
			}
			if err := validateTextureLayers(resource.description, firstLayer, lastLayer); err != nil {
				return fmt.Errorf("surface %d: %w", handle, err)
			}
			h.retainResource(resource)
			context.surfaces[handle] = hostSurface{
				resourceID: payload[1], resource: resource, format: format, level: level,
				firstLayer: firstLayer, lastLayer: lastLayer,
			}
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
				h.gl.disable(glProgramPointSize)
				h.gl.pointSize(1)
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
				h.gl.disable(glStencilTest)
				h.gl.stencilMaskSeparate(glFrontAndBack, ^uint32(0))
				break
			}
			state, ok := context.depthStencilAlpha[payload[0]]
			if !ok {
				return fmt.Errorf("unknown depth/stencil/alpha state %d", payload[0])
			}
			h.applyDepthStencilAlpha(context, state)
		case 4, 6, 7, 8:
			// Other fixed-function state is represented by core GL defaults
			// for this first accelerated path.
		}
	case 3: // VIRGL_CCMD_DESTROY_OBJECT
		if len(payload) != 1 {
			return errors.New("invalid destroy payload")
		}
		switch command.Object {
		case 1:
			delete(context.blendStates, payload[0])
		case 2:
			delete(context.rasterizers, payload[0])
		case 3:
			delete(context.depthStencilAlpha, payload[0])
		case 4:
			h.deleteProgramsForShader(context, payload[0])
			delete(context.shaders, payload[0])
			delete(context.shaderAssemblies, payload[0])
		case 5:
			delete(context.vertexElements, payload[0])
		case 6:
			if view, ok := context.samplerViews[payload[0]]; ok {
				h.releaseResource(view.resource)
			}
			delete(context.samplerViews, payload[0])
		case 7:
			if state, ok := context.samplerStates[payload[0]]; ok {
				h.gl.deleteSamplers(1, &state.id)
			}
			delete(context.samplerStates, payload[0])
		case 8:
			if surface, ok := context.surfaces[payload[0]]; ok {
				h.releaseResource(surface.resource)
			}
			delete(context.surfaces, payload[0])
		default:
			return fmt.Errorf("unsupported object type %d", command.Object)
		}
	case 4: // VIRGL_CCMD_SET_VIEWPORT_STATE
		if len(payload) < 7 {
			return errors.New("truncated viewport")
		}
		scaleX := float64(math.Float32frombits(payload[1]))
		scaleY := float64(math.Float32frombits(payload[2]))
		scaleZ := float64(math.Float32frombits(payload[3]))
		translateX := float64(math.Float32frombits(payload[4]))
		translateY := float64(math.Float32frombits(payload[5]))
		translateZ := float64(math.Float32frombits(payload[6]))
		minX := math.Min(translateX-scaleX, translateX+scaleX)
		minY := math.Min(translateY-scaleY, translateY+scaleY)
		context.viewport = hostViewport{
			x:       int32(math.Round(minX)),
			y:       int32(math.Round(minY)),
			width:   int32(math.Round(math.Abs(scaleX * 2))),
			height:  int32(math.Round(math.Abs(scaleY * 2))),
			adjustY: 1,
			near:    translateZ - scaleZ,
			far:     translateZ + scaleZ,
		}
		if scaleY < 0 {
			context.viewport.adjustY = -1
		}
		context.viewportSet = true
		h.applyViewport(context.viewport)
	case 5: // VIRGL_CCMD_SET_FRAMEBUFFER_STATE
		if len(payload) < 2 {
			return errors.New("truncated framebuffer state")
		}
		colorCount := int(payload[0])
		if colorCount > len(context.colorSurfaces) || len(payload) != colorCount+2 {
			return fmt.Errorf("invalid framebuffer color surface count %d", colorCount)
		}
		var colorSurfaces [8]uint32
		for index, handle := range payload[2:] {
			if handle == 0 {
				continue
			}
			surface, ok := context.surfaces[handle]
			if !ok || surface.resource == nil || surface.resource.framebuffer == 0 {
				return fmt.Errorf("unknown color surface %d", handle)
			}
			colorSurfaces[index] = handle
		}
		context.colorSurfaces = colorSurfaces
		context.depthSurface = payload[1]
		return h.bindContextFramebuffer(context)
	case 6: // VIRGL_CCMD_SET_VERTEX_BUFFERS
		if len(payload) < 3 || len(payload)%3 != 0 {
			return errors.New("invalid vertex buffer state")
		}
		var bindings [16]hostVertexBuffer
		if len(payload)/3 > len(bindings) {
			return errors.New("too many vertex buffers")
		}
		for index := 0; index < len(payload)/3; index++ {
			resourceID := payload[index*3+2]
			if resourceID == 0 {
				continue
			}
			resource := h.resources[resourceID]
			if resource == nil || resource.buffer == 0 {
				return fmt.Errorf("vertex buffer %d refers to unknown buffer resource %d", index, resourceID)
			}
			bindings[index] = hostVertexBuffer{
				stride:     payload[index*3],
				offset:     payload[index*3+1],
				resourceID: resourceID,
				resource:   resource,
			}
		}
		for _, binding := range context.vertexBuffers {
			h.releaseResource(binding.resource)
		}
		for _, binding := range bindings {
			h.retainResource(binding.resource)
		}
		context.vertexBuffers = bindings
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
			// Gallium full clears ignore the bound stencil write masks.
			h.gl.stencilMaskSeparate(glFrontAndBack, ^uint32(0))
			if len(payload) >= 8 {
				h.gl.clearStencil(int32(payload[7]))
			}
		}
		// Gallium full clears are not restricted by rasterizer scissoring.
		scissorEnabled := context.boundRasterizer != 0 &&
			context.rasterizers[context.boundRasterizer].state&(1<<14) != 0
		if scissorEnabled {
			h.gl.disable(glScissorTest)
		}
		h.gl.clear(mask)
		if mask&glDepthBufferBit != 0 {
			h.restoreDepthWriteMask(context)
		}
		if mask&glStencilBufferBit != 0 {
			h.restoreStencilWriteMasks(context)
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
		var resource *hostResource
		if payload[0] != 0 {
			resource = h.resources[payload[0]]
			if resource == nil || resource.buffer == 0 {
				return fmt.Errorf("unknown index buffer %d", payload[0])
			}
		}
		h.releaseResource(context.indexResource)
		h.retainResource(resource)
		context.indexBuffer = payload[0]
		context.indexResource = resource
		context.indexSize = 0
		context.indexOffset = 0
		if resource != nil {
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
	case 13: // VIRGL_CCMD_SET_STENCIL_REF
		if len(payload) != 1 {
			return errors.New("invalid stencil reference")
		}
		context.stencilRef = [2]uint8{uint8(payload[0]), uint8(payload[0] >> 8)}
		if context.boundDSA != 0 {
			h.applyDepthStencilAlpha(context, context.depthStencilAlpha[context.boundDSA])
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
			context.rasterizers[context.boundRasterizer].state&(1<<14) != 0 {
			h.applyScissor(context.scissors[0])
		}
	case 16: // VIRGL_CCMD_BLIT
		return h.blit(context, payload)
	case 17: // VIRGL_CCMD_RESOURCE_COPY_REGION
		return h.copyResourceRegion(context, payload)
	case 27: // VIRGL_CCMD_SET_UNIFORM_BUFFER
		if len(payload) != 5 || payload[0] > tgsiFragment || payload[1] >= 16 {
			return errors.New("invalid uniform buffer binding")
		}
		stage, index := payload[0], payload[1]
		binding := hostUniformBuffer{}
		if payload[4] != 0 {
			resource := h.resources[payload[4]]
			if resource == nil || resource.buffer == 0 {
				return fmt.Errorf("uniform buffer refers to unknown buffer resource %d", payload[4])
			}
			offset, length := payload[2], payload[3]
			if offset > uint32(len(resource.bufferBytes)) || length > uint32(len(resource.bufferBytes))-offset ||
				offset%16 != 0 || length%16 != 0 {
				return fmt.Errorf("uniform buffer range %d..%d is invalid for %d-byte resource %d",
					offset, uint64(offset)+uint64(length), len(resource.bufferBytes), payload[4])
			}
			binding = hostUniformBuffer{resourceID: payload[4], resource: resource, offset: offset, length: length}
		}
		h.releaseResource(context.uniformBuffers[stage][index].resource)
		h.retainResource(binding.resource)
		context.uniformBuffers[stage][index] = binding
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
	case 22, 23, 24, 32:
		// Empty index state, clip/sample state, and
		// tessellation state do not require additional GL calls yet.
	default:
		return fmt.Errorf("command is not implemented")
	}
	return nil
}

func (c *hostContext) createShader(payload []uint32) error {
	const (
		shaderContinuation = uint32(1 << 31)
		maxShaderBytes     = uint32(4 << 20)
	)
	if len(payload) < 5 {
		return errors.New("truncated shader")
	}
	handle, stage, offlen, numTokens, streamOutputs := payload[0], payload[1], payload[2], payload[3], payload[4]
	if streamOutputs != 0 {
		return fmt.Errorf("shader stream outputs %d are unsupported", streamOutputs)
	}
	chunk := shaderBytes(payload[5:])
	if offlen&shaderContinuation == 0 {
		totalBytes := offlen
		if totalBytes == 0 || totalBytes > maxShaderBytes {
			return fmt.Errorf("shader byte length %d is invalid", totalBytes)
		}
		paddedBytes := (totalBytes + 3) &^ 3
		if uint32(len(chunk)) > paddedBytes {
			return fmt.Errorf("shader first chunk has %d bytes, total is %d", len(chunk), totalBytes)
		}
		assembly := &hostShaderAssembly{
			stage:      stage,
			numTokens:  numTokens,
			totalBytes: totalBytes,
			nextOffset: uint32(len(chunk)),
			text:       make([]byte, paddedBytes),
		}
		copy(assembly.text, chunk)
		if assembly.nextOffset < paddedBytes {
			c.shaderAssemblies[handle] = assembly
			return nil
		}
		return c.finishShader(handle, assembly)
	}

	assembly := c.shaderAssemblies[handle]
	if assembly == nil {
		return fmt.Errorf("shader continuation for unknown handle %d", handle)
	}
	offset := offlen &^ shaderContinuation
	if stage != assembly.stage || numTokens != assembly.numTokens {
		return fmt.Errorf("shader continuation metadata changed for handle %d", handle)
	}
	if offset != assembly.nextOffset {
		return fmt.Errorf("shader continuation offset %d, want %d", offset, assembly.nextOffset)
	}
	if uint32(len(chunk)) > uint32(len(assembly.text))-offset {
		return fmt.Errorf("shader continuation exceeds length %d", assembly.totalBytes)
	}
	copy(assembly.text[offset:], chunk)
	assembly.nextOffset += uint32(len(chunk))
	if assembly.nextOffset < uint32(len(assembly.text)) {
		return nil
	}
	delete(c.shaderAssemblies, handle)
	return c.finishShader(handle, assembly)
}

func (c *hostContext) finishShader(handle uint32, assembly *hostShaderAssembly) error {
	lastDword := assembly.text[len(assembly.text)-4:]
	if !strings.ContainsRune(string(lastDword), '\x00') {
		return fmt.Errorf("shader %d is not NUL terminated", handle)
	}
	text := string(assembly.text[:assembly.totalBytes])
	if index := strings.IndexByte(text, 0); index >= 0 {
		text = text[:index]
	}
	stage, source, err := translateTGSI(text)
	if err != nil {
		return err
	}
	if stage != assembly.stage {
		return fmt.Errorf("shader payload type %d disagrees with TGSI stage %d", assembly.stage, stage)
	}
	c.nextShaderGeneration++
	c.shaders[handle] = hostShader{
		stage: stage, source: source, generation: c.nextShaderGeneration,
	}
	return nil
}

func shaderBytes(words []uint32) []byte {
	result := make([]byte, 0, len(words)*4)
	for _, word := range words {
		result = append(result, byte(word), byte(word>>8), byte(word>>16), byte(word>>24))
	}
	return result
}

func validateTextureLayers(description virtio.GPUResource3D, first, last uint32) error {
	if description.Target == 0 {
		return nil
	}
	if first > last {
		return fmt.Errorf("texture layer range %d..%d is inverted", first, last)
	}
	limit := uint32(1)
	switch description.Target {
	case 4:
		limit = 6
	case 7:
		limit = description.ArraySize
	}
	if last >= limit {
		return fmt.Errorf("texture layer range %d..%d exceeds %d layers", first, last, limit)
	}
	return nil
}

func (h *darwinHost) draw(context *hostContext, payload []uint32) error {
	start, count, mode, indexed := payload[0], payload[1], payload[2], payload[3] != 0
	instanceCount, startInstance := uint32(1), uint32(0)
	if len(payload) > 4 {
		instanceCount = payload[4]
	}
	if len(payload) > 6 {
		startInstance = payload[6]
	}
	if instanceCount == 0 {
		return nil
	}
	if instanceCount > math.MaxInt32 {
		return fmt.Errorf("instance count %d exceeds host GL", instanceCount)
	}
	if startInstance != 0 {
		return fmt.Errorf("start instance %d is unsupported", startInstance)
	}
	if !context.hasColorSurface() && context.depthSurface == 0 {
		return errors.New("draw has no framebuffer")
	}
	if err := h.bindContextFramebuffer(context); err != nil {
		return err
	}
	if context.boundRasterizer != 0 {
		h.applyFrontFace(context, context.rasterizers[context.boundRasterizer].state)
	}
	elements := context.vertexElements[context.boundVertexElements]
	h.gl.bindVertexArray(h.vao)
	var enabledAttributes uint32
	for index, element := range elements {
		if element.bufferIndex >= uint32(len(context.vertexBuffers)) {
			return fmt.Errorf("vertex element %d uses invalid buffer index %d", index, element.bufferIndex)
		}
		binding := context.vertexBuffers[element.bufferIndex]
		buffer := binding.resource
		if buffer == nil || buffer.buffer == 0 {
			return fmt.Errorf("vertex element %d refers to unknown buffer %d", index, binding.resourceID)
		}
		components, dataType, normalized, ok := vertexFormat(element.format)
		if !ok {
			return fmt.Errorf("vertex element %d uses unsupported format %d", index, element.format)
		}
		if binding.stride == 0 {
			// A zero-stride vertex buffer supplies one current attribute value,
			// but multiple vertex elements may still select different values
			// from that binding. Mesa uses this for constants such as color and
			// point size packed into one buffer.
			value, err := constantVertexAttribute(buffer.bufferBytes, binding.offset+element.offset, element.format)
			if err != nil {
				return fmt.Errorf("vertex element %d constant value: %w", index, err)
			}
			h.gl.disableVertexAttrib(uint32(index))
			h.gl.vertexAttribDivisor(uint32(index), 0)
			h.gl.vertexAttrib4f(uint32(index), value[0], value[1], value[2], value[3])
			continue
		}
		h.publishBuffer(buffer)
		h.gl.bindBuffer(glArrayBuffer, buffer.buffer)
		h.gl.vertexAttribPtr(uint32(index), components, dataType, normalized, int32(binding.stride),
			uintptr(binding.offset+element.offset))
		h.gl.vertexAttribDivisor(uint32(index), element.instanceDivisor)
		h.gl.enableVertexAttrib(uint32(index))
		enabledAttributes |= 1 << uint(index)
	}
	retiredAttributes := h.enabledVertexAttributes &^ enabledAttributes
	for retiredAttributes != 0 {
		index := uint32(bits.TrailingZeros32(retiredAttributes))
		h.gl.disableVertexAttrib(index)
		h.gl.vertexAttribDivisor(index, 0)
		retiredAttributes &^= 1 << index
	}
	h.enabledVertexAttributes = enabledAttributes

	var pointSpriteCoordinates uint32
	if mode == 0 && context.boundRasterizer != 0 {
		rasterizer := context.rasterizers[context.boundRasterizer]
		if rasterizer.state&(1<<7) != 0 {
			pointSpriteCoordinates = rasterizer.spriteCoordinateEnable
		}
	}
	program, err := h.programFor(context, pointSpriteCoordinates)
	if err != nil {
		return err
	}
	if h.currentProgram != program.id {
		h.gl.useProgram(program.id)
		h.currentProgram = program.id
	}
	if program.winsysAdjustY >= 0 {
		adjustY := float32(1)
		if context.viewportSet {
			adjustY = context.viewport.adjustY
		}
		h.gl.uniform1f(program.winsysAdjustY, adjustY)
	}
	for stage := tgsiVertex; stage <= tgsiFragment; stage++ {
		for index, location := range program.constants[stage] {
			if location < 0 {
				continue
			}
			binding := context.uniformBuffers[stage][index]
			if binding.resource != nil && binding.length != 0 {
				bytes := binding.resource.bufferBytes[binding.offset : binding.offset+binding.length]
				h.gl.uniform4fv(location, int32(len(bytes)/16), (*float32)(unsafe.Pointer(&bytes[0])))
				continue
			}
			if index == 0 && len(context.constants[stage]) != 0 {
				constants := context.constants[stage]
				h.gl.uniform4fv(location, int32(len(constants)/4), &constants[0])
			}
		}
	}
	for stage := tgsiVertex; stage <= tgsiFragment; stage++ {
		for slot, viewHandle := range context.boundSamplerViews[stage] {
			if viewHandle == 0 {
				continue
			}
			view := context.samplerViews[viewHandle]
			texture := view.resource
			if texture == nil || texture.texture == 0 {
				return fmt.Errorf("sampler view %d has no texture", viewHandle)
			}
			textureUnit := uint32(stage*16 + slot)
			h.gl.activeTexture(glTexture0 + textureUnit)
			h.gl.bindTexture(texture.textureTarget, texture.texture)
			if !texture.samplerViewConfigured || texture.appliedSamplerView != view {
				if err := h.applySamplerView(view); err != nil {
					return fmt.Errorf("sampler view %d: %w", viewHandle, err)
				}
				texture.appliedSamplerView = view
				texture.samplerViewConfigured = true
			}
			if stateHandle := context.boundSamplerStates[stage][slot]; stateHandle != 0 {
				state := context.samplerStates[stateHandle]
				h.gl.bindSampler(textureUnit, state.id)
				if location := program.explicitLODCrossovers[stage][slot]; location >= 0 {
					h.gl.uniform1f(location, explicitLODCrossover(view, state))
				}
			} else {
				h.gl.bindSampler(textureUnit, 0)
				if location := program.explicitLODCrossovers[stage][slot]; location >= 0 {
					h.gl.uniform1f(location, 0)
				}
			}
			location := program.samplers[stage][slot]
			if location >= 0 {
				h.gl.uniform1i(location, int32(textureUnit))
			}
		}
	}

	var glMode uint32
	switch mode {
	case 0:
		glMode = glPoints
	case 1:
		glMode = glLines
	case 2:
		glMode = glLineLoop
	case 3:
		glMode = glLineStrip
	case 4:
		glMode = glTriangles
	case 5:
		glMode = glTriangleStrip
	case 6:
		glMode = glTriangleFan
	default:
		return fmt.Errorf("unsupported primitive mode %d", mode)
	}
	if indexed {
		buffer := context.indexResource
		if buffer == nil || buffer.buffer == 0 {
			return fmt.Errorf("draw refers to unknown index buffer %d", context.indexBuffer)
		}
		h.publishBuffer(buffer)
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
		// VirGL's index-buffer binding already carries the byte offset for the
		// draw. Unlike non-indexed draws, DRAW_VBO.start is not added again by
		// the reference renderer; doing so selects unrelated indices from Mesa's
		// shared streaming buffer as soon as a draw has a nonzero start.
		offset := uintptr(context.indexOffset)
		indexBias := int32(0)
		if len(payload) > 5 {
			indexBias = int32(payload[5])
		}
		if instanceCount > 1 && indexBias != 0 {
			h.gl.drawElementsInstancedBaseVertex(glMode, int32(count), indexType, offset, int32(instanceCount), indexBias)
		} else if instanceCount > 1 {
			h.gl.drawElementsInstanced(glMode, int32(count), indexType, offset, int32(instanceCount))
		} else if indexBias != 0 {
			h.gl.drawElementsBaseVertex(glMode, int32(count), indexType, offset, indexBias)
		} else {
			h.gl.drawElements(glMode, int32(count), indexType, offset)
		}
		if primitiveRestart {
			h.gl.disable(glPrimitiveRestart)
		}
	} else {
		if instanceCount > 1 {
			h.gl.drawArraysInstanced(glMode, int32(start), int32(count), int32(instanceCount))
		} else {
			h.gl.drawArrays(glMode, int32(start), int32(count))
		}
	}
	return nil
}

func framebufferAttachmentForSurface(surface hostSurface) (hostFramebufferAttachment, error) {
	if surface.resource == nil || surface.resource.texture == 0 {
		return hostFramebufferAttachment{}, errors.New("surface has no texture")
	}
	if surface.firstLayer != surface.lastLayer {
		return hostFramebufferAttachment{}, fmt.Errorf("layered surface range %d..%d requires geometry-shader rendering", surface.firstLayer, surface.lastLayer)
	}
	return hostFramebufferAttachment{
		texture: surface.resource.texture,
		target:  surface.resource.description.Target,
		level:   surface.level,
		layer:   surface.firstLayer,
	}, nil
}

func (h *darwinHost) attachFramebufferSurface(target, attachment uint32, surface hostSurface) error {
	binding, err := framebufferAttachmentForSurface(surface)
	if err != nil {
		return err
	}
	return h.attachTextureLayer(target, attachment, surface.resource, binding.level, binding.layer)
}

func (h *darwinHost) attachTextureLayer(target, attachment uint32, resource *hostResource, level, layer uint32) error {
	switch resource.description.Target {
	case 2:
		if layer != 0 {
			return fmt.Errorf("2D texture layer %d is invalid", layer)
		}
		h.gl.framebufferTexture(target, attachment, glTexture2D, resource.texture, int32(level))
	case 4:
		if layer >= 6 {
			return fmt.Errorf("cube texture face %d is invalid", layer)
		}
		h.gl.framebufferTexture(target, attachment, glTextureCubeMapPositiveX+layer, resource.texture, int32(level))
	case 7:
		if layer >= resource.description.ArraySize {
			return fmt.Errorf("2D array texture layer %d exceeds %d layers", layer, resource.description.ArraySize)
		}
		h.gl.framebufferTextureLayer(target, attachment, resource.texture, int32(level), int32(layer))
	default:
		return fmt.Errorf("framebuffer texture target %d is unsupported", resource.description.Target)
	}
	return nil
}

func (h *darwinHost) bindContextFramebuffer(context *hostContext) error {
	firstColorSurface := context.firstColorSurface()
	if firstColorSurface == 0 {
		if context.depthSurface != 0 {
			surface, ok := context.surfaces[context.depthSurface]
			if !ok {
				return fmt.Errorf("unknown bound depth surface %d", context.depthSurface)
			}
			target := surface.resource
			if target == nil || (!target.depth && !target.stencil) || target.texture == 0 {
				return fmt.Errorf("bound depth/stencil surface %d has no depth or stencil texture", context.depthSurface)
			}
			depthSurfaceBinding, err := framebufferAttachmentForSurface(surface)
			if err != nil {
				return fmt.Errorf("bound depth/stencil surface %d: %w", context.depthSurface, err)
			}
			attachment := uint32(glDepthAttachment)
			if target.depth && target.stencil || target.packedStencil {
				attachment = glDepthStencilAttachment
			} else if target.stencil {
				attachment = glStencilAttachment
			}
			if h.framebufferBindingValid && h.boundFramebuffer == h.depthOnlyFBO &&
				h.boundDepthSurface == depthSurfaceBinding && h.boundDepthAttachment == attachment {
				return nil
			}
			h.gl.bindFramebuffer(glFramebuffer, h.depthOnlyFBO)
			h.gl.framebufferTexture(glFramebuffer, glDepthAttachment, glTexture2D, 0, 0)
			h.gl.framebufferTexture(glFramebuffer, glStencilAttachment, glTexture2D, 0, 0)
			h.gl.framebufferTexture(glFramebuffer, glDepthStencilAttachment, glTexture2D, 0, 0)
			if err := h.attachFramebufferSurface(glFramebuffer, attachment, surface); err != nil {
				return err
			}
			h.gl.drawBuffer(glNone)
			h.gl.readBuffer(glNone)
			if status := h.gl.checkFramebuffer(glFramebuffer); status != glFramebufferComplete {
				return fmt.Errorf("VirGL depth-only framebuffer status %#x", status)
			}
			h.framebufferBindingValid = true
			h.boundFramebuffer = h.depthOnlyFBO
			h.boundColorAttachments = [8]hostFramebufferAttachment{}
			h.boundDepthTexture = target.texture
			h.boundDepthAttachment = attachment
			h.boundDepthSurface = depthSurfaceBinding
			return nil
		}
		if h.framebufferBindingValid && h.boundFramebuffer == 0 {
			return nil
		}
		h.gl.bindFramebuffer(glFramebuffer, 0)
		h.framebufferBindingValid = true
		h.boundFramebuffer = 0
		h.boundColorAttachments = [8]hostFramebufferAttachment{}
		h.boundDepthTexture = 0
		h.boundDepthAttachment = 0
		h.boundDepthSurface = hostFramebufferAttachment{}
		return nil
	}
	surface, ok := context.surfaces[firstColorSurface]
	if !ok {
		return fmt.Errorf("unknown bound color surface %d", firstColorSurface)
	}
	target := surface.resource
	if target == nil {
		return fmt.Errorf("bound color surface %d refers to missing resource %d", firstColorSurface, surface.resourceID)
	}
	if target.framebuffer == 0 {
		return fmt.Errorf(
			"bound color surface %d resource %d has no framebuffer (target=%d format=%d size=%dx%d depth=%t)",
			firstColorSurface, surface.resourceID, target.description.Target, target.description.Format,
			target.description.Width, target.description.Height, target.depth,
		)
	}
	var depthTexture, depthAttachment uint32
	var depthSurfaceBinding hostFramebufferAttachment
	var boundDepthSurface hostSurface
	if context.depthSurface != 0 {
		depthSurface, ok := context.surfaces[context.depthSurface]
		if !ok {
			return fmt.Errorf("unknown bound depth surface %d", context.depthSurface)
		}
		depthResource := depthSurface.resource
		if depthResource == nil || (!depthResource.depth && !depthResource.stencil) || depthResource.texture == 0 {
			return fmt.Errorf("bound depth/stencil surface %d is not a depth or stencil texture", context.depthSurface)
		}
		depthTexture = depthResource.texture
		boundDepthSurface = depthSurface
		var err error
		depthSurfaceBinding, err = framebufferAttachmentForSurface(depthSurface)
		if err != nil {
			return fmt.Errorf("bound depth/stencil surface %d: %w", context.depthSurface, err)
		}
		depthAttachment = glDepthAttachment
		if depthResource.depth && depthResource.stencil || depthResource.packedStencil {
			depthAttachment = glDepthStencilAttachment
		} else if depthResource.stencil {
			depthAttachment = glStencilAttachment
		}
	}
	var colorAttachments [8]hostFramebufferAttachment
	for index, handle := range context.colorSurfaces {
		if handle == 0 {
			continue
		}
		colorSurface, ok := context.surfaces[handle]
		if !ok || colorSurface.resource == nil || colorSurface.resource.texture == 0 {
			return fmt.Errorf("unknown bound color surface %d at slot %d", handle, index)
		}
		attachment, err := framebufferAttachmentForSurface(colorSurface)
		if err != nil {
			return fmt.Errorf("bound color surface %d at slot %d: %w", handle, index, err)
		}
		colorAttachments[index] = attachment
	}
	if h.framebufferBindingValid && h.boundFramebuffer == target.framebuffer &&
		h.boundColorAttachments == colorAttachments &&
		h.boundDepthSurface == depthSurfaceBinding && h.boundDepthAttachment == depthAttachment {
		return nil
	}
	h.gl.bindFramebuffer(glFramebuffer, target.framebuffer)
	// Framebuffer objects are context-local in the GL model used by VirGL.
	// This backend multiplexes VirGL subcontexts through one native context,
	// so a framebuffer object's attachment state may have been changed by the
	// previously active subcontext. Restore the selected subcontext's complete
	// attachment set whenever its framebuffer is rebound.
	h.gl.framebufferTexture(glFramebuffer, glDepthAttachment, glTexture2D, 0, 0)
	h.gl.framebufferTexture(glFramebuffer, glStencilAttachment, glTexture2D, 0, 0)
	h.gl.framebufferTexture(glFramebuffer, glDepthStencilAttachment, glTexture2D, 0, 0)
	var drawBuffers [8]uint32
	for index := range context.colorSurfaces {
		attachment := glColorAttachment0 + uint32(index)
		h.gl.framebufferTexture(glFramebuffer, attachment, glTexture2D, 0, 0)
		handle := context.colorSurfaces[index]
		if handle == 0 {
			drawBuffers[index] = glNone
			continue
		}
		if err := h.attachFramebufferSurface(glFramebuffer, attachment, context.surfaces[handle]); err != nil {
			return err
		}
		drawBuffers[index] = attachment
	}
	h.gl.drawBuffers(int32(len(drawBuffers)), &drawBuffers[0])
	if depthTexture != 0 {
		if err := h.attachFramebufferSurface(glFramebuffer, depthAttachment, boundDepthSurface); err != nil {
			return err
		}
	}
	if status := h.gl.checkFramebuffer(glFramebuffer); status != glFramebufferComplete {
		return fmt.Errorf("VirGL framebuffer status %#x", status)
	}
	h.framebufferBindingValid = true
	h.boundFramebuffer = target.framebuffer
	h.boundColorAttachments = colorAttachments
	h.boundDepthTexture = depthTexture
	h.boundDepthAttachment = depthAttachment
	h.boundDepthSurface = depthSurfaceBinding
	return nil
}

func (h *darwinHost) activateContext(context *hostContext) error {
	if context == nil {
		return errors.New("VirGL context has no selected subcontext")
	}
	if h.activeContext == context {
		return nil
	}
	if context.boundBlend == 0 {
		h.applyDefaultBlend()
	} else {
		state, ok := context.blendStates[context.boundBlend]
		if !ok {
			return fmt.Errorf("unknown bound blend state %d", context.boundBlend)
		}
		if err := h.applyBlend(state); err != nil {
			return err
		}
	}
	if context.boundRasterizer == 0 {
		h.gl.disable(glCullFace)
		h.gl.disable(glScissorTest)
		h.gl.disable(glProgramPointSize)
		h.gl.disable(glPolygonOffsetFill)
		h.gl.pointSize(1)
	} else {
		state, ok := context.rasterizers[context.boundRasterizer]
		if !ok {
			return fmt.Errorf("unknown bound rasterizer %d", context.boundRasterizer)
		}
		h.applyRasterizer(context, state)
	}
	if context.boundDSA == 0 {
		h.gl.disable(glDepthTest)
		h.gl.depthMask(true)
		h.gl.disable(glStencilTest)
		h.gl.stencilMaskSeparate(glFrontAndBack, ^uint32(0))
	} else {
		state, ok := context.depthStencilAlpha[context.boundDSA]
		if !ok {
			return fmt.Errorf("unknown bound depth/stencil/alpha state %d", context.boundDSA)
		}
		h.applyDepthStencilAlpha(context, state)
	}
	h.gl.blendColor(context.blendColor[0], context.blendColor[1], context.blendColor[2], context.blendColor[3])
	if context.viewportSet {
		h.applyViewport(context.viewport)
	}
	h.activeContext = context
	return nil
}

func (h *darwinHost) applyViewport(viewport hostViewport) {
	h.gl.viewport(viewport.x, viewport.y, viewport.width, viewport.height)
	h.gl.depthRange(viewport.near, viewport.far)
}

func (h *darwinHost) restoreDepthWriteMask(context *hostContext) {
	if context.boundDSA == 0 {
		h.gl.depthMask(true)
		return
	}
	h.gl.depthMask(context.depthStencilAlpha[context.boundDSA].state&(1<<1) != 0)
}

func (h *darwinHost) restoreStencilWriteMasks(context *hostContext) {
	if context.boundDSA == 0 {
		h.gl.stencilMaskSeparate(glFrontAndBack, ^uint32(0))
		return
	}
	state := context.depthStencilAlpha[context.boundDSA]
	front := state.stencil[0]
	back := front
	if state.stencil[1]&1 != 0 {
		back = state.stencil[1]
	}
	h.gl.stencilMaskSeparate(glFront, (front>>21)&0xff)
	h.gl.stencilMaskSeparate(glBack, (back>>21)&0xff)
}

func (h *darwinHost) programFor(context *hostContext, pointSpriteCoordinates uint32) (hostProgram, error) {
	vertexHandle := context.boundShaders[tgsiVertex]
	fragmentHandle := context.boundShaders[tgsiFragment]
	vertex := context.shaders[vertexHandle]
	fragment := context.shaders[fragmentHandle]
	if vertex.source == "" || fragment.source == "" {
		return hostProgram{}, errors.New("draw has incomplete shader state")
	}
	key := hostProgramKey{
		context: context, vertexHandle: vertexHandle, fragmentHandle: fragmentHandle,
		vertexGeneration: vertex.generation, fragmentGeneration: fragment.generation,
		pointSpriteCoordinates: pointSpriteCoordinates,
	}
	if program, ok := h.programs[key]; ok {
		h.programUseSequence++
		program.lastUsed = h.programUseSequence
		h.programs[key] = program
		return program, nil
	}
	h.evictPrograms(maxHostPrograms - 1)
	fragmentSource := pointSpriteFragmentSource(fragment.source, pointSpriteCoordinates)
	vertexSource := linkTGSIInterfaces(vertex.source, fragmentSource)
	id, err := h.gl.compileProgram(vertexSource, fragmentSource)
	if err != nil {
		return hostProgram{}, err
	}
	h.programUseSequence++
	program := hostProgram{
		id:            id,
		lastUsed:      h.programUseSequence,
		winsysAdjustY: uniformLocation(h.gl, id, "uWinsysAdjustY"),
	}
	for stage := tgsiVertex; stage <= tgsiFragment; stage++ {
		for buffer := range program.constants[stage] {
			program.constants[stage][buffer] = uniformLocation(h.gl, id, tgsiConstantName(uint32(stage), buffer)+"[0]")
		}
		for slot := range program.samplers[stage] {
			program.samplers[stage][slot] = uniformLocation(h.gl, id, tgsiSamplerName(uint32(stage), slot))
			program.explicitLODCrossovers[stage][slot] = uniformLocation(h.gl, id, tgsiSamplerLODCrossoverName(uint32(stage), slot))
		}
	}
	h.programs[key] = program
	return program, nil
}

func (h *darwinHost) applyRasterizer(context *hostContext, rasterizer hostRasterizer) {
	state := rasterizer.state
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
	h.applyFrontFace(context, state)
	if state&(1<<14) != 0 {
		h.gl.enable(glScissorTest)
		h.applyScissor(context.scissors[0])
	} else {
		h.gl.disable(glScissorTest)
	}
	if state&(1<<24) != 0 {
		h.gl.enable(glProgramPointSize)
	} else {
		h.gl.disable(glProgramPointSize)
		// Gallium commonly leaves point_size at zero for rasterizer states
		// used only by triangle draws. GL_POINT_SIZE rejects zero even though
		// the value cannot affect those draws, leaving a sticky host GL error.
		h.gl.pointSize(min(64, max(1, rasterizer.pointSize)))
	}
	if state&(1<<7) != 0 {
		origin := int32(glLowerLeft)
		if state&(1<<6) != 0 {
			origin = glUpperLeft
		}
		h.gl.pointParameteri(glPointSpriteCoordOrigin, origin)
	}
	if state&(1<<20) != 0 {
		h.gl.enable(glPolygonOffsetFill)
		h.gl.polygonOffset(rasterizer.offsetScale, rasterizer.offsetUnits)
	} else {
		h.gl.disable(glPolygonOffsetFill)
	}
}

func (h *darwinHost) applyFrontFace(context *hostContext, rasterizer uint32) {
	frontCCW := rasterizer&(1<<15) != 0
	if !context.framebufferOriginUpperLeft() {
		frontCCW = !frontCCW
	}
	if frontCCW {
		h.gl.frontFace(glCCW)
	} else {
		h.gl.frontFace(glCW)
	}
}

func (c *hostContext) framebufferOriginUpperLeft() bool {
	if colorSurface := c.firstColorSurface(); colorSurface != 0 {
		if surface := c.surfaces[colorSurface]; surface.resource != nil {
			return surface.resource.description.Flags&1 != 0
		}
	}
	if c.depthSurface != 0 {
		if surface := c.surfaces[c.depthSurface]; surface.resource != nil {
			return surface.resource.description.Flags&1 != 0
		}
	}
	return false
}

func (c *hostContext) hasColorSurface() bool {
	return c.firstColorSurface() != 0
}

func (c *hostContext) firstColorSurface() uint32 {
	for _, surface := range c.colorSurfaces {
		if surface != 0 {
			return surface
		}
	}
	return 0
}

func (h *darwinHost) applyDepthStencilAlpha(context *hostContext, state hostDepthStencilAlpha) {
	if state.state&1 == 0 {
		h.gl.disable(glDepthTest)
	} else {
		h.gl.enable(glDepthTest)
		functions := [...]uint32{glNever, glLess, glEqual, glLEqual, glGreater, glNotEqual, glGEqual, glAlways}
		h.gl.depthFunc(functions[(state.state>>2)&7])
	}
	h.gl.depthMask(state.state&(1<<1) != 0)

	front := state.stencil[0]
	back := front
	backReference := context.stencilRef[1]
	if state.stencil[1]&1 != 0 {
		back = state.stencil[1]
	} else {
		// A disabled separate back-face state means the complete front state
		// applies to both faces. That includes stencil_refs[0]; using the stale
		// back reference silently breaks replacement operations on back faces.
		backReference = context.stencilRef[0]
	}
	if front&1 == 0 {
		h.gl.disable(glStencilTest)
		return
	}
	h.gl.enable(glStencilTest)
	functions := [...]uint32{glNever, glLess, glEqual, glLEqual, glGreater, glNotEqual, glGEqual, glAlways}
	operations := [...]uint32{glKeep, glZero, glReplace, glIncrement, glDecrement, glIncrementWrap, glDecrementWrap, glInvert}
	applyFace := func(face uint32, packed uint32, reference uint8) {
		h.gl.stencilFuncSeparate(face, functions[(packed>>1)&7], int32(reference), (packed>>13)&0xff)
		h.gl.stencilOpSeparate(face,
			operations[(packed>>4)&7], operations[(packed>>10)&7], operations[(packed>>7)&7])
		h.gl.stencilMaskSeparate(face, (packed>>21)&0xff)
	}
	applyFace(glFront, front, context.stencilRef[0])
	applyFace(glBack, back, backReference)
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
	h.gl.samplerParameteri(state.id, glTextureWrapS, wrap(state.state&7))
	h.gl.samplerParameteri(state.id, glTextureWrapT, wrap((state.state>>3)&7))
	h.gl.samplerParameteri(state.id, glTextureWrapR, wrap((state.state>>6)&7))
	linearMin := state.state&(1<<9) != 0
	var minFilter int32
	switch (state.state >> 11) & 3 {
	case 2: // PIPE_TEX_MIPFILTER_NONE
		if linearMin {
			minFilter = glLinear
		} else {
			minFilter = glNearest
		}
	case 0: // PIPE_TEX_MIPFILTER_NEAREST
		if linearMin {
			minFilter = glLinearMipmapNearest
		} else {
			minFilter = glNearestMipmapNearest
		}
	case 1: // PIPE_TEX_MIPFILTER_LINEAR
		if linearMin {
			minFilter = glLinearMipmapLinear
		} else {
			minFilter = glNearestMipmapLinear
		}
	default:
		minFilter = glNearest
	}
	magFilter := int32(glNearest)
	if state.state&(1<<13) != 0 {
		magFilter = glLinear
	}
	h.gl.samplerParameteri(state.id, glTextureMinFilter, minFilter)
	h.gl.samplerParameteri(state.id, glTextureMagFilter, magFilter)
	h.gl.samplerParameterf(state.id, glTextureLODBias, state.lodBias)
	h.gl.samplerParameterf(state.id, glTextureMinLOD, state.minLOD)
	h.gl.samplerParameterf(state.id, glTextureMaxLOD, state.maxLOD)
	h.gl.samplerParameterfv(state.id, glTextureBorderColor, &state.borderColor[0])
	if state.state&(1<<15) != 0 {
		h.gl.samplerParameteri(state.id, glTextureCompareMode, glCompareRefToTexture)
		functions := [...]int32{glNever, glLess, glEqual, glLEqual, glGreater, glNotEqual, glGEqual, glAlways}
		h.gl.samplerParameteri(state.id, glTextureCompareFunc, functions[(state.state>>16)&7])
	} else {
		h.gl.samplerParameteri(state.id, glTextureCompareMode, glNone)
	}
}

// OpenGL ES uses a half-level minification/magnification crossover when the
// magnification filter is linear and the minification image filter is nearest
// with mipmaps. Apple's desktop GL driver instead switches explicit-LOD cube
// lookups at zero. Shaders subtract this value only on the magnification side
// of the crossover, preserving the original LOD for mip selection.
func explicitLODCrossover(view hostSamplerView, state hostSamplerState) float32 {
	if view.resource == nil || view.resource.description.Target != 4 ||
		state.state&(1<<9) != 0 || state.state&(1<<13) == 0 ||
		(state.state>>11)&3 == 2 {
		return 0
	}
	return 0.5
}

func (h *darwinHost) applySamplerView(view hostSamplerView) error {
	if view.resource == nil || view.resource.textureTarget == 0 {
		return errors.New("sampler view has no texture target")
	}
	target := view.resource.textureTarget
	h.gl.texParameteri(target, glTextureBaseLevel, int32(view.firstLevel))
	h.gl.texParameteri(target, glTextureMaxLevel, int32(view.lastLevel))
	swizzles := [...]int32{glRed, glGreen, glBlue, glAlpha, glZero, glOne}
	parameters := [...]uint32{glTextureSwizzleR, glTextureSwizzleG, glTextureSwizzleB, glTextureSwizzleA}
	for index, value := range view.swizzle {
		if value >= uint32(len(swizzles)) {
			return fmt.Errorf("invalid component swizzle %d", value)
		}
		swizzle := swizzles[value]
		// Gallium's X8 formats carry padding, not alpha. The format table's
		// RGB1 swizzle is applied before the sampler-view swizzle, so any view
		// component that selects A must observe 1.0 regardless of the padding
		// byte supplied by the guest.
		if value == 3 && (view.format == 2 || view.format == 134) {
			swizzle = glOne
		}
		h.gl.texParameteri(target, parameters[index], swizzle)
	}
	return nil
}

func (h *darwinHost) applyDefaultBlend() {
	h.gl.disable(glBlend)
	h.gl.disable(glDither)
	h.gl.colorMask(true, true, true, true)
}

func (h *darwinHost) applyBlend(state hostBlendState) error {
	if state.state&(1<<2) != 0 {
		h.gl.enable(glDither)
	} else {
		h.gl.disable(glDither)
	}
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

func (h *darwinHost) restoreScissor(context *hostContext) {
	if context.boundRasterizer != 0 && context.rasterizers[context.boundRasterizer].state&(1<<14) != 0 {
		h.gl.enable(glScissorTest)
		h.applyScissor(context.scissors[0])
		return
	}
	h.gl.disable(glScissorTest)
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
	h.framebufferBindingValid = false
	dstZ, srcZ := payload[8], payload[17]
	validLayer := func(resource *hostResource, layer uint32) bool {
		switch resource.description.Target {
		case 2:
			return layer == 0
		case 4:
			return layer < 6
		case 7:
			return layer < resource.description.ArraySize
		default:
			return false
		}
	}
	if payload[11] != 1 || payload[20] != 1 {
		return errors.New("Darwin texture blit supports one source and destination layer")
	}
	if !validLayer(src, srcZ) || !validLayer(dst, dstZ) {
		return fmt.Errorf("texture blit layers source %d target %d and destination %d target %d are invalid",
			srcZ, src.description.Target, dstZ, dst.description.Target)
	}
	h.gl.bindFramebuffer(glReadFramebuffer, h.blitReadFBO)
	if err := h.attachTextureLayer(glReadFramebuffer, glColorAttachment0, src, srcLevel, srcZ); err != nil {
		return err
	}
	if status := h.gl.checkFramebuffer(glReadFramebuffer); status != glFramebufferComplete {
		return fmt.Errorf("VirGL blit source framebuffer status %#x", status)
	}
	h.gl.bindFramebuffer(glDrawFramebuffer, h.blitDrawFBO)
	if err := h.attachTextureLayer(glDrawFramebuffer, glColorAttachment0, dst, dstLevel, dstZ); err != nil {
		return err
	}
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
	if payload[0]&(1<<10) != 0 {
		minXY, maxXY := payload[1], payload[2]
		h.gl.enable(glScissorTest)
		h.applyScissor(hostScissor{
			minX: minXY & 0xffff,
			minY: minXY >> 16,
			maxX: maxXY & 0xffff,
			maxY: maxXY >> 16,
		})
	} else {
		h.gl.disable(glScissorTest)
	}
	srcY1, srcY2 := virglBlitY(src, srcLevel, int32(payload[16]), int32(payload[19]))
	dstY1, dstY2 := virglBlitY(dst, dstLevel, int32(payload[7]), int32(payload[10]))
	h.gl.blitFramebuffer(
		int32(payload[15]), srcY1, int32(payload[15])+int32(payload[18]), srcY2,
		int32(payload[6]), dstY1, int32(payload[6])+int32(payload[9]), dstY2,
		mask, filter,
	)
	h.restoreScissor(context)
	return h.bindContextFramebuffer(context)
}

func virglBlitY(resource *hostResource, level uint32, y, height int32) (int32, int32) {
	if resource.description.Flags&1 == 0 {
		return y + height, y
	}
	resourceHeight := int32(resource.description.Height >> level)
	if resourceHeight == 0 {
		resourceHeight = 1
	}
	return resourceHeight - y - height, resourceHeight - y
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
		if width == 0 {
			return nil
		}
		bytes := append([]byte(nil), src.bufferBytes[int(srcX):int(srcX+width)]...)
		copy(dst.bufferBytes[int(dstX):int(dstX+width)], bytes)
		h.markBufferDirty(dst, dstX, width)
		return nil
	}
	if src.texture == 0 || dst.texture == 0 {
		return errors.New("resource copy region requires matching buffer or texture resources")
	}
	layerCount := func(resource *hostResource) uint32 {
		switch resource.description.Target {
		case 2:
			return 1
		case 4:
			return 6
		case 7:
			return resource.description.ArraySize
		default:
			return 0
		}
	}
	srcLayers, dstLayers := layerCount(src), layerCount(dst)
	if depth == 0 {
		return nil
	}
	if srcZ > srcLayers || depth > srcLayers-srcZ || dstZ > dstLayers || depth > dstLayers-dstZ {
		return fmt.Errorf("texture copy layer ranges source %d..%d target %d and destination %d..%d target %d are invalid",
			srcZ, srcZ+depth-1, src.description.Target, dstZ, dstZ+depth-1, dst.description.Target)
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
	} else if src.stencil {
		attachment = glStencilAttachment
		if src.packedStencil {
			attachment = glDepthStencilAttachment
		}
		mask = glStencilBufferBit
	}
	h.framebufferBindingValid = false
	h.gl.disable(glScissorTest)
	srcY1, srcY2 := virglCopyY(src, srcLevel, int32(srcY), int32(height))
	dstY1, dstY2 := virglCopyY(dst, dstLevel, int32(dstY), int32(height))
	for layer := uint32(0); layer < depth; layer++ {
		h.gl.bindFramebuffer(glReadFramebuffer, h.blitReadFBO)
		h.gl.framebufferTexture(glReadFramebuffer, glColorAttachment0, glTexture2D, 0, 0)
		h.gl.framebufferTexture(glReadFramebuffer, glDepthAttachment, glTexture2D, 0, 0)
		h.gl.framebufferTexture(glReadFramebuffer, glStencilAttachment, glTexture2D, 0, 0)
		h.gl.framebufferTexture(glReadFramebuffer, glDepthStencilAttachment, glTexture2D, 0, 0)
		if err := h.attachTextureLayer(glReadFramebuffer, attachment, src, srcLevel, srcZ+layer); err != nil {
			return err
		}
		if status := h.gl.checkFramebuffer(glReadFramebuffer); status != glFramebufferComplete {
			return fmt.Errorf("VirGL copy source framebuffer status %#x", status)
		}
		h.gl.bindFramebuffer(glDrawFramebuffer, h.blitDrawFBO)
		h.gl.framebufferTexture(glDrawFramebuffer, glColorAttachment0, glTexture2D, 0, 0)
		h.gl.framebufferTexture(glDrawFramebuffer, glDepthAttachment, glTexture2D, 0, 0)
		h.gl.framebufferTexture(glDrawFramebuffer, glStencilAttachment, glTexture2D, 0, 0)
		h.gl.framebufferTexture(glDrawFramebuffer, glDepthStencilAttachment, glTexture2D, 0, 0)
		if err := h.attachTextureLayer(glDrawFramebuffer, attachment, dst, dstLevel, dstZ+layer); err != nil {
			return err
		}
		if status := h.gl.checkFramebuffer(glDrawFramebuffer); status != glFramebufferComplete {
			return fmt.Errorf("VirGL copy destination framebuffer status %#x", status)
		}
		h.gl.blitFramebuffer(
			int32(srcX), srcY1, int32(srcX+width), srcY2,
			int32(dstX), dstY1, int32(dstX+width), dstY2,
			mask, glNearest,
		)
	}
	h.restoreScissor(context)
	return h.bindContextFramebuffer(context)
}

func virglCopyY(resource *hostResource, level uint32, y, height int32) (int32, int32) {
	if resource.description.Flags&1 == 0 {
		return y, y + height
	}
	resourceHeight := int32(resource.description.Height >> level)
	if resourceHeight == 0 {
		resourceHeight = 1
	}
	return resourceHeight - y - height, resourceHeight - y
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
	case format >= 48 && format <= 51:
		return int32(format - 47), glUnsignedShort, true, true
	case format >= 52 && format <= 55:
		return int32(format - 51), glUnsignedShort, false, true
	case format >= 56 && format <= 59:
		return int32(format - 55), glShort, true, true
	case format >= 60 && format <= 63:
		return int32(format - 59), glShort, false, true
	case format >= 64 && format <= 67:
		return int32(format - 63), glUnsignedByte, true, true
	case format >= 69 && format <= 72:
		return int32(format - 68), glUnsignedByte, false, true
	case format >= 74 && format <= 77:
		return int32(format - 73), glByte, true, true
	case format >= 82 && format <= 85:
		return int32(format - 81), glByte, false, true
	case format >= 87 && format <= 90:
		return int32(format - 86), glFixed, false, true
	default:
		return 0, 0, false, false
	}
}

func constantVertexAttribute(data []byte, offset, format uint32) ([4]float32, error) {
	components, dataType, normalized, ok := vertexFormat(format)
	if !ok {
		return [4]float32{}, fmt.Errorf("unsupported format %d", format)
	}
	componentBytes := 1
	if dataType == glFloat || dataType == glFixed {
		componentBytes = 4
	} else if dataType == glUnsignedShort || dataType == glShort {
		componentBytes = 2
	}
	end := uint64(offset) + uint64(components)*uint64(componentBytes)
	if end > uint64(len(data)) {
		return [4]float32{}, fmt.Errorf("range %d..%d exceeds %d-byte buffer", offset, end, len(data))
	}
	value := [4]float32{0, 0, 0, 1}
	for component := 0; component < int(components); component++ {
		start := int(offset) + component*componentBytes
		switch dataType {
		case glFloat:
			value[component] = math.Float32frombits(binary.LittleEndian.Uint32(data[start:]))
		case glFixed:
			value[component] = float32(int32(binary.LittleEndian.Uint32(data[start:]))) / 65536
		case glUnsignedShort:
			raw := binary.LittleEndian.Uint16(data[start:])
			value[component] = float32(raw)
			if normalized {
				value[component] /= 65535
			}
		case glShort:
			raw := int16(binary.LittleEndian.Uint16(data[start:]))
			value[component] = float32(raw)
			if normalized {
				value[component] = max(value[component]/32767, -1)
			}
		case glUnsignedByte:
			value[component] = float32(data[start])
			if normalized {
				value[component] /= 255
			}
		case glByte:
			value[component] = float32(int8(data[start]))
			if normalized {
				value[component] = max(value[component]/127, -1)
			}
		}
	}
	return value, nil
}

func (h *darwinHost) retainResource(resource *hostResource) {
	if resource != nil {
		resource.references++
	}
}

func (h *darwinHost) publishBuffer(resource *hostResource) {
	if resource == nil || resource.buffer == 0 || !resource.bufferDirty {
		return
	}
	h.gl.bindBuffer(glArrayBuffer, resource.buffer)
	start, end := resource.bufferDirtyStart, resource.bufferDirtyEnd
	if start == 0 && end == uint32(len(resource.bufferBytes)) {
		h.gl.bufferData(glArrayBuffer, len(resource.bufferBytes), glPointer(resource.bufferBytes), glStreamDraw)
	} else {
		bytes := resource.bufferBytes[int(start):int(end)]
		h.gl.bufferSubData(glArrayBuffer, int(start), len(bytes), glPointer(bytes))
	}
	resource.bufferDirty = false
}

func (h *darwinHost) markBufferDirty(resource *hostResource, start, size uint32) {
	if size == 0 {
		return
	}
	end := start + size
	if !resource.bufferDirty {
		resource.bufferDirty = true
		resource.bufferDirtyStart = start
		resource.bufferDirtyEnd = end
		return
	}
	if start < resource.bufferDirtyStart {
		resource.bufferDirtyStart = start
	}
	if end > resource.bufferDirtyEnd {
		resource.bufferDirtyEnd = end
	}
}

func (h *darwinHost) releaseResource(resource *hostResource) {
	if resource == nil {
		return
	}
	resource.references--
	if resource.references > 0 {
		return
	}
	h.deleteResource(resource)
	delete(h.allResources, resource)
}

func (h *darwinHost) releaseContextResources(context *hostContext) {
	for _, subcontext := range context.subcontexts {
		h.releaseContextResources(subcontext)
	}
	for _, surface := range context.surfaces {
		h.releaseResource(surface.resource)
	}
	for _, view := range context.samplerViews {
		h.releaseResource(view.resource)
	}
	for _, state := range context.samplerStates {
		h.gl.deleteSamplers(1, &state.id)
	}
	for _, binding := range context.vertexBuffers {
		h.releaseResource(binding.resource)
	}
	for stage := range context.uniformBuffers {
		for _, binding := range context.uniformBuffers[stage] {
			h.releaseResource(binding.resource)
		}
	}
	h.releaseResource(context.indexResource)
	h.deleteContextPrograms(context)
	if h.activeContext == context {
		h.activeContext = nil
	}
}

func (h *darwinHost) deleteContextPrograms(context *hostContext) {
	for key, program := range h.programs {
		if key.context == context {
			h.deleteProgram(key, program)
		}
	}
}

func (h *darwinHost) deleteProgramsForShader(context *hostContext, handle uint32) {
	for key, program := range h.programs {
		if key.context == context && (key.vertexHandle == handle || key.fragmentHandle == handle) {
			h.deleteProgram(key, program)
		}
	}
}

func (h *darwinHost) evictPrograms(limit int) {
	for len(h.programs) > limit {
		var oldestKey hostProgramKey
		var oldestProgram hostProgram
		found := false
		for key, program := range h.programs {
			if program.id == h.currentProgram {
				continue
			}
			if !found || program.lastUsed < oldestProgram.lastUsed {
				oldestKey = key
				oldestProgram = program
				found = true
			}
		}
		if !found {
			return
		}
		h.deleteProgram(oldestKey, oldestProgram)
	}
}

func (h *darwinHost) deleteProgram(key hostProgramKey, program hostProgram) {
	if h.currentProgram == program.id {
		h.gl.useProgram(0)
		h.currentProgram = 0
	}
	h.gl.deleteProgram(program.id)
	delete(h.programs, key)
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
	h.releaseNativeFrames()
	for resource := range h.allResources {
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
	if h.depthOnlyFBO != 0 {
		h.gl.deleteFramebuffers(1, &h.depthOnlyFBO)
	}
	for _, program := range h.programs {
		h.gl.deleteProgram(program.id)
	}
}

func (h *darwinHost) releaseNativeFrames() {
	for index := range h.nativeFrames {
		frame := &h.nativeFrames[index]
		if frame.producerFence != 0 {
			h.gl.deleteSync(frame.producerFence)
		}
		if frame.consumerFence != 0 {
			h.gl.deleteSync(frame.consumerFence)
		}
		if frame.texture != 0 {
			h.gl.deleteTextures(1, &frame.texture)
		}
		*frame = hostNativeFrame{}
	}
}

func (h *darwinHost) close() error {
	h.once.Do(func() {
		close(h.stop)
		<-h.done
	})
	return nil
}
