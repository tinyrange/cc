package term

import (
	"context"
	"errors"
	"image"
	"image/draw"
	"io"
	"strings"
	"sync"

	"github.com/charmbracelet/x/vt"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
	"github.com/tinyrange/cc/internal/gowin/window"
)

var ErrWindowClosed = errors.New("window closed by user")

type Hooks struct {
	// OnResize is called when the terminal grid size changes.
	OnResize func(cols, rows int)
	// OnFrame is called once per rendered frame.
	OnFrame func() error
}

type Terminal struct {
	win graphics.Window
	tex graphics.Texture
	txt *text.Renderer

	emu *vt.SafeEmulator

	// Pipe used to expose VT-generated input as an io.Reader (for virtio-console).
	inR *io.PipeReader
	inW *io.PipeWriter

	// inputQ decouples VT input generation (term.Read) from the downstream pipe
	// write to avoid backpressure making keystrokes appear to “drop”.
	inputQ chan []byte

	closeOnce sync.Once
	closeCh   chan struct{}

	lastCols int
	lastRows int
}

func New(title string, width, height int) (*Terminal, error) {
	win, err := graphics.New(title, width, height)
	if err != nil {
		return nil, err
	}

	txt, err := text.Load(win)
	if err != nil {
		win.PlatformWindow().Close()
		return nil, err
	}

	// Create a 1x1 texture of all white for background/cursor quads.
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	tex, err := win.NewTexture(img)
	if err != nil {
		win.PlatformWindow().Close()
		return nil, err
	}

	emu := vt.NewSafeEmulator(80, 40)

	inR, inW := io.Pipe()

	t := &Terminal{
		win:      win,
		tex:      tex,
		txt:      txt,
		emu:      emu,
		inR:      inR,
		inW:      inW,
		inputQ:   make(chan []byte, 1024),
		closeCh:  make(chan struct{}),
		lastCols: 80,
		lastRows: 40,
	}

	// VT -> pipe (input).
	go t.readVTIntoQueue()
	go t.drainQueueToPipe()

	return t, nil
}

// Read implements io.Reader. It exposes the VT-generated input stream.
func (t *Terminal) Read(p []byte) (int, error) {
	if t == nil || t.inR == nil {
		return 0, io.EOF
	}
	return t.inR.Read(p)
}

// Write implements io.Writer. It feeds bytes into the VT emulator (guest output).
func (t *Terminal) Write(p []byte) (int, error) {
	if t == nil || t.emu == nil {
		return 0, io.EOF
	}
	return t.emu.Write(p)
}

func (t *Terminal) Close() error {
	if t == nil {
		return nil
	}
	t.closeOnce.Do(func() {
		close(t.closeCh)
		if t.emu != nil {
			_ = t.emu.Close()
		}
		if t.inW != nil {
			_ = t.inW.Close()
		}
		if t.inR != nil {
			_ = t.inR.Close()
		}
	})
	return nil
}

func (t *Terminal) readVTIntoQueue() {
	buf := make([]byte, 4096)
	for {
		n, err := t.emu.Read(buf)
		if n > 0 {
			b := make([]byte, n)
			copy(b, buf[:n])
			select {
			case t.inputQ <- b:
			case <-t.closeCh:
				close(t.inputQ)
				return
			}
		}
		if err != nil {
			close(t.inputQ)
			return
		}
	}
}

func (t *Terminal) drainQueueToPipe() {
	for {
		select {
		case b, ok := <-t.inputQ:
			if !ok {
				_ = t.inW.Close()
				return
			}
			for len(b) > 0 {
				n, err := t.inW.Write(b)
				if n > 0 {
					b = b[n:]
				}
				if err != nil || n == 0 {
					return
				}
			}
		case <-t.closeCh:
			_ = t.inW.Close()
			return
		}
	}
}

