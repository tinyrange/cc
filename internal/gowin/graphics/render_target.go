package graphics

import (
	"fmt"

	glpkg "github.com/tinyrange/cc/internal/gowin/gl"
)

// RenderTarget represents an off-screen render target (FBO + texture).
type RenderTarget interface {
	// Bind makes this render target the current drawing destination.
	// Call Unbind() when done to restore the default framebuffer.
	Bind()

	// Unbind restores the default framebuffer (screen).
	Unbind()

	// Texture returns the texture containing the rendered content.
	Texture() Texture

	// Size returns the dimensions of this render target.
	Size() (width, height int)

	// Resize changes the render target size (recreates GPU resources).
	Resize(width, height int) error

	// Destroy releases GPU resources.
	Destroy()
}

type glRenderTarget struct {
	fbo     uint32
	texture *glTexture
	width   int
	height  int
	gl      glpkg.OpenGL
	window  *glWindow
}

// NewRenderTarget creates an off-screen render target for render-to-texture.
func (w *glWindow) NewRenderTarget(width, height int) (RenderTarget, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid render target size: %dx%d", width, height)
	}

	rt := &glRenderTarget{
		width:  width,
		height: height,
		gl:     w.gl,
		window: w,
	}

	if err := rt.create(); err != nil {
		return nil, err
	}

	return rt, nil
}

func (rt *glRenderTarget) create() error {
	gl := rt.gl

	// Create texture for color attachment
	var texID uint32
	gl.GenTextures(1, &texID)
	gl.BindTexture(glpkg.Texture2D, texID)
	gl.TexParameteri(glpkg.Texture2D, glpkg.TextureMinFilter, glpkg.Linear)
	gl.TexParameteri(glpkg.Texture2D, glpkg.TextureMagFilter, glpkg.Linear)
	gl.TexParameteri(glpkg.Texture2D, glpkg.TextureWrapS, glpkg.ClampToEdge)
	gl.TexParameteri(glpkg.Texture2D, glpkg.TextureWrapT, glpkg.ClampToEdge)
	gl.TexImage2D(
		glpkg.Texture2D,
		0,
		int32(glpkg.RGBA),
		int32(rt.width),
		int32(rt.height),
		0,
		glpkg.RGBA,
		glpkg.UnsignedByte,
		nil,
	)

	rt.texture = &glTexture{id: texID, w: rt.width, h: rt.height}

	// Create framebuffer
	gl.GenFramebuffers(1, &rt.fbo)
	gl.BindFramebuffer(glpkg.Framebuffer, rt.fbo)
	gl.FramebufferTexture2D(
		glpkg.Framebuffer,
		glpkg.ColorAttachment0,
		glpkg.Texture2D,
		texID,
		0,
	)

	// Check completeness
	status := gl.CheckFramebufferStatus(glpkg.Framebuffer)
	if status != glpkg.FramebufferComplete {
		rt.Destroy()
		return fmt.Errorf("framebuffer incomplete: status 0x%X", status)
	}

	// Restore default framebuffer
	gl.BindFramebuffer(glpkg.Framebuffer, 0)

	return nil
}

func (rt *glRenderTarget) Bind() {
	rt.gl.BindFramebuffer(glpkg.Framebuffer, rt.fbo)
	rt.gl.Viewport(0, 0, int32(rt.width), int32(rt.height))
}

func (rt *glRenderTarget) Unbind() {
	rt.gl.BindFramebuffer(glpkg.Framebuffer, 0)
	// Caller is responsible for restoring viewport
}

func (rt *glRenderTarget) Texture() Texture {
	return rt.texture
}

func (rt *glRenderTarget) Size() (int, int) {
	return rt.width, rt.height
}

func (rt *glRenderTarget) Resize(width, height int) error {
	if width == rt.width && height == rt.height {
		return nil
	}
	rt.Destroy()
	rt.width = width
	rt.height = height
	return rt.create()
}

func (rt *glRenderTarget) Destroy() {
	if rt.fbo != 0 {
		rt.gl.DeleteFramebuffers(1, &rt.fbo)
		rt.fbo = 0
	}
	if rt.texture != nil && rt.texture.id != 0 {
		id := rt.texture.id
		rt.gl.DeleteTextures(1, &id)
		rt.texture = nil
	}
}
