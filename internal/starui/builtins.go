package starui

import (
	"fmt"
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/ui"
	"go.starlark.net/starlark"
)

// WidgetValue wraps a ui.Widget for use in Starlark.
type WidgetValue struct {
	Widget ui.Widget
}

var _ starlark.Value = (*WidgetValue)(nil)

func (w *WidgetValue) String() string        { return fmt.Sprintf("<Widget %T>", w.Widget) }
func (w *WidgetValue) Type() string          { return "Widget" }
func (w *WidgetValue) Freeze()               {}
func (w *WidgetValue) Truth() starlark.Bool  { return w.Widget != nil }
func (w *WidgetValue) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable type: Widget") }

// ColorValue wraps a color.Color for use in Starlark.
type ColorValue struct {
	Color color.Color
}

var _ starlark.Value = (*ColorValue)(nil)

func (c *ColorValue) String() string {
	r, g, b, a := c.Color.RGBA()
	return fmt.Sprintf("rgba(%d, %d, %d, %d)", r>>8, g>>8, b>>8, a>>8)
}
func (c *ColorValue) Type() string          { return "Color" }
func (c *ColorValue) Freeze()               {}
func (c *ColorValue) Truth() starlark.Bool  { return c.Color != nil }
func (c *ColorValue) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable type: Color") }

// InsetsValue wraps EdgeInsets for use in Starlark.
type InsetsValue struct {
	Insets ui.EdgeInsets
}

var _ starlark.Value = (*InsetsValue)(nil)

func (i *InsetsValue) String() string {
	return fmt.Sprintf("insets(%g, %g, %g, %g)", i.Insets.Left, i.Insets.Top, i.Insets.Right, i.Insets.Bottom)
}
func (i *InsetsValue) Type() string          { return "Insets" }
func (i *InsetsValue) Freeze()               {}
func (i *InsetsValue) Truth() starlark.Bool  { return true }
func (i *InsetsValue) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable type: Insets") }

// CallbackValue wraps a Starlark callable for deferred execution.
type CallbackValue struct {
	thread *starlark.Thread
	fn     starlark.Callable
	args   starlark.Tuple
}

// Call executes the callback.
func (c *CallbackValue) Call() {
	if c.fn != nil && c.thread != nil {
		starlark.Call(c.thread, c.fn, c.args, nil)
	}
}

// Helper functions for extracting values from Starlark

func extractColor(v starlark.Value) color.Color {
	if v == nil || v == starlark.None {
		return nil
	}
	if cv, ok := v.(*ColorValue); ok {
		return cv.Color
	}
	return nil
}

func extractInsets(v starlark.Value) ui.EdgeInsets {
	if v == nil || v == starlark.None {
		return ui.EdgeInsets{}
	}
	if iv, ok := v.(*InsetsValue); ok {
		return iv.Insets
	}
	// Support tuple (left, top, right, bottom) or single number
	if tuple, ok := v.(starlark.Tuple); ok {
		switch len(tuple) {
		case 1:
			if n, ok := starlark.AsFloat(tuple[0]); ok {
				return ui.All(float32(n))
			}
		case 2:
			h, _ := starlark.AsFloat(tuple[0])
			v, _ := starlark.AsFloat(tuple[1])
			return ui.Symmetric(float32(h), float32(v))
		case 4:
			l, _ := starlark.AsFloat(tuple[0])
			t, _ := starlark.AsFloat(tuple[1])
			r, _ := starlark.AsFloat(tuple[2])
			b, _ := starlark.AsFloat(tuple[3])
			return ui.Only(float32(l), float32(t), float32(r), float32(b))
		}
	}
	// Support single number for uniform padding
	if n, ok := starlark.AsFloat(v); ok {
		return ui.All(float32(n))
	}
	return ui.EdgeInsets{}
}

func extractWidget(v starlark.Value) ui.Widget {
	if v == nil || v == starlark.None {
		return nil
	}
	if wv, ok := v.(*WidgetValue); ok {
		return wv.Widget
	}
	return nil
}

