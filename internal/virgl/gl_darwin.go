//go:build darwin

package virgl

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	glArrayBuffer             = 0x8892
	glElementArrayBuffer      = 0x8893
	glStreamDraw              = 0x88e0
	glStaticDraw              = 0x88e4
	glTexture2D               = 0x0de1
	glTexture0                = 0x84c0
	glRGBA8                   = 0x8058
	glRGBA                    = 0x1908
	glBGRA                    = 0x80e1
	glUnsignedByte            = 0x1401
	glShort                   = 0x1402
	glUnsignedShort           = 0x1403
	glUnsignedInt             = 0x1405
	glUnsignedInt248          = 0x84fa
	glFloat                   = 0x1406
	glFramebuffer             = 0x8d40
	glReadFramebuffer         = 0x8ca8
	glDrawFramebuffer         = 0x8ca9
	glColorAttachment0        = 0x8ce0
	glDepthAttachment         = 0x8d00
	glDepthStencilAttachment  = 0x821a
	glFramebufferComplete     = 0x8cd5
	glDepthComponent          = 0x1902
	glDepthComponent24        = 0x81a6
	glDepthStencil            = 0x84f9
	glDepth24Stencil8         = 0x88f0
	glVertexShader            = 0x8b31
	glFragmentShader          = 0x8b30
	glCompileStatus           = 0x8b81
	glLinkStatus              = 0x8b82
	glInfoLogLength           = 0x8b84
	glColorBufferBit          = 0x00004000
	glDepthBufferBit          = 0x00000100
	glStencilBufferBit        = 0x00000400
	glPoints                  = 0x0000
	glLines                   = 0x0001
	glLineLoop                = 0x0002
	glLineStrip               = 0x0003
	glTriangles               = 0x0004
	glTriangleStrip           = 0x0005
	glTriangleFan             = 0x0006
	glCullFace                = 0x0b44
	glFront                   = 0x0404
	glBack                    = 0x0405
	glFrontAndBack            = 0x0408
	glCW                      = 0x0900
	glCCW                     = 0x0901
	glDepthTest               = 0x0b71
	glBlend                   = 0x0be2
	glScissorTest             = 0x0c11
	glPrimitiveRestart        = 0x8f9d
	glProgramPointSize        = 0x8642
	glNever                   = 0x0200
	glLess                    = 0x0201
	glEqual                   = 0x0202
	glLEqual                  = 0x0203
	glGreater                 = 0x0204
	glNotEqual                = 0x0205
	glGEqual                  = 0x0206
	glAlways                  = 0x0207
	glUnpackAlignment         = 0x0cf5
	glPackAlignment           = 0x0d05
	glTextureMinFilter        = 0x2801
	glTextureMagFilter        = 0x2800
	glTextureMinLOD           = 0x813a
	glTextureMaxLOD           = 0x813b
	glTextureLODBias          = 0x8501
	glTextureBaseLevel        = 0x813c
	glTextureMaxLevel         = 0x813d
	glTextureBorderColor      = 0x1004
	glTextureCompareMode      = 0x884c
	glTextureCompareFunc      = 0x884d
	glCompareRefToTexture     = 0x884e
	glNone                    = 0
	glLinear                  = 0x2601
	glNearest                 = 0x2600
	glNearestMipmapNearest    = 0x2700
	glLinearMipmapNearest     = 0x2701
	glNearestMipmapLinear     = 0x2702
	glLinearMipmapLinear      = 0x2703
	glRepeat                  = 0x2901
	glClampToEdge             = 0x812f
	glClampToBorder           = 0x812d
	glMirroredRepeat          = 0x8370
	glTextureWrapS            = 0x2802
	glTextureWrapT            = 0x2803
	glTextureSwizzleR         = 0x8e42
	glTextureSwizzleG         = 0x8e43
	glTextureSwizzleB         = 0x8e44
	glTextureSwizzleA         = 0x8e45
	glRed                     = 0x1903
	glGreen                   = 0x1904
	glBlue                    = 0x1905
	glAlpha                   = 0x1906
	glZero                    = 0
	glOne                     = 1
	glSrcColor                = 0x0300
	glOneMinusSrcColor        = 0x0301
	glSrcAlpha                = 0x0302
	glOneMinusSrcAlpha        = 0x0303
	glDstAlpha                = 0x0304
	glOneMinusDstAlpha        = 0x0305
	glDstColor                = 0x0306
	glOneMinusDstColor        = 0x0307
	glSrcAlphaSaturate        = 0x0308
	glConstantColor           = 0x8001
	glOneMinusConstantColor   = 0x8002
	glConstantAlpha           = 0x8003
	glOneMinusConstantAlpha   = 0x8004
	glSrc1Color               = 0x88f9
	glOneMinusSrc1Color       = 0x88fa
	glSrc1Alpha               = 0x8589
	glOneMinusSrc1Alpha       = 0x88fb
	glFuncAdd                 = 0x8006
	glMin                     = 0x8007
	glMax                     = 0x8008
	glFuncSubtract            = 0x800a
	glFuncReverseSubtract     = 0x800b
	glSyncGPUCommandsComplete = 0x9117
	glTimeoutIgnored          = ^uint64(0)
)

