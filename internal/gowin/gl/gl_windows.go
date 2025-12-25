//go:build windows

package gl

import (
	"math"
	"syscall"
	"unsafe"
)

type Proc interface {
	Call(args ...uintptr) (uintptr, uintptr, error)
}

type openglProc uintptr

func (p openglProc) Call(args ...uintptr) (uintptr, uintptr, error) {
	return syscall.SyscallN(uintptr(p), args...)
}

type openGL struct {
	clearColor    Proc
	clear         Proc
	viewport      Proc
	enable        Proc
	disable       Proc
	genTextures   Proc
	bindTexture   Proc
	texImage2D    Proc
	texSubImage2D Proc
	texParameteri Proc
	pixelStorei   Proc
	activeTexture Proc
	blendFunc     Proc
	readPixels    Proc
	getString     Proc

	// Buffer operations
	genBuffers    Proc
	deleteBuffers Proc
	bindBuffer    Proc
	bufferData    Proc
	bufferSubData Proc

	// VAO operations
	genVertexArrays         Proc
	deleteVertexArrays      Proc
	bindVertexArray         Proc
	vertexAttribPointer     Proc
	enableVertexAttribArray Proc

	// Shader operations
	createShader     Proc
	shaderSource     Proc
	compileShader    Proc
	getShaderiv      Proc
	getShaderInfoLog Proc
	deleteShader     Proc

	// Program operations
	createProgram     Proc
	attachShader      Proc
	linkProgram       Proc
	getProgramiv      Proc
	getProgramInfoLog Proc
	useProgram        Proc
	deleteProgram     Proc

	// Uniform operations
	getUniformLocation Proc
	getAttribLocation  Proc
	uniform1i          Proc
	uniform4f          Proc
	uniformMatrix4fv   Proc

	// Drawing
	drawArrays Proc
}

func (gl *openGL) ClearColor(r, g, b, a float32) {
	gl.clearColor.Call(f32(r), f32(g), f32(b), f32(a))
}

func (gl *openGL) Clear(mask uint32) {
	gl.clear.Call(uintptr(mask))
}

func (gl *openGL) Viewport(x, y, width, height int32) {
	gl.viewport.Call(uintptr(x), uintptr(y), uintptr(width), uintptr(height))
}

func (gl *openGL) Enable(cap uint32) {
	gl.enable.Call(uintptr(cap))
}

func (gl *openGL) Disable(cap uint32) {
	gl.disable.Call(uintptr(cap))
}

func (gl *openGL) GenTextures(n int32, textures *uint32) {
	gl.genTextures.Call(uintptr(n), uintptr(unsafe.Pointer(textures)))
}

func (gl *openGL) BindTexture(target, texture uint32) {
	gl.bindTexture.Call(uintptr(target), uintptr(texture))
}

func (gl *openGL) TexImage2D(target uint32, level, internalFormat, width, height, border int32, format, xtype uint32, pixels unsafe.Pointer) {
	gl.texImage2D.Call(uintptr(target), uintptr(level), uintptr(internalFormat), uintptr(width), uintptr(height), uintptr(border), uintptr(format), uintptr(xtype), uintptr(pixels))
}

func (gl *openGL) TexSubImage2D(target uint32, level, xoffset, yoffset, width, height int32, format, xtype uint32, pixels unsafe.Pointer) {
	gl.texSubImage2D.Call(uintptr(target), uintptr(level), uintptr(xoffset), uintptr(yoffset), uintptr(width), uintptr(height), uintptr(format), uintptr(xtype), uintptr(pixels))
}

func (gl *openGL) TexParameteri(target, pname uint32, param int32) {
	gl.texParameteri.Call(uintptr(target), uintptr(pname), uintptr(param))
}

func (gl *openGL) PixelStorei(pname uint32, param int32) {
	gl.pixelStorei.Call(uintptr(pname), uintptr(param))
}

func (gl *openGL) ActiveTexture(texture uint32) {
	gl.activeTexture.Call(uintptr(texture))
}

func (gl *openGL) BlendFunc(sfactor, dfactor uint32) {
	gl.blendFunc.Call(uintptr(sfactor), uintptr(dfactor))
}

func (gl *openGL) ReadPixels(x, y, width, height int32, format, xtype uint32, pixels unsafe.Pointer) {
	gl.readPixels.Call(uintptr(x), uintptr(y), uintptr(width), uintptr(height), uintptr(format), uintptr(xtype), uintptr(pixels))
}

func (gl *openGL) GetString(name uint32) string {
	ptr, _, _ := gl.getString.Call(uintptr(name))
	return gostring((*byte)(unsafe.Pointer(ptr)))
}

func (gl *openGL) GenBuffers(n int32, buffers *uint32) {
	gl.genBuffers.Call(uintptr(n), uintptr(unsafe.Pointer(buffers)))
}

func (gl *openGL) DeleteBuffers(n int32, buffers *uint32) {
	gl.deleteBuffers.Call(uintptr(n), uintptr(unsafe.Pointer(buffers)))
}

