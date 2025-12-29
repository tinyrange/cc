package graphics

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"time"
	"unsafe"

	glpkg "github.com/tinyrange/cc/internal/gowin/gl"
	"github.com/tinyrange/cc/internal/gowin/window"
)

const (
	vertexShaderSource = `#version 130
in vec2 a_position;
in vec2 a_texCoord;
in vec4 a_color;

out vec2 v_texCoord;
out vec4 v_color;

uniform mat4 u_proj;

void main() {
	gl_Position = u_proj * vec4(a_position, 0.0, 1.0);
	v_texCoord = a_texCoord;
	v_color = a_color;
}`

	fragmentShaderSource = `#version 130
in vec2 v_texCoord;
in vec4 v_color;

out vec4 fragColor;

uniform sampler2D u_texture;

void main() {
	fragColor = texture(u_texture, v_texCoord) * v_color;
}`
)

type glWindow struct {
	platform window.Window
	gl       glpkg.OpenGL

	clearEnabled bool
	clearColor   color.Color
	scale        float32

	// GL3 resources
	shaderProgram uint32
	vao           uint32
	vbo           uint32
	projUniform   int32
}

type glTexture struct {
	id uint32
	w  int
	h  int
}

type glFrame struct {
	w *glWindow
}

// Screenshot implements Frame.
func (f glFrame) Screenshot() (image.Image, error) {
	bw, bh := f.w.platform.BackingSize()
	rgba := image.NewRGBA(image.Rect(0, 0, bw, bh))
	f.w.gl.ReadPixels(0, 0, int32(bw), int32(bh), glpkg.RGBA, glpkg.UnsignedByte, unsafe.Pointer(&rgba.Pix[0]))

	// Flip the image vertically
	flipped := image.NewRGBA(image.Rect(0, 0, bw, bh))
	for y := 0; y < bh; y++ {
		srcStart := y * rgba.Stride
		srcEnd := srcStart + rgba.Stride
		dstStart := (bh - 1 - y) * flipped.Stride
		dstEnd := dstStart + flipped.Stride
		copy(flipped.Pix[dstStart:dstEnd], rgba.Pix[srcStart:srcEnd])
	}

	return flipped, nil
}

// New returns a Window backed by OpenGL implementation.
func New(title string, width, height int) (Window, error) {
	return newWithProfile(title, width, height, true)
}

func newWithProfile(title string, width, height int, useCoreProfile bool) (Window, error) {
	platform, err := window.New(title, width, height, useCoreProfile)
	if err != nil {
		return nil, err
	}
	gl, err := platform.GL()
	if err != nil {
		platform.Close()
		return nil, err
	}

	// Check GL version
	versionStr := gl.GetString(glpkg.Version)
	var major, minor int
	if _, err := fmt.Sscanf(versionStr, "%d.%d", &major, &minor); err != nil || major < 3 {
		platform.Close()
		return nil, fmt.Errorf("OpenGL 3.0+ required, got version: %s", versionStr)
	}

	gl.Enable(glpkg.Blend)
	gl.BlendFunc(glpkg.SrcAlpha, glpkg.OneMinusSrcAlpha)

	w := &glWindow{
		platform:     platform,
		gl:           gl,
		clearEnabled: true,
		clearColor:   ColorBlack,
		scale:        platform.Scale(),
	}

	// Create shader program
	program, err := createShaderProgram(gl, vertexShaderSource, fragmentShaderSource)
	if err != nil {
		platform.Close()
		return nil, fmt.Errorf("failed to create shader program: %v", err)
	}
	w.shaderProgram = program
	w.projUniform = gl.GetUniformLocation(program, "u_proj")

	// Create VAO and VBO
	var vao, vbo uint32
	gl.GenVertexArrays(1, &vao)
	gl.GenBuffers(1, &vbo)
	w.vao = vao
	w.vbo = vbo

	gl.BindVertexArray(vao)
	gl.BindBuffer(glpkg.ArrayBuffer, vbo)
	// Allocate buffer for 6 vertices (2 triangles) * (2 pos + 2 tex + 4 color) floats
	gl.BufferData(glpkg.ArrayBuffer, 6*8*4, nil, glpkg.DynamicDraw)

	// Set up vertex attributes
	// Position: 2 floats at offset 0
	posLoc := gl.GetAttribLocation(program, "a_position")
	texLoc := gl.GetAttribLocation(program, "a_texCoord")
	colLoc := gl.GetAttribLocation(program, "a_color")
	gl.VertexAttribPointer(uint32(posLoc), 2, glpkg.Float, false, 8*4, 0)
	gl.EnableVertexAttribArray(uint32(posLoc))
	// TexCoord: 2 floats at offset 2*4 = 8
	gl.VertexAttribPointer(uint32(texLoc), 2, glpkg.Float, false, 8*4, 8)
	gl.EnableVertexAttribArray(uint32(texLoc))
	// Color: 4 floats at offset 4*4 = 16
	gl.VertexAttribPointer(uint32(colLoc), 4, glpkg.Float, false, 8*4, 16)
	gl.EnableVertexAttribArray(uint32(colLoc))

	return w, nil
}