func extractWidgets(v starlark.Value) []ui.Widget {
	if v == nil || v == starlark.None {
		return nil
	}
	// Handle list of widgets
	if list, ok := v.(*starlark.List); ok {
		widgets := make([]ui.Widget, 0, list.Len())
		for i := 0; i < list.Len(); i++ {
			if w := extractWidget(list.Index(i)); w != nil {
				widgets = append(widgets, w)
			}
		}
		return widgets
	}
	// Handle tuple of widgets
	if tuple, ok := v.(starlark.Tuple); ok {
		widgets := make([]ui.Widget, 0, len(tuple))
		for _, item := range tuple {
			if w := extractWidget(item); w != nil {
				widgets = append(widgets, w)
			}
		}
		return widgets
	}
	// Single widget
	if w := extractWidget(v); w != nil {
		return []ui.Widget{w}
	}
	return nil
}

func extractFloat(v starlark.Value, def float32) float32 {
	if v == nil || v == starlark.None {
		return def
	}
	if f, ok := starlark.AsFloat(v); ok {
		return float32(f)
	}
	return def
}

func extractInt(v starlark.Value, def int) int {
	if v == nil || v == starlark.None {
		return def
	}
	if i, err := starlark.AsInt32(v); err == nil {
		return int(i)
	}
	return def
}

func extractString(v starlark.Value) string {
	if v == nil || v == starlark.None {
		return ""
	}
	if s, ok := starlark.AsString(v); ok {
		return s
	}
	return ""
}

func extractBool(v starlark.Value, def bool) bool {
	if v == nil || v == starlark.None {
		return def
	}
	return bool(v.Truth())
}

// extractCallback extracts a callable and wraps it for later execution.
func extractCallback(thread *starlark.Thread, v starlark.Value) func() {
	if v == nil || v == starlark.None {
		return nil
	}
	if fn, ok := v.(starlark.Callable); ok {
		return func() {
			// Execute in a new thread to avoid issues
			newThread := &starlark.Thread{
				Name: "callback",
				Print: func(_ *starlark.Thread, msg string) {
					fmt.Println("[starui callback]", msg)
				},
			}
			// Copy locals from original thread
			if ctx := thread.Local("ctx"); ctx != nil {
				newThread.SetLocal("ctx", ctx)
			}
			starlark.Call(newThread, fn, nil, nil)
		}
	}
	return nil
}

// Row creates a horizontal flex container.
// Row(children=[], gap=0, padding=None, background=None, main_align="main_start", cross_align="cross_start")
func builtinRow(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var children starlark.Value
	var gap starlark.Value
	var padding starlark.Value
	var background starlark.Value
	var mainAlign starlark.Value
	var crossAlign starlark.Value
	var flex starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"children?", &children,
		"gap?", &gap,
		"padding?", &padding,
		"background?", &background,
		"main_align?", &mainAlign,
		"cross_align?", &crossAlign,
		"flex?", &flex,
	); err != nil {
		return nil, err
	}

	row := ui.Row()

	if gap != nil && gap != starlark.None {
		row.WithGap(extractFloat(gap, 0))
	}
	if padding != nil && padding != starlark.None {
		row.WithPadding(extractInsets(padding))
	}
	if background != nil && background != starlark.None {
		row.WithBackground(extractColor(background))
	}
	if mainAlign != nil && mainAlign != starlark.None {
		row.WithMainAlignment(parseMainAlignment(extractString(mainAlign)))
	}
	if crossAlign != nil && crossAlign != starlark.None {
		row.WithCrossAlignment(parseCrossAlignment(extractString(crossAlign)))
	}

	// Add children
	widgets := extractWidgets(children)
	for _, w := range widgets {
		// Check if widget has flex property (we'll need to extract this from WidgetValue)
		row.AddChild(w, ui.DefaultFlexParams())
	}

	return &WidgetValue{Widget: row}, nil
}