func (gl *openGL) BindBuffer(target uint32, buffer uint32) {
	gl.bindBuffer.Call(uintptr(target), uintptr(buffer))
}

func (gl *openGL) BufferData(target uint32, size int, data unsafe.Pointer, usage uint32) {
	gl.bufferData.Call(uintptr(target), uintptr(size), uintptr(data), uintptr(usage))
}

func (gl *openGL) BufferSubData(target uint32, offset int, size int, data unsafe.Pointer) {
	gl.bufferSubData.Call(uintptr(target), uintptr(offset), uintptr(size), uintptr(data))
}

func (gl *openGL) GenVertexArrays(n int32, arrays *uint32) {
	gl.genVertexArrays.Call(uintptr(n), uintptr(unsafe.Pointer(arrays)))
}

func (gl *openGL) DeleteVertexArrays(n int32, arrays *uint32) {
	gl.deleteVertexArrays.Call(uintptr(n), uintptr(unsafe.Pointer(arrays)))
}

func (gl *openGL) BindVertexArray(array uint32) {
	gl.bindVertexArray.Call(uintptr(array))
}

func (gl *openGL) VertexAttribPointer(index uint32, size int32, xtype uint32, normalized bool, stride int32, offset unsafe.Pointer) {
	var norm uintptr
	if normalized {
		norm = 1
	}
	gl.vertexAttribPointer.Call(uintptr(index), uintptr(size), uintptr(xtype), norm, uintptr(stride), uintptr(offset))
}

func (gl *openGL) EnableVertexAttribArray(index uint32) {
	gl.enableVertexAttribArray.Call(uintptr(index))
}

func (gl *openGL) CreateShader(xtype uint32) uint32 {
	ret, _, _ := gl.createShader.Call(uintptr(xtype))
	return uint32(ret)
}

func (gl *openGL) ShaderSource(shader uint32, source string) {
	srcBytes := []byte(source)
	srcPtr := &srcBytes[0]
	length := int32(len(source))
	gl.shaderSource.Call(uintptr(shader), 1, uintptr(unsafe.Pointer(&srcPtr)), uintptr(unsafe.Pointer(&length)))
}

func (gl *openGL) CompileShader(shader uint32) {
	gl.compileShader.Call(uintptr(shader))
}

func (gl *openGL) GetShaderiv(shader uint32, pname uint32, params *int32) {
	gl.getShaderiv.Call(uintptr(shader), uintptr(pname), uintptr(unsafe.Pointer(params)))
}

func (gl *openGL) GetShaderInfoLog(shader uint32) string {
	var length int32
	gl.getShaderiv.Call(uintptr(shader), 0x8B84, uintptr(unsafe.Pointer(&length))) // INFO_LOG_LENGTH
	if length == 0 {
		return ""
	}
	log := make([]byte, length)
	var logLen int32
	gl.getShaderInfoLog.Call(uintptr(shader), uintptr(length), uintptr(unsafe.Pointer(&logLen)), uintptr(unsafe.Pointer(&log[0])))
	return string(log[:logLen])
}

func (gl *openGL) DeleteShader(shader uint32) {
	gl.deleteShader.Call(uintptr(shader))
}

func (gl *openGL) CreateProgram() uint32 {
	ret, _, _ := gl.createProgram.Call()
	return uint32(ret)
}

func (gl *openGL) AttachShader(program uint32, shader uint32) {
	gl.attachShader.Call(uintptr(program), uintptr(shader))
}

func (gl *openGL) LinkProgram(program uint32) {
	gl.linkProgram.Call(uintptr(program))
}

func (gl *openGL) GetProgramiv(program uint32, pname uint32, params *int32) {
	gl.getProgramiv.Call(uintptr(program), uintptr(pname), uintptr(unsafe.Pointer(params)))
}

func (gl *openGL) GetProgramInfoLog(program uint32) string {
	var length int32
	gl.getProgramiv.Call(uintptr(program), 0x8B84, uintptr(unsafe.Pointer(&length))) // INFO_LOG_LENGTH
	if length == 0 {
		return ""
	}
	log := make([]byte, length)
	var logLen int32
	gl.getProgramInfoLog.Call(uintptr(program), uintptr(length), uintptr(unsafe.Pointer(&logLen)), uintptr(unsafe.Pointer(&log[0])))
	return string(log[:logLen])
}

func (gl *openGL) UseProgram(program uint32) {
	gl.useProgram.Call(uintptr(program))
}

func (gl *openGL) DeleteProgram(program uint32) {
	gl.deleteProgram.Call(uintptr(program))
}

func (gl *openGL) GetUniformLocation(program uint32, name string) int32 {
	nameBytes := []byte(name)
	nameBytes = append(nameBytes, 0)
	ret, _, _ := gl.getUniformLocation.Call(uintptr(program), uintptr(unsafe.Pointer(&nameBytes[0])))
	return int32(ret)
}

