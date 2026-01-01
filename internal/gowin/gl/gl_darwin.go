//go:build darwin

package gl

import (
	"unsafe"

	"github.com/ebitengine/purego"
)

type openGL struct {
	clearColor    func(float32, float32, float32, float32)
	clear         func(uint32)
	viewport      func(int32, int32, int32, int32)
	enable        func(uint32)
	disable       func(uint32)
	genTextures   func(int32, *uint32)
	bindTexture   func(uint32, uint32)
	texImage2D    func(uint32, int32, int32, int32, int32, int32, uint32, uint32, uintptr)
	texSubImage2D func(uint32, int32, int32, int32, int32, int32, uint32, uint32, uintptr)
	texParameteri func(uint32, uint32, int32)
	pixelStorei   func(uint32, int32)
	activeTexture func(uint32)
	blendFunc     func(uint32, uint32)
	readPixels    func(int32, int32, int32, int32, uint32, uint32, uintptr)
	getString     func(uint32) *byte

	// Buffer operations
	genBuffers    func(int32, *uint32)
	deleteBuffers func(int32, *uint32)
	bindBuffer    func(uint32, uint32)
	bufferData    func(uint32, int, uintptr, uint32)
	bufferSubData func(uint32, int, int, uintptr)

	// VAO operations
	genVertexArrays         func(int32, *uint32)
	deleteVertexArrays      func(int32, *uint32)
	bindVertexArray         func(uint32)
	vertexAttribPointer     func(uint32, int32, uint32, bool, int32, uintptr)
	enableVertexAttribArray func(uint32)

	// Shader operations
	createShader     func(uint32) uint32
	shaderSource     func(uint32, int32, **byte, *int32)
	compileShader    func(uint32)
	getShaderiv      func(uint32, uint32, *int32)
	getShaderInfoLog func(uint32, int32, *int32, *byte)
	deleteShader     func(uint32)

	// Program operations
	createProgram     func() uint32
	attachShader      func(uint32, uint32)
	linkProgram       func(uint32)
	getProgramiv      func(uint32, uint32, *int32)
	getProgramInfoLog func(uint32, int32, *int32, *byte)
	useProgram        func(uint32)
	deleteProgram     func(uint32)

	// Uniform operations
	getUniformLocation func(uint32, *byte) int32
	getAttribLocation  func(uint32, *byte) int32
	uniform1i          func(int32, int32)
	uniform4f          func(int32, float32, float32, float32, float32)
	uniformMatrix4fv   func(int32, int32, bool, *float32)

	// Drawing
	drawArrays   func(uint32, int32, int32)
	drawElements func(uint32, int32, uint32, uintptr)
}

func (gl *openGL) ClearColor(r, g, b, a float32) {
	gl.clearColor(r, g, b, a)
}

func (gl *openGL) Clear(mask uint32) {
	gl.clear(mask)
}

func (gl *openGL) Viewport(x, y, width, height int32) {
	gl.viewport(x, y, width, height)
}

func (gl *openGL) Enable(cap uint32) {
	gl.enable(cap)
}

func (gl *openGL) Disable(cap uint32) {
	gl.disable(cap)
}

func (gl *openGL) GenTextures(n int32, textures *uint32) {
	gl.genTextures(n, textures)
}

func (gl *openGL) BindTexture(target, texture uint32) {
	gl.bindTexture(target, texture)
}

func (gl *openGL) TexImage2D(target uint32, level, internalFormat, width, height, border int32, format, xtype uint32, pixels unsafe.Pointer) {
	gl.texImage2D(target, level, internalFormat, width, height, border, format, xtype, uintptr(pixels))
}

func (gl *openGL) TexSubImage2D(target uint32, level, xoffset, yoffset, width, height int32, format, xtype uint32, pixels unsafe.Pointer) {
	gl.texSubImage2D(target, level, xoffset, yoffset, width, height, format, xtype, uintptr(pixels))
}

func (gl *openGL) TexParameteri(target, pname uint32, param int32) {
	gl.texParameteri(target, pname, param)
}

func (gl *openGL) PixelStorei(pname uint32, param int32) {
	gl.pixelStorei(pname, param)
}

func (gl *openGL) ActiveTexture(texture uint32) {
	gl.activeTexture(texture)
}