// Column creates a vertical flex container.
func builtinColumn(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var children starlark.Value
	var gap starlark.Value
	var padding starlark.Value
	var background starlark.Value
	var mainAlign starlark.Value
	var crossAlign starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"children?", &children,
		"gap?", &gap,
		"padding?", &padding,
		"background?", &background,
		"main_align?", &mainAlign,
		"cross_align?", &crossAlign,
	); err != nil {
		return nil, err
	}

	col := ui.Column()

	if gap != nil && gap != starlark.None {
		col.WithGap(extractFloat(gap, 0))
	}
	if padding != nil && padding != starlark.None {
		col.WithPadding(extractInsets(padding))
	}
	if background != nil && background != starlark.None {
		col.WithBackground(extractColor(background))
	}
	if mainAlign != nil && mainAlign != starlark.None {
		col.WithMainAlignment(parseMainAlignment(extractString(mainAlign)))
	}
	if crossAlign != nil && crossAlign != starlark.None {
		col.WithCrossAlignment(parseCrossAlignment(extractString(crossAlign)))
	}

	// Add children
	widgets := extractWidgets(children)
	for _, w := range widgets {
		col.AddChild(w, ui.DefaultFlexParams())
	}

	return &WidgetValue{Widget: col}, nil
}

// Stack layers children on top of each other.
func builtinStack(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var children starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"children?", &children,
	); err != nil {
		return nil, err
	}

	stack := ui.NewStack()

	widgets := extractWidgets(children)
	for _, w := range widgets {
		stack.AddChild(w)
	}

	return &WidgetValue{Widget: stack}, nil
}

// Center centers its child.
func builtinCenter(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var child starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"child", &child,
	); err != nil {
		return nil, err
	}

	widget := extractWidget(child)
	if widget == nil {
		return nil, fmt.Errorf("Center requires a child widget")
	}

	return &WidgetValue{Widget: ui.NewCenter(widget)}, nil
}

// Padding adds padding around a child.
func builtinPadding(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var child starlark.Value
	var padding starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"child", &child,
		"padding?", &padding,
	); err != nil {
		return nil, err
	}

	widget := extractWidget(child)
	if widget == nil {
		return nil, fmt.Errorf("Padding requires a child widget")
	}

	insets := extractInsets(padding)
	return &WidgetValue{Widget: ui.NewPadding(widget, insets)}, nil
}

// Spacer creates flexible space.
func builtinSpacer(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var width starlark.Value
	var height starlark.Value
	var flex starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"width?", &width,
		"height?", &height,
		"flex?", &flex,
	); err != nil {
		return nil, err
	}

	spacer := ui.NewSpacer()

	w := extractFloat(width, 0)
	h := extractFloat(height, 0)
	if w > 0 || h > 0 {
		spacer.WithSize(w, h)
	}

	return &WidgetValue{Widget: spacer}, nil
}

// Align positions a child within available space.
func builtinAlign(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var child starlark.Value
	var alignment starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"child", &child,
		"alignment?", &alignment,
	); err != nil {
		return nil, err
	}

	widget := extractWidget(child)
	if widget == nil {
		return nil, fmt.Errorf("Align requires a child widget")
	}

	h, v := parseAlignment(extractString(alignment))
	return &WidgetValue{Widget: ui.NewAlign(widget, h, v)}, nil
}

// Text creates a text label.
func builtinText(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var text string
	var size starlark.Value
	var col starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"text", &text,
		"size?", &size,
		"color?", &col,
	); err != nil {
		return nil, err
	}

	label := ui.NewLabel(text)

	if size != nil && size != starlark.None {
		label.WithSize(float64(extractFloat(size, 16)))
	}
	if col != nil && col != starlark.None {
		label.WithColor(extractColor(col))
	}

	return &WidgetValue{Widget: label}, nil
}