type hostGL struct {
	genTextures            func(int32, *uint32)
	deleteTextures         func(int32, *uint32)
	bindTexture            func(uint32, uint32)
	activeTexture          func(uint32)
	texImage2D             func(uint32, int32, int32, int32, int32, int32, uint32, uint32, uintptr)
	texSubImage2D          func(uint32, int32, int32, int32, int32, int32, uint32, uint32, uintptr)
	texParameteri          func(uint32, uint32, int32)
	texParameterf          func(uint32, uint32, float32)
	texParameterfv         func(uint32, uint32, *float32)
	pixelStorei            func(uint32, int32)
	genBuffers             func(int32, *uint32)
	deleteBuffers          func(int32, *uint32)
	bindBuffer             func(uint32, uint32)
	bufferData             func(uint32, int, uintptr, uint32)
	bufferSubData          func(uint32, int, int, uintptr)
	getBufferSubData       func(uint32, int, int, uintptr)
	genVertexArrays        func(int32, *uint32)
	deleteVertexArrays     func(int32, *uint32)
	bindVertexArray        func(uint32)
	vertexAttribPtr        func(uint32, int32, uint32, bool, int32, uintptr)
	vertexAttrib4f         func(uint32, float32, float32, float32, float32)
	enableVertexAttrib     func(uint32)
	disableVertexAttrib    func(uint32)
	genFramebuffers        func(int32, *uint32)
	deleteFramebuffers     func(int32, *uint32)
	bindFramebuffer        func(uint32, uint32)
	framebufferTexture     func(uint32, uint32, uint32, uint32, int32)
	checkFramebuffer       func(uint32) uint32
	blitFramebuffer        func(int32, int32, int32, int32, int32, int32, int32, int32, uint32, uint32)
	createShader           func(uint32) uint32
	shaderSource           func(uint32, int32, **byte, *int32)
	compileShader          func(uint32)
	getShaderiv            func(uint32, uint32, *int32)
	getShaderInfoLog       func(uint32, int32, *int32, *byte)
	deleteShader           func(uint32)
	createProgram          func() uint32
	attachShader           func(uint32, uint32)
	linkProgram            func(uint32)
	getProgramiv           func(uint32, uint32, *int32)
	getProgramInfoLog      func(uint32, int32, *int32, *byte)
	deleteProgram          func(uint32)
	useProgram             func(uint32)
	getUniformLocation     func(uint32, *byte) int32
	uniformMatrix4fv       func(int32, int32, bool, *float32)
	uniform4fv             func(int32, int32, *float32)
	uniform1f              func(int32, float32)
	uniform1i              func(int32, int32)
	viewport               func(int32, int32, int32, int32)
	depthRange             func(float64, float64)
	scissor                func(int32, int32, int32, int32)
	clearColor             func(float32, float32, float32, float32)
	clearDepth             func(float64)
	clearStencil           func(int32)
	clear                  func(uint32)
	enable                 func(uint32)
	disable                func(uint32)
	cullFace               func(uint32)
	frontFace              func(uint32)
	depthFunc              func(uint32)
	depthMask              func(bool)
	blendColor             func(float32, float32, float32, float32)
	blendFuncSeparate      func(uint32, uint32, uint32, uint32)
	blendEquationSeparate  func(uint32, uint32)
	colorMask              func(bool, bool, bool, bool)
	drawArrays             func(uint32, int32, int32)
	drawElements           func(uint32, int32, uint32, uintptr)
	drawElementsBaseVertex func(uint32, int32, uint32, uintptr, int32)
	primitiveRestartIndex  func(uint32)
	readPixels             func(int32, int32, int32, int32, uint32, uint32, uintptr)
	drawBuffer             func(uint32)
	readBuffer             func(uint32)
	fenceSync              func(uint32, uint32) uintptr
	waitSync               func(uintptr, uint32, uint64)
	deleteSync             func(uintptr)
	flush                  func()
	finish                 func()
	getError               func() uint32
}