func (t *Terminal) Run(ctx context.Context, hooks Hooks) error {
	if ctx == nil {
		ctx = context.Background()
	}

	err := t.win.Loop(func(f graphics.Frame) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		width, height := f.WindowSize()
		t.txt.SetViewport(int32(width), int32(height))

		const (
			padX     = float32(10)
			padY     = float32(10)
			fontSize = 16.0
		)

		cellW := t.txt.Advance(fontSize, "M")
		cellH := t.txt.LineHeight(fontSize)
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

		if cols != t.emu.Width() || rows != t.emu.Height() {
			t.emu.Resize(cols, rows)
			t.lastCols, t.lastRows = cols, rows
			if hooks.OnResize != nil {
				hooks.OnResize(cols, rows)
			}
		}

		toVTMods := func(m window.KeyMods) vt.KeyMod {
			var mod vt.KeyMod
			if (m & window.ModShift) != 0 {
				mod |= vt.ModShift
			}
			if (m & window.ModCtrl) != 0 {
				mod |= vt.ModCtrl
			}
			if (m & window.ModAlt) != 0 {
				mod |= vt.ModAlt
			}
			if (m & window.ModSuper) != 0 {
				mod |= vt.ModMeta
			}
			return mod
		}

		// Raw input events from the platform window (preferred).
		events := t.win.PlatformWindow().DrainInputEvents()
		sawRawText := false
		for _, ev := range events {
			switch ev.Type {
			case window.InputEventText:
				txt := ev.Text
				if txt == "" {
					continue
				}
				// Filter out control characters; we handle those via key events.
				txt = strings.Map(func(r rune) rune {
					if r < 0x20 || r == 0x7f {
						return -1
					}
					return r
				}, txt)
				if txt != "" {
					sawRawText = true
					t.emu.SendText(txt)
				}

			case window.InputEventKeyDown:
				mod := toVTMods(ev.Mods)

				// Ctrl+[A-Z] keys.
				if (mod & vt.ModCtrl) != 0 {
					if ev.Key >= window.KeyA && ev.Key <= window.KeyZ {
						t.emu.SendKey(vt.KeyPressEvent{
							Code: rune('a' + (ev.Key - window.KeyA)),
							Mod:  vt.ModCtrl,
						})
						continue
					}
					if ev.Key == window.KeySpace {
						t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeySpace, Mod: vt.ModCtrl})
						continue
					}
				}

				// Special keys via VT key map (escape sequences, etc.).
				switch ev.Key {
				case window.KeyEnter:
					t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyEnter, Mod: mod})
				case window.KeyTab:
					t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyTab, Mod: mod})
				case window.KeyBackspace:
					t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyBackspace, Mod: mod})
				case window.KeyEscape:
					t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyEscape, Mod: mod})
				case window.KeyUp:
					t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyUp, Mod: mod})
				case window.KeyDown:
					t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyDown, Mod: mod})
				case window.KeyLeft:
					t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyLeft, Mod: mod})
				case window.KeyRight:
					t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyRight, Mod: mod})
				case window.KeyDelete:
					t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyDelete, Mod: mod})
				case window.KeyHome:
					t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyHome, Mod: mod})
				case window.KeyEnd:
					t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyEnd, Mod: mod})
				case window.KeyPageUp:
					t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyPgUp, Mod: mod})
				case window.KeyPageDown:
					t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyPgDown, Mod: mod})
				case window.KeyInsert:
					t.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyInsert, Mod: mod})
				}
			}
		}

		// Fallback: platform text buffer (for backends that don't emit Text events yet).
		if !sawRawText {
			if txt := f.TextInput(); txt != "" {
				// Filter out control characters; we handle those via key events.
				txt = strings.Map(func(r rune) rune {
					if r < 0x20 || r == 0x7f {
						return -1
					}
					return r
				}, txt)
				if txt != "" {
					t.emu.SendText(txt)
				}
			}
		}

		if hooks.OnFrame != nil {
			if err := hooks.OnFrame(); err != nil {
				return err
			}
		}

		// Clear window to terminal background.
		t.win.SetClearColor(t.emu.BackgroundColor())

		bgDefault := t.emu.BackgroundColor()
		fgDefault := t.emu.ForegroundColor()

		// Render cells.
		for y := 0; y < t.emu.Height(); y++ {
			for x := 0; x < t.emu.Width(); {
				cell := t.emu.CellAt(x, y)
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
					f.RenderQuad(x0, y0, float32(w)*cellW, cellH, t.tex, bg)
				}

				// Avoid drawing blank spaces.
				if content != "" && content != " " && fg != nil {
					// Heuristic baseline: place text near the bottom of the cell.
					t.txt.RenderText(content, x0, y0+cellH-2, fontSize, fg)
				}
				x += w
			}
		}

		// Cursor.
		cur := t.emu.CursorPosition()
		if cur.X >= 0 && cur.Y >= 0 && cur.X < t.emu.Width() && cur.Y < t.emu.Height() {
			x0 := padX + float32(cur.X)*cellW
			y0 := padY + float32(cur.Y)*cellH
			f.RenderQuad(x0, y0, cellW, cellH, t.tex, t.emu.CursorColor())
		}

		return nil
	})

	// Treat a graceful loop exit as a user-closed window.
	if err == nil && ctx.Err() == nil {
		return ErrWindowClosed
	}
	if errors.Is(err, ErrWindowClosed) {
		return ErrWindowClosed
	}
	return err
}
