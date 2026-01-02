package gl

import "unsafe"

const (
	// ColorBufferBit is a mask used with Clear to clear the color buffer.
	ColorBufferBit = 0x00004000

	// Texture2D is the texture target for 2D textures.
	Texture2D = 0x0DE1

	// UnpackAlignment specifies the alignment requirements for pixel data
	// when uploading textures (PixelStorei).
	UnpackAlignment = 0x0CF5

	// Intensity is a legacy internal texture format.
	Intensity = 0x8049

	// TextureWrapS selects the wrapping function for texture coordinate S.
	TextureWrapS = 0x2802
	// TextureWrapT selects the wrapping function for texture coordinate T.
	TextureWrapT = 0x2803

	// TextureMinFilter selects the texture minification filter.
	TextureMinFilter = 0x2801
	// TextureMagFilter selects the texture magnification filter.
	TextureMagFilter = 0x2800

	// Nearest selects nearest-neighbor filtering.
	Nearest = 0x2600
	// Linear selects linear filtering.
	Linear = 0x2601

	// ClampToEdge clamps texture coordinates to the edge of the texture.
	ClampToEdge = 0x812F

	// Alpha is a legacy pixel format representing alpha only.
	Alpha = 0x1906
	// RGBA is a pixel format representing red/green/blue/alpha.
	RGBA = 0x1908
	// Red is a pixel format representing red only (OpenGL 3.0+).
	Red = 0x1903
	// R8 is an internal texture format for 8-bit red channel (OpenGL 3.0+).
	R8 = 0x8229

	// UnsignedByte is a pixel data type indicating 8-bit unsigned values.
	UnsignedByte = 0x1401
	// UnsignedInt is a data type indicating 32-bit unsigned integer values.
	UnsignedInt = 0x1405
	// Float is a data type indicating 32-bit floating point values.
	Float = 0x1406

	// Triangles is a primitive type for drawing triangles.
	Triangles = 0x0004
	// TriangleStrip is a primitive type for drawing a connected strip of triangles.
	TriangleStrip = 0x0005

	// ArrayBuffer is the target for vertex buffer objects.
	ArrayBuffer = 0x8892
	// ElementArrayBuffer is the target for index (element) buffer objects.
	ElementArrayBuffer = 0x8893
	// StaticDraw indicates that buffer data will be modified once and used many times.
	StaticDraw = 0x88E4
	// DynamicDraw indicates that buffer data will be modified repeatedly and used many times.
	DynamicDraw = 0x88E8

	// Shader types
	VertexShader   = 0x8B31
	FragmentShader = 0x8B30

	// Shader/Program status
	CompileStatus = 0x8B81
	LinkStatus    = 0x8B82

	// Texture unit
	Texture0 = 0x84C0

	// Blending capabilities and factors.
	Blend            = 0x0BE2
	SrcAlpha         = 0x0302
	OneMinusSrcAlpha = 0x0303

	// Texture formats.
	LuminanceAlpha = 0x190A

	// GetString parameters.
	//
	// Vendor returns the company responsible for the GL implementation.
	Vendor = 0x1F00
	// Version returns the GL version string of the current context.
	Version = 0x1F02
)

