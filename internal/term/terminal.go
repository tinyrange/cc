package term

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"io"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
	"github.com/tinyrange/cc/internal/gowin/window"
	"github.com/tinyrange/cc/internal/timeslice"
)

var ErrWindowClosed = errors.New("window closed by user")

type Hooks struct {
	// OnResize is called when the terminal grid size changes.
	OnResize func(cols, rows int)
	// OnFrame is called once per rendered frame.
	OnFrame func() error
}

// View is an embeddable terminal view that can be rendered inside an existing
// graphics.Window loop. It also implements io.Reader/io.Writer for wiring to a
// guest console.
type View struct {
	win graphics.Window
	tex graphics.Texture
	txt *text.Renderer

	emu *vt.SafeEmulator

	// Grid caches cell state for incremental rendering.
	grid *Grid

	// bgBuffer manages batched background rendering.
	bgBuffer *BackgroundBuffer

	// Insets reserve window space that the terminal renderer should not draw into
	// (e.g. an app-level top bar).
	insetLeft   float32
	insetTop    float32
	insetRight  float32
	insetBottom float32

	// Pipe used to expose VT-generated input as an io.Reader (for virtio-console).
	inR *io.PipeReader
	inW *io.PipeWriter

	// inputQ decouples VT input generation (term.Read) from the downstream pipe
	// write to avoid backpressure making keystrokes appear to "drop".
	inputQ chan []byte

	closeOnce sync.Once
	closeCh   chan struct{}

	lastCols int
	lastRows int

	// Rendering layout cached from last frame.
	cellW, cellH     float32
	originX, originY float32

	// Track layout changes for buffer rebuilding.
	lastCellW, lastCellH     float32
	lastOriginX, lastOriginY float32
	lastPadX, lastPadY       float32
}

// Terminal is a convenience wrapper that owns its own window and renders a View
// into it.
type Terminal struct {
	win  graphics.Window
	view *View
}

func New(title string, width, height int) (*Terminal, error) {
	win, err := graphics.New(title, width, height)
	if err != nil {
		return nil, err
	}

	view, err := NewView(win)
	if err != nil {
		win.PlatformWindow().Close()
		return nil, err
	}

	return &Terminal{win: win, view: view}, nil
}

func NewView(win graphics.Window) (*View, error) {
	txt, err := text.Load(win)
	if err != nil {
		return nil, err
	}

	// Create a 1x1 texture of all white for background/cursor quads.
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	tex, err := win.NewTexture(img)
	if err != nil {
		return nil, err
	}

	// Create background buffer for batched rendering.
	bgBuffer, err := NewBackgroundBuffer(win, tex, 80, 40)
	if err != nil {
		return nil, err
	}

	emu := vt.NewSafeEmulator(80, 40)
	disableVTQueriesThatBreakGuests(emu)

	inR, inW := io.Pipe()

	v := &View{
		win:      win,
		tex:      tex,
		txt:      txt,
		emu:      emu,
		grid:     NewGrid(80, 40),
		bgBuffer: bgBuffer,
		inR:      inR,
		inW:      inW,
		inputQ:   make(chan []byte, 1024),
		closeCh:  make(chan struct{}),
		lastCols: 80,
		lastRows: 40,
	}

	// VT -> pipe (input).
	go v.readVTIntoQueue()
	go v.drainQueueToPipe()

	return v, nil
}

// Grid returns the internal grid for testing and introspection.
func (v *View) Grid() *Grid {
	if v == nil {
		return nil
	}
	return v.grid
}

// SetInsets configures pixel insets for rendering and sizing the terminal grid.
// The terminal will render within the content rect:
// [left, top] → [windowWidth-right, windowHeight-bottom].
func (v *View) SetInsets(left, top, right, bottom float32) {
	if v == nil {
		return
	}
	if left < 0 {
		left = 0
	}
	if top < 0 {
		top = 0
	}
	if right < 0 {
		right = 0
	}
	if bottom < 0 {
		bottom = 0
	}
	v.insetLeft = left
	v.insetTop = top
	v.insetRight = right
	v.insetBottom = bottom
}

