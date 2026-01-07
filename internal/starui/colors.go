package starui

import (
	"fmt"
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/ui"
	"go.starlark.net/starlark"
)

// Tailwind CSS color palette
// https://tailwindcss.com/docs/customizing-colors

var grayPalette = map[int]color.Color{
	50:  color.RGBA{R: 249, G: 250, B: 251, A: 255},
	100: color.RGBA{R: 243, G: 244, B: 246, A: 255},
	200: color.RGBA{R: 229, G: 231, B: 235, A: 255},
	300: color.RGBA{R: 209, G: 213, B: 219, A: 255},
	400: color.RGBA{R: 156, G: 163, B: 175, A: 255},
	500: color.RGBA{R: 107, G: 114, B: 128, A: 255},
	600: color.RGBA{R: 75, G: 85, B: 99, A: 255},
	700: color.RGBA{R: 55, G: 65, B: 81, A: 255},
	800: color.RGBA{R: 31, G: 41, B: 55, A: 255},
	900: color.RGBA{R: 17, G: 24, B: 39, A: 255},
	950: color.RGBA{R: 3, G: 7, B: 18, A: 255},
}

var redPalette = map[int]color.Color{
	50:  color.RGBA{R: 254, G: 242, B: 242, A: 255},
	100: color.RGBA{R: 254, G: 226, B: 226, A: 255},
	200: color.RGBA{R: 254, G: 202, B: 202, A: 255},
	300: color.RGBA{R: 252, G: 165, B: 165, A: 255},
	400: color.RGBA{R: 248, G: 113, B: 113, A: 255},
	500: color.RGBA{R: 239, G: 68, B: 68, A: 255},
	600: color.RGBA{R: 220, G: 38, B: 38, A: 255},
	700: color.RGBA{R: 185, G: 28, B: 28, A: 255},
	800: color.RGBA{R: 153, G: 27, B: 27, A: 255},
	900: color.RGBA{R: 127, G: 29, B: 29, A: 255},
	950: color.RGBA{R: 69, G: 10, B: 10, A: 255},
}

var orangePalette = map[int]color.Color{
	50:  color.RGBA{R: 255, G: 247, B: 237, A: 255},
	100: color.RGBA{R: 255, G: 237, B: 213, A: 255},
	200: color.RGBA{R: 254, G: 215, B: 170, A: 255},
	300: color.RGBA{R: 253, G: 186, B: 116, A: 255},
	400: color.RGBA{R: 251, G: 146, B: 60, A: 255},
	500: color.RGBA{R: 249, G: 115, B: 22, A: 255},
	600: color.RGBA{R: 234, G: 88, B: 12, A: 255},
	700: color.RGBA{R: 194, G: 65, B: 12, A: 255},
	800: color.RGBA{R: 154, G: 52, B: 18, A: 255},
	900: color.RGBA{R: 124, G: 45, B: 18, A: 255},
	950: color.RGBA{R: 67, G: 20, B: 7, A: 255},
}

var yellowPalette = map[int]color.Color{
	50:  color.RGBA{R: 254, G: 252, B: 232, A: 255},
	100: color.RGBA{R: 254, G: 249, B: 195, A: 255},
	200: color.RGBA{R: 254, G: 240, B: 138, A: 255},
	300: color.RGBA{R: 253, G: 224, B: 71, A: 255},
	400: color.RGBA{R: 250, G: 204, B: 21, A: 255},
	500: color.RGBA{R: 234, G: 179, B: 8, A: 255},
	600: color.RGBA{R: 202, G: 138, B: 4, A: 255},
	700: color.RGBA{R: 161, G: 98, B: 7, A: 255},
	800: color.RGBA{R: 133, G: 77, B: 14, A: 255},
	900: color.RGBA{R: 113, G: 63, B: 18, A: 255},
	950: color.RGBA{R: 66, G: 32, B: 6, A: 255},
}

var greenPalette = map[int]color.Color{
	50:  color.RGBA{R: 240, G: 253, B: 244, A: 255},
	100: color.RGBA{R: 220, G: 252, B: 231, A: 255},
	200: color.RGBA{R: 187, G: 247, B: 208, A: 255},
	300: color.RGBA{R: 134, G: 239, B: 172, A: 255},
	400: color.RGBA{R: 74, G: 222, B: 128, A: 255},
	500: color.RGBA{R: 34, G: 197, B: 94, A: 255},
	600: color.RGBA{R: 22, G: 163, B: 74, A: 255},
	700: color.RGBA{R: 21, G: 128, B: 61, A: 255},
	800: color.RGBA{R: 22, G: 101, B: 52, A: 255},
	900: color.RGBA{R: 20, G: 83, B: 45, A: 255},
	950: color.RGBA{R: 5, G: 46, B: 22, A: 255},
}

