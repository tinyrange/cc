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

func (r *Renderer) SetViewport(width, height int32) {
	if r != nil && r.stash != nil {
		// Apply scale factor to match the graphics system's coordinate system
		scaledWidth := float32(width) / r.scale
		scaledHeight := float32(height) / r.scale
		r.stash.SetViewport(int32(scaledWidth), int32(scaledHeight))
		r.stash.SetScale(r.scale)
		r.stash.SetGraphicsShader(r.graphicsShader)
	}
}
