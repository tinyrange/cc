package virgl

import (
	"encoding/binary"
	"math"
)

const (
	capsetVirGL         = 1
	capsetVirGL2        = 2
	capsetVersion       = 1
	capsetVersion2      = 2
	capsetV1Size        = 308
	capsetV2Size        = 1376
	capsetV2LimitsStart = capsetV1Size
	capsetMaxTexture2D  = 4096
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
	for _, format := range []uint32{16, 18, 19, 20, 21} {
		// Mesa derives depth-bearing EGL configs from the ordinary sampler and
		// render support masks. The legacy depthstencil mask alone is not
		// sufficient to expose those configs.
		setFormat(samplerMaskOffset, format)
		setFormat(renderMaskOffset, format)
		setFormat(depthStencilMaskOffset, format)
	}
	for _, format := range []uint32{28, 29, 30, 31, 64, 65, 66, 67, 87, 88, 89, 90} {
		setFormat(vertexBufferMaskOffset, format)
	}
	// primitive_restart and blend_eq_sep.
	put(booleanSetOffset, (1<<6)|(1<<7))
	put(glslLevelOffset+0, 150)     // GLSL 1.50
	put(glslLevelOffset+4, 256)     // max texture array layers
	put(glslLevelOffset+8, 0)       // max streamout buffers
	put(glslLevelOffset+12, 0)      // max dual-source render targets
	put(glslLevelOffset+16, 4)      // max render targets
	put(glslLevelOffset+20, 1)      // max samples
	put(glslLevelOffset+24, 0x3fff) // points through triangle fan
	put(glslLevelOffset+28, 0)      // max TBO size
	put(glslLevelOffset+32, 12)     // max uniform blocks
	put(glslLevelOffset+36, 1)      // max viewports
	put(glslLevelOffset+40, 0)      // max texture gather components
	return data
}

func buildCapsetV2() []byte {
	data := make([]byte, capsetV2Size)
	copy(data, buildCapsetV1())
	put := func(offset int, value uint32) {
		binary.LittleEndian.PutUint32(data[offset:offset+4], value)
	}
	putFloat := func(offset int, value float32) {
		put(offset, math.Float32bits(value))
	}
	setFormat := func(maskOffset int, format uint32) {
		word := format / 32
		bit := format % 32
		offset := maskOffset + int(word)*4
		put(offset, binary.LittleEndian.Uint32(data[offset:offset+4])|(1<<bit))
	}

	// struct virgl_caps_v2 extends v1. This first profile intentionally adds
	// the extensible ABI and measured limits without claiming any new command
	// family. Later GL 3.x/4.x slices can raise individual fields only after
	// their decoder, TGSI, host GL, transfer, and conformance paths exist.
	put(0, capsetVersion2)
	const (
		minAliasedPointSizeOffset = capsetV2LimitsStart
		maxAliasedPointSizeOffset = minAliasedPointSizeOffset + 4
		minSmoothPointSizeOffset  = maxAliasedPointSizeOffset + 4
		maxSmoothPointSizeOffset  = minSmoothPointSizeOffset + 4
		minAliasedLineWidthOffset = maxSmoothPointSizeOffset + 4
		maxAliasedLineWidthOffset = minAliasedLineWidthOffset + 4
		minSmoothLineWidthOffset  = maxAliasedLineWidthOffset + 4
		maxSmoothLineWidthOffset  = minSmoothLineWidthOffset + 4
		maxTextureLODBiasOffset   = maxSmoothLineWidthOffset + 4
		maxVertexOutputsOffset    = maxTextureLODBiasOffset + 4 + 8
		maxVertexAttribsOffset    = maxVertexOutputsOffset + 4
		maxTexture2DSizeOffset    = capsetV2LimitsStart + 176
		hostFeatureVersionOffset  = capsetV2LimitsStart + 248
		readbackFormatsOffset     = hostFeatureVersionOffset + 4
		rendererOffset            = capsetV2LimitsStart + 388
		maxAnisotropyOffset       = rendererOffset + 64
		maxShaderSamplersOffset   = maxAnisotropyOffset + 4
	)
	putFloat(minAliasedPointSizeOffset, 1)
	putFloat(maxAliasedPointSizeOffset, 64)
	putFloat(minSmoothPointSizeOffset, 1)
	putFloat(maxSmoothPointSizeOffset, 64)
	putFloat(minAliasedLineWidthOffset, 1)
	putFloat(maxAliasedLineWidthOffset, 1)
	putFloat(minSmoothLineWidthOffset, 1)
	putFloat(maxSmoothLineWidthOffset, 1)
	putFloat(maxTextureLODBiasOffset, 16)
	put(maxVertexOutputsOffset, 16)
	put(maxVertexAttribsOffset, 16)
	put(maxTexture2DSizeOffset, capsetMaxTexture2D)
	// Zero is deliberate: modern host-feature fallbacks stay disabled until
	// the corresponding Gallium command behavior is implemented and tested.
	put(hostFeatureVersionOffset, 0)
	for _, format := range []uint32{1, 2, 67, 68, 121, 134} {
		setFormat(readbackFormatsOffset, format)
	}
	copy(data[rendererOffset:rendererOffset+64], []byte("vmsh Darwin VirGL"))
	putFloat(maxAnisotropyOffset, 1)
	put(maxShaderSamplersOffset, 16)
	return data
}