var bluePalette = map[int]color.Color{
	50:  color.RGBA{R: 239, G: 246, B: 255, A: 255},
	100: color.RGBA{R: 219, G: 234, B: 254, A: 255},
	200: color.RGBA{R: 191, G: 219, B: 254, A: 255},
	300: color.RGBA{R: 147, G: 197, B: 253, A: 255},
	400: color.RGBA{R: 96, G: 165, B: 250, A: 255},
	500: color.RGBA{R: 59, G: 130, B: 246, A: 255},
	600: color.RGBA{R: 37, G: 99, B: 235, A: 255},
	700: color.RGBA{R: 29, G: 78, B: 216, A: 255},
	800: color.RGBA{R: 30, G: 64, B: 175, A: 255},
	900: color.RGBA{R: 30, G: 58, B: 138, A: 255},
	950: color.RGBA{R: 23, G: 37, B: 84, A: 255},
}

var indigoPalette = map[int]color.Color{
	50:  color.RGBA{R: 238, G: 242, B: 255, A: 255},
	100: color.RGBA{R: 224, G: 231, B: 255, A: 255},
	200: color.RGBA{R: 199, G: 210, B: 254, A: 255},
	300: color.RGBA{R: 165, G: 180, B: 252, A: 255},
	400: color.RGBA{R: 129, G: 140, B: 248, A: 255},
	500: color.RGBA{R: 99, G: 102, B: 241, A: 255},
	600: color.RGBA{R: 79, G: 70, B: 229, A: 255},
	700: color.RGBA{R: 67, G: 56, B: 202, A: 255},
	800: color.RGBA{R: 55, G: 48, B: 163, A: 255},
	900: color.RGBA{R: 49, G: 46, B: 129, A: 255},
	950: color.RGBA{R: 30, G: 27, B: 75, A: 255},
}

var purplePalette = map[int]color.Color{
	50:  color.RGBA{R: 250, G: 245, B: 255, A: 255},
	100: color.RGBA{R: 243, G: 232, B: 255, A: 255},
	200: color.RGBA{R: 233, G: 213, B: 255, A: 255},
	300: color.RGBA{R: 216, G: 180, B: 254, A: 255},
	400: color.RGBA{R: 192, G: 132, B: 252, A: 255},
	500: color.RGBA{R: 168, G: 85, B: 247, A: 255},
	600: color.RGBA{R: 147, G: 51, B: 234, A: 255},
	700: color.RGBA{R: 126, G: 34, B: 206, A: 255},
	800: color.RGBA{R: 107, G: 33, B: 168, A: 255},
	900: color.RGBA{R: 88, G: 28, B: 135, A: 255},
	950: color.RGBA{R: 59, G: 7, B: 100, A: 255},
}

var pinkPalette = map[int]color.Color{
	50:  color.RGBA{R: 253, G: 242, B: 248, A: 255},
	100: color.RGBA{R: 252, G: 231, B: 243, A: 255},
	200: color.RGBA{R: 251, G: 207, B: 232, A: 255},
	300: color.RGBA{R: 249, G: 168, B: 212, A: 255},
	400: color.RGBA{R: 244, G: 114, B: 182, A: 255},
	500: color.RGBA{R: 236, G: 72, B: 153, A: 255},
	600: color.RGBA{R: 219, G: 39, B: 119, A: 255},
	700: color.RGBA{R: 190, G: 24, B: 93, A: 255},
	800: color.RGBA{R: 157, G: 23, B: 77, A: 255},
	900: color.RGBA{R: 131, G: 24, B: 67, A: 255},
	950: color.RGBA{R: 80, G: 7, B: 36, A: 255},
}

// Color builtin functions

func builtinRGB(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var r, g, b int
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "r", &r, "g", &g, "b", &b); err != nil {
		return nil, err
	}
	return &ColorValue{Color: color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}}, nil
}

func builtinRGBA(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var r, g, b, a int
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "r", &r, "g", &g, "b", &b, "a", &a); err != nil {
		return nil, err
	}
	return &ColorValue{Color: color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(a)}}, nil
}