func (gl *openGL) BlendFunc(sfactor, dfactor uint32) {
	gl.blendFunc(sfactor, dfactor)
}

func (gl *openGL) ReadPixels(x, y, width, height int32, format, xtype uint32, pixels unsafe.Pointer) {
	// Note: On macOS, glReadPixels reads from the lower-left corner,
	// so we need to adjust the y coordinate accordingly.
	gl.readPixels(x, y, width, height, format, xtype, uintptr(pixels))
}

func (gl *openGL) GetString(name uint32) string {
	ptr := gl.getString(name)
	return gostring((*byte)(unsafe.Pointer(ptr)))
}

func (gl *openGL) GenBuffers(n int32, buffers *uint32) {
	gl.genBuffers(n, buffers)
}

func (gl *openGL) DeleteBuffers(n int32, buffers *uint32) {
	gl.deleteBuffers(n, buffers)
}

func (gl *openGL) BindBuffer(target uint32, buffer uint32) {
	gl.bindBuffer(target, buffer)
}

func (gl *openGL) BufferData(target uint32, size int, data unsafe.Pointer, usage uint32) {
	gl.bufferData(target, size, uintptr(data), usage)
}

func (gl *openGL) BufferSubData(target uint32, offset int, size int, data unsafe.Pointer) {
	gl.bufferSubData(target, offset, size, uintptr(data))
}

func (gl *openGL) GenVertexArrays(n int32, arrays *uint32) {
	gl.genVertexArrays(n, arrays)
}

func (gl *openGL) DeleteVertexArrays(n int32, arrays *uint32) {
	gl.deleteVertexArrays(n, arrays)
}

func (gl *openGL) BindVertexArray(array uint32) {
	gl.bindVertexArray(array)
}

func (gl *openGL) VertexAttribPointer(index uint32, size int32, xtype uint32, normalized bool, stride int32, offset uintptr) {
	gl.vertexAttribPointer(index, size, xtype, normalized, stride, offset)
}

func (gl *openGL) EnableVertexAttribArray(index uint32) {
	gl.enableVertexAttribArray(index)
}

func (gl *openGL) CreateShader(xtype uint32) uint32 {
	return gl.createShader(xtype)
}

func (gl *openGL) ShaderSource(shader uint32, source string) {
	srcBytes := []byte(source)
	srcPtr := &srcBytes[0]
	length := int32(len(source))
	gl.shaderSource(shader, 1, &srcPtr, &length)
}

func (gl *openGL) CompileShader(shader uint32) {
	gl.compileShader(shader)
}

func (gl *openGL) GetShaderiv(shader uint32, pname uint32, params *int32) {
	gl.getShaderiv(shader, pname, params)
}

func (gl *openGL) GetShaderInfoLog(shader uint32) string {
	var length int32
	gl.getShaderiv(shader, 0x8B84, &length) // INFO_LOG_LENGTH
	if length == 0 {
		return ""
	}
	log := make([]byte, length)
	gl.getShaderInfoLog(shader, length, &length, &log[0])
	return string(log[:length])
}

func (gl *openGL) DeleteShader(shader uint32) {
	gl.deleteShader(shader)
}

func (gl *openGL) CreateProgram() uint32 {
	return gl.createProgram()
}

func (gl *openGL) AttachShader(program uint32, shader uint32) {
	gl.attachShader(program, shader)
}

func (gl *openGL) LinkProgram(program uint32) {
	gl.linkProgram(program)
}

func (gl *openGL) GetProgramiv(program uint32, pname uint32, params *int32) {
	gl.getProgramiv(program, pname, params)
}

func (gl *openGL) GetProgramInfoLog(program uint32) string {
	var length int32
	gl.getProgramiv(program, 0x8B84, &length) // INFO_LOG_LENGTH
	if length == 0 {
		return ""
	}
	log := make([]byte, length)
	gl.getProgramInfoLog(program, length, &length, &log[0])
	return string(log[:length])
}

func (gl *openGL) UseProgram(program uint32) {
	gl.useProgram(program)
}

func (gl *openGL) DeleteProgram(program uint32) {
	gl.deleteProgram(program)
}

