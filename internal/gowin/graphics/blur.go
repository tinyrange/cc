package graphics

import (
	"fmt"
	"unsafe"

	glpkg "github.com/tinyrange/cc/internal/gowin/gl"
)

// Blur shader sources (separable Gaussian blur)
const blurVertexShaderSource = `#version 150
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

const blurFragmentShaderSource = `#version 150
in vec2 v_texCoord;
in vec4 v_color;

out vec4 fragColor;

uniform sampler2D u_texture;
uniform vec2 u_direction;    // (1/width, 0) for horizontal, (0, 1/height) for vertical
uniform float u_radius;      // Blur radius multiplier

// 13-tap Gaussian weights (sigma ~= 4.0 for smoother, more visible blur)
// Weights computed for offsets 0, 1, 2, 3, 4, 5, 6
const float weights[7] = float[](
	0.1061154, // center
	0.1028506, // 1
	0.0936552, // 2
	0.0801001, // 3
	0.0643218, // 4
	0.0485090, // 5
	0.0343425  // 6
);

void main() {
	vec4 result = texture(u_texture, v_texCoord) * weights[0];

	for (int i = 1; i < 7; i++) {
		vec2 offset = u_direction * float(i) * u_radius;
		result += texture(u_texture, v_texCoord + offset) * weights[i];
		result += texture(u_texture, v_texCoord - offset) * weights[i];
	}

	fragColor = result * v_color;
}`

// BlurEffect provides a reusable blur effect with configurable radius.
type BlurEffect struct {
	window *glWindow

	// Shader program for blur
	program       uint32
	projUniform   int32
	modelUniform  int32
	dirUniform    int32
	radiusUniform int32

	// Ping-pong render targets for multi-pass blur
	rtA RenderTarget
	rtB RenderTarget

	// Current dimensions
	width  int
	height int

	// Fullscreen quad VAO/VBO
	vao uint32
	vbo uint32
}

// NewBlurEffect creates a reusable blur effect processor.
func (w *glWindow) NewBlurEffect() (*BlurEffect, error) {
	be := &BlurEffect{
		window: w,
	}

	// Create blur shader program
	program, err := createShaderProgram(w.gl, blurVertexShaderSource, blurFragmentShaderSource)
	if err != nil {
		return nil, fmt.Errorf("failed to create blur shader: %v", err)
	}
	be.program = program
	be.projUniform = w.gl.GetUniformLocation(program, "u_proj")
	be.modelUniform = w.gl.GetUniformLocation(program, "u_model")
	be.dirUniform = w.gl.GetUniformLocation(program, "u_direction")
	be.radiusUniform = w.gl.GetUniformLocation(program, "u_radius")

	// Create VAO/VBO for fullscreen quad
	var vao, vbo uint32
	w.gl.GenVertexArrays(1, &vao)
	w.gl.GenBuffers(1, &vbo)
	be.vao = vao
	be.vbo = vbo

	w.gl.BindVertexArray(vao)
	w.gl.BindBuffer(glpkg.ArrayBuffer, vbo)
	w.gl.BufferData(glpkg.ArrayBuffer, 6*8*4, nil, glpkg.DynamicDraw)

	// Set up vertex attributes
	posLoc := w.gl.GetAttribLocation(program, "a_position")
	texLoc := w.gl.GetAttribLocation(program, "a_texCoord")
	colLoc := w.gl.GetAttribLocation(program, "a_color")
	w.gl.VertexAttribPointer(uint32(posLoc), 2, glpkg.Float, false, 8*4, 0)
	w.gl.EnableVertexAttribArray(uint32(posLoc))
	w.gl.VertexAttribPointer(uint32(texLoc), 2, glpkg.Float, false, 8*4, 8)
	w.gl.EnableVertexAttribArray(uint32(texLoc))
	w.gl.VertexAttribPointer(uint32(colLoc), 4, glpkg.Float, false, 8*4, 16)
	w.gl.EnableVertexAttribArray(uint32(colLoc))

	return be, nil
}

// EnsureSize ensures the blur targets match the given dimensions.
func (be *BlurEffect) EnsureSize(width, height int) error {
	if width == be.width && height == be.height && be.rtA != nil {
		return nil
	}

	be.width = width
	be.height = height

	var err error
	if be.rtA != nil {
		err = be.rtA.Resize(width, height)
	} else {
		be.rtA, err = be.window.NewRenderTarget(width, height)
	}
	if err != nil {
		return err
	}

	if be.rtB != nil {
		err = be.rtB.Resize(width, height)
	} else {
		be.rtB, err = be.window.NewRenderTarget(width, height)
	}
	return err
}

// Apply performs a Gaussian blur on the source texture.
// Returns a texture containing the blurred result.
// The 'passes' parameter controls blur quality (1-4 recommended).
func (be *BlurEffect) Apply(source Texture, radius float32, passes int) Texture {
	if be.rtA == nil || be.rtB == nil {
		return source
	}

	gl := be.window.gl

	// Save current shader program
	oldProgram := be.window.shaderProgram

	// Use blur shader
	gl.UseProgram(be.program)

	// Set up orthographic projection for fullscreen quad
	proj := orthoMatrix(0, float32(be.width), float32(be.height), 0, -1, 1)
	gl.UniformMatrix4fv(be.projUniform, 1, false, &proj[0])
	model := IdentityMat4()
	gl.UniformMatrix4fv(be.modelUniform, 1, false, &model[0])
	gl.Uniform1f(be.radiusUniform, radius)

	// Initial blit: source -> rtA (no blur, just copy)
	// Source is a normal texture (screenshot), so use non-flipped UV
	be.rtA.Bind()
	gl.ClearColor(0, 0, 0, 0)
	gl.Clear(glpkg.ColorBufferBit)
	gl.Uniform2f(be.dirUniform, 0, 0)
	gl.Uniform1f(be.radiusUniform, 0) // No blur for initial copy
	be.drawFullscreenQuadFromTexture(source)
	be.rtA.Unbind()

	// Reset radius for blur passes
	gl.Uniform1f(be.radiusUniform, radius)

	src := be.rtA
	dst := be.rtB

	for i := 0; i < passes; i++ {
		// Horizontal pass - reading from FBO, so use flipped UV
		dst.Bind()
		gl.ClearColor(0, 0, 0, 0)
		gl.Clear(glpkg.ColorBufferBit)
		gl.Uniform2f(be.dirUniform, 1.0/float32(be.width), 0)
		be.drawFullscreenQuadFromFBO(src.Texture())
		dst.Unbind()

		src, dst = dst, src

		// Vertical pass - reading from FBO, so use flipped UV
		dst.Bind()
		gl.ClearColor(0, 0, 0, 0)
		gl.Clear(glpkg.ColorBufferBit)
		gl.Uniform2f(be.dirUniform, 0, 1.0/float32(be.height))
		be.drawFullscreenQuadFromFBO(src.Texture())
		dst.Unbind()

		src, dst = dst, src
	}

	// Restore original shader program
	gl.UseProgram(oldProgram)

	// Restore viewport to window size
	bw, bh := be.window.platform.BackingSize()
	gl.Viewport(0, 0, int32(bw), int32(bh))

	return src.Texture()
}

// drawFullscreenQuadFromFBO draws a fullscreen quad reading from an FBO texture.
// FBO textures have inverted Y, so we flip the V coordinate to compensate.
func (be *BlurEffect) drawFullscreenQuadFromFBO(tex Texture) {
	be.drawQuadWithUV(tex, true)
}

// drawFullscreenQuadFromTexture draws a fullscreen quad reading from a normal texture.
// Normal textures have (0,0) at top-left, so we use standard UV mapping.
func (be *BlurEffect) drawFullscreenQuadFromTexture(tex Texture) {
	be.drawQuadWithUV(tex, false)
}

func (be *BlurEffect) drawQuadWithUV(tex Texture, flipV bool) {
	gl := be.window.gl
	t, ok := tex.(*glTexture)
	if !ok {
		return
	}

	gl.ActiveTexture(glpkg.Texture0)
	gl.BindTexture(glpkg.Texture2D, t.id)
	texUniform := gl.GetUniformLocation(be.program, "u_texture")
	gl.Uniform1i(texUniform, 0)

	w := float32(be.width)
	h := float32(be.height)

	var vertices [6 * 8]float32
	if flipV {
		// Flipped V for FBO textures: FBO (0,0) is bottom-left, but we want top-left
		vertices = [6 * 8]float32{
			// Triangle 1
			0, 0, 0, 1, 1, 1, 1, 1, // position top-left, read tex bottom-left
			w, 0, 1, 1, 1, 1, 1, 1, // position top-right, read tex bottom-right
			0, h, 0, 0, 1, 1, 1, 1, // position bottom-left, read tex top-left
			// Triangle 2
			w, 0, 1, 1, 1, 1, 1, 1, // position top-right, read tex bottom-right
			w, h, 1, 0, 1, 1, 1, 1, // position bottom-right, read tex top-right
			0, h, 0, 0, 1, 1, 1, 1, // position bottom-left, read tex top-left
		}
	} else {
		// Standard UV for normal textures: (0,0) is top-left
		vertices = [6 * 8]float32{
			// Triangle 1
			0, 0, 0, 0, 1, 1, 1, 1, // position top-left, read tex top-left
			w, 0, 1, 0, 1, 1, 1, 1, // position top-right, read tex top-right
			0, h, 0, 1, 1, 1, 1, 1, // position bottom-left, read tex bottom-left
			// Triangle 2
			w, 0, 1, 0, 1, 1, 1, 1, // position top-right, read tex top-right
			w, h, 1, 1, 1, 1, 1, 1, // position bottom-right, read tex bottom-right
			0, h, 0, 1, 1, 1, 1, 1, // position bottom-left, read tex bottom-left
		}
	}

	gl.BindVertexArray(be.vao)
	gl.BindBuffer(glpkg.ArrayBuffer, be.vbo)
	gl.BufferSubData(glpkg.ArrayBuffer, 0, len(vertices)*4, unsafe.Pointer(&vertices[0]))
	gl.DrawArrays(glpkg.Triangles, 0, 6)
}

// Destroy releases GPU resources.
func (be *BlurEffect) Destroy() {
	if be.rtA != nil {
		be.rtA.Destroy()
		be.rtA = nil
	}
	if be.rtB != nil {
		be.rtB.Destroy()
		be.rtB = nil
	}
	if be.program != 0 {
		be.window.gl.DeleteProgram(be.program)
		be.program = 0
	}
	if be.vao != 0 {
		be.window.gl.DeleteVertexArrays(1, &be.vao)
		be.vao = 0
	}
	if be.vbo != 0 {
		be.window.gl.DeleteBuffers(1, &be.vbo)
		be.vbo = 0
	}
}
