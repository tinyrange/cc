// Command html is a kitchen sink demo for the HTML renderer.
// It renders various HTML elements with Tailwind CSS classes and takes a screenshot.
//
// Usage:
//
//	go run ./internal/cmd/html -o screenshot.png
package main

import (
	"flag"
	"fmt"
	"image/color"
	"image/png"
	"os"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
	"github.com/tinyrange/cc/internal/gowin/ui"
	"github.com/tinyrange/cc/internal/gowin/ui/html"
)

const kitchenSinkHTML = `
<div class="flex flex-col gap-6 p-8 bg-background">
	<!-- Header -->
	<div class="flex flex-row justify-between items-center">
		<h1 class="text-3xl text-primary">HTML Kitchen Sink</h1>
		<span class="text-sm text-muted">Tailwind CSS + gowin/ui</span>
	</div>

	<!-- Cards Row -->
	<div class="flex flex-row gap-4">
		<!-- Card 1: Text Styles -->
		<div class="flex flex-col gap-3 p-6 bg-card rounded-lg">
			<h2 class="text-xl text-primary">Typography</h2>
			<span class="text-xs text-secondary">text-xs (12px)</span>
			<span class="text-sm text-secondary">text-sm (14px)</span>
			<span class="text-base text-secondary">text-base (16px)</span>
			<span class="text-lg text-secondary">text-lg (18px)</span>
			<span class="text-xl text-primary">text-xl (20px)</span>
			<span class="text-2xl text-primary">text-2xl (24px)</span>
		</div>

		<!-- Card 2: Colors -->
		<div class="flex flex-col gap-3 p-6 bg-card rounded-lg">
			<h2 class="text-xl text-primary">Colors</h2>
			<span class="text-base text-primary">text-primary</span>
			<span class="text-base text-secondary">text-secondary</span>
			<span class="text-base text-muted">text-muted</span>
			<span class="text-base text-accent">text-accent</span>
			<span class="text-base text-success">text-success</span>
			<span class="text-base text-danger">text-danger</span>
			<span class="text-base text-warning">text-warning</span>
		</div>

		<!-- Card 3: Buttons -->
		<div class="flex flex-col gap-3 p-6 bg-card rounded-lg">
			<h2 class="text-xl text-primary">Buttons</h2>
			<button class="bg-btn text-primary px-4 py-2 rounded">Default</button>
			<button class="bg-accent text-dark px-4 py-2 rounded">Primary</button>
			<button class="bg-success text-dark px-4 py-2 rounded">Success</button>
			<button class="bg-danger text-dark px-4 py-2 rounded">Danger</button>
			<button class="bg-warning text-dark px-4 py-2 rounded">Warning</button>
		</div>
	</div>

	<!-- Spacing & Layout Demo -->
	<div class="flex flex-col gap-4 p-6 bg-card rounded-lg">
		<h2 class="text-xl text-primary">Layout & Spacing</h2>

		<div class="flex flex-row gap-2">
			<div class="p-2 bg-accent rounded-sm">
				<span class="text-sm text-dark">p-2</span>
			</div>
			<div class="p-4 bg-accent rounded-sm">
				<span class="text-sm text-dark">p-4</span>
			</div>
			<div class="p-6 bg-accent rounded-sm">
				<span class="text-sm text-dark">p-6</span>
			</div>
		</div>

		<div class="flex flex-row gap-4 items-center">
			<span class="text-sm text-secondary">Justify:</span>
			<div class="flex flex-row justify-start p-2 bg-btn rounded" style="width: 200px">
				<span class="text-xs text-muted">start</span>
			</div>
			<div class="flex flex-row justify-center p-2 bg-btn rounded" style="width: 200px">
				<span class="text-xs text-muted">center</span>
			</div>
			<div class="flex flex-row justify-end p-2 bg-btn rounded" style="width: 200px">
				<span class="text-xs text-muted">end</span>
			</div>
		</div>
	</div>

	<!-- Border Radius Demo -->
	<div class="flex flex-col gap-4 p-6 bg-card rounded-lg">
		<h2 class="text-xl text-primary">Border Radius</h2>
		<div class="flex flex-row gap-4 items-end">
			<div class="flex flex-col items-center gap-2">
				<div class="p-4 bg-accent rounded-none">
					<span class="text-xs text-dark">none</span>
				</div>
			</div>
			<div class="flex flex-col items-center gap-2">
				<div class="p-4 bg-accent rounded-sm">
					<span class="text-xs text-dark">sm</span>
				</div>
			</div>
			<div class="flex flex-col items-center gap-2">
				<div class="p-4 bg-accent rounded-md">
					<span class="text-xs text-dark">md</span>
				</div>
			</div>
			<div class="flex flex-col items-center gap-2">
				<div class="p-4 bg-accent rounded-lg">
					<span class="text-xs text-dark">lg</span>
				</div>
			</div>
			<div class="flex flex-col items-center gap-2">
				<div class="p-4 bg-accent rounded-xl">
					<span class="text-xs text-dark">xl</span>
				</div>
			</div>
			<div class="flex flex-col items-center gap-2">
				<div class="p-4 bg-accent rounded-2xl">
					<span class="text-xs text-dark">2xl</span>
				</div>
			</div>
		</div>
	</div>

	<!-- Dialog Example -->
	<div class="flex flex-col gap-4 p-6 bg-card rounded-xl">
		<h3 class="text-lg text-primary">Example Dialog</h3>
		<p class="text-secondary text-sm">This demonstrates a typical dialog layout with title, description, and action buttons.</p>
		<div class="flex flex-row gap-3 justify-end">
			<button data-onclick="cancel" class="bg-btn text-primary px-5 py-2 rounded-md">Cancel</button>
			<button data-onclick="confirm" class="bg-accent text-dark px-5 py-2 rounded-md">Confirm</button>
		</div>
	</div>

	<!-- Inputs -->
	<div class="flex flex-col gap-4 p-6 bg-card rounded-lg">
		<h2 class="text-xl text-primary">Form Inputs</h2>
		<div class="flex flex-row gap-4 items-center">
			<label class="text-sm text-secondary">Text Input:</label>
			<input type="text" placeholder="Enter text here..." class="bg-btn text-primary p-3 rounded-md" />
		</div>
		<div class="flex flex-row gap-4 items-center">
			<label class="text-sm text-secondary">Checkbox:</label>
			<input type="checkbox" data-bind="checkbox1" />
		</div>
	</div>
</div>
`

