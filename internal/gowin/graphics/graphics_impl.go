package graphics

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"time"
	"unsafe"

	glpkg "github.com/tinyrange/cc/internal/gowin/gl"
	"github.com/tinyrange/cc/internal/gowin/window"
)

const (
	vertexShaderSource = `#version 150
in vec2 a_position;
in vec2 a_texCoord;
in vec4 a_color;

out vec2 v_texCoord;
out vec4 v_color;

uniform mat4 u_proj;
uniform mat4 u_model;

void main() {
	gl_Position = u_proj * u_model * vec4(a_position, 0.0, 1.0);
	v_texCoord = a_texCoord;
	v_color = a_color;
}`

	fragmentShaderSource = `#version 150
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
	modelUniform  int32

	// Lazily-created 1x1 white texture for callers that pass nil.
	whiteTex *glTexture

	// Meshes created via NewMesh, for cleanup when the window loop exits.
	meshes []*glMesh
}

type glTexture struct {
	id uint32
	w  int
	h  int
}

type glMesh struct {
	vao uint32
	vbo uint32
	ebo uint32

	indexCount int32

	tex *glTexture
}

func (*glMesh) isMesh() {}

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
	w.modelUniform = gl.GetUniformLocation(program, "u_model")

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
		for _, m := range w.meshes {
			if m == nil {
				continue
			}
			if m.vao != 0 {
				va := m.vao
				w.gl.DeleteVertexArrays(1, &va)
			}
			if m.vbo != 0 {
				buf := m.vbo
				w.gl.DeleteBuffers(1, &buf)
			}
			if m.ebo != 0 {
				buf := m.ebo
				w.gl.DeleteBuffers(1, &buf)
			}
		}
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
	// Refresh scale every frame. Some platforms can change DPI/scale at runtime
	// (e.g. moving between monitors), and macOS can report updated backing metrics
	// after maximize/fullscreen transitions.
	w.scale = w.platform.Scale()

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
	model := IdentityMat4()
	w.gl.UniformMatrix4fv(w.modelUniform, 1, false, &model[0])

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
	bw, bh := f.w.platform.BackingSize()
	// The graphics coordinate system is logical units (backing/scale).
	return int(math.Round(float64(float32(bw) / f.w.scale))), int(math.Round(float64(float32(bh) / f.w.scale)))
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

func (f glFrame) TextInput() string {
	return f.w.platform.TextInput()
}

func (f glFrame) RenderQuad(x, y, width, height float32, tex Texture, c color.Color) {
	var t *glTexture
	if tex == nil {
		t = f.w.getWhiteTexture()
	} else {
		var ok bool
		t, ok = tex.(*glTexture)
		if !ok {
			return
		}
	}

	// Bind texture
	f.w.gl.ActiveTexture(glpkg.Texture0)
	f.w.gl.BindTexture(glpkg.Texture2D, t.id)
	texUniform := f.w.gl.GetUniformLocation(f.w.shaderProgram, "u_texture")
	f.w.gl.Uniform1i(texUniform, 0)

	// Convert color to float32 RGBA
	rgba := ColorToFloat32(c)

	// Ensure quads render with identity model matrix.
	model := IdentityMat4()
	f.w.gl.UseProgram(f.w.shaderProgram)
	f.w.gl.UniformMatrix4fv(f.w.modelUniform, 1, false, &model[0])

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

func (w *glWindow) NewMesh(vertices []Vertex, indices []uint32, tex Texture) (Mesh, error) {
	if len(vertices) == 0 || len(indices) == 0 {
		return &glMesh{}, nil
	}

	var t *glTexture
	if tex == nil {
		t = w.getWhiteTexture()
	} else {
		var ok bool
		t, ok = tex.(*glTexture)
		if !ok {
			return nil, fmt.Errorf("unsupported texture implementation")
		}
	}

	// Create VAO/VBO/EBO for this mesh.
	var vao, vbo, ebo uint32
	w.gl.GenVertexArrays(1, &vao)
	w.gl.GenBuffers(1, &vbo)
	w.gl.GenBuffers(1, &ebo)

	w.gl.BindVertexArray(vao)

	// Vertex buffer.
	w.gl.BindBuffer(glpkg.ArrayBuffer, vbo)
	w.gl.BufferData(
		glpkg.ArrayBuffer,
		len(vertices)*8*4,
		unsafe.Pointer(&vertices[0]),
		glpkg.StaticDraw,
	)

	// Index buffer.
	w.gl.BindBuffer(glpkg.ElementArrayBuffer, ebo)
	w.gl.BufferData(
		glpkg.ElementArrayBuffer,
		len(indices)*4,
		unsafe.Pointer(&indices[0]),
		glpkg.StaticDraw,
	)

	// Set up vertex attributes for this VAO.
	posLoc := w.gl.GetAttribLocation(w.shaderProgram, "a_position")
	texLoc := w.gl.GetAttribLocation(w.shaderProgram, "a_texCoord")
	colLoc := w.gl.GetAttribLocation(w.shaderProgram, "a_color")
	w.gl.VertexAttribPointer(uint32(posLoc), 2, glpkg.Float, false, 8*4, 0)
	w.gl.EnableVertexAttribArray(uint32(posLoc))
	w.gl.VertexAttribPointer(uint32(texLoc), 2, glpkg.Float, false, 8*4, 8)
	w.gl.EnableVertexAttribArray(uint32(texLoc))
	w.gl.VertexAttribPointer(uint32(colLoc), 4, glpkg.Float, false, 8*4, 16)
	w.gl.EnableVertexAttribArray(uint32(colLoc))

	m := &glMesh{
		vao:        vao,
		vbo:        vbo,
		ebo:        ebo,
		indexCount: int32(len(indices)),
		tex:        t,
	}
	w.meshes = append(w.meshes, m)
	return m, nil
}

func (f glFrame) RenderMesh(mesh Mesh, opts DrawOptions) {
	m, ok := mesh.(*glMesh)
	if !ok || m == nil || m.vao == 0 || m.indexCount == 0 {
		return
	}

	f.w.gl.UseProgram(f.w.shaderProgram)

	// Projection is set in prepareFrame(); just set model.
	model := opts.Model
	if model == (Mat4{}) {
		model = IdentityMat4()
	}
	f.w.gl.UniformMatrix4fv(f.w.modelUniform, 1, false, &model[0])

	// Bind texture.
	f.w.gl.ActiveTexture(glpkg.Texture0)
	if m.tex != nil {
		f.w.gl.BindTexture(glpkg.Texture2D, m.tex.id)
	} else {
		t := f.w.getWhiteTexture()
		f.w.gl.BindTexture(glpkg.Texture2D, t.id)
	}
	texUniform := f.w.gl.GetUniformLocation(f.w.shaderProgram, "u_texture")
	f.w.gl.Uniform1i(texUniform, 0)

	// Draw.
	f.w.gl.BindVertexArray(m.vao)
	f.w.gl.DrawElements(glpkg.Triangles, m.indexCount, glpkg.UnsignedInt, 0)
}

func (t *glTexture) Size() (int, int) {
	return t.w, t.h
}

func (w *glWindow) getWhiteTexture() *glTexture {
	if w.whiteTex != nil {
		return w.whiteTex
	}

	var texID uint32
	w.gl.GenTextures(1, &texID)
	w.gl.BindTexture(glpkg.Texture2D, texID)
	w.gl.TexParameteri(glpkg.Texture2D, glpkg.TextureMinFilter, glpkg.Nearest)
	w.gl.TexParameteri(glpkg.Texture2D, glpkg.TextureMagFilter, glpkg.Nearest)

	// 1x1 RGBA pixel (white).
	pix := [4]byte{0xff, 0xff, 0xff, 0xff}
	w.gl.TexImage2D(
		glpkg.Texture2D,
		0,
		int32(glpkg.RGBA),
		1,
		1,
		0,
		glpkg.RGBA,
		glpkg.UnsignedByte,
		unsafe.Pointer(&pix[0]),
	)

	w.whiteTex = &glTexture{id: texID, w: 1, h: 1}
	return w.whiteTex
}
