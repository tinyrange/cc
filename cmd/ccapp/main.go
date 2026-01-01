package main

import (
	"fmt"
	"image/color"
	"os"
	"runtime"

	"github.com/tinyrange/cc/internal/gowin/graphics"
)

type Application struct {
	window graphics.Window
}

func (app *Application) Run() error {
	var err error

	app.window, err = graphics.New("CrumbleCracker", 1024, 768)
	if err != nil {
		return fmt.Errorf("failed to create window: %w", err)
	}

	app.window.SetClear(true)
	app.window.SetClearColor(color.RGBA{R: 10, G: 10, B: 10, A: 255})

	return app.window.Loop(func(f graphics.Frame) error {
		f.RenderQuad(0, 0, 1024, 768, nil, graphics.ColorWhite)
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
