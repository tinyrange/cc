package ui

import (
	"image/color"
	"strings"
)

// WrapLabel displays text with automatic word wrapping.
type WrapLabel struct {
	BaseWidget

	text  string
	style LabelStyle

	// Cached layout data
	lines      []string
	lineHeight float32
	maxWidth   float32
}

// NewWrapLabel creates a new wrapping label.
func NewWrapLabel(text string) *WrapLabel {
	return &WrapLabel{
		BaseWidget: NewBaseWidget(),
		text:       text,
		style:      DefaultLabelStyle(),
	}
}

func (l *WrapLabel) WithStyle(style LabelStyle) *WrapLabel {
	l.style = style
	return l
}

func (l *WrapLabel) WithColor(c color.Color) *WrapLabel {
	l.style.TextColor = c
	return l
}

func (l *WrapLabel) WithSize(size float64) *WrapLabel {
	l.style.TextSize = size
	return l
}

func (l *WrapLabel) SetText(text string) {
	l.text = text
	l.lines = nil
	l.maxWidth = 0
}

func (l *WrapLabel) GetText() string {
	return l.text
}

func (l *WrapLabel) Layout(ctx *LayoutContext, constraints Constraints) Size {
	if ctx.TextRenderer == nil {
		// Rough estimate if no renderer
		l.lineHeight = float32(l.style.TextSize) * 1.2
		l.lines = []string{l.text}
		w := float32(len(l.text)) * float32(l.style.TextSize) * 0.6
		return Size{W: clamp(w, constraints.MinW, constraints.MaxW), H: l.lineHeight}
	}

	l.lineHeight = ctx.TextRenderer.LineHeight(l.style.TextSize)
	maxWidth := constraints.MaxW

	// Re-wrap if width changed or lines not cached
	if l.lines == nil || l.maxWidth != maxWidth {
		l.maxWidth = maxWidth
		l.lines = l.wrapText(ctx, maxWidth)
	}

	// Calculate size
	height := l.lineHeight * float32(len(l.lines))
	width := maxWidth
	if len(l.lines) == 1 {
		// Single line - use actual text width
		width = ctx.TextRenderer.Advance(l.style.TextSize, l.lines[0])
	}

	w := clamp(width, constraints.MinW, constraints.MaxW)
	h := clamp(height, constraints.MinH, constraints.MaxH)

	return Size{W: w, H: h}
}

// wrapText breaks text into lines that fit within maxWidth.
func (l *WrapLabel) wrapText(ctx *LayoutContext, maxWidth float32) []string {
	if maxWidth <= 0 || l.text == "" {
		return []string{l.text}
	}

	var lines []string
	var currentLine strings.Builder
	var currentWidth float32

	words := strings.Fields(l.text)
	spaceWidth := ctx.TextRenderer.Advance(l.style.TextSize, " ")

	for i, word := range words {
		wordWidth := ctx.TextRenderer.Advance(l.style.TextSize, word)

		// Check if word fits on current line
		needSpace := currentLine.Len() > 0
		spaceNeeded := float32(0)
		if needSpace {
			spaceNeeded = spaceWidth
		}

		if currentWidth+spaceNeeded+wordWidth <= maxWidth {
			// Word fits - add it
			if needSpace {
				currentLine.WriteString(" ")
			}
			currentLine.WriteString(word)
			currentWidth += spaceNeeded + wordWidth
		} else if currentLine.Len() == 0 {
			// Word doesn't fit and line is empty - need to break the word
			lines = append(lines, l.breakWord(ctx, word, maxWidth)...)
			currentWidth = 0
		} else {
			// Word doesn't fit - start new line
			lines = append(lines, currentLine.String())
			currentLine.Reset()

			// Check if word itself needs breaking
			if wordWidth > maxWidth {
				lines = append(lines, l.breakWord(ctx, word, maxWidth)...)
				currentWidth = 0
			} else {
				currentLine.WriteString(word)
				currentWidth = wordWidth
			}
		}

		// Handle last word
		if i == len(words)-1 && currentLine.Len() > 0 {
			lines = append(lines, currentLine.String())
		}
	}

	// Handle empty text or trailing content
	if len(lines) == 0 {
		lines = []string{""}
	}

	return lines
}

// breakWord breaks a single word into multiple lines when it's too long.
func (l *WrapLabel) breakWord(ctx *LayoutContext, word string, maxWidth float32) []string {
	var lines []string
	var currentLine strings.Builder
	var currentWidth float32

	for _, r := range word {
		charStr := string(r)
		charWidth := ctx.TextRenderer.Advance(l.style.TextSize, charStr)

		if currentWidth+charWidth <= maxWidth || currentLine.Len() == 0 {
			// Character fits, or we need at least one character per line
			currentLine.WriteRune(r)
			currentWidth += charWidth
		} else {
			// Character doesn't fit - start new line
			lines = append(lines, currentLine.String())
			currentLine.Reset()
			currentLine.WriteRune(r)
			currentWidth = charWidth
		}
	}

	if currentLine.Len() > 0 {
		lines = append(lines, currentLine.String())
	}

	return lines
}

func (l *WrapLabel) Draw(ctx *DrawContext) {
	if !l.visible || ctx.Text == nil || len(l.lines) == 0 {
		return
	}

	bounds := l.Bounds()
	y := bounds.Y

	ctx.Text.BeginBatch()
	for _, line := range l.lines {
		// Text baseline is at Y + lineHeight * 0.85 (same as Label)
		ctx.Text.AddText(line, bounds.X, y+l.lineHeight*0.85, l.style.TextSize, l.style.TextColor)
		y += l.lineHeight
	}
	ctx.Text.EndBatch()
}

func (l *WrapLabel) HandleEvent(ctx *EventContext, event Event) bool {
	return false // WrapLabels don't handle events
}
