package virtio

import (
	"log/slog"
	"sync"
	"unsafe"

	gll "github.com/tinyrange/cc/internal/gowin/gl"
	"github.com/tinyrange/cc/internal/gowin/window"
)

// DisplayManager coordinates GPU and Input devices with a Gowin window.
// It handles the render loop on the main thread and forwards input events.
type DisplayManager struct {
	GPU      *GPU
	Keyboard *Input
	Tablet   *Input

	window window.Window

	mu        sync.Mutex
	pixels    []byte
	width     uint32
	height    uint32
	format    uint32
	dirty     bool
	textureID uint32

	// GL resources
	shaderProgram uint32
	vao           uint32
	vbo           uint32
	initialized   bool

	// Track previous input state for diffing
	prevKeys    map[window.Key]bool
	prevButtons map[window.Button]bool
	prevCursorX float32
	prevCursorY float32
}

// NewDisplayManager creates a new display manager.
func NewDisplayManager(gpu *GPU, keyboard *Input, tablet *Input) *DisplayManager {
	dm := &DisplayManager{
		GPU:         gpu,
		Keyboard:    keyboard,
		Tablet:      tablet,
		prevKeys:    make(map[window.Key]bool),
		prevButtons: make(map[window.Button]bool),
	}

	// Set up GPU flush callback
	if gpu != nil {
		gpu.OnFlush = dm.onGPUFlush
	}

	return dm
}

// onGPUFlush is called when the GPU flushes a resource
func (dm *DisplayManager) onGPUFlush(resourceID uint32, x, y, w, h uint32, pixels []byte, stride uint32) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// Store the latest framebuffer data
	dm.pixels = make([]byte, len(pixels))
	copy(dm.pixels, pixels)
	dm.width = w
	dm.height = h
	dm.dirty = true
}

// SetWindow sets the Gowin window to use for display and input
func (dm *DisplayManager) SetWindow(w window.Window) {
	dm.window = w

	// Notify GPU of logical window size (physical / scale).
	// The guest renders at logical resolution; OpenGL scales to physical.
	if dm.GPU != nil {
		width, height := w.BackingSize()
		scale := w.Scale()
		if scale < 1.0 {
			scale = 1.0
		}
		logicalWidth := uint32(float32(width) / scale)
		logicalHeight := uint32(float32(height) / scale)
		dm.GPU.SetDisplaySize(logicalWidth, logicalHeight)
	}
}

// Poll polls the window for events and forwards them to input devices.
// Returns false if the window was closed.
func (dm *DisplayManager) Poll() bool {
	if dm.window == nil {
		return true
	}

	if !dm.window.Poll() {
		return false
	}

	// Handle window resize - use logical dimensions for GPU
	width, height := dm.window.BackingSize()
	scale := dm.window.Scale()
	if scale < 1.0 {
		scale = 1.0
	}
	logicalWidth := uint32(float32(width) / scale)
	logicalHeight := uint32(float32(height) / scale)
	if dm.GPU != nil && (logicalWidth != dm.width || logicalHeight != dm.height) {
		dm.GPU.SetDisplaySize(logicalWidth, logicalHeight)
	}

	// Process keyboard input
	dm.processKeyboardInput()

	// Process mouse/tablet input
	dm.processTabletInput()

	return true
}

func (dm *DisplayManager) processKeyboardInput() {
	if dm.Keyboard == nil {
		return
	}

	// Check all keys in the mapping
	for gowinKey, linuxCode := range GowinKeyToLinux {
		if linuxCode == KEY_RESERVED {
			continue
		}

		state := dm.window.GetKeyState(gowinKey)
		isDown := state.IsDown()
		wasDown := dm.prevKeys[gowinKey]

		if isDown && !wasDown {
			// Key just pressed
			dm.Keyboard.InjectKeyEvent(linuxCode, true)
		} else if !isDown && wasDown {
			// Key just released
			dm.Keyboard.InjectKeyEvent(linuxCode, false)
		}

		dm.prevKeys[gowinKey] = isDown
	}
}

