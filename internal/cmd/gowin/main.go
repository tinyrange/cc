package main

import (
	"flag"
	"image"
	"image/color"
	"os"

	"github.com/tinyrange/cc/internal/gowin/graphics"
)

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.Parse(os.Args[1:])

	win, err := graphics.New("gowin", 1024, 768)
	if err != nil {
		panic(err)
	}

	// Make a 1x1 white texture.
	tex := image.NewRGBA(image.Rect(0, 0, 1, 1))
	tex.SetRGBA(0, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	texture, err := win.NewTexture(tex)
	if err != nil {
		panic(err)
	}

	win.Loop(func(f graphics.Frame) error {
		f.RenderQuad(100, 100, 100, 100, texture, color.White)
		return nil
	})
}
