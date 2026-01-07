package ui

import (
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/window"
)

// ScrollView provides a scrollable content area.
type ScrollView struct {
	BaseWidget

	content Widget
	scrollX float32
	scrollY float32

	// Scroll bar state
	draggingThumbX  bool
	draggingThumbY  bool
	thumbDragOffset float32

	// Cached
	contentSize Size

	// Config
	scrollbarWidth float32
	showHorizontal bool
	showVertical   bool

	// Colors
	trackColor color.Color
	thumbColor color.Color

	// Viewport for clipping
	viewportRect Rect
}

// NewScrollView creates a new scroll view.
func NewScrollView(content Widget) *ScrollView {
	return &ScrollView{
		BaseWidget:     NewBaseWidget(),
		content:        content,
		scrollbarWidth: 8,
		showVertical:   true,
		showHorizontal: true,
		trackColor:     color.RGBA{R: 48, G: 48, B: 48, A: 255},
		thumbColor:     color.RGBA{R: 100, G: 100, B: 100, A: 255},
	}
}

func (s *ScrollView) WithScrollbarWidth(w float32) *ScrollView {
	s.scrollbarWidth = w
	return s
}

func (s *ScrollView) WithVerticalOnly() *ScrollView {
	s.showHorizontal = false
	s.showVertical = true
	return s
}

func (s *ScrollView) WithHorizontalOnly() *ScrollView {
	s.showHorizontal = true
	s.showVertical = false
	return s
}

func (s *ScrollView) SetContent(content Widget) {
	s.content = content
}

func (s *ScrollView) SetScroll(x, y float32) {
	s.scrollX = x
	s.scrollY = y
	s.clampScroll()
}

func (s *ScrollView) GetScrollX() float32 {
	return s.scrollX
}

func (s *ScrollView) GetScrollY() float32 {
	return s.scrollY
}

func (s *ScrollView) Layout(ctx *LayoutContext, constraints Constraints) Size {
	// Layout content with unlimited constraints to get natural size
	if s.content != nil {
		s.contentSize = s.content.Layout(ctx, Unconstrained())
	}

	// ScrollView takes all available space
	return Size{
		W: constraints.MaxW,
		H: constraints.MaxH,
	}
}

func (s *ScrollView) SetBounds(bounds Rect) {
	s.BaseWidget.SetBounds(bounds)
	s.viewportRect = bounds
	s.clampScroll()
	s.updateContentBounds()
}

func (s *ScrollView) clampScroll() {
	bounds := s.Bounds()
	maxScrollX := max(0, s.contentSize.W-bounds.W)
	maxScrollY := max(0, s.contentSize.H-bounds.H)
	s.scrollX = clamp(s.scrollX, 0, maxScrollX)
	s.scrollY = clamp(s.scrollY, 0, maxScrollY)
}

func (s *ScrollView) updateContentBounds() {
	if s.content == nil {
		return
	}
	bounds := s.Bounds()
	contentBounds := Rect{
		X: bounds.X - s.scrollX,
		Y: bounds.Y - s.scrollY,
		W: s.contentSize.W,
		H: s.contentSize.H,
	}
	s.content.SetBounds(contentBounds)
}

func (s *ScrollView) Draw(ctx *DrawContext) {
	if !s.visible {
		return
	}

	bounds := s.Bounds()

	// Draw content (relies on natural clipping or scissoring if implemented)
	if s.content != nil {
		s.content.Draw(ctx)
	}

	// Draw scrollbars
	maxScrollX := max(0, s.contentSize.W-bounds.W)
	maxScrollY := max(0, s.contentSize.H-bounds.H)

	// Vertical scrollbar
	if s.showVertical && maxScrollY > 0 {
		trackRect := Rect{
			X: bounds.X + bounds.W - s.scrollbarWidth,
			Y: bounds.Y,
			W: s.scrollbarWidth,
			H: bounds.H,
		}
		ctx.Frame.RenderQuad(trackRect.X, trackRect.Y, trackRect.W, trackRect.H, nil, s.trackColor)

		thumbH := bounds.H * (bounds.H / s.contentSize.H)
		if thumbH < 30 {
			thumbH = 30
		}
		thumbY := bounds.Y
		if maxScrollY > 0 {
			thumbY = bounds.Y + (bounds.H-thumbH)*(s.scrollY/maxScrollY)
		}
		ctx.Frame.RenderQuad(trackRect.X, thumbY, trackRect.W, thumbH, nil, s.thumbColor)
	}

	// Horizontal scrollbar
	if s.showHorizontal && maxScrollX > 0 {
		trackRect := Rect{
			X: bounds.X,
			Y: bounds.Y + bounds.H - s.scrollbarWidth,
			W: bounds.W,
			H: s.scrollbarWidth,
		}
		// Adjust width if vertical scrollbar is also visible
		if s.showVertical && maxScrollY > 0 {
			trackRect.W -= s.scrollbarWidth
		}
		ctx.Frame.RenderQuad(trackRect.X, trackRect.Y, trackRect.W, trackRect.H, nil, s.trackColor)

		thumbW := trackRect.W * (bounds.W / s.contentSize.W)
		if thumbW < 30 {
			thumbW = 30
		}
		thumbX := trackRect.X
		if maxScrollX > 0 {
			thumbX = trackRect.X + (trackRect.W-thumbW)*(s.scrollX/maxScrollX)
		}
		ctx.Frame.RenderQuad(thumbX, trackRect.Y, thumbW, trackRect.H, nil, s.thumbColor)
	}
}