func (dm *DisplayManager) processTabletInput() {
	if dm.Tablet == nil {
		return
	}

	// Get cursor position
	cursorX, cursorY := dm.window.Cursor()
	width, height := dm.window.BackingSize()

	// Check if cursor moved
	if cursorX != dm.prevCursorX || cursorY != dm.prevCursorY {
		x := NormalizeTabletCoord(cursorX, width)
		y := NormalizeTabletCoord(cursorY, height)
		dm.Tablet.InjectMouseMove(x, y)
		dm.prevCursorX = cursorX
		dm.prevCursorY = cursorY
	}

	// Check button states
	for gowinButton, linuxCode := range GowinButtonToLinux {
		state := dm.window.GetButtonState(gowinButton)
		isDown := state.IsDown()
		wasDown := dm.prevButtons[gowinButton]

		if isDown != wasDown {
			dm.Tablet.InjectButtonEvent(linuxCode, isDown)
			dm.Tablet.InjectSynReport()
			dm.prevButtons[gowinButton] = isDown
		}
	}
}

const vertexShaderSource = `#version 130
in vec2 position;
in vec2 texCoord;
out vec2 fragTexCoord;
void main() {
    gl_Position = vec4(position, 0.0, 1.0);
    fragTexCoord = texCoord;
}
` + "\x00"

const fragmentShaderSource = `#version 130
in vec2 fragTexCoord;
out vec4 fragColor;
uniform sampler2D tex;
void main() {
    fragColor = texture(tex, fragTexCoord);
}
` + "\x00"

// initGL initializes OpenGL resources for rendering
func (dm *DisplayManager) initGL(gl gll.OpenGL) error {
	if dm.initialized {
		return nil
	}

	// Compile shaders
	vertexShader := gl.CreateShader(gll.VertexShader)
	gl.ShaderSource(vertexShader, vertexShaderSource)
	gl.CompileShader(vertexShader)

	var status int32
	gl.GetShaderiv(vertexShader, gll.CompileStatus, &status)
	if status == 0 {
		log := gl.GetShaderInfoLog(vertexShader)
		slog.Error("display: vertex shader compile failed", "log", log)
		return nil
	}

	fragmentShader := gl.CreateShader(gll.FragmentShader)
	gl.ShaderSource(fragmentShader, fragmentShaderSource)
	gl.CompileShader(fragmentShader)

	gl.GetShaderiv(fragmentShader, gll.CompileStatus, &status)
	if status == 0 {
		log := gl.GetShaderInfoLog(fragmentShader)
		slog.Error("display: fragment shader compile failed", "log", log)
		return nil
	}

	// Link program
	dm.shaderProgram = gl.CreateProgram()
	gl.AttachShader(dm.shaderProgram, vertexShader)
	gl.AttachShader(dm.shaderProgram, fragmentShader)
	gl.LinkProgram(dm.shaderProgram)

	gl.GetProgramiv(dm.shaderProgram, gll.LinkStatus, &status)
	if status == 0 {
		log := gl.GetProgramInfoLog(dm.shaderProgram)
		slog.Error("display: program link failed", "log", log)
		return nil
	}

	gl.DeleteShader(vertexShader)
	gl.DeleteShader(fragmentShader)

	// Create VAO and VBO for fullscreen quad
	gl.GenVertexArrays(1, &dm.vao)
	gl.BindVertexArray(dm.vao)

	gl.GenBuffers(1, &dm.vbo)
	gl.BindBuffer(gll.ArrayBuffer, dm.vbo)

	// Fullscreen quad vertices: position (x,y) + texcoord (u,v)
	// Flip Y texture coord to account for OpenGL's bottom-left origin
	vertices := []float32{
		// position    // texcoord
		-1.0, -1.0, 0.0, 1.0, // bottom-left
		1.0, -1.0, 1.0, 1.0, // bottom-right
		-1.0, 1.0, 0.0, 0.0, // top-left
		1.0, 1.0, 1.0, 0.0, // top-right
	}

	gl.BufferData(gll.ArrayBuffer, len(vertices)*4, unsafe.Pointer(&vertices[0]), gll.StaticDraw)

	positionLoc := gl.GetAttribLocation(dm.shaderProgram, "position\x00")
	texCoordLoc := gl.GetAttribLocation(dm.shaderProgram, "texCoord\x00")

	if positionLoc >= 0 {
		gl.EnableVertexAttribArray(uint32(positionLoc))
		gl.VertexAttribPointer(uint32(positionLoc), 2, gll.Float, false, 4*4, nil)
	}

	if texCoordLoc >= 0 {
		gl.EnableVertexAttribArray(uint32(texCoordLoc))
		// Offset for texture coordinates (skip 2 floats = 8 bytes)
		// #nosec G103 - this is a standard OpenGL vertex attribute offset pattern
		texCoordOffset := unsafe.Pointer(uintptr(2 * 4)) //nolint:staticcheck
		gl.VertexAttribPointer(uint32(texCoordLoc), 2, gll.Float, false, 4*4, texCoordOffset)
	}

	gl.BindVertexArray(0)

	// Create texture
	gl.GenTextures(1, &dm.textureID)
	gl.BindTexture(gll.Texture2D, dm.textureID)
	gl.TexParameteri(gll.Texture2D, gll.TextureMinFilter, int32(gll.Linear))
	gl.TexParameteri(gll.Texture2D, gll.TextureMagFilter, int32(gll.Linear))
	gl.TexParameteri(gll.Texture2D, gll.TextureWrapS, int32(gll.ClampToEdge))
	gl.TexParameteri(gll.Texture2D, gll.TextureWrapT, int32(gll.ClampToEdge))

	dm.initialized = true
	return nil
}

