//go:build linux

package gl

import (
	"unsafe"

	"github.com/ebitengine/purego"
)

// The Linux loader uses glXGetProcAddressARB to load OpenGL 3.0+ functions.
type openGL struct {
	clearColor    func(float32, float32, float32, float32)
	clear         func(uint32)
	viewport      func(int32, int32, int32, int32)
	enable        func(uint32)
	disable       func(uint32)
	genTextures   func(int32, *uint32)
	bindTexture   func(uint32, uint32)
	texImage2D    func(uint32, int32, int32, int32, int32, int32, uint32, uint32, unsafe.Pointer)
	texSubImage2D func(uint32, int32, int32, int32, int32, int32, uint32, uint32, unsafe.Pointer)
	texParameteri func(uint32, uint32, int32)
	pixelStorei   func(uint32, int32)
	activeTexture func(uint32)
	blendFunc     func(uint32, uint32)
	readPixels    func(int32, int32, int32, int32, uint32, uint32, unsafe.Pointer)
	getString     func(uint32) *byte

	// Buffer operations
	genBuffers    func(int32, *uint32)
	deleteBuffers func(int32, *uint32)
	bindBuffer    func(uint32, uint32)
	bufferData    func(uint32, int, unsafe.Pointer, uint32)
	bufferSubData func(uint32, int, int, unsafe.Pointer)

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

	// Proc address function
	getProcAddress func(*byte) unsafe.Pointer
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
	gl.texImage2D(target, level, internalFormat, width, height, border, format, xtype, pixels)
}

func (gl *openGL) TexSubImage2D(target uint32, level, xoffset, yoffset, width, height int32, format, xtype uint32, pixels unsafe.Pointer) {
	gl.texSubImage2D(target, level, xoffset, yoffset, width, height, format, xtype, pixels)
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
	gl.readPixels(x, y, width, height, format, xtype, pixels)
}

func (gl *openGL) GetString(name uint32) string {
	ptr := gl.getString(name)
	return gostring(ptr)
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
	gl.bufferData(target, size, data, usage)
}

func (gl *openGL) BufferSubData(target uint32, offset int, size int, data unsafe.Pointer) {
	gl.bufferSubData(target, offset, size, data)
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
	handle, err := purego.Dlopen("libGL.so.1", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, err
	}

	// Load glXGetProcAddressARB
	var glXGetProcAddressARB func(*byte) unsafe.Pointer
	if _, err := purego.Dlsym(handle, "glXGetProcAddressARB"); err == nil {
		purego.RegisterLibFunc(&glXGetProcAddressARB, handle, "glXGetProcAddressARB")
	} else {
		// Fallback: try glXGetProcAddress
		if _, err := purego.Dlsym(handle, "glXGetProcAddress"); err == nil {
			purego.RegisterLibFunc(&glXGetProcAddressARB, handle, "glXGetProcAddress")
		} else {
			return nil, err
		}
	}

	// Helper to load a function via glXGetProcAddressARB
	loadFunc := func(name string) unsafe.Pointer {
		nameBytes := []byte(name)
		nameBytes = append(nameBytes, 0)
		return glXGetProcAddressARB(&nameBytes[0])
	}

	gl := &openGL{}
	gl.getProcAddress = glXGetProcAddressARB

	// Load basic functions - try libGL first, then glXGetProcAddressARB
	register := func(dst interface{}, name string) {
		if _, err := purego.Dlsym(handle, name); err == nil {
			purego.RegisterLibFunc(dst, handle, name)
		} else {
			// Try via glXGetProcAddressARB
			ptr := loadFunc(name)
			if ptr != nil {
				purego.RegisterFunc(dst, uintptr(ptr))
			}
		}
	}

	// Load all functions
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

	// Load GL3 functions via glXGetProcAddressARB
	purego.RegisterFunc(&gl.genBuffers, uintptr(loadFunc("glGenBuffers")))
	purego.RegisterFunc(&gl.deleteBuffers, uintptr(loadFunc("glDeleteBuffers")))
	purego.RegisterFunc(&gl.bindBuffer, uintptr(loadFunc("glBindBuffer")))
	purego.RegisterFunc(&gl.bufferData, uintptr(loadFunc("glBufferData")))
	purego.RegisterFunc(&gl.bufferSubData, uintptr(loadFunc("glBufferSubData")))
	purego.RegisterFunc(&gl.genVertexArrays, uintptr(loadFunc("glGenVertexArrays")))
	purego.RegisterFunc(&gl.deleteVertexArrays, uintptr(loadFunc("glDeleteVertexArrays")))
	purego.RegisterFunc(&gl.bindVertexArray, uintptr(loadFunc("glBindVertexArray")))
	purego.RegisterFunc(&gl.vertexAttribPointer, uintptr(loadFunc("glVertexAttribPointer")))
	purego.RegisterFunc(&gl.enableVertexAttribArray, uintptr(loadFunc("glEnableVertexAttribArray")))
	purego.RegisterFunc(&gl.createShader, uintptr(loadFunc("glCreateShader")))
	purego.RegisterFunc(&gl.shaderSource, uintptr(loadFunc("glShaderSource")))
	purego.RegisterFunc(&gl.compileShader, uintptr(loadFunc("glCompileShader")))
	purego.RegisterFunc(&gl.getShaderiv, uintptr(loadFunc("glGetShaderiv")))
	purego.RegisterFunc(&gl.getShaderInfoLog, uintptr(loadFunc("glGetShaderInfoLog")))
	purego.RegisterFunc(&gl.deleteShader, uintptr(loadFunc("glDeleteShader")))
	purego.RegisterFunc(&gl.createProgram, uintptr(loadFunc("glCreateProgram")))
	purego.RegisterFunc(&gl.attachShader, uintptr(loadFunc("glAttachShader")))
	purego.RegisterFunc(&gl.linkProgram, uintptr(loadFunc("glLinkProgram")))
	purego.RegisterFunc(&gl.getProgramiv, uintptr(loadFunc("glGetProgramiv")))
	purego.RegisterFunc(&gl.getProgramInfoLog, uintptr(loadFunc("glGetProgramInfoLog")))
	purego.RegisterFunc(&gl.useProgram, uintptr(loadFunc("glUseProgram")))
	purego.RegisterFunc(&gl.deleteProgram, uintptr(loadFunc("glDeleteProgram")))
	purego.RegisterFunc(&gl.getUniformLocation, uintptr(loadFunc("glGetUniformLocation")))
	purego.RegisterFunc(&gl.getAttribLocation, uintptr(loadFunc("glGetAttribLocation")))
	purego.RegisterFunc(&gl.uniform1i, uintptr(loadFunc("glUniform1i")))
	purego.RegisterFunc(&gl.uniform4f, uintptr(loadFunc("glUniform4f")))
	purego.RegisterFunc(&gl.uniformMatrix4fv, uintptr(loadFunc("glUniformMatrix4fv")))
	purego.RegisterFunc(&gl.drawArrays, uintptr(loadFunc("glDrawArrays")))
	purego.RegisterFunc(&gl.drawElements, uintptr(loadFunc("glDrawElements")))

	return gl, nil
}
