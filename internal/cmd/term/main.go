package main

import (
	"image"
	"image/draw"
	"runtime"
	"strings"

	"github.com/charmbracelet/x/vt"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
	"github.com/tinyrange/cc/internal/gowin/window"
)

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	win, err := graphics.New("term", 1024, 768)
	if err != nil {
		panic(err)
	}

	t, err := text.Load(win)
	if err != nil {
		panic(err)
	}

	pty, err := startLoginShell()
	if err != nil {
		panic(err)
	}
	defer pty.Close()

	// create a 1x1 texture of all white
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	tex, err := win.NewTexture(img)
	if err != nil {
		panic(err)
	}

	term := vt.NewSafeEmulator(80, 40)
	defer term.Close()

	// PTY -> VT (output)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := pty.Read(buf)
			if n > 0 {
				_, _ = term.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// VT -> PTY (input generated via SendText/SendKey).
	// We intentionally decouple reading from the VT input pipe from writing to the
	// PTY. PTY writes can block under heavy output, which would otherwise
	// backpressure into SendText/SendKey and make keystrokes appear to “drop”.
	inputQ := make(chan []byte, 1024)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := term.Read(buf)
			if n > 0 {
				b := make([]byte, n)
				copy(b, buf[:n])
				inputQ <- b
			}
			if err != nil {
				close(inputQ)
				return
			}
		}
	}()
	go func() {
		for b := range inputQ {
			for len(b) > 0 {
				n, err := pty.Write(b)
				if n > 0 {
					b = b[n:]
				}
				if err != nil {
					return
				}
				if n == 0 {
					// Avoid busy looping on pathological 0-byte writes.
					return
				}
			}
		}
	}()

	if err := win.Loop(func(f graphics.Frame) error {
		width, height := f.WindowSize()

		t.SetViewport(int32(width), int32(height))

		const (
			padX     = float32(10)
			padY     = float32(10)
			fontSize = 16.0
		)

		cellW := t.Advance(fontSize, "M")
		cellH := t.LineHeight(fontSize)
		if cellW <= 0 {
			cellW = 8
		}
		if cellH <= 0 {
			cellH = 16
		}

		cols := int((float32(width) - 2*padX) / cellW)
		rows := int((float32(height) - 2*padY) / cellH)
		if cols < 1 {
			cols = 1
		}
		if rows < 1 {
			rows = 1
		}

		if cols != term.Width() || rows != term.Height() {
			term.Resize(cols, rows)
			_ = pty.Resize(cols, rows)
		}

		// Build modifiers.
		var mod vt.KeyMod
		if f.GetKeyState(window.KeyLeftShift).IsDown() || f.GetKeyState(window.KeyRightShift).IsDown() {
			mod |= vt.ModShift
		}
		if f.GetKeyState(window.KeyLeftControl).IsDown() || f.GetKeyState(window.KeyRightControl).IsDown() {
			mod |= vt.ModCtrl
		}
		if f.GetKeyState(window.KeyLeftAlt).IsDown() || f.GetKeyState(window.KeyRightAlt).IsDown() {
			mod |= vt.ModAlt
		}
		if f.GetKeyState(window.KeyLeftSuper).IsDown() || f.GetKeyState(window.KeyRightSuper).IsDown() {
			mod |= vt.ModMeta
		}

		keyTriggered := func(k window.Key) bool {
			st := f.GetKeyState(k)
			return st == window.KeyStatePressed || st == window.KeyStateRepeated
		}

		// Special keys via VT key map (escape sequences, etc.).
		if keyTriggered(window.KeyEnter) {
			term.SendKey(vt.KeyPressEvent{Code: vt.KeyEnter, Mod: mod})
		}
		if keyTriggered(window.KeyTab) {
			term.SendKey(vt.KeyPressEvent{Code: vt.KeyTab, Mod: mod})
		}
		if keyTriggered(window.KeyBackspace) {
			term.SendKey(vt.KeyPressEvent{Code: vt.KeyBackspace, Mod: mod})
		}
		if keyTriggered(window.KeyEscape) {
			term.SendKey(vt.KeyPressEvent{Code: vt.KeyEscape, Mod: mod})
		}
		if keyTriggered(window.KeyUp) {
			term.SendKey(vt.KeyPressEvent{Code: vt.KeyUp, Mod: mod})
		}
		if keyTriggered(window.KeyDown) {
			term.SendKey(vt.KeyPressEvent{Code: vt.KeyDown, Mod: mod})
		}
		if keyTriggered(window.KeyLeft) {
			term.SendKey(vt.KeyPressEvent{Code: vt.KeyLeft, Mod: mod})
		}
		if keyTriggered(window.KeyRight) {
			term.SendKey(vt.KeyPressEvent{Code: vt.KeyRight, Mod: mod})
		}
		if keyTriggered(window.KeyDelete) {
			term.SendKey(vt.KeyPressEvent{Code: vt.KeyDelete, Mod: mod})
		}
		if keyTriggered(window.KeyHome) {
			term.SendKey(vt.KeyPressEvent{Code: vt.KeyHome, Mod: mod})
		}
		if keyTriggered(window.KeyEnd) {
			term.SendKey(vt.KeyPressEvent{Code: vt.KeyEnd, Mod: mod})
		}
		if keyTriggered(window.KeyPageUp) {
			term.SendKey(vt.KeyPressEvent{Code: vt.KeyPgUp, Mod: mod})
		}
		if keyTriggered(window.KeyPageDown) {
			term.SendKey(vt.KeyPressEvent{Code: vt.KeyPgDown, Mod: mod})
		}
		if keyTriggered(window.KeyInsert) {
			term.SendKey(vt.KeyPressEvent{Code: vt.KeyInsert, Mod: mod})
		}

		// Ctrl+[A-Z] keys.
		if mod&vt.ModCtrl != 0 {
			for i := 0; i < 26; i++ {
				if keyTriggered(window.Key(int(window.KeyA) + i)) {
					term.SendKey(vt.KeyPressEvent{Code: rune('a' + i), Mod: vt.ModCtrl})
				}
			}
			if keyTriggered(window.KeySpace) {
				term.SendKey(vt.KeyPressEvent{Code: vt.KeySpace, Mod: vt.ModCtrl})
			}
		}

		// Printable text input.
		if txt := f.TextInput(); txt != "" {
			// Filter out control characters; we handle those via key events.
			txt = strings.Map(func(r rune) rune {
				if r < 0x20 || r == 0x7f {
					return -1
				}
				return r
			}, txt)
			if txt != "" {
				term.SendText(txt)
			}
		}

		// Clear window to terminal background.
		win.SetClearColor(term.BackgroundColor())

		bgDefault := term.BackgroundColor()
		fgDefault := term.ForegroundColor()

		// Render cells.
		for y := 0; y < term.Height(); y++ {
			for x := 0; x < term.Width(); {
				cell := term.CellAt(x, y)
				w := 1

				content := " "
				fg := fgDefault
				bg := bgDefault
				var attrs uint8

				if cell != nil {
					content = cell.Content
					if cell.Width > 1 {
						w = cell.Width
					}
					if cell.Style.Fg != nil {
						fg = cell.Style.Fg
					}
					if cell.Style.Bg != nil {
						bg = cell.Style.Bg
					}
					attrs = uint8(cell.Style.Attrs)
				}

				// Reverse video.
				if attrs&uint8(1<<5) != 0 { // uv.AttrReverse
					fg, bg = bg, fg
				}

				x0 := padX + float32(x)*cellW
				y0 := padY + float32(y)*cellH

				if bg != nil && bg != bgDefault {
					f.RenderQuad(x0, y0, float32(w)*cellW, cellH, tex, bg)
				}

				// Avoid drawing blank spaces.
				if content != "" && content != " " && fg != nil {
					// Heuristic baseline: place text near the bottom of the cell.
					t.RenderText(content, x0, y0+cellH-2, fontSize, fg)
				}
				x += w
			}
		}

		// Cursor.
		cur := term.CursorPosition()
		if cur.X >= 0 && cur.Y >= 0 && cur.X < term.Width() && cur.Y < term.Height() {
			x0 := padX + float32(cur.X)*cellW
			y0 := padY + float32(cur.Y)*cellH
			f.RenderQuad(x0, y0, cellW, cellH, tex, term.CursorColor())
		}

		return nil
	}); err != nil {
		panic(err)
	}
}
