// ccinstaller is a small binary that handles replacing the main application binary.
// It is embedded in the main application and extracted when an update is triggered.
package main

import (
	"flag"
	"fmt"
	"image/color"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
)

// Tokyo Night colors (matching ccapp)
var (
	colorBackground = color.RGBA{R: 0x1a, G: 0x1b, B: 0x26, A: 255}
	colorForeground = color.RGBA{R: 0xc0, G: 0xca, B: 0xf5, A: 255}
	colorAccent     = color.RGBA{R: 0x7a, G: 0xa2, B: 0xf7, A: 255}
	colorSuccess    = color.RGBA{R: 0x9e, G: 0xce, B: 0x6a, A: 255}
	colorError      = color.RGBA{R: 0xf7, G: 0x76, B: 0x8e, A: 255}
)

// InstallerUI manages the installer window with thread-safe state updates.
type InstallerUI struct {
	window graphics.Window
	text   *text.Renderer

	// Thread-safe state
	mu       sync.Mutex
	status   string
	progress float64
	hasError bool
	done     bool
	exitCode int
}

func newInstallerUI() (*InstallerUI, error) {
	win, err := graphics.New("CrumbleCracker Update", 450, 180)
	if err != nil {
		return nil, fmt.Errorf("create window: %w", err)
	}

	win.SetClear(true)
	win.SetClearColor(colorBackground)

	textRenderer, err := text.Load(win)
	if err != nil {
		return nil, fmt.Errorf("load text renderer: %w", err)
	}

	return &InstallerUI{
		window:   win,
		text:     textRenderer,
		status:   "Preparing to update...",
		progress: 0,
	}, nil
}

func (ui *InstallerUI) setStatus(status string) {
	ui.mu.Lock()
	ui.status = status
	ui.mu.Unlock()
}

func (ui *InstallerUI) setProgress(progress float64) {
	ui.mu.Lock()
	ui.progress = progress
	ui.mu.Unlock()
}

func (ui *InstallerUI) setError(err error) {
	ui.mu.Lock()
	ui.status = fmt.Sprintf("Error: %v", err)
	ui.hasError = true
	ui.mu.Unlock()
}

func (ui *InstallerUI) finish(exitCode int) {
	ui.mu.Lock()
	ui.done = true
	ui.exitCode = exitCode
	ui.mu.Unlock()
}

func (ui *InstallerUI) getState() (status string, progress float64, hasError bool, done bool, exitCode int) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	return ui.status, ui.progress, ui.hasError, ui.done, ui.exitCode
}

// runLoop runs the main render loop on the main thread.
// It returns when the update is complete.
func (ui *InstallerUI) runLoop() int {
	var exitCode int

	ui.window.Loop(func(f graphics.Frame) error {
		status, progress, hasError, done, code := ui.getState()

		if done {
			exitCode = code
			return fmt.Errorf("done")
		}

		ui.renderFrame(f, status, progress, hasError)
		return nil
	})

	return exitCode
}

func (ui *InstallerUI) renderFrame(f graphics.Frame, status string, progress float64, hasError bool) {
	width, height := f.WindowSize()
	scale := ui.window.Scale()

	// Set viewport for text rendering
	ui.text.SetViewport(int32(width), int32(height))

	// Center everything
	centerX := float32(width) / 2
	centerY := float32(height) / 2

	// Title - centered, larger
	titleSize := 24.0 * float64(scale)
	titleText := "Updating CrumbleCracker"
	titleWidth := ui.text.Advance(titleSize, titleText)
	titleX := (float32(width) - titleWidth) / 2
	titleY := centerY - 35*scale
	ui.text.RenderText(titleText, titleX, titleY, titleSize, colorForeground)

	// Status text - centered
	statusSize := 14.0 * float64(scale)
	statusColor := colorForeground
	if hasError {
		statusColor = colorError
	}
	statusWidth := ui.text.Advance(statusSize, status)
	statusX := (float32(width) - statusWidth) / 2
	statusY := centerY + 5*scale
	ui.text.RenderText(status, statusX, statusY, statusSize, statusColor)

	// Progress bar
	barWidth := float32(width) - 80*scale
	barHeight := 8 * scale
	barX := 40 * scale
	barY := centerY + 35*scale

	// Background bar (always shown)
	f.RenderQuad(barX, barY, barWidth, barHeight, nil, color.RGBA{R: 0x24, G: 0x28, B: 0x3b, A: 255})

	// Progress fill
	if progress > 0 && !hasError {
		fillWidth := barWidth * float32(progress)
		if fillWidth > 0 {
			fillColor := colorAccent
			if progress >= 1.0 {
				fillColor = colorSuccess
			}
			f.RenderQuad(barX, barY, fillWidth, barHeight, nil, fillColor)
		}
	}

	// Percentage text below bar
	if !hasError {
		percentText := fmt.Sprintf("%.0f%%", progress*100)
		percentSize := 12.0 * float64(scale)
		percentWidth := ui.text.Advance(percentSize, percentText)
		percentX := centerX - percentWidth/2
		percentY := barY + barHeight + 18*scale
		ui.text.RenderText(percentText, percentX, percentY, percentSize, colorForeground)
	}
}

func (ui *InstallerUI) close() {
	ui.window.PlatformWindow().Close()
}

func main() {
	runtime.LockOSThread()

	staging := flag.String("staging", "", "Path to staging directory with new version")
	target := flag.String("target", "", "Path to target application to replace")
	restart := flag.Bool("restart", true, "Restart application after install")
	mainPID := flag.Int("pid", 0, "PID of main app to wait for (optional)")
	flag.Parse()

	if *staging == "" || *target == "" {
		log.Fatal("missing required flags: -staging and -target")
	}

	// Create UI
	ui, err := newInstallerUI()
	if err != nil {
		log.Fatalf("failed to create UI: %v", err)
	}
	defer ui.close()

	// Run the update process in a background goroutine
	go func() {
		// Wait for main app to exit if PID provided
		if *mainPID > 0 {
			ui.setStatus("Waiting for CrumbleCracker to close...")
			ui.setProgress(0.1)
			waitForProcessExit(*mainPID)
		} else {
			// Give a moment for the main app to exit
			time.Sleep(500 * time.Millisecond)
		}

		// Perform platform-specific installation
		ui.setStatus("Replacing application files...")
		ui.setProgress(0.4)

		if err := install(*staging, *target, ui); err != nil {
			ui.setError(err)
			time.Sleep(5 * time.Second)
			ui.finish(1)
			return
		}

		ui.setProgress(1.0)
		ui.setStatus("Update complete! Restarting...")

		// Cleanup staging directory
		os.RemoveAll(*staging)

		// Restart the app
		if *restart {
			if err := launchApp(*target); err != nil {
				ui.setError(fmt.Errorf("failed to launch: %v", err))
				time.Sleep(5 * time.Second)
				ui.finish(1)
				return
			}
		}

		ui.finish(0)
	}()

	// Run the render loop on the main thread
	exitCode := ui.runLoop()
	os.Exit(exitCode)
}

// waitForProcessExit is defined in platform-specific files:
// - wait_unix.go for Unix systems (uses signal 0 to check process existence)
// - wait_windows.go for Windows (uses WaitForSingleObject)