// Render renders the current framebuffer to the window.
// This should be called from the main thread's render loop.
func (dm *DisplayManager) Render() {
	if dm.window == nil {
		return
	}

	gl, err := dm.window.GL()
	if err != nil {
		slog.Error("display: failed to get GL context", "err", err)
		return
	}

	if !dm.initialized {
		if err := dm.initGL(gl); err != nil {
			slog.Error("display: failed to initialize GL", "err", err)
			return
		}
	}

	dm.mu.Lock()
	pixels := dm.pixels
	width := dm.width
	height := dm.height
	dirty := dm.dirty
	dm.dirty = false
	dm.mu.Unlock()

	winWidth, winHeight := dm.window.BackingSize()
	gl.Viewport(0, 0, int32(winWidth), int32(winHeight))

	if pixels == nil || width == 0 || height == 0 {
		// No framebuffer yet, just clear to black
		gl.ClearColor(0, 0, 0, 1)
		gl.Clear(gll.ColorBufferBit)
		return
	}

	// Update texture if dirty
	if dirty {
		// Convert BGRA to RGBA if needed
		rgbaPixels := convertBGRAtoRGBA(pixels, width, height)

		gl.BindTexture(gll.Texture2D, dm.textureID)
		gl.TexImage2D(
			gll.Texture2D,
			0,
			int32(gll.RGBA),
			int32(width),
			int32(height),
			0,
			gll.RGBA,
			gll.UnsignedByte,
			unsafe.Pointer(&rgbaPixels[0]),
		)
	}

	// Clear and render textured quad
	gl.ClearColor(0, 0, 0, 1)
	gl.Clear(gll.ColorBufferBit)

	gl.UseProgram(dm.shaderProgram)

	gl.ActiveTexture(gll.Texture0)
	gl.BindTexture(gll.Texture2D, dm.textureID)

	texLoc := gl.GetUniformLocation(dm.shaderProgram, "tex\x00")
	if texLoc >= 0 {
		gl.Uniform1i(texLoc, 0)
	}

	gl.BindVertexArray(dm.vao)
	gl.DrawArrays(gll.TriangleStrip, 0, 4)
	gl.BindVertexArray(0)

	gl.UseProgram(0)
}

// Swap swaps the window buffers
func (dm *DisplayManager) Swap() {
	if dm.window != nil {
		dm.window.Swap()
	}
}

// convertBGRAtoRGBA converts BGRA pixel data to RGBA
func convertBGRAtoRGBA(pixels []byte, width, height uint32) []byte {
	if len(pixels) == 0 {
		return pixels
	}

	// Create a copy to avoid modifying the original
	result := make([]byte, len(pixels))

	for i := 0; i < len(pixels); i += 4 {
		if i+3 < len(pixels) {
			// BGRA -> RGBA: swap B and R
			result[i+0] = pixels[i+2] // R = B
			result[i+1] = pixels[i+1] // G = G
			result[i+2] = pixels[i+0] // B = R
			result[i+3] = pixels[i+3] // A = A
		}
	}

	return result
}

// GetFramebuffer returns the current framebuffer for external use
func (dm *DisplayManager) GetFramebuffer() (pixels []byte, width, height uint32, ok bool) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if dm.pixels == nil || dm.width == 0 || dm.height == 0 {
		return nil, 0, 0, false
	}

	pixels = make([]byte, len(dm.pixels))
	copy(pixels, dm.pixels)
	return pixels, dm.width, dm.height, true
}
