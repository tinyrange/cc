package graphics

import (
	"image"
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/window"
)

// ColorToFloat32 converts a color.Color to RGBA float32 values in the range [0, 1].
func ColorToFloat32(c color.Color) [4]float32 {
	r, g, b, a := c.RGBA()
	// RGBA() returns values in range [0, 0xffff], convert to [0, 1]
	return [4]float32{
		float32(r) / 0xffff,
		float32(g) / 0xffff,
		float32(b) / 0xffff,
		float32(a) / 0xffff,
	}
}

// Default colors using image/color types
var (
	ColorBlack     = color.RGBA{R: 0, G: 0, B: 0, A: 255}
	ColorWhite     = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	ColorRed       = color.RGBA{R: 255, G: 0, B: 0, A: 255}
	ColorGreen     = color.RGBA{R: 0, G: 255, B: 0, A: 255}
	ColorBlue      = color.RGBA{R: 0, G: 0, B: 255, A: 255}
	ColorYellow    = color.RGBA{R: 255, G: 255, B: 0, A: 255}
	ColorCyan      = color.RGBA{R: 0, G: 255, B: 255, A: 255}
	ColorMagenta   = color.RGBA{R: 255, G: 0, B: 255, A: 255}
	ColorGray      = color.RGBA{R: 128, G: 128, B: 128, A: 255}
	ColorDarkGray  = color.RGBA{R: 64, G: 64, B: 64, A: 255}
	ColorLightGray = color.RGBA{R: 192, G: 192, B: 192, A: 255}
)

type Frame interface {
	WindowSize() (width, height int)
	CursorPos() (x, y float32)

	GetKeyState(key window.Key) window.KeyState
	GetButtonState(button window.Button) window.ButtonState
	// TextInput returns the UTF-8 text entered since the last call to TextInput.
	TextInput() string

	RenderQuad(x, y, width, height float32, tex Texture, color color.Color)
	RenderMesh(mesh Mesh, opts DrawOptions)

	Screenshot() (image.Image, error)
}

type Texture interface {
	Size() (width, height int)
}

type Window interface {
	// Return the platform-specific window implementation.
	PlatformWindow() window.Window

	// Create a new texture from an image.
	NewTexture(image.Image) (Texture, error)
	// NewMesh uploads a set of vertices/indices to the GPU for repeated rendering.
	NewMesh(vertices []Vertex, indices []uint32, tex Texture) (Mesh, error)
	// NewDynamicMesh creates a mesh that supports efficient partial vertex updates.
	NewDynamicMesh(maxVertices, maxIndices int, tex Texture) (DynamicMesh, error)

	SetClear(enabled bool)
	SetClearColor(color color.Color)

	// Scale returns the display scaling factor (e.g., 1.0 for 96 DPI, 2.0 for 192 DPI).
	Scale() float32

	// Call f for each frame until it returns an error.
	Loop(func(f Frame) error) error

	// GetShaderProgram returns the graphics shader program ID for state restoration.
	GetShaderProgram() uint32

	// NewRenderTarget creates an off-screen render target for render-to-texture.
	NewRenderTarget(width, height int) (RenderTarget, error)

	// NewBlurEffect creates a reusable blur effect processor.
	NewBlurEffect() (*BlurEffect, error)
}

// DynamicMesh supports efficient partial vertex updates via BufferSubData.
type DynamicMesh interface {
	Mesh
	// UpdateVertices updates a range of vertices starting at the given offset.
	UpdateVertices(offset int, vertices []Vertex)
	// UpdateAllVertices updates the entire vertex buffer.
	UpdateAllVertices(vertices []Vertex)
	// UpdateIndices updates the index buffer.
	UpdateIndices(indices []uint32)
	// Resize changes the buffer capacity (recreates GPU buffers).
	Resize(vertexCount, indexCount int)
	// VertexCount returns the current vertex capacity.
	VertexCount() int
}

// Each platform implements a New() method to return a Window.
