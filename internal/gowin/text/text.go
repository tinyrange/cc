package text

import (
	_ "embed"
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/graphics"
)

//go:embed RobotoMono-VariableFont_wght.ttf
var EMBEDDED_FONT []byte

type Renderer struct {
	stash          *Stash
	font           int
	scale          float32
	graphicsShader uint32
}

func Load(win graphics.Window) (*Renderer, error) {
	gl, err := win.PlatformWindow().GL()
	if err != nil {
		return nil, err
	}

	stash := New(gl, 1024, 1024)
	stash.SetYInverted(true)
	fontIdx, err := stash.AddFontFromMemory(EMBEDDED_FONT)
	if err != nil {
		return nil, err
	}

	return &Renderer{
		stash:          stash,
		font:           fontIdx,
		scale:          win.Scale(),
		graphicsShader: win.GetShaderProgram(),
	}, nil
}

func (r *Renderer) RenderText(s string, x, y float32, size float64, c color.Color) float32 {
	if r == nil || r.stash == nil {
		return x
	}

	r.stash.BeginDraw()
	rgba := graphics.ColorToFloat32(c)
	next := r.stash.DrawText(r.font, size, float64(x), float64(y), s, rgba)
	r.stash.EndDraw()
	return float32(next)
}

// BeginBatch starts a batched text rendering session. Call AddText to add text,
// then EndBatch to flush all text in a single draw call.
func (r *Renderer) BeginBatch() {
	if r == nil || r.stash == nil {
		return
	}
	r.stash.BeginDraw()
}

// AddText adds text to the current batch without flushing. Must be called between
// BeginBatch and EndBatch. Returns the x-advance.
func (r *Renderer) AddText(s string, x, y float32, size float64, c color.Color) float32 {
	if r == nil || r.stash == nil {
		return x
	}
	rgba := graphics.ColorToFloat32(c)
	return float32(r.stash.DrawText(r.font, size, float64(x), float64(y), s, rgba))
}

// EndBatch flushes all batched text in a single draw call.
func (r *Renderer) EndBatch() {
	if r == nil || r.stash == nil {
		return
	}
	r.stash.EndDraw()
}

func (r *Renderer) SetViewport(width, height int32) {
	if r != nil && r.stash != nil {
		// The graphics coordinate system is already logical units.
		r.stash.SetViewport(width, height)
		r.stash.SetScale(r.scale)
		r.stash.SetGraphicsShader(r.graphicsShader)
	}
}

// Advance returns the x-advance (in logical pixels) for rendering s at the given size.
func (r *Renderer) Advance(size float64, s string) float32 {
	if r == nil || r.stash == nil {
		return 0
	}
	return float32(r.stash.GetAdvance(r.font, size, s))
}

// LineHeight returns the line height (in logical pixels) at the given size.
func (r *Renderer) LineHeight(size float64) float32 {
	if r == nil || r.stash == nil {
		return 0
	}
	_, _, lineHeight := r.stash.VMetrics(r.font, size)
	return float32(lineHeight)
}
