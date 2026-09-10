// Package gowin presents a display.Session in a native, cgo-free window.
package gowin

import (
	"context"
	"fmt"
	"github.com/tinyrange/gowin/gl"
	"github.com/tinyrange/gowin/window"
	"image"
	"j5.nz/cc/display"
	"time"
	"unsafe"
)

type Options struct {
	Title         string
	Width, Height int
	// Advance optionally advances a cooperatively scheduled VM. It must return
	// when its context expires. All session accesses occur on the window thread,
	// after Advance returns, avoiding concurrent reads of mutable guest RAM.
	Advance func(context.Context) error
}

// Run owns the window until it is closed. Call from the application's main OS
// thread (lock it in main's init). Closing the window does not close the session.
func Run(ctx context.Context, s display.Session, o Options) error {
	if o.Width <= 0 {
		o.Width = 1280
	}
	if o.Height <= 0 {
		o.Height = 832
	}
	if o.Title == "" {
		o.Title = "Virtual machine"
	}
	w, err := window.New(o.Title, o.Width, o.Height, true)
	if err != nil {
		return err
	}
	defer w.Close()
	setCursorHidden := cursorVisibility()
	defer setCursorHidden(false)
	g, err := w.GL()
	if err != nil {
		return err
	}
	program := g.CreateProgram()
	defer g.DeleteProgram(program)
	for _, src := range []struct {
		typ  uint32
		text string
	}{
		{gl.VertexShader, "#version 150\nin vec2 position; out vec2 uv; void main(){gl_Position=vec4(position,0,1);uv=vec2((position.x+1)/2,(1-position.y)/2);}"},
		{gl.FragmentShader, "#version 150\nuniform sampler2D screen; in vec2 uv; out vec4 color; void main(){color=vec4(texture(screen,uv).rgb,1);}"},
	} {
		sh := g.CreateShader(src.typ)
		g.ShaderSource(sh, src.text)
		g.CompileShader(sh)
		var ok int32
		g.GetShaderiv(sh, gl.CompileStatus, &ok)
		if ok == 0 {
			msg := g.GetShaderInfoLog(sh)
			g.DeleteShader(sh)
			return fmt.Errorf("display shader: %s", msg)
		}
		g.AttachShader(program, sh)
		g.DeleteShader(sh)
	}
	g.LinkProgram(program)
	var ok int32
	g.GetProgramiv(program, gl.LinkStatus, &ok)
	if ok == 0 {
		return fmt.Errorf("display program: %s", g.GetProgramInfoLog(program))
	}
	g.UseProgram(program)
	g.Uniform1i(g.GetUniformLocation(program, "screen"), 0)
	var vao, vbo, texture uint32
	g.GenVertexArrays(1, &vao)
	defer g.DeleteVertexArrays(1, &vao)
	g.BindVertexArray(vao)
	g.GenBuffers(1, &vbo)
	defer g.DeleteBuffers(1, &vbo)
	g.BindBuffer(gl.ArrayBuffer, vbo)
	vertices := [8]float32{-1, -1, 1, -1, -1, 1, 1, 1}
	g.BufferData(gl.ArrayBuffer, 32, unsafe.Pointer(&vertices[0]), gl.StaticDraw)
	pos := uint32(g.GetAttribLocation(program, "position"))
	g.EnableVertexAttribArray(pos)
	g.VertexAttribPointer(pos, 2, gl.Float, false, 8, 0)
	g.GenTextures(1, &texture)
	defer g.DeleteTextures(1, &texture)
	g.ActiveTexture(gl.Texture0)
	g.BindTexture(gl.Texture2D, texture)
	g.TexParameteri(gl.Texture2D, gl.TextureMinFilter, gl.Linear)
	g.TexParameteri(gl.Texture2D, gl.TextureMagFilter, gl.Linear)
	g.TexParameteri(gl.Texture2D, gl.TextureWrapS, gl.ClampToEdge)
	g.TexParameteri(gl.Texture2D, gl.TextureWrapT, gl.ClampToEdge)
	keys := map[window.Key]bool{}
	var buttons uint8
	var lastX, lastY uint32
	defer func() {
		for key, down := range keys {
			if down {
				_ = s.Key(keycodes[key], false)
			}
		}
		if buttons != 0 {
			_ = s.Pointer(lastX, lastY, 0, buttons)
		}
	}()
	tw, th := 0, 0
	var generation uint64
	for w.Poll() {
		if err := ctx.Err(); err != nil {
			return err
		}
		width, height := s.Size()
		bw, bh := w.BackingSize()

		left, top, vw, vh := fit(bw, bh, width, height)
		cursorX, cursorY := w.Cursor()
		_, _, cursorInside := pointerPosition(cursorX, cursorY, left, top, vw, vh, width, height)
		setCursorHidden(cursorInside)
		events := w.DrainInputEvents()
		for index, e := range events {
			if e.Type == window.InputEventMouseMove && index+1 < len(events) && events[index+1].Type == window.InputEventMouseMove {
				continue
			}
			switch e.Type {
			case window.InputEventKeyDown, window.InputEventKeyUp, window.InputEventFlagsChanged:
				code, known := keycodes[e.Key]
				if !known {
					continue
				}
				down := e.Type == window.InputEventKeyDown
				if e.Type == window.InputEventFlagsChanged {
					down = w.GetKeyState(e.Key).IsDown()
				}
				if down != keys[e.Key] {
					if err := s.Key(code, down); err != nil {
						return err
					}
					keys[e.Key] = down
				}
			case window.InputEventMouseDown, window.InputEventMouseUp, window.InputEventMouseMove:
				if width <= 0 || height <= 0 || vw <= 0 || vh <= 0 {
					continue
				}
				x, y, inside := pointerPosition(e.MouseX, e.MouseY, left, top, vw, vh, width, height)
				if !inside && buttons == 0 {
					continue
				}
				previous := buttons
				var mask uint8
				switch e.Button {
				case window.ButtonLeft:
					mask = 1
				case window.ButtonMiddle:
					mask = 2
				case window.ButtonRight:
					mask = 4
				}
				if e.Type == window.InputEventMouseDown {
					buttons |= mask
				}
				if e.Type == window.InputEventMouseUp {
					buttons &^= mask
				}
				if e.Type == window.InputEventMouseMove && uint32(x) == lastX && uint32(y) == lastY {
					continue
				}
				lastX, lastY = uint32(x), uint32(y)
				if err := s.Pointer(lastX, lastY, buttons, previous); err != nil {
					return err
				}
			case window.InputEventScroll:
				if sc, ok := s.(display.HighResolutionScroller); ok {
					if err := sc.Scroll(int32(e.ScrollX*120), int32(e.ScrollY*120)); err != nil {
						return err
					}
				}
			}
		}
		// Focus loss clears Gowin's state. Release held modifiers/buttons so they
		// cannot remain stuck in the guest after switching to another application.
		for key, down := range keys {
			if down && !w.GetKeyState(key).IsDown() {
				if err := s.Key(keycodes[key], false); err != nil {
					return err
				}
				keys[key] = false
			}
		}
		previous := buttons
		for _, b := range []struct {
			k window.Button
			m uint8
		}{{window.ButtonLeft, 1}, {window.ButtonMiddle, 2}, {window.ButtonRight, 4}} {
			if !w.GetButtonState(b.k).IsDown() {
				buttons &^= b.m
			}
		}
		if previous != buttons {
			if err := s.Pointer(lastX, lastY, buttons, previous); err != nil {
				return err
			}
		}
		if o.Advance != nil {
			step, cancel := context.WithTimeout(ctx, 12*time.Millisecond)
			err := o.Advance(step)
			cancel()
			if err != nil {
				return err
			}
		}
		f := s.Snapshot(image.Rectangle{}, generation, true)
		if len(f.Pixels) > 0 {
			if f.Width != tw || f.Height != th {
				tw, th = f.Width, f.Height
				g.TexImage2D(gl.Texture2D, 0, gl.RGBA, int32(tw), int32(th), 0, 0x80e1, gl.UnsignedByte, nil)
			}
			g.TexSubImage2D(gl.Texture2D, 0, int32(f.Rect.Min.X), int32(f.Rect.Min.Y), int32(f.Rect.Dx()), int32(f.Rect.Dy()), 0x80e1, gl.UnsignedByte, unsafe.Pointer(&f.Pixels[0]))
			generation = f.Generation
		}
		g.Viewport(0, 0, int32(bw), int32(bh))
		g.ClearColor(0, 0, 0, 1)
		g.Clear(gl.ColorBufferBit)
		if tw > 0 && th > 0 {
			left, top, vw, vh = fit(bw, bh, tw, th)
			g.Viewport(int32(left), int32(bh-top-vh), int32(vw), int32(vh))
			g.DrawArrays(gl.TriangleStrip, 0, 4)
		}
		w.Swap()
	}
	return nil
}

func pointerPosition(x, y float32, left, top, w, h, guestW, guestH int) (int, int, bool) {
	if w <= 0 || h <= 0 || guestW <= 0 || guestH <= 0 {
		return 0, 0, false
	}
	x -= float32(left)
	y -= float32(top)
	inside := x >= 0 && y >= 0 && x < float32(w) && y < float32(h)
	return max(0, min(guestW-1, int(x*float32(guestW)/float32(w)))), max(0, min(guestH-1, int(y*float32(guestH)/float32(h)))), inside
}
func fit(w, h, gw, gh int) (x, y, vw, vh int) {
	if gw <= 0 || gh <= 0 {
		return 0, 0, w, h
	}
	vw = w
	vh = w * gh / gw
	if vh > h {
		vh = h
		vw = h * gw / gh
	}
	return (w - vw) / 2, (h - vh) / 2, vw, vh
}