func (s *ScrollView) HandleEvent(ctx *EventContext, event Event) bool {
	bounds := s.Bounds()
	maxScrollX := max(0, s.contentSize.W-bounds.W)
	maxScrollY := max(0, s.contentSize.H-bounds.H)

	switch e := event.(type) {
	case *ScrollEvent:
		if bounds.Contains(e.X, e.Y) {
			s.scrollY -= e.DeltaY * 40
			s.scrollX -= e.DeltaX * 40
			s.clampScroll()
			s.updateContentBounds()
			return true
		}

	case *MouseButtonEvent:
		if e.Button == window.ButtonLeft {
			if e.Pressed {
				// Check vertical thumb
				if s.showVertical && maxScrollY > 0 {
					thumbH := bounds.H * (bounds.H / s.contentSize.H)
					if thumbH < 30 {
						thumbH = 30
					}
					thumbY := bounds.Y
					if maxScrollY > 0 {
						thumbY = bounds.Y + (bounds.H-thumbH)*(s.scrollY/maxScrollY)
					}
					thumbRect := Rect{
						X: bounds.X + bounds.W - s.scrollbarWidth,
						Y: thumbY,
						W: s.scrollbarWidth,
						H: thumbH,
					}
					if thumbRect.Contains(e.X, e.Y) {
						s.draggingThumbY = true
						s.thumbDragOffset = e.Y - thumbY
						return true
					}
				}

				// Check horizontal thumb
				if s.showHorizontal && maxScrollX > 0 {
					trackW := bounds.W
					if s.showVertical && maxScrollY > 0 {
						trackW -= s.scrollbarWidth
					}
					thumbW := trackW * (bounds.W / s.contentSize.W)
					if thumbW < 30 {
						thumbW = 30
					}
					thumbX := bounds.X
					if maxScrollX > 0 {
						thumbX = bounds.X + (trackW-thumbW)*(s.scrollX/maxScrollX)
					}
					thumbRect := Rect{
						X: thumbX,
						Y: bounds.Y + bounds.H - s.scrollbarWidth,
						W: thumbW,
						H: s.scrollbarWidth,
					}
					if thumbRect.Contains(e.X, e.Y) {
						s.draggingThumbX = true
						s.thumbDragOffset = e.X - thumbX
						return true
					}
				}
			} else {
				s.draggingThumbX = false
				s.draggingThumbY = false
			}
		}

	case *MouseMoveEvent:
		if s.draggingThumbY && maxScrollY > 0 {
			thumbH := bounds.H * (bounds.H / s.contentSize.H)
			if thumbH < 30 {
				thumbH = 30
			}
			newThumbY := clamp(e.Y-s.thumbDragOffset, bounds.Y, bounds.Y+bounds.H-thumbH)
			if bounds.H-thumbH > 0 {
				s.scrollY = (newThumbY - bounds.Y) / (bounds.H - thumbH) * maxScrollY
			}
			s.clampScroll()
			s.updateContentBounds()
			return true
		}
		if s.draggingThumbX && maxScrollX > 0 {
			trackW := bounds.W
			if s.showVertical && maxScrollY > 0 {
				trackW -= s.scrollbarWidth
			}
			thumbW := trackW * (bounds.W / s.contentSize.W)
			if thumbW < 30 {
				thumbW = 30
			}
			newThumbX := clamp(e.X-s.thumbDragOffset, bounds.X, bounds.X+trackW-thumbW)
			if trackW-thumbW > 0 {
				s.scrollX = (newThumbX - bounds.X) / (trackW - thumbW) * maxScrollX
			}
			s.clampScroll()
			s.updateContentBounds()
			return true
		}
	}

	// Dispatch to content
	if s.content != nil {
		// Always dispatch MouseMoveEvent so children can update hover state
		// even when mouse moves outside the viewport
		if _, isMove := event.(*MouseMoveEvent); isMove {
			return s.content.HandleEvent(ctx, event)
		}
		// For other events, only dispatch if mouse is within bounds
		if bounds.Contains(s.getMousePos(event)) {
			return s.content.HandleEvent(ctx, event)
		}
	}
	return false
}

func (s *ScrollView) getMousePos(event Event) (float32, float32) {
	switch e := event.(type) {
	case *MouseMoveEvent:
		return e.X, e.Y
	case *MouseButtonEvent:
		return e.X, e.Y
	case *ScrollEvent:
		return e.X, e.Y
	}
	return 0, 0
}

func (s *ScrollView) Children() []Widget {
	if s.content != nil {
		return []Widget{s.content}
	}
	return nil
}