// disableVTQueriesThatBreakGuests prevents the VT emulator from writing certain
// automatic "terminal replies" (like cursor position reports) into the input
// stream. Some guest userspace (notably minimal shells/prompts) can end up
// echoing these bytes, which appears as a constant stream of stuck input and
// breaks interactive sessions.
//
// We still allow normal user input (SendKey/SendText) and special keys.
func disableVTQueriesThatBreakGuests(emu *vt.SafeEmulator) {
	if emu == nil {
		return
	}

	// Device Status Report (DSR): CSI n
	// We swallow CPR (n=6) and Operating Status (n=5) to avoid unsolicited replies.
	emu.RegisterCsiHandler('n', func(params ansi.Params) bool {
		n, _, ok := params.Param(0, 1)
		if !ok || n == 0 {
			return false
		}
		switch n {
		case 5, 6:
			return true
		default:
			return false
		}
	})

	// DEC private DSR: CSI ? n
	// We swallow Extended Cursor Position Report (n=6).
	emu.RegisterCsiHandler(ansi.Command('?', 0, 'n'), func(params ansi.Params) bool {
		n, _, ok := params.Param(0, 1)
		if !ok || n == 0 {
			return false
		}
		if n == 6 {
			return true
		}
		return false
	})

	// Device Attributes: CSI c and CSI > c
	// Some programs probe terminal type and then (mis)use the replies as input.
	emu.RegisterCsiHandler('c', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 0)
		// Only swallow the standard query form (CSI 0 c).
		if n == 0 {
			return true
		}
		return false
	})
	emu.RegisterCsiHandler(ansi.Command('>', 0, 'c'), func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 0)
		if n == 0 {
			return true
		}
		return false
	})
}

// Read implements io.Reader. It exposes the VT-generated input stream.
func (v *View) Read(p []byte) (int, error) {
	if v == nil || v.inR == nil {
		return 0, io.EOF
	}
	return v.inR.Read(p)
}

// Write implements io.Writer. It feeds bytes into the VT emulator (guest output).
func (v *View) Write(p []byte) (int, error) {
	if v == nil || v.emu == nil {
		return 0, io.EOF
	}
	return v.emu.Write(p)
}

func (v *View) Close() error {
	if v == nil {
		return nil
	}
	v.closeOnce.Do(func() {
		close(v.closeCh)
		if v.emu != nil {
			_ = v.emu.Close()
		}
		if v.inW != nil {
			_ = v.inW.Close()
		}
		if v.inR != nil {
			_ = v.inR.Close()
		}
	})
	return nil
}

func (t *Terminal) Read(p []byte) (int, error) {
	if t == nil || t.view == nil {
		return 0, io.EOF
	}
	return t.view.Read(p)
}

func (t *Terminal) Write(p []byte) (int, error) {
	if t == nil || t.view == nil {
		return 0, io.EOF
	}
	return t.view.Write(p)
}

func (t *Terminal) Close() error {
	if t == nil {
		return nil
	}
	if t.view != nil {
		_ = t.view.Close()
	}
	if t.win != nil {
		t.win.PlatformWindow().Close()
	}
	return nil
}

func (v *View) readVTIntoQueue() {
	buf := make([]byte, 4096)
	for {
		n, err := v.emu.Read(buf)
		if n > 0 {
			b := make([]byte, n)
			copy(b, buf[:n])
			select {
			case v.inputQ <- b:
			case <-v.closeCh:
				close(v.inputQ)
				return
			}
		}
		if err != nil {
			close(v.inputQ)
			return
		}
	}
}

func (v *View) drainQueueToPipe() {
	for {
		select {
		case b, ok := <-v.inputQ:
			if !ok {
				_ = v.inW.Close()
				return
			}
			for len(b) > 0 {
				n, err := v.inW.Write(b)
				if n > 0 {
					b = b[n:]
				}
				if err != nil || n == 0 {
					return
				}
			}
		case <-v.closeCh:
			_ = v.inW.Close()
			return
		}
	}
}

var (
	tsViewStepBegin        = timeslice.RegisterKind("view_step_begin", 0)
	tsViewStepResize       = timeslice.RegisterKind("view_step_resize", 0)
	tsViewStepSendText     = timeslice.RegisterKind("view_step_send_text", 0)
	tsViewStepSendKey      = timeslice.RegisterKind("view_step_send_key", 0)
	tsViewStepTextInput    = timeslice.RegisterKind("view_step_text_input", 0)
	tsViewStepOnFrame      = timeslice.RegisterKind("view_step_on_frame", 0)
	tsViewStepSyncGrid     = timeslice.RegisterKind("view_step_sync_grid", 0)
	tsViewStepUpdateCursor = timeslice.RegisterKind("view_step_update_cursor", 0)
	tsViewStepRenderGrid   = timeslice.RegisterKind("view_step_render_grid", 0)
	tsViewStepClearDirty   = timeslice.RegisterKind("view_step_clear_dirty", 0)
)