func main() {
	output := flag.String("o", "screenshot.png", "output file path")
	width := flag.Int("w", 1200, "window width")
	height := flag.Int("h", 900, "window height")
	flag.Parse()

	if err := run(*output, *width, *height); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(output string, width, height int) error {
	// Create window
	win, err := graphics.New("HTML Kitchen Sink", width, height)
	if err != nil {
		return fmt.Errorf("failed to create window: %w", err)
	}

	// Load text renderer
	textRenderer, err := text.Load(win)
	if err != nil {
		return fmt.Errorf("failed to create text renderer: %w", err)
	}

	// Parse HTML
	doc, err := html.Parse(kitchenSinkHTML)
	if err != nil {
		return fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Register handlers (just for demo)
	doc.SetHandler("cancel", func() {
		fmt.Println("Cancel clicked")
	})
	doc.SetHandler("confirm", func() {
		fmt.Println("Confirm clicked")
	})

	// Render HTML to widget tree
	ctx := &html.RenderContext{
		Window:       win,
		TextRenderer: textRenderer,
	}
	widget := doc.Render(ctx)

	// Create root and set widget
	root := ui.NewRoot(textRenderer)
	root.SetChild(widget)

	// Setup window
	win.SetClear(true)
	win.SetClearColor(hex("#1a1b26")) // Tokyo Night background

	frameCount := 0
	var screenshot error

	// Run render loop
	err = win.Loop(func(f graphics.Frame) error {
		// Render the UI
		root.DrawOnly(f)

		frameCount++

		// Take screenshot on second frame (after first render completes)
		if frameCount == 2 {
			img, err := f.Screenshot()
			if err != nil {
				screenshot = fmt.Errorf("failed to take screenshot: %w", err)
				return screenshot
			}

			file, err := os.Create(output)
			if err != nil {
				screenshot = fmt.Errorf("failed to create output file: %w", err)
				return screenshot
			}
			defer file.Close()

			if err := png.Encode(file, img); err != nil {
				screenshot = fmt.Errorf("failed to encode PNG: %w", err)
				return screenshot
			}

			fmt.Printf("Screenshot saved to %s\n", output)
			return fmt.Errorf("done") // Exit the loop
		}

		return nil
	})

	// "done" error is expected
	if err != nil && err.Error() == "done" {
		return screenshot
	}

	return err
}

// hex parses a CSS hex color.
func hex(s string) color.RGBA {
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	var r, g, b uint8
	if len(s) == 6 {
		r = hexDigit(s[0])<<4 | hexDigit(s[1])
		g = hexDigit(s[2])<<4 | hexDigit(s[3])
		b = hexDigit(s[4])<<4 | hexDigit(s[5])
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

func hexDigit(c byte) uint8 {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}