// Box creates a colored rectangle.
func builtinBox(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var col starlark.Value
	var width starlark.Value
	var height starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"color?", &col,
		"width?", &width,
		"height?", &height,
	); err != nil {
		return nil, err
	}

	c := extractColor(col)
	if c == nil {
		c = color.RGBA{R: 10, G: 10, B: 10, A: 255}
	}

	box := ui.NewBox(c)

	w := extractFloat(width, 0)
	h := extractFloat(height, 0)
	if w > 0 || h > 0 {
		box.WithSize(w, h)
	}

	return &WidgetValue{Widget: box}, nil
}

// Button creates a clickable button.
func builtinButton(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var text string
	var onClick starlark.Value
	var minWidth starlark.Value
	var minHeight starlark.Value
	var background starlark.Value
	var hoverBackground starlark.Value
	var pressedBackground starlark.Value
	var textColor starlark.Value
	var textSize starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"text", &text,
		"on_click?", &onClick,
		"min_width?", &minWidth,
		"min_height?", &minHeight,
		"background?", &background,
		"hover_background?", &hoverBackground,
		"pressed_background?", &pressedBackground,
		"text_color?", &textColor,
		"text_size?", &textSize,
	); err != nil {
		return nil, err
	}

	btn := ui.NewButton(text)

	if minWidth != nil || minHeight != nil {
		w := extractFloat(minWidth, 60)
		h := extractFloat(minHeight, 32)
		btn.WithMinSize(w, h)
	}

	if onClick != nil && onClick != starlark.None {
		handler := extractCallback(thread, onClick)
		if handler != nil {
			btn.OnClick(handler)
		}
	}

	// Custom styling
	style := ui.DefaultButtonStyle()
	if background != nil && background != starlark.None {
		style.BackgroundNormal = extractColor(background)
	}
	if hoverBackground != nil && hoverBackground != starlark.None {
		style.BackgroundHovered = extractColor(hoverBackground)
	}
	if pressedBackground != nil && pressedBackground != starlark.None {
		style.BackgroundPressed = extractColor(pressedBackground)
	}
	if textColor != nil && textColor != starlark.None {
		style.TextColor = extractColor(textColor)
	}
	if textSize != nil && textSize != starlark.None {
		style.TextSize = float64(extractFloat(textSize, 14))
	}
	btn.WithStyle(style)

	return &WidgetValue{Widget: btn}, nil
}

// Card creates a container with styling.
func builtinCard(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var child starlark.Value
	var onClick starlark.Value
	var width starlark.Value
	var height starlark.Value
	var background starlark.Value
	var hoverBackground starlark.Value
	var borderColor starlark.Value
	var hoverBorderColor starlark.Value
	var borderWidth starlark.Value
	var padding starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"child?", &child,
		"on_click?", &onClick,
		"width?", &width,
		"height?", &height,
		"background?", &background,
		"hover_background?", &hoverBackground,
		"border_color?", &borderColor,
		"hover_border_color?", &hoverBorderColor,
		"border_width?", &borderWidth,
		"padding?", &padding,
	); err != nil {
		return nil, err
	}

	content := extractWidget(child)
	card := ui.NewCard(content)

	// Normal style
	style := ui.CardStyle{
		BackgroundColor: extractColor(background),
		BorderColor:     extractColor(borderColor),
		BorderWidth:     extractFloat(borderWidth, 1),
		Padding:         extractInsets(padding),
	}
	card.WithStyle(style)

	// Hover style
	if hoverBackground != nil || hoverBorderColor != nil {
		hoverStyle := style
		if hoverBackground != nil && hoverBackground != starlark.None {
			hoverStyle.BackgroundColor = extractColor(hoverBackground)
		}
		if hoverBorderColor != nil && hoverBorderColor != starlark.None {
			hoverStyle.BorderColor = extractColor(hoverBorderColor)
		}
		card.WithHoverStyle(hoverStyle)
	}

	// Fixed size
	w := extractFloat(width, 0)
	h := extractFloat(height, 0)
	if w > 0 && h > 0 {
		card.WithFixedSize(w, h)
	}

	// Click handler
	if onClick != nil && onClick != starlark.None {
		handler := extractCallback(thread, onClick)
		if handler != nil {
			card.OnClick(handler)
		}
	}

	return &WidgetValue{Widget: card}, nil
}

