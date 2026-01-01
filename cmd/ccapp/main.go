package main

import (
	"fmt"
	"image/color"
	"os"
	"runtime"
	"time"

	"github.com/tinyrange/cc/internal/assets"
	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
	"github.com/tinyrange/cc/internal/gowin/window"
)

type Application struct {
	window graphics.Window
	text   *text.Renderer
	logo   *graphics.SVG

	start time.Time

	// UI state
	scrollX       float32
	selectedIndex int // -1 means list view

	prevLeftDown  bool
	draggingThumb bool
	thumbDragDX   float32
}

type item struct {
	Name        string
	Description string
}

type rect struct {
	x float32
	y float32
	w float32
	h float32
}

func (r rect) contains(px, py float32) bool {
	return px >= r.x && px <= r.x+r.w && py >= r.y && py <= r.y+r.h
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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

	app.start = time.Now()
	app.selectedIndex = -1

	return app.window.Loop(func(f graphics.Frame) error {
		w, h := f.WindowSize()
		app.text.SetViewport(int32(w), int32(h))

		// Pull raw input events (wheel deltas live here).
		var wheelX, wheelY float32
		for _, ev := range app.window.PlatformWindow().DrainInputEvents() {
			if ev.Type == window.InputEventScroll {
				wheelX += ev.ScrollX
				wheelY += ev.ScrollY
			}
		}

		mx, my := f.CursorPos()
		leftDown := f.GetButtonState(window.ButtonLeft).IsDown()
		justPressed := leftDown && !app.prevLeftDown
		justReleased := !leftDown && app.prevLeftDown
		app.prevLeftDown = leftDown

		// Layout uses the actual window bounds directly.
		winW := float32(w)
		winH := float32(h)
		padding := float32(20)

		// Top bar.
		topBarH := float32(32)
		f.RenderQuad(0, 0, winW, topBarH, nil, color.RGBA{R: 22, G: 22, B: 22, A: 255})
		app.text.RenderText("File", padding, 22, 16, graphics.ColorWhite)

		// Title below top bar.
		titleY := topBarH + 50
		app.text.RenderText("CrumbleCracker", padding, titleY, 48, graphics.ColorWhite)
		app.text.RenderText("Please select a environment to boot", padding, titleY+30, 20, graphics.ColorWhite)

		// Back button (only in detail view).
		backRect := rect{x: 80, y: 6, w: 70, h: topBarH - 12}
		if app.selectedIndex >= 0 {
			f.RenderQuad(backRect.x, backRect.y, backRect.w, backRect.h, nil, color.RGBA{R: 40, G: 40, B: 40, A: 255})
			app.text.RenderText("Back", backRect.x+14, 22, 14, graphics.ColorWhite)
			if justPressed && backRect.contains(mx, my) {
				app.selectedIndex = -1
			}
		}

		// Logo in bottom-right corner, overlapping content area.
		// Position it so it extends to the window edges.
		if app.logo != nil {
			// Logo diameter - proportional to window size.
			logoSize := winH * 0.75
			if logoSize > winW*0.75 {
				logoSize = winW * 0.75
			}
			if logoSize < 280 {
				logoSize = 280
			}

			// Position: bottom-right corner, partially extending beyond window edges.
			logoX := winW - logoSize + logoSize*0.35
			logoY := winH - logoSize + logoSize*0.35

			t := float32(time.Since(app.start).Seconds())
			// Slower in the center, faster at the edges: split by SVG groups.
			app.logo.DrawGroupRotated(f, "inner-circle", logoX, logoY, logoSize, logoSize, t*0.4)
			app.logo.DrawGroupRotated(f, "morse-circle", logoX, logoY, logoSize, logoSize, -t*0.9)
			app.logo.DrawGroupRotated(f, "outer-circle", logoX, logoY, logoSize, logoSize, t*1.6)
		}

		// Center/left content - cards area.
		items := []item{
			{Name: "Kernel Bringup", Description: "Boot and init sequence"},
			{Name: "Sway Desktop", Description: "Wayland + input stack"},
			{Name: "Linux Image", Description: "OCI + VFS integration"},
			{Name: "Device Models", Description: "virtio, PCI, serial"},
			{Name: "Assembler", Description: "amd64/arm64/riscv emit"},
			{Name: "Netstack", Description: "TCP + DNS tests"},
		}

		if app.selectedIndex >= 0 && app.selectedIndex < len(items) {
			// Detail view: show selected item name centered.
			name := items[app.selectedIndex].Name
			size := 38.0
			textW := app.text.Advance(size, name)
			cx := winW * 0.5
			cy := winH * 0.5
			app.text.RenderText(name, cx-textW*0.5, cy, size, graphics.ColorWhite)
			return nil
		} else {
			// List view - cards below title.
			listX := padding
			listY := titleY + 120
			// Leave room on right for logo overlap.
			viewW := winW - padding*2
			cardW := float32(180)
			cardH := float32(180)
			gap := float32(24)
			viewport := rect{x: listX, y: listY, w: viewW, h: cardH + 80}

			// draw a rectangle overlaying the viewport
			f.RenderQuad(0, listY-20, winW, cardH+160, nil, color.RGBA{R: 255, G: 255, B: 255, A: 10})

			contentWidth := float32(len(items))*(cardW+gap) - gap
			maxScroll := float32(0)
			if contentWidth > viewport.w {
				maxScroll = contentWidth - viewport.w
			}

			// Wheel scroll when hovering the list area.
			if (wheelX != 0 || wheelY != 0) && viewport.contains(mx, my) {
				// Map vertical wheel to horizontal scrolling (gallery).
				app.scrollX -= wheelY * 40
				app.scrollX -= wheelX * 40
			}
			app.scrollX = clamp(app.scrollX, 0, maxScroll)

			// Scrollbar (bottom).
			barH := float32(8)
			barY := viewport.y + viewport.h + 16
			bar := rect{x: viewport.x, y: barY, w: viewport.w, h: barH}
			f.RenderQuad(bar.x, bar.y, bar.w, bar.h, nil, color.RGBA{R: 48, G: 48, B: 48, A: 255})

			thumbW := bar.w
			if contentWidth > 0 {
				thumbW = bar.w * (bar.w / contentWidth)
			}
			if thumbW < 30 {
				thumbW = 30
			}
			if thumbW > bar.w {
				thumbW = bar.w
			}
			thumbX := bar.x
			if maxScroll > 0 {
				thumbX = bar.x + (bar.w-thumbW)*(app.scrollX/maxScroll)
			}
			thumb := rect{x: thumbX, y: bar.y, w: thumbW, h: bar.h}
			f.RenderQuad(thumb.x, thumb.y, thumb.w, thumb.h, nil, color.RGBA{R: 100, G: 100, B: 100, A: 255})

			if justPressed && thumb.contains(mx, my) {
				app.draggingThumb = true
				app.thumbDragDX = mx - thumb.x
			}
			if app.draggingThumb && leftDown {
				newThumbX := clamp(mx-app.thumbDragDX, bar.x, bar.x+bar.w-thumbW)
				if bar.w-thumbW > 0 {
					app.scrollX = (newThumbX - bar.x) / (bar.w - thumbW) * maxScroll
				} else {
					app.scrollX = 0
				}
			}
			if justReleased {
				app.draggingThumb = false
			}

			// Draw cards.
			for i, it := range items {
				x := viewport.x + float32(i)*(cardW+gap) - app.scrollX
				card := rect{x: x, y: viewport.y, w: cardW, h: cardH + 60}

				// Simple clipping by skipping offscreen cards.
				if card.x+card.w < viewport.x-50 || card.x > viewport.x+viewport.w+50 {
					continue
				}

				// Card border (like the sketch).
				borderColor := color.RGBA{R: 80, G: 80, B: 80, A: 255}
				f.RenderQuad(card.x, card.y, card.w, 1, nil, borderColor)         // top
				f.RenderQuad(card.x, card.y+cardH, card.w, 1, nil, borderColor)   // bottom of image area
				f.RenderQuad(card.x, card.y, 1, cardH, nil, borderColor)          // left
				f.RenderQuad(card.x+card.w-1, card.y, 1, cardH, nil, borderColor) // right

				// Title + description below card.
				app.text.RenderText(it.Name, card.x, card.y+cardH+24, 16, graphics.ColorWhite)
				app.text.RenderText(it.Description, card.x, card.y+cardH+44, 12, graphics.ColorGray)

				if justPressed && viewport.contains(mx, my) && card.contains(mx, my) {
					app.selectedIndex = i
				}
			}

			return nil
		}
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