func createShaderProgram(gl glpkg.OpenGL, vertexSrc, fragmentSrc string) (uint32, error) {
	// Create and compile vertex shader
	vertexShader := gl.CreateShader(glpkg.VertexShader)
	gl.ShaderSource(vertexShader, vertexSrc)
	gl.CompileShader(vertexShader)
	var status int32
	gl.GetShaderiv(vertexShader, glpkg.CompileStatus, &status)
	if status == 0 {
		log := gl.GetShaderInfoLog(vertexShader)
		gl.DeleteShader(vertexShader)
		return 0, fmt.Errorf("vertex shader compilation failed: %s", log)
	}

	// Create and compile fragment shader
	fragmentShader := gl.CreateShader(glpkg.FragmentShader)
	gl.ShaderSource(fragmentShader, fragmentSrc)
	gl.CompileShader(fragmentShader)
	gl.GetShaderiv(fragmentShader, glpkg.CompileStatus, &status)
	if status == 0 {
		log := gl.GetShaderInfoLog(fragmentShader)
		gl.DeleteShader(vertexShader)
		gl.DeleteShader(fragmentShader)
		return 0, fmt.Errorf("fragment shader compilation failed: %s", log)
	}

	// Create program and link
	program := gl.CreateProgram()
	gl.AttachShader(program, vertexShader)
	gl.AttachShader(program, fragmentShader)
	gl.LinkProgram(program)
	gl.GetProgramiv(program, glpkg.LinkStatus, &status)
	if status == 0 {
		log := gl.GetProgramInfoLog(program)
		gl.DeleteShader(vertexShader)
		gl.DeleteShader(fragmentShader)
		gl.DeleteProgram(program)
		return 0, fmt.Errorf("program linking failed: %s", log)
	}

	// Shaders can be deleted after linking
	gl.DeleteShader(vertexShader)
	gl.DeleteShader(fragmentShader)

	return program, nil
}

func (w *glWindow) PlatformWindow() window.Window {
	return w.platform
}

func (w *glWindow) Scale() float32 {
	return w.scale
}

func (w *glWindow) GetShaderProgram() uint32 {
	return w.shaderProgram
}

func (w *glWindow) NewTexture(img image.Image) (Texture, error) {
	nrgba := image.NewNRGBA(img.Bounds())
	draw.Draw(nrgba, nrgba.Bounds(), img, img.Bounds().Min, draw.Src)

	var texID uint32
	w.gl.GenTextures(1, &texID)
	w.gl.BindTexture(glpkg.Texture2D, texID)
	w.gl.TexParameteri(glpkg.Texture2D, glpkg.TextureMinFilter, glpkg.Nearest)
	w.gl.TexParameteri(glpkg.Texture2D, glpkg.TextureMagFilter, glpkg.Nearest)

	if len(nrgba.Pix) > 0 {
		w.gl.TexImage2D(
			glpkg.Texture2D,
			0,
			int32(glpkg.RGBA),
			int32(nrgba.Rect.Dx()),
			int32(nrgba.Rect.Dy()),
			0,
			glpkg.RGBA,
			glpkg.UnsignedByte,
			unsafe.Pointer(&nrgba.Pix[0]),
		)
	}

	return &glTexture{id: texID, w: nrgba.Rect.Dx(), h: nrgba.Rect.Dy()}, nil
}

func (w *glWindow) SetClear(enabled bool) {
	w.clearEnabled = enabled
}

func (w *glWindow) SetClearColor(c color.Color) {
	w.clearColor = c
}