// Logo creates the animated logo widget.
func builtinLogo(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var width starlark.Value
	var height starlark.Value
	var speed1 starlark.Value
	var speed2 starlark.Value
	var speed3 starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"width?", &width,
		"height?", &height,
		"speed1?", &speed1,
		"speed2?", &speed2,
		"speed3?", &speed3,
	); err != nil {
		return nil, err
	}

	// Get logo from context
	ctx := thread.Local("ctx")
	if ctx == nil {
		return nil, fmt.Errorf("Logo requires app context")
	}
	appCtx, ok := ctx.(*AppContext)
	if !ok || appCtx.Logo == nil {
		// Return empty spacer if no logo
		return &WidgetValue{Widget: ui.NewSpacer()}, nil
	}

	logo := ui.NewAnimatedLogo(appCtx.Logo)

	w := extractFloat(width, 400)
	h := extractFloat(height, 400)
	logo.WithSize(w, h)

	s1 := extractFloat(speed1, 0.5)
	s2 := extractFloat(speed2, -0.8)
	s3 := extractFloat(speed3, 1.2)
	logo.WithSpeeds(s1, s2, s3)

	return &WidgetValue{Widget: logo}, nil
}

// ScrollView creates a scrollable container.
func builtinScrollView(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var child starlark.Value
	var horizontal starlark.Value
	var scrollbarWidth starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"child", &child,
		"horizontal?", &horizontal,
		"scrollbar_width?", &scrollbarWidth,
	); err != nil {
		return nil, err
	}

	content := extractWidget(child)
	if content == nil {
		return nil, fmt.Errorf("ScrollView requires a child widget")
	}

	sv := ui.NewScrollView(content)

	if extractBool(horizontal, false) {
		sv.WithHorizontalOnly()
	}

	if scrollbarWidth != nil && scrollbarWidth != starlark.None {
		sv.WithScrollbarWidth(extractFloat(scrollbarWidth, 8))
	}

	return &WidgetValue{Widget: sv}, nil
}

// builtinPrint prints values to the console.
func builtinPrint(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	for i, arg := range args {
		if i > 0 {
			fmt.Print(" ")
		}
		fmt.Print(arg)
	}
	fmt.Println()
	return starlark.None, nil
}

// Alignment parsing helpers

func parseAlignment(s string) (h, v float32) {
	switch s {
	case "top_left":
		return 0, 0
	case "top_center":
		return 0.5, 0
	case "top_right":
		return 1, 0
	case "center_left":
		return 0, 0.5
	case "center_center", "center":
		return 0.5, 0.5
	case "center_right":
		return 1, 0.5
	case "bottom_left":
		return 0, 1
	case "bottom_center":
		return 0.5, 1
	case "bottom_right":
		return 1, 1
	default:
		return 0.5, 0.5
	}
}

func parseMainAlignment(s string) ui.MainAxisAlignment {
	switch s {
	case "main_start", "start":
		return ui.MainAxisStart
	case "main_center", "center":
		return ui.MainAxisCenter
	case "main_end", "end":
		return ui.MainAxisEnd
	case "main_space_between", "space_between":
		return ui.MainAxisSpaceBetween
	case "main_space_around", "space_around":
		return ui.MainAxisSpaceAround
	case "main_space_evenly", "space_evenly":
		return ui.MainAxisSpaceEvenly
	default:
		return ui.MainAxisStart
	}
}

func parseCrossAlignment(s string) ui.CrossAxisAlignment {
	switch s {
	case "cross_start", "start":
		return ui.CrossAxisStart
	case "cross_center", "center":
		return ui.CrossAxisCenter
	case "cross_end", "end":
		return ui.CrossAxisEnd
	case "cross_stretch", "stretch":
		return ui.CrossAxisStretch
	default:
		return ui.CrossAxisStart
	}
}