func (gl *openGL) GetAttribLocation(program uint32, name string) int32 {
	nameBytes := []byte(name)
	nameBytes = append(nameBytes, 0)
	ret, _, _ := gl.getAttribLocation.Call(uintptr(program), uintptr(unsafe.Pointer(&nameBytes[0])))
	return int32(ret)
}

func (gl *openGL) Uniform1i(location int32, v0 int32) {
	gl.uniform1i.Call(uintptr(location), uintptr(v0))
}

func (gl *openGL) Uniform4f(location int32, v0, v1, v2, v3 float32) {
	gl.uniform4f.Call(uintptr(location), f32(v0), f32(v1), f32(v2), f32(v3))
}

func (gl *openGL) UniformMatrix4fv(location int32, count int32, transpose bool, value *float32) {
	var trans uintptr
	if transpose {
		trans = 1
	}
	gl.uniformMatrix4fv.Call(uintptr(location), uintptr(count), trans, uintptr(unsafe.Pointer(value)))
}

func (gl *openGL) DrawArrays(mode uint32, first int32, count int32) {
	gl.drawArrays.Call(uintptr(mode), uintptr(first), uintptr(count))
}

func Load() (OpenGL, error) {
	opengl32 := syscall.NewLazyDLL("opengl32.dll")
	wglGetProcAddress := opengl32.NewProc("wglGetProcAddress")

	// Helper to load a function via wglGetProcAddress or opengl32
	loadProc := func(name string) Proc {
		// Try wglGetProcAddress first (for GL3+ functions)
		nameBytes := []byte(name)
		nameBytes = append(nameBytes, 0)
		ptr, _, _ := wglGetProcAddress.Call(uintptr(unsafe.Pointer(&nameBytes[0])))
		if ptr != 0 {
			return openglProc(ptr)
		} else {
			// Fallback to opengl32.dll
			return opengl32.NewProc(name)
		}
	}

	gl := &openGL{
		clearColor:    opengl32.NewProc("glClearColor"),
		clear:         opengl32.NewProc("glClear"),
		viewport:      opengl32.NewProc("glViewport"),
		enable:        opengl32.NewProc("glEnable"),
		disable:       opengl32.NewProc("glDisable"),
		genTextures:   opengl32.NewProc("glGenTextures"),
		bindTexture:   opengl32.NewProc("glBindTexture"),
		texImage2D:    opengl32.NewProc("glTexImage2D"),
		texSubImage2D: opengl32.NewProc("glTexSubImage2D"),
		texParameteri: opengl32.NewProc("glTexParameteri"),
		pixelStorei:   opengl32.NewProc("glPixelStorei"),
		activeTexture: loadProc("glActiveTexture"),
		blendFunc:     opengl32.NewProc("glBlendFunc"),
		readPixels:    opengl32.NewProc("glReadPixels"),
		getString:     opengl32.NewProc("glGetString"),

		// GL3 functions via wglGetProcAddress
		genBuffers:              loadProc("glGenBuffers"),
		deleteBuffers:           loadProc("glDeleteBuffers"),
		bindBuffer:              loadProc("glBindBuffer"),
		bufferData:              loadProc("glBufferData"),
		bufferSubData:           loadProc("glBufferSubData"),
		genVertexArrays:         loadProc("glGenVertexArrays"),
		deleteVertexArrays:      loadProc("glDeleteVertexArrays"),
		bindVertexArray:         loadProc("glBindVertexArray"),
		vertexAttribPointer:     loadProc("glVertexAttribPointer"),
		enableVertexAttribArray: loadProc("glEnableVertexAttribArray"),
		createShader:            loadProc("glCreateShader"),
		shaderSource:            loadProc("glShaderSource"),
		compileShader:           loadProc("glCompileShader"),
		getShaderiv:             loadProc("glGetShaderiv"),
		getShaderInfoLog:        loadProc("glGetShaderInfoLog"),
		deleteShader:            loadProc("glDeleteShader"),
		createProgram:           loadProc("glCreateProgram"),
		attachShader:            loadProc("glAttachShader"),
		linkProgram:             loadProc("glLinkProgram"),
		getProgramiv:            loadProc("glGetProgramiv"),
		getProgramInfoLog:       loadProc("glGetProgramInfoLog"),
		useProgram:              loadProc("glUseProgram"),
		deleteProgram:           loadProc("glDeleteProgram"),
		getUniformLocation:      loadProc("glGetUniformLocation"),
		getAttribLocation:       loadProc("glGetAttribLocation"),
		uniform1i:               loadProc("glUniform1i"),
		uniform4f:               loadProc("glUniform4f"),
		uniformMatrix4fv:        loadProc("glUniformMatrix4fv"),
		drawArrays:              loadProc("glDrawArrays"),
	}
	return gl, nil
}

func f32(v float32) uintptr {
	return uintptr(math.Float32bits(v))
}

func f64(v float64) uintptr {
	return uintptr(math.Float64bits(v))
}