func colorFromPalette(palette map[int]color.Color, shade int) color.Color {
	if c, ok := palette[shade]; ok {
		return c
	}
	// Default to 500 if invalid shade
	return palette[500]
}

func builtinColorGray(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var shade int = 500
	if len(args) > 0 {
		if s, ok := starlark.AsInt32(args[0]); ok {
			shade = int(s)
		}
	}
	return &ColorValue{Color: colorFromPalette(grayPalette, shade)}, nil
}

func builtinColorRed(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var shade int = 500
	if len(args) > 0 {
		if s, ok := starlark.AsInt32(args[0]); ok {
			shade = int(s)
		}
	}
	return &ColorValue{Color: colorFromPalette(redPalette, shade)}, nil
}

func builtinColorOrange(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var shade int = 500
	if len(args) > 0 {
		if s, ok := starlark.AsInt32(args[0]); ok {
			shade = int(s)
		}
	}
	return &ColorValue{Color: colorFromPalette(orangePalette, shade)}, nil
}

func builtinColorYellow(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var shade int = 500
	if len(args) > 0 {
		if s, ok := starlark.AsInt32(args[0]); ok {
			shade = int(s)
		}
	}
	return &ColorValue{Color: colorFromPalette(yellowPalette, shade)}, nil
}

func builtinColorGreen(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var shade int = 500
	if len(args) > 0 {
		if s, ok := starlark.AsInt32(args[0]); ok {
			shade = int(s)
		}
	}
	return &ColorValue{Color: colorFromPalette(greenPalette, shade)}, nil
}

func builtinColorBlue(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var shade int = 500
	if len(args) > 0 {
		if s, ok := starlark.AsInt32(args[0]); ok {
			shade = int(s)
		}
	}
	return &ColorValue{Color: colorFromPalette(bluePalette, shade)}, nil
}

func builtinColorIndigo(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var shade int = 500
	if len(args) > 0 {
		if s, ok := starlark.AsInt32(args[0]); ok {
			shade = int(s)
		}
	}
	return &ColorValue{Color: colorFromPalette(indigoPalette, shade)}, nil
}

func builtinColorPurple(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var shade int = 500
	if len(args) > 0 {
		if s, ok := starlark.AsInt32(args[0]); ok {
			shade = int(s)
		}
	}
	return &ColorValue{Color: colorFromPalette(purplePalette, shade)}, nil
}

func builtinColorPink(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var shade int = 500
	if len(args) > 0 {
		if s, ok := starlark.AsInt32(args[0]); ok {
			shade = int(s)
		}
	}
	return &ColorValue{Color: colorFromPalette(pinkPalette, shade)}, nil
}

func builtinColorWhite(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return &ColorValue{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}}, nil
}

func builtinColorBlack(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return &ColorValue{Color: color.RGBA{R: 0, G: 0, B: 0, A: 255}}, nil
}

func builtinColorTransparent(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return &ColorValue{Color: color.RGBA{R: 0, G: 0, B: 0, A: 0}}, nil
}

// Spacing utilities

func builtinInsets(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	switch len(args) {
	case 1:
		// All sides equal
		v, err := starlark.AsFloat(args[0])
		if err != nil {
			return nil, fmt.Errorf("insets: expected number, got %s", args[0].Type())
		}
		return &InsetsValue{Insets: ui.All(float32(v))}, nil
	case 2:
		// Horizontal, Vertical
		h, err := starlark.AsFloat(args[0])
		if err != nil {
			return nil, fmt.Errorf("insets: expected number, got %s", args[0].Type())
		}
		v, err := starlark.AsFloat(args[1])
		if err != nil {
			return nil, fmt.Errorf("insets: expected number, got %s", args[1].Type())
		}
		return &InsetsValue{Insets: ui.Symmetric(float32(h), float32(v))}, nil
	case 4:
		// Left, Top, Right, Bottom
		l, _ := starlark.AsFloat(args[0])
		t, _ := starlark.AsFloat(args[1])
		r, _ := starlark.AsFloat(args[2])
		b, _ := starlark.AsFloat(args[3])
		return &InsetsValue{Insets: ui.Only(float32(l), float32(t), float32(r), float32(b))}, nil
	default:
		return nil, fmt.Errorf("insets: expected 1, 2, or 4 arguments, got %d", len(args))
	}
}