func loadHostGL() (*hostGL, error) {
	handle, err := purego.Dlopen("/System/Library/Frameworks/OpenGL.framework/OpenGL", purego.RTLD_GLOBAL|purego.RTLD_LAZY)
	if err != nil {
		return nil, fmt.Errorf("load OpenGL for VirGL: %w", err)
	}
	register := func(destination any, name string) {
		purego.RegisterLibFunc(destination, handle, name)
	}
	gl := &hostGL{}
	register(&gl.genTextures, "glGenTextures")
	register(&gl.deleteTextures, "glDeleteTextures")
	register(&gl.bindTexture, "glBindTexture")
	register(&gl.activeTexture, "glActiveTexture")
	register(&gl.texImage2D, "glTexImage2D")
	register(&gl.texSubImage2D, "glTexSubImage2D")
	register(&gl.texParameteri, "glTexParameteri")
	register(&gl.texParameterf, "glTexParameterf")
	register(&gl.texParameterfv, "glTexParameterfv")
	register(&gl.pixelStorei, "glPixelStorei")
	register(&gl.genBuffers, "glGenBuffers")
	register(&gl.deleteBuffers, "glDeleteBuffers")
	register(&gl.bindBuffer, "glBindBuffer")
	register(&gl.bufferData, "glBufferData")
	register(&gl.bufferSubData, "glBufferSubData")
	register(&gl.getBufferSubData, "glGetBufferSubData")
	register(&gl.genVertexArrays, "glGenVertexArrays")
	register(&gl.deleteVertexArrays, "glDeleteVertexArrays")
	register(&gl.bindVertexArray, "glBindVertexArray")
	register(&gl.vertexAttribPtr, "glVertexAttribPointer")
	register(&gl.vertexAttrib4f, "glVertexAttrib4f")
	register(&gl.enableVertexAttrib, "glEnableVertexAttribArray")
	register(&gl.disableVertexAttrib, "glDisableVertexAttribArray")
	register(&gl.genFramebuffers, "glGenFramebuffers")
	register(&gl.deleteFramebuffers, "glDeleteFramebuffers")
	register(&gl.bindFramebuffer, "glBindFramebuffer")
	register(&gl.framebufferTexture, "glFramebufferTexture2D")
	register(&gl.checkFramebuffer, "glCheckFramebufferStatus")
	register(&gl.blitFramebuffer, "glBlitFramebuffer")
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
	register(&gl.deleteProgram, "glDeleteProgram")
	register(&gl.useProgram, "glUseProgram")
	register(&gl.getUniformLocation, "glGetUniformLocation")
	register(&gl.uniformMatrix4fv, "glUniformMatrix4fv")
	register(&gl.uniform4fv, "glUniform4fv")
	register(&gl.uniform1f, "glUniform1f")
	register(&gl.uniform1i, "glUniform1i")
	register(&gl.viewport, "glViewport")
	register(&gl.depthRange, "glDepthRange")
	register(&gl.scissor, "glScissor")
	register(&gl.clearColor, "glClearColor")
	register(&gl.clearDepth, "glClearDepth")
	register(&gl.clearStencil, "glClearStencil")
	register(&gl.clear, "glClear")
	register(&gl.enable, "glEnable")
	register(&gl.disable, "glDisable")
	register(&gl.cullFace, "glCullFace")
	register(&gl.frontFace, "glFrontFace")
	register(&gl.depthFunc, "glDepthFunc")
	register(&gl.depthMask, "glDepthMask")
	register(&gl.blendColor, "glBlendColor")
	register(&gl.blendFuncSeparate, "glBlendFuncSeparate")
	register(&gl.blendEquationSeparate, "glBlendEquationSeparate")
	register(&gl.colorMask, "glColorMask")
	register(&gl.drawArrays, "glDrawArrays")
	register(&gl.drawElements, "glDrawElements")
	register(&gl.drawElementsBaseVertex, "glDrawElementsBaseVertex")
	register(&gl.primitiveRestartIndex, "glPrimitiveRestartIndex")
	register(&gl.readPixels, "glReadPixels")
	register(&gl.drawBuffer, "glDrawBuffer")
	register(&gl.readBuffer, "glReadBuffer")
	register(&gl.fenceSync, "glFenceSync")
	register(&gl.waitSync, "glWaitSync")
	register(&gl.deleteSync, "glDeleteSync")
	register(&gl.flush, "glFlush")
	register(&gl.finish, "glFinish")
	register(&gl.getError, "glGetError")
	return gl, nil
}