func (gl *openGL) GetUniformLocation(program uint32, name string) int32 {
	nameBytes := []byte(name)
	nameBytes = append(nameBytes, 0)
	return gl.getUniformLocation(program, &nameBytes[0])
}

func (gl *openGL) GetAttribLocation(program uint32, name string) int32 {
	nameBytes := []byte(name)
	nameBytes = append(nameBytes, 0)
	return gl.getAttribLocation(program, &nameBytes[0])
}

func (gl *openGL) Uniform1i(location int32, v0 int32) {
	gl.uniform1i(location, v0)
}

func (gl *openGL) Uniform4f(location int32, v0, v1, v2, v3 float32) {
	gl.uniform4f(location, v0, v1, v2, v3)
}

func (gl *openGL) UniformMatrix4fv(location int32, count int32, transpose bool, value *float32) {
	gl.uniformMatrix4fv(location, count, transpose, value)
}

func (gl *openGL) DrawArrays(mode uint32, first int32, count int32) {
	gl.drawArrays(mode, first, count)
}

func (gl *openGL) DrawElements(mode uint32, count int32, xtype uint32, indices uintptr) {
	gl.drawElements(mode, count, xtype, indices)
}

func Load() (OpenGL, error) {
	handle, err := purego.Dlopen("/System/Library/Frameworks/OpenGL.framework/OpenGL", purego.RTLD_GLOBAL|purego.RTLD_LAZY)
	if err != nil {
		return nil, err
	}
	register := func(dst interface{}, name string) {
		purego.RegisterLibFunc(dst, handle, name)
	}

	gl := &openGL{}
	register(&gl.clearColor, "glClearColor")
	register(&gl.clear, "glClear")
	register(&gl.viewport, "glViewport")
	register(&gl.enable, "glEnable")
	register(&gl.disable, "glDisable")
	register(&gl.genTextures, "glGenTextures")
	register(&gl.bindTexture, "glBindTexture")
	register(&gl.texImage2D, "glTexImage2D")
	register(&gl.texSubImage2D, "glTexSubImage2D")
	register(&gl.texParameteri, "glTexParameteri")
	register(&gl.pixelStorei, "glPixelStorei")
	register(&gl.activeTexture, "glActiveTexture")
	register(&gl.blendFunc, "glBlendFunc")
	register(&gl.readPixels, "glReadPixels")
	register(&gl.getString, "glGetString")

	// GL3 functions
	register(&gl.genBuffers, "glGenBuffers")
	register(&gl.deleteBuffers, "glDeleteBuffers")
	register(&gl.bindBuffer, "glBindBuffer")
	register(&gl.bufferData, "glBufferData")
	register(&gl.bufferSubData, "glBufferSubData")
	register(&gl.genVertexArrays, "glGenVertexArrays")
	register(&gl.deleteVertexArrays, "glDeleteVertexArrays")
	register(&gl.bindVertexArray, "glBindVertexArray")
	register(&gl.vertexAttribPointer, "glVertexAttribPointer")
	register(&gl.enableVertexAttribArray, "glEnableVertexAttribArray")
	register(&gl.createShader, "glCreateShader")
	register(&gl.shaderSource, "glShaderSource")
	register(&gl.compileShader, "glCompileShader")
	register(&gl.getShaderiv, "glGetShaderiv")
	register(&gl.getShaderInfoLog, "glGetShaderInfoLog")
	register(&gl.deleteShader, "glDeleteShader")
	register(&gl.createProgram, "glCreateProgram")
	register(&gl.attachShader, "glAttachShader")
	register(&gl.linkProgram, "glLinkProgram")
	register(&gl.getProgramiv, "glGetProgramiv")
	register(&gl.getProgramInfoLog, "glGetProgramInfoLog")
	register(&gl.useProgram, "glUseProgram")
	register(&gl.deleteProgram, "glDeleteProgram")
	register(&gl.getUniformLocation, "glGetUniformLocation")
	register(&gl.getAttribLocation, "glGetAttribLocation")
	register(&gl.uniform1i, "glUniform1i")
	register(&gl.uniform4f, "glUniform4f")
	register(&gl.uniformMatrix4fv, "glUniformMatrix4fv")
	register(&gl.drawArrays, "glDrawArrays")
	register(&gl.drawElements, "glDrawElements")

	return gl, nil
}
