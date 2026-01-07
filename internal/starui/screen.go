package starui

import (
	"time"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
	"github.com/tinyrange/cc/internal/gowin/ui"
	"github.com/tinyrange/cc/internal/gowin/window"
)

// StarlarkScreen wraps a Starlark-rendered screen for use in ccapp.
type StarlarkScreen struct {
	root   *ui.Root
	widget ui.Widget
	engine *Engine
	name   string

	// For logo animation
	startTime time.Time
	logos     []*ui.AnimatedLogo
}

// NewStarlarkScreen creates a screen from a Starlark screen definition.
func NewStarlarkScreen(engine *Engine, name string, text *text.Renderer) (*StarlarkScreen, error) {
	screen, err := engine.RenderScreen(name)
	if err != nil {
		return nil, err
	}

	root := ui.NewRoot(text)
	root.SetChild(screen.Root)

	s := &StarlarkScreen{
		root:      root,
		widget:    screen.Root,
		engine:    engine,
		name:      name,
		startTime: time.Now(),
	}

	// Find any animated logos in the tree
	s.findLogos(screen.Root)

	return s, nil
}

// findLogos recursively finds AnimatedLogo widgets in the tree.
func (s *StarlarkScreen) findLogos(w ui.Widget) {
	if logo, ok := w.(*ui.AnimatedLogo); ok {
		s.logos = append(s.logos, logo)
	}
	for _, child := range w.Children() {
		s.findLogos(child)
	}
}

// Rebuild re-renders the screen from Starlark (for dynamic updates).
func (s *StarlarkScreen) Rebuild(text *text.Renderer) error {
	screen, err := s.engine.RenderScreen(s.name)
	if err != nil {
		return err
	}

	s.widget = screen.Root
	s.root.SetChild(screen.Root)

	// Re-find logos
	s.logos = nil
	s.findLogos(screen.Root)

	return nil
}

// Update updates animation state.
func (s *StarlarkScreen) Update(f graphics.Frame) {
	t := float32(time.Since(s.startTime).Seconds())
	for _, logo := range s.logos {
		logo.SetTime(t)
	}
	s.root.InvalidateLayout()
}

// Render renders the screen.
func (s *StarlarkScreen) Render(f graphics.Frame, pw window.Window) error {
	s.Update(f)
	s.root.Step(f, pw)
	return nil
}

// SetStartTime sets the animation start time.
func (s *StarlarkScreen) SetStartTime(t time.Time) {
	s.startTime = t
}