func (gl *hostGL) compileProgram(vertexSource, fragmentSource string) (uint32, error) {
	compile := func(kind uint32, source string) (uint32, error) {
		shader := gl.createShader(kind)
		sourceBytes := []byte(source)
		sourcePointer := &sourceBytes[0]
		length := int32(len(sourceBytes))
		gl.shaderSource(shader, 1, &sourcePointer, &length)
		gl.compileShader(shader)
		var status int32
		gl.getShaderiv(shader, glCompileStatus, &status)
		if status == 0 {
			return 0, fmt.Errorf("compile VirGL host shader: %s", gl.shaderLog(shader))
		}
		return shader, nil
	}
	vertex, err := compile(glVertexShader, vertexSource)
	if err != nil {
		return 0, err
	}
	defer gl.deleteShader(vertex)
	fragment, err := compile(glFragmentShader, fragmentSource)
	if err != nil {
		return 0, err
	}
	defer gl.deleteShader(fragment)
	program := gl.createProgram()
	gl.attachShader(program, vertex)
	gl.attachShader(program, fragment)
	gl.linkProgram(program)
	var status int32
	gl.getProgramiv(program, glLinkStatus, &status)
	if status == 0 {
		defer gl.deleteProgram(program)
		return 0, fmt.Errorf("link VirGL host program: %s", gl.programLog(program))
	}
	return program, nil
}

func (gl *hostGL) shaderLog(shader uint32) string {
	var length int32
	gl.getShaderiv(shader, glInfoLogLength, &length)
	if length <= 1 {
		return "no shader log"
	}
	result := make([]byte, length)
	gl.getShaderInfoLog(shader, length, &length, &result[0])
	return string(result[:length])
}

func (gl *hostGL) programLog(program uint32) string {
	var length int32
	gl.getProgramiv(program, glInfoLogLength, &length)
	if length <= 1 {
		return "no program log"
	}
	result := make([]byte, length)
	gl.getProgramInfoLog(program, length, &length, &result[0])
	return string(result[:length])
}

func glPointer(bytes []byte) uintptr {
	if len(bytes) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&bytes[0]))
}

func uniformLocation(gl *hostGL, program uint32, name string) int32 {
	bytes := append([]byte(name), 0)
	return gl.getUniformLocation(program, &bytes[0])
}