// Step processes one frame of input and renders the terminal cells into the
// provided graphics.Frame. This allows embedding the terminal view inside
// an existing window loop (e.g. CCApp).
func (v *View) Step(f graphics.Frame, hooks Hooks) error {
	width, height := f.WindowSize()
	v.txt.SetViewport(int32(width), int32(height))

	stats := timeslice.NewState()

	const (
		padX     = float32(10)
		padY     = float32(10)
		fontSize = 16.0
	)

	cellW := v.txt.Advance(fontSize, "M")
	cellH := v.txt.LineHeight(fontSize)
	if cellW <= 0 {
		cellW = 8
	}
	if cellH <= 0 {
		cellH = 16
	}

	// Cache layout parameters.
	v.cellW = cellW
	v.cellH = cellH

	insetL := v.insetLeft
	insetT := v.insetTop
	insetR := v.insetRight
	insetB := v.insetBottom

	usableW := float32(width) - insetL - insetR
	usableH := float32(height) - insetT - insetB
	if usableW < 1 {
		usableW = 1
	}
	if usableH < 1 {
		usableH = 1
	}

	cols := int((usableW - 2*padX) / cellW)
	rows := int((usableH - 2*padY) / cellH)
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}

	stats.Record(tsViewStepBegin)

	// Handle resize.
	resized := false
	if cols != v.emu.Width() || rows != v.emu.Height() {
		v.emu.Resize(cols, rows)
		v.grid.Resize(cols, rows)
		v.bgBuffer.Resize(cols, rows)
		v.lastCols, v.lastRows = cols, rows
		resized = true
		if hooks.OnResize != nil {
			hooks.OnResize(cols, rows)
		}

		stats.Record(tsViewStepResize)
	}

	// Cache origin.
	v.originX = insetL
	v.originY = insetT

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
	events := v.win.PlatformWindow().DrainInputEvents()
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
				v.emu.SendText(txt)
			}

			stats.Record(tsViewStepSendText)

		case window.InputEventKeyDown:
			mod := toVTMods(ev.Mods)

			// Ctrl+[A-Z] keys.
			if (mod & vt.ModCtrl) != 0 {
				if ev.Key >= window.KeyA && ev.Key <= window.KeyZ {
					v.emu.SendKey(vt.KeyPressEvent{
						Code: rune('a' + (ev.Key - window.KeyA)),
						Mod:  vt.ModCtrl,
					})
					continue
				}
				if ev.Key == window.KeySpace {
					v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeySpace, Mod: vt.ModCtrl})
					continue
				}
			}

			// Special keys via VT key map (escape sequences, etc.).
			switch ev.Key {
			case window.KeyEnter:
				v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyEnter, Mod: mod})
			case window.KeyTab:
				v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyTab, Mod: mod})
			case window.KeyBackspace:
				v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyBackspace, Mod: mod})
			case window.KeyEscape:
				v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyEscape, Mod: mod})
			case window.KeyUp:
				v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyUp, Mod: mod})
			case window.KeyDown:
				v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyDown, Mod: mod})
			case window.KeyLeft:
				v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyLeft, Mod: mod})
			case window.KeyRight:
				v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyRight, Mod: mod})
			case window.KeyDelete:
				v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyDelete, Mod: mod})
			case window.KeyHome:
				v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyHome, Mod: mod})
			case window.KeyEnd:
				v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyEnd, Mod: mod})
			case window.KeyPageUp:
				v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyPgUp, Mod: mod})
			case window.KeyPageDown:
				v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyPgDown, Mod: mod})
			case window.KeyInsert:
				v.emu.SendKey(vt.KeyPressEvent{Code: vt.KeyInsert, Mod: mod})
			}

			stats.Record(tsViewStepSendKey)
		}
	}

	// IMPORTANT: some backends (notably Cocoa) both queue InputEventText events
	// and also buffer the same characters for Frame.TextInput(). If we consume
	// the raw events but don't drain TextInput, the buffered text will be
	// returned on a later frame and cause duplicate keystrokes.
	if sawRawText {
		_ = f.TextInput()
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
				v.emu.SendText(txt)
			}

			stats.Record(tsViewStepTextInput)
		}
	}

	if hooks.OnFrame != nil {
		if err := hooks.OnFrame(); err != nil {
			return err
		}

		stats.Record(tsViewStepOnFrame)
	}

	// Clear window to terminal background.
	v.win.SetClearColor(v.emu.BackgroundColor())

	bgDefault := v.emu.BackgroundColor()
	fgDefault := v.emu.ForegroundColor()

	// Sync grid from VT emulator and track changes.
	v.syncGridFromEmulator(bgDefault, fgDefault, resized)

	stats.Record(tsViewStepSyncGrid)

	// Update cursor position in grid (marks old/new positions dirty).
	cur := v.emu.CursorPosition()
	v.grid.UpdateCursor(cur.X, cur.Y)

	stats.Record(tsViewStepUpdateCursor)

	// Render cells using the grid.
	v.renderGrid(stats, f, bgDefault, fgDefault, padX, padY, fontSize)

	stats.Record(tsViewStepRenderGrid)

	// Clear dirty flags after rendering.
	v.grid.ClearDirty()

	stats.Record(tsViewStepClearDirty)

	return nil
}

