package main

import (
	"fmt"
	"image/color"
	"os"
	"runtime"

	"github.com/tinyrange/cc/internal/assets"
	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
)

type Application struct {
	window graphics.Window
	text   *text.Renderer
	logo   *graphics.SVG
}

func (app *Application) Run() error {
	var err error

	app.window, err = graphics.New("CrumbleCracker", 1024, 768)
	if err != nil {
		return fmt.Errorf("failed to create window: %w", err)
	}

	app.text, err = text.Load(app.window)
	if err != nil {
		return fmt.Errorf("failed to create text renderer: %w", err)
	}

	app.logo, err = graphics.LoadSVG(app.window, assets.LogoWhite)
	if err != nil {
		return fmt.Errorf("failed to load logo svg: %w", err)
	}

	app.window.SetClear(true)
	app.window.SetClearColor(color.RGBA{R: 10, G: 10, B: 10, A: 255})

	return app.window.Loop(func(f graphics.Frame) error {
		w, h := f.WindowSize()
		app.text.SetViewport(int32(w), int32(h))

		app.text.RenderText("Hello, World!", 50, 50, 16, graphics.ColorWhite)

		if app.logo != nil {
			side := float32(w)
			if h < w {
				side = float32(h)
			}
			if side > 100 {
				side -= 100
			}
			x := (float32(w) - side) * 0.5
			y := (float32(h) - side) * 0.5
			app.logo.Draw(f, x, y, side, side)
		}

		return nil
	})
}

func main() {
	if runtime.GOOS == "darwin" {
		runtime.LockOSThread()
	}

	app := Application{}

	if err := app.Run(); err != nil {
		os.Exit(1)
	}
}
