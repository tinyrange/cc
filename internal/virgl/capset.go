package virgl

import "encoding/binary"

const (
	capsetVirGL   = 1
	capsetVersion = 1
	capsetV1Size  = 308
)

func buildCapsetV1() []byte {
	data := make([]byte, capsetV1Size)
	put := func(offset int, value uint32) {
		binary.LittleEndian.PutUint32(data[offset:offset+4], value)
	}
	setFormat := func(maskOffset int, format uint32) {
		word := format / 32
		bit := format % 32
		offset := maskOffset + int(word)*4
		put(offset, binary.LittleEndian.Uint32(data[offset:offset+4])|(1<<bit))
	}

	// struct virgl_caps_v1. Keep this deliberately small: every advertised
	// format and limit must have a decoder/backend implementation.
	put(0, capsetVersion)
	const (
		samplerMaskOffset      = 4
		renderMaskOffset       = samplerMaskOffset + 64
		depthStencilMaskOffset = renderMaskOffset + 64
		vertexBufferMaskOffset = depthStencilMaskOffset + 64
		booleanSetOffset       = vertexBufferMaskOffset + 64
		glslLevelOffset        = booleanSetOffset + 4
	)
	for _, format := range []uint32{1, 2, 67, 68, 121, 134} {
		setFormat(samplerMaskOffset, format)
		setFormat(renderMaskOffset, format)
	}
	for _, format := range []uint32{16, 18, 19, 21} {
		// Mesa derives depth-bearing EGL configs from the ordinary sampler and
		// render support masks. The legacy depthstencil mask alone is not
		// sufficient to expose those configs.
		setFormat(samplerMaskOffset, format)
		setFormat(renderMaskOffset, format)
		setFormat(depthStencilMaskOffset, format)
	}
	for _, format := range []uint32{28, 29, 30, 31, 64, 65, 66, 67} {
		setFormat(vertexBufferMaskOffset, format)
	}
	// primitive_restart and blend_eq_sep.
	put(booleanSetOffset, (1<<6)|(1<<7))
	put(glslLevelOffset+0, 150)     // GLSL 1.50
	put(glslLevelOffset+4, 1)       // max texture array layers
	put(glslLevelOffset+8, 0)       // max streamout buffers
	put(glslLevelOffset+12, 0)      // max dual-source render targets
	put(glslLevelOffset+16, 1)      // max render targets
	put(glslLevelOffset+20, 1)      // max samples
	put(glslLevelOffset+24, 0x3fff) // points through triangle fan
	put(glslLevelOffset+28, 0)      // max TBO size
	put(glslLevelOffset+32, 0)      // max uniform blocks
	put(glslLevelOffset+36, 1)      // max viewports
	put(glslLevelOffset+40, 0)      // max texture gather components
	return data
}