// syncGridFromEmulator copies cell state from VT emulator to grid,
// marking changed cells as dirty.
func (v *View) syncGridFromEmulator(bgDefault, fgDefault color.Color, forceFullSync bool) {
	cols, rows := v.grid.Size()

	// If forced (e.g., after resize), mark all dirty.
	if forceFullSync {
		v.grid.MarkAllDirty()
	}

	for y := 0; y < rows; y++ {
		for x := 0; x < cols; {
			cell := v.emu.CellAt(x, y)
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
			if attrs&uint8(1<<5) != 0 {
				fg, bg = bg, fg
			}

			// Update grid cell (this marks dirty if changed).
			v.grid.SetCell(x, y, content, w, fg, bg, attrs)

			x += w
		}
	}
}

var (
	tsViewStepRenderGridBgUpdate = timeslice.RegisterKind("view_step_render_grid_bg_update", 0)
	tsViewStepRenderGridBgRender = timeslice.RegisterKind("view_step_render_grid_bg_render", 0)
	tsViewStepRenderGridText     = timeslice.RegisterKind("view_step_render_grid_text", 0)
	tsViewStepRenderGridCursor   = timeslice.RegisterKind("view_step_render_grid_cursor", 0)
)

// renderGrid renders all cells using batched rendering.
// Backgrounds are rendered in 1 draw call, text in 1 draw call, cursor in 1 draw call.
func (v *View) renderGrid(stats *timeslice.Recorder, f graphics.Frame, bgDefault, fgDefault color.Color, padX, padY, fontSize float32) {
	cols, rows := v.grid.Size()
	originX := v.originX
	originY := v.originY
	cellW := v.cellW
	cellH := v.cellH

	// Check if layout changed - requires full buffer rebuild.
	layoutChanged := cellW != v.lastCellW || cellH != v.lastCellH ||
		originX != v.lastOriginX || originY != v.lastOriginY ||
		padX != v.lastPadX || padY != v.lastPadY

	// Check if full rebuild is needed (resize, first frame, or layout change).
	needsFullRebuild := v.bgBuffer.NeedsFullRebuild() || layoutChanged

	if layoutChanged {
		v.bgBuffer.SetLayout(originX, originY, padX, padY, cellW, cellH)
		v.lastCellW, v.lastCellH = cellW, cellH
		v.lastOriginX, v.lastOriginY = originX, originY
		v.lastPadX, v.lastPadY = padX, padY
	}

	if needsFullRebuild {
		// Full rebuild when layout changes, after resize, or on first frame.
		v.bgBuffer.UpdateAll(v.grid, bgDefault)
		v.bgBuffer.ClearFullRebuild()
	} else {
		// Incremental update for dirty cells only.
		v.bgBuffer.UpdateDirty(v.grid, bgDefault)
	}

	stats.Record(tsViewStepRenderGridBgUpdate)

	// Render all backgrounds in one draw call.
	v.bgBuffer.Render(f)

	stats.Record(tsViewStepRenderGridBgRender)

	// Render all text in one batched draw call.
	v.txt.BeginBatch()
	for y := range rows {
		for x := 0; x < cols; {
			cell := v.grid.CellAt(x, y)
			if cell == nil {
				x++
				continue
			}

			w := max(cell.Width, 1)

			// Render text if not blank.
			if cell.Content != "" && cell.Content != " " {
				x0 := originX + padX + float32(x)*cellW
				y0 := originY + padY + float32(y)*cellH
				fg := cell.Fg
				if fg == nil {
					fg = fgDefault
				}
				v.txt.AddText(cell.Content, x0, y0+cellH-2, float64(fontSize), fg)
			}

			x += w
		}
	}
	v.txt.EndBatch()

	stats.Record(tsViewStepRenderGridText)

	// Render cursor (1 draw call).
	curX, curY := v.grid.CursorPosition()
	if curX >= 0 && curY >= 0 && curX < cols && curY < rows {
		x0 := originX + padX + float32(curX)*cellW
		y0 := originY + padY + float32(curY)*cellH
		f.RenderQuad(x0, y0, cellW, cellH, v.tex, v.emu.CursorColor())
	}

	stats.Record(tsViewStepRenderGridCursor)
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
		return t.view.Step(f, hooks)
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