// OpenGL describes the subset of OpenGL entry points used by this package.
//
// Implementations typically wrap platform-specific GL bindings. All methods are
// expected to operate on the currently current GL context for the calling thread.
type OpenGL interface {
	// ClearColor sets the clear color used by Clear when clearing the color buffer.
	ClearColor(r, g, b, a float32)

	// Clear clears buffers to preset values (e.g., ColorBufferBit).
	Clear(mask uint32)

	// Viewport sets the affine transformation of x and y from normalized device
	// coordinates to window coordinates.
	Viewport(x, y, width, height int32)

	// Enable enables a server-side GL capability (e.g., Blend).
	Enable(cap uint32)

	// Disable disables a server-side GL capability.
	Disable(cap uint32)

	// GenTextures generates texture object names.
	GenTextures(n int32, textures *uint32)

	// BindTexture binds a named texture to a texturing target (e.g., Texture2D).
	BindTexture(target, texture uint32)

	// TexImage2D specifies a two-dimensional texture image.
	//
	// The pixels pointer may be nil to allocate storage without uploading data.
	TexImage2D(
		target uint32,
		level int32,
		internalformat int32,
		width int32,
		height int32,
		border int32,
		format uint32,
		xtype uint32,
		pixels unsafe.Pointer,
	)

	// TexSubImage2D specifies a sub-region of an existing two-dimensional texture image.
	TexSubImage2D(
		target uint32,
		level int32,
		xoffset int32,
		yoffset int32,
		width int32,
		height int32,
		format uint32,
		xtype uint32,
		pixels unsafe.Pointer,
	)

	// TexParameteri sets texture parameters for the currently bound texture.
	TexParameteri(target, pname uint32, param int32)

	// PixelStorei sets pixel storage modes (e.g., UnpackAlignment).
	PixelStorei(pname uint32, param int32)

	// ActiveTexture selects the active texture unit.
	ActiveTexture(texture uint32)

	// BlendFunc specifies the pixel arithmetic for blending (e.g., SrcAlpha and OneMinusSrcAlpha).
	BlendFunc(sfactor, dfactor uint32)

	// Buffer operations
	GenBuffers(n int32, buffers *uint32)
	DeleteBuffers(n int32, buffers *uint32)
	BindBuffer(target uint32, buffer uint32)
	BufferData(target uint32, size int, data unsafe.Pointer, usage uint32)
	BufferSubData(target uint32, offset int, size int, data unsafe.Pointer)

	// Vertex Array Object operations
	GenVertexArrays(n int32, arrays *uint32)
	DeleteVertexArrays(n int32, arrays *uint32)
	BindVertexArray(array uint32)
	VertexAttribPointer(index uint32, size int32, xtype uint32, normalized bool, stride int32, offset uintptr)
	EnableVertexAttribArray(index uint32)

	// Shader operations
	CreateShader(xtype uint32) uint32
	ShaderSource(shader uint32, source string)
	CompileShader(shader uint32)
	GetShaderiv(shader uint32, pname uint32, params *int32)
	GetShaderInfoLog(shader uint32) string
	DeleteShader(shader uint32)

	// Program operations
	CreateProgram() uint32
	AttachShader(program uint32, shader uint32)
	LinkProgram(program uint32)
	GetProgramiv(program uint32, pname uint32, params *int32)
	GetProgramInfoLog(program uint32) string
	UseProgram(program uint32)
	DeleteProgram(program uint32)

	// Uniform operations
	GetUniformLocation(program uint32, name string) int32
	// GetAttribLocation returns the location of an attribute variable.
	GetAttribLocation(program uint32, name string) int32
	Uniform1i(location int32, v0 int32)
	Uniform4f(location int32, v0, v1, v2, v3 float32)
	UniformMatrix4fv(location int32, count int32, transpose bool, value *float32)

	// Drawing
	DrawArrays(mode uint32, first int32, count int32)
	DrawElements(mode uint32, count int32, xtype uint32, indices uintptr)

	// ReadPixels reads a block of pixels from the framebuffer into client memory.
	ReadPixels(
		x int32,
		y int32,
		width int32,
		height int32,
		format uint32,
		xtype uint32,
		pixels unsafe.Pointer,
	)

	// GetString returns a string describing a GL property for the current context.
	//
	// Common names are Vendor and Version.
	// If the name is not recognized or no context is current, implementations may
	// return the empty string.
	GetString(name uint32) string
}

func gostring(ptr *byte) string {
	if ptr == nil {
		return ""
	}
	var bytes []byte
	for p := ptr; *p != 0; p = (*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + 1)) {
		bytes = append(bytes, *p)
	}
	return string(bytes)
}