func (w *glWindow) Loop(step func(f Frame) error) error {
	defer w.platform.Close()
	defer func() {
		var vao, vbo uint32 = w.vao, w.vbo
		w.gl.DeleteVertexArrays(1, &vao)
		w.gl.DeleteBuffers(1, &vbo)
		w.gl.DeleteProgram(w.shaderProgram)
	}()

	frame := glFrame{w: w}
	for w.platform.Poll() {
		w.prepareFrame()

		if err := step(frame); err != nil {
			return err
		}

		w.platform.Swap()
		time.Sleep(time.Second / 120)
	}
	return nil
}

func (w *glWindow) prepareFrame() {
	bw, bh := w.platform.BackingSize()

	w.gl.Viewport(0, 0, int32(bw), int32(bh))

	// Compute orthographic projection matrix
	// Scale coordinates by scale factor
	width := float32(bw) / w.scale
	height := float32(bh) / w.scale
	proj := orthoMatrix(0, width, height, 0, -1, 1)

	// Use shader program and set projection matrix
	w.gl.UseProgram(w.shaderProgram)
	w.gl.BindVertexArray(w.vao)
	w.gl.UniformMatrix4fv(w.projUniform, 1, false, &proj[0])

	if w.clearEnabled {
		rgba := ColorToFloat32(w.clearColor)
		w.gl.ClearColor(rgba[0], rgba[1], rgba[2], rgba[3])
		w.gl.Clear(glpkg.ColorBufferBit)
	}
}

// orthoMatrix creates an orthographic projection matrix (column-major)
func orthoMatrix(left, right, bottom, top, near, far float32) [16]float32 {
	// Column-major order
	return [16]float32{
		2.0 / (right - left), 0, 0, 0,
		0, 2.0 / (top - bottom), 0, 0,
		0, 0, -2.0 / (far - near), 0,
		-(right + left) / (right - left), -(top + bottom) / (top - bottom), -(far + near) / (far - near), 1,
	}
}

func (f glFrame) WindowSize() (int, int) {
	return f.w.platform.BackingSize()
}

func (f glFrame) CursorPos() (float32, float32) {
	x, y := f.w.platform.Cursor()
	// Convert from physical pixel coordinates to logical coordinates
	// by dividing by the scale factor
	return x / f.w.scale, y / f.w.scale
}

func (f glFrame) GetKeyState(key window.Key) window.KeyState {
	return f.w.platform.GetKeyState(key)
}

func (f glFrame) GetButtonState(button window.Button) window.ButtonState {
	return f.w.platform.GetButtonState(button)
}

func (f glFrame) RenderQuad(x, y, width, height float32, tex Texture, c color.Color) {
	t, ok := tex.(*glTexture)
	if !ok {
		return
	}

	// Bind texture
	f.w.gl.ActiveTexture(glpkg.Texture0)
	f.w.gl.BindTexture(glpkg.Texture2D, t.id)
	texUniform := f.w.gl.GetUniformLocation(f.w.shaderProgram, "u_texture")
	f.w.gl.Uniform1i(texUniform, 0)

	// Convert color to float32 RGBA
	rgba := ColorToFloat32(c)

	// Update vertex buffer with quad data (2 triangles)
	vertices := [6 * 8]float32{
		// Triangle 1
		x, y, 0, 0, rgba[0], rgba[1], rgba[2], rgba[3], // top-left
		x + width, y, 1, 0, rgba[0], rgba[1], rgba[2], rgba[3], // top-right
		x, y + height, 0, 1, rgba[0], rgba[1], rgba[2], rgba[3], // bottom-left
		// Triangle 2
		x + width, y, 1, 0, rgba[0], rgba[1], rgba[2], rgba[3], // top-right
		x + width, y + height, 1, 1, rgba[0], rgba[1], rgba[2], rgba[3], // bottom-right
		x, y + height, 0, 1, rgba[0], rgba[1], rgba[2], rgba[3], // bottom-left
	}

	f.w.gl.BindBuffer(glpkg.ArrayBuffer, f.w.vbo)
	f.w.gl.BufferSubData(glpkg.ArrayBuffer, 0, len(vertices)*4, unsafe.Pointer(&vertices[0]))

	// Draw
	f.w.gl.BindVertexArray(f.w.vao)
	f.w.gl.DrawArrays(glpkg.Triangles, 0, 6)
}

func (t *glTexture) Size() (int, int) {
	return t.w, t.h
}
