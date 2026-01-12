package html

import (
	"image/color"
	"strconv"
	"strings"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/ui"
)

// fixedSizer wraps a widget and constrains it to fixed dimensions.
type fixedSizer struct {
	ui.BaseWidget
	child  ui.Widget
	width  float32 // 0 = use child's natural width
	height float32 // 0 = use child's natural height
}

func newFixedSizer(child ui.Widget, width, height float32) *fixedSizer {
	return &fixedSizer{
		BaseWidget: ui.NewBaseWidget(),
		child:      child,
		width:      width,
		height:     height,
	}
}

func (f *fixedSizer) Layout(ctx *ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if f.child == nil {
		return ui.Size{W: f.width, H: f.height}
	}

	// Calculate child constraints based on fixed dimensions
	childConstraints := constraints
	if f.width > 0 {
		childConstraints.MinW = f.width
		childConstraints.MaxW = f.width
	}
	if f.height > 0 {
		childConstraints.MinH = f.height
		childConstraints.MaxH = f.height
	}

	childSize := f.child.Layout(ctx, childConstraints)

	// Return fixed dimensions if set, otherwise child's size
	w := childSize.W
	if f.width > 0 {
		w = f.width
	}
	h := childSize.H
	if f.height > 0 {
		h = f.height
	}
	return ui.Size{W: w, H: h}
}

func (f *fixedSizer) SetBounds(bounds ui.Rect) {
	f.BaseWidget.SetBounds(bounds)
	if f.child != nil {
		f.child.SetBounds(bounds)
	}
}

func (f *fixedSizer) Draw(ctx *ui.DrawContext) {
	if f.child != nil {
		f.child.Draw(ctx)
	}
}

func (f *fixedSizer) HandleEvent(ctx *ui.EventContext, event ui.Event) bool {
	if f.child != nil {
		return f.child.HandleEvent(ctx, event)
	}
	return false
}

func (f *fixedSizer) Children() []ui.Widget {
	if f.child != nil {
		return []ui.Widget{f.child}
	}
	return nil
}

// percentSizer wraps a widget and sizes it as a percentage of available width.
type percentSizer struct {
	ui.BaseWidget
	child   ui.Widget
	percent float32 // 0-1
}

func newPercentSizer(child ui.Widget, percent float32) *percentSizer {
	return &percentSizer{
		BaseWidget: ui.NewBaseWidget(),
		child:      child,
		percent:    percent,
	}
}

func (p *percentSizer) Layout(ctx *ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if p.child == nil {
		return ui.Size{}
	}
	// Calculate width as percentage of max available
	targetWidth := constraints.MaxW * p.percent
	childConstraints := ui.Constraints{
		MinW: targetWidth,
		MaxW: targetWidth,
		MinH: constraints.MinH,
		MaxH: constraints.MaxH,
	}
	childSize := p.child.Layout(ctx, childConstraints)
	return ui.Size{W: targetWidth, H: childSize.H}
}

func (p *percentSizer) SetBounds(bounds ui.Rect) {
	p.BaseWidget.SetBounds(bounds)
	if p.child != nil {
		p.child.SetBounds(bounds)
	}
}

func (p *percentSizer) Draw(ctx *ui.DrawContext) {
	if p.child != nil {
		p.child.Draw(ctx)
	}
}

func (p *percentSizer) HandleEvent(ctx *ui.EventContext, event ui.Event) bool {
	if p.child != nil {
		return p.child.HandleEvent(ctx, event)
	}
	return false
}

func (p *percentSizer) Children() []ui.Widget {
	if p.child != nil {
		return []ui.Widget{p.child}
	}
	return nil
}

// fullWidthSizer forces child to fill available width.
type fullWidthSizer struct {
	ui.BaseWidget
	child ui.Widget
}

func newFullWidthSizer(child ui.Widget) *fullWidthSizer {
	return &fullWidthSizer{
		BaseWidget: ui.NewBaseWidget(),
		child:      child,
	}
}

func (f *fullWidthSizer) Layout(ctx *ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if f.child == nil {
		return ui.Size{W: constraints.MaxW, H: 0}
	}
	// Force child to fill available width
	childConstraints := ui.Constraints{
		MinW: constraints.MaxW,
		MaxW: constraints.MaxW,
		MinH: constraints.MinH,
		MaxH: constraints.MaxH,
	}
	childSize := f.child.Layout(ctx, childConstraints)
	return ui.Size{W: constraints.MaxW, H: childSize.H}
}

func (f *fullWidthSizer) SetBounds(bounds ui.Rect) {
	f.BaseWidget.SetBounds(bounds)
	if f.child != nil {
		f.child.SetBounds(bounds)
	}
}

func (f *fullWidthSizer) Draw(ctx *ui.DrawContext) {
	if f.child != nil {
		f.child.Draw(ctx)
	}
}

func (f *fullWidthSizer) HandleEvent(ctx *ui.EventContext, event ui.Event) bool {
	if f.child != nil {
		return f.child.HandleEvent(ctx, event)
	}
	return false
}

func (f *fullWidthSizer) Children() []ui.Widget {
	if f.child != nil {
		return []ui.Widget{f.child}
	}
	return nil
}

// parseInlineStyle parses CSS style attribute and applies to StyleSet.
func parseInlineStyle(style string, ss *StyleSet) {
	for _, decl := range strings.Split(style, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		parts := strings.SplitN(decl, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "width":
			if strings.HasSuffix(value, "%") {
				pct, err := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 32)
				if err == nil {
					ss.WidthPercent = float32(pct / 100)
				}
			} else if strings.HasSuffix(value, "px") {
				w, err := strconv.ParseFloat(strings.TrimSuffix(value, "px"), 32)
				if err == nil {
					v := float32(w)
					ss.Width = &v
				}
			}
		case "height":
			if strings.HasSuffix(value, "px") {
				h, err := strconv.ParseFloat(strings.TrimSuffix(value, "px"), 32)
				if err == nil {
					v := float32(h)
					ss.Height = &v
				}
			}
		}
	}
}

// builder converts HTML nodes to widgets.
type builder struct {
	doc *Document
	ctx *RenderContext
}

// build converts a node tree to a widget.
func (b *builder) build(n *node) ui.Widget {
	if n == nil {
		return nil
	}

	if n.isText() {
		return b.buildTextNode(n)
	}

	return b.buildElement(n)
}

// buildElement converts an element node to a widget.
func (b *builder) buildElement(n *node) ui.Widget {
	switch n.tag {
	case "root":
		return b.buildContainer(n)
	case "div":
		return b.buildDiv(n)
	case "span", "label":
		return b.buildSpan(n)
	case "p":
		return b.buildParagraph(n)
	case "h1", "h2", "h3", "h4", "h5", "h6":
		return b.buildHeading(n)
	case "button":
		return b.buildButton(n)
	case "input":
		return b.buildInput(n)
	case "svg":
		return b.buildSVG(n)
	default:
		// Treat unknown elements as divs
		return b.buildDiv(n)
	}
}

// buildTextNode creates a label from a text node.
func (b *builder) buildTextNode(n *node) ui.Widget {
	if n.text == "" {
		return nil
	}
	return ui.NewLabel(n.text)
}

// buildContainer builds a container for root/multiple children.
func (b *builder) buildContainer(n *node) ui.Widget {
	styles := ParseClasses(n.classes)

	container := b.createFlexContainer(styles)

	for _, child := range n.children {
		if w := b.build(child); w != nil {
			childStyles := ParseClasses(child.classes)
			params := b.flexParamsFromStyles(childStyles)
			container.AddChild(w, params)
		}
	}

	return container
}

// buildDiv builds a FlexContainer from a div element.
func (b *builder) buildDiv(n *node) ui.Widget {
	styles := ParseClasses(n.classes)

	// Parse inline style attribute
	if styleAttr := n.getAttribute("style"); styleAttr != "" {
		parseInlineStyle(styleAttr, &styles)
	}

	container := b.createFlexContainer(styles)

	// Apply background color (unless we have a gradient, which needs a Card)
	hasGradient := styles.GradientDir != GradientNone && styles.GradientFrom != nil && styles.GradientTo != nil
	if styles.BackgroundColor != nil && !hasGradient {
		container.WithBackground(*styles.BackgroundColor)
	}

	// Build children
	for _, child := range n.children {
		if w := b.build(child); w != nil {
			childStyles := ParseClasses(child.classes)
			params := b.flexParamsFromStyles(childStyles)
			container.AddChild(w, params)
		}
	}

	// Check if we need borders
	hasBorder := styles.BorderWidth > 0 || styles.BorderTop > 0 || styles.BorderRight > 0 ||
		styles.BorderBottom > 0 || styles.BorderLeft > 0

	// If we have styling that needs a Card wrapper (corner radius, background, gradient, border), wrap it
	needsCard := (styles.CornerRadius != nil && *styles.CornerRadius > 0) || hasGradient || hasBorder
	if needsCard {
		cardStyle := ui.CardStyle{}
		if styles.CornerRadius != nil {
			cardStyle.CornerRadius = *styles.CornerRadius
		}
		if styles.BackgroundColor != nil {
			cardStyle.BackgroundColor = *styles.BackgroundColor
			container.WithBackground(nil) // Let card handle background
		}

		// Apply gradient
		if hasGradient {
			cardStyle.GradientDirection = convertGradientDirection(styles.GradientDir)
			cardStyle.GradientStops = buildGradientStops(styles.GradientFrom, styles.GradientVia, styles.GradientTo)
		}

		// Apply border
		if hasBorder {
			// Use overall border width if set, otherwise use individual sides
			if styles.BorderWidth > 0 {
				cardStyle.BorderWidth = styles.BorderWidth
			} else {
				// For now, use max of individual borders as uniform border
				maxBorder := styles.BorderTop
				if styles.BorderRight > maxBorder {
					maxBorder = styles.BorderRight
				}
				if styles.BorderBottom > maxBorder {
					maxBorder = styles.BorderBottom
				}
				if styles.BorderLeft > maxBorder {
					maxBorder = styles.BorderLeft
				}
				cardStyle.BorderWidth = maxBorder
			}
			if styles.BorderColor != nil {
				cardStyle.BorderColor = *styles.BorderColor
			} else {
				// Default border color
				cardStyle.BorderColor = color.RGBA{R: 200, G: 200, B: 200, A: 255}
			}
		}

		cardStyle.Padding = styles.Padding

		// Reset container padding since card handles it
		container.WithPadding(ui.EdgeInsets{})

		card := ui.NewCard(container).WithStyle(cardStyle)
		if b.ctx != nil && b.ctx.Window != nil {
			card.WithGraphicsWindow(b.ctx.Window)
		}

		// Apply fixed size if specified
		var fixedW, fixedH float32
		if styles.Width != nil {
			fixedW = *styles.Width
		}
		if styles.Height != nil {
			fixedH = *styles.Height
		}
		if fixedW > 0 || fixedH > 0 {
			card.WithFixedSize(fixedW, fixedH)
		}

		// Wrap in percentSizer if percentage width is set
		var cardResult ui.Widget = card
		if styles.WidthPercent > 0 {
			cardResult = newPercentSizer(cardResult, styles.WidthPercent)
		}
		// Wrap in fullWidthSizer if w-full is set
		if styles.FullWidth {
			cardResult = newFullWidthSizer(cardResult)
		}
		return cardResult
	}

	// Determine if we need any sizer wrappers
	var result ui.Widget = container

	// Check for fixed dimensions
	var fixedW, fixedH float32
	if styles.Width != nil {
		fixedW = *styles.Width
	}
	if styles.Height != nil {
		fixedH = *styles.Height
	}
	if fixedW > 0 || fixedH > 0 {
		result = newFixedSizer(result, fixedW, fixedH)
	}

	// Wrap in percentSizer if percentage width is set
	if styles.WidthPercent > 0 {
		result = newPercentSizer(result, styles.WidthPercent)
	}

	// Wrap in fullWidthSizer if w-full is set
	if styles.FullWidth {
		result = newFullWidthSizer(result)
	}

	return result
}

// convertGradientDirection maps our HTML gradient directions to graphics directions.
func convertGradientDirection(dir GradientDirection) graphics.GradientDirection {
	switch dir {
	case GradientToTop:
		return graphics.GradientVertical // Will need to reverse stops
	case GradientToBottom:
		return graphics.GradientVertical
	case GradientToLeft:
		return graphics.GradientHorizontal // Will need to reverse stops
	case GradientToRight:
		return graphics.GradientHorizontal
	case GradientToBottomRight:
		return graphics.GradientDiagonalTL // 135deg: top-left to bottom-right
	case GradientToBottomLeft:
		return graphics.GradientDiagonalTR // 45deg reversed
	case GradientToTopRight:
		return graphics.GradientDiagonalTR // 45deg: top-right to bottom-left
	case GradientToTopLeft:
		return graphics.GradientDiagonalTL // 135deg reversed
	default:
		return graphics.GradientNone
	}
}

// buildGradientStops creates ColorStop array from from/via/to colors.
func buildGradientStops(from, via, to *color.RGBA) []graphics.ColorStop {
	var stops []graphics.ColorStop

	if from != nil {
		stops = append(stops, graphics.ColorStop{Position: 0.0, Color: *from})
	}
	if via != nil {
		stops = append(stops, graphics.ColorStop{Position: 0.5, Color: *via})
	}
	if to != nil {
		stops = append(stops, graphics.ColorStop{Position: 1.0, Color: *to})
	}

	return stops
}

// createFlexContainer creates a FlexContainer with styles applied.
func (b *builder) createFlexContainer(styles StyleSet) *ui.FlexContainer {
	var container *ui.FlexContainer

	if styles.Axis == ui.AxisHorizontal {
		container = ui.Row()
	} else {
		container = ui.Column()
	}

	container.WithPadding(styles.Padding)
	container.WithGap(styles.Gap)
	container.WithMainAlignment(styles.MainAlign)
	container.WithCrossAlignment(styles.CrossAlign)

	return container
}

// flexParamsFromStyles extracts FlexLayoutParams from styles.
func (b *builder) flexParamsFromStyles(styles StyleSet) ui.FlexLayoutParams {
	return ui.FlexLayoutParams{
		Flex:   styles.Flex,
		Margin: styles.Margin,
	}
}

// buildSpan builds a Label from a span/label element.
func (b *builder) buildSpan(n *node) ui.Widget {
	text := n.textContent()
	styles := ParseClasses(n.classes)

	label := ui.NewLabel(text)

	if styles.TextColor != nil {
		label.WithColor(*styles.TextColor)
	}
	if styles.TextSize != nil {
		label.WithSize(*styles.TextSize)
	}

	return label
}

// buildParagraph builds a WrapLabel from a p element.
func (b *builder) buildParagraph(n *node) ui.Widget {
	text := n.textContent()
	styles := ParseClasses(n.classes)

	label := ui.NewWrapLabel(text)

	if styles.TextColor != nil {
		label.WithColor(*styles.TextColor)
	}
	if styles.TextSize != nil {
		label.WithSize(*styles.TextSize)
	}

	return label
}

// buildHeading builds a styled Label from h1-h6 elements.
func (b *builder) buildHeading(n *node) ui.Widget {
	text := n.textContent()
	styles := ParseClasses(n.classes)

	// Determine text size
	var textSize float64
	if styles.TextSize != nil {
		textSize = *styles.TextSize
	} else if size, ok := headingSizes[n.tag]; ok {
		textSize = size
	} else {
		textSize = 24 // default heading size
	}

	// Check if this should be gradient text
	if styles.GradientText {
		return b.buildGradientLabel(text, textSize)
	}

	label := ui.NewLabel(text)
	label.WithSize(textSize)

	if styles.TextColor != nil {
		label.WithColor(*styles.TextColor)
	}

	return label
}

// buildGradientLabel creates a GradientLabel with the CrumbleCracker title gradient.
func (b *builder) buildGradientLabel(text string, textSize float64) ui.Widget {
	// CrumbleCracker gradient: #FF9500 0% → #F43F7A 40% → #8B5CF6 70% → #06B6D4 100%
	stops := []graphics.ColorStop{
		{Position: 0.0, Color: color.RGBA{R: 0xFF, G: 0x95, B: 0x00, A: 255}},  // mango-500
		{Position: 0.4, Color: color.RGBA{R: 0xF4, G: 0x3F, B: 0x7A, A: 255}},  // berry-500
		{Position: 0.7, Color: color.RGBA{R: 0x8B, G: 0x5C, B: 0xF6, A: 255}},  // grape-500
		{Position: 1.0, Color: color.RGBA{R: 0x06, G: 0xB6, B: 0xD4, A: 255}},  // ocean-500
	}

	return ui.NewGradientLabel(text).
		WithSize(textSize).
		WithGradient(stops)
}

// buildButton builds a Button from a button element.
// If the button contains SVG children and no text, it renders as an icon button.
func (b *builder) buildButton(n *node) ui.Widget {
	text := strings.TrimSpace(n.textContent())
	styles := ParseClasses(n.classes)

	// Check if button has SVG child (icon button)
	hasIconChild := false
	for _, child := range n.children {
		if child.tag == "svg" {
			hasIconChild = true
			break
		}
	}

	// If it's an icon-only button, render as Card with content
	if hasIconChild && text == "" {
		return b.buildIconButton(n, styles)
	}

	// Regular text button
	btn := ui.NewButton(text)

	// Build button style
	btnStyle := ui.DefaultButtonStyle()

	if styles.BackgroundColor != nil {
		btnStyle.BackgroundNormal = *styles.BackgroundColor
		// Generate hover/pressed variants
		btnStyle.BackgroundHovered = lighten(*styles.BackgroundColor, 0.1)
		btnStyle.BackgroundPressed = darken(*styles.BackgroundColor, 0.1)
	}

	// Apply gradient if specified (overrides solid background)
	if styles.GradientDir != GradientNone && (styles.GradientFrom != nil || styles.GradientTo != nil) {
		btnStyle.GradientDirection = convertGradientDirection(styles.GradientDir)
		btnStyle.GradientStops = buildGradientStops(styles.GradientFrom, styles.GradientVia, styles.GradientTo)
		// Set hover/pressed using gradient end color for smooth transitions
		if styles.GradientTo != nil {
			btnStyle.BackgroundHovered = lighten(*styles.GradientTo, 0.1)
			btnStyle.BackgroundPressed = darken(*styles.GradientTo, 0.1)
		} else if styles.GradientFrom != nil {
			btnStyle.BackgroundHovered = lighten(*styles.GradientFrom, 0.1)
			btnStyle.BackgroundPressed = darken(*styles.GradientFrom, 0.1)
		}
	}

	if styles.TextColor != nil {
		btnStyle.TextColor = *styles.TextColor
	}

	if styles.TextSize != nil {
		btnStyle.TextSize = *styles.TextSize
	}

	if styles.CornerRadius != nil {
		btnStyle.CornerRadius = *styles.CornerRadius
	}

	btnStyle.Padding = styles.Padding

	if styles.MinWidth > 0 {
		btnStyle.MinWidth = styles.MinWidth
	}
	if styles.MinHeight > 0 {
		btnStyle.MinHeight = styles.MinHeight
	}
	if styles.Width != nil {
		btnStyle.MinWidth = *styles.Width
	}
	if styles.Height != nil {
		btnStyle.MinHeight = *styles.Height
	}

	btn.WithStyle(btnStyle)

	// Set graphics window for gradient/rounded corner rendering
	if b.ctx != nil && b.ctx.Window != nil {
		btn.WithGraphicsWindow(b.ctx.Window)
	}

	// Attach click handler
	if handlerName := n.getAttribute("data-onclick"); handlerName != "" {
		if handler, ok := b.doc.handlers[handlerName]; ok {
			if fn, ok := handler.(func()); ok {
				btn.OnClick(fn)
			}
		}
	}

	return btn
}

// buildIconButton builds a button with icon content using Card.
func (b *builder) buildIconButton(n *node, styles StyleSet) ui.Widget {
	// Build child content (SVG icon)
	container := b.createFlexContainer(styles)
	for _, child := range n.children {
		if w := b.build(child); w != nil {
			container.AddChild(w, ui.DefaultFlexParams())
		}
	}

	// Build Card style
	cardStyle := ui.CardStyle{
		Padding: styles.Padding,
	}

	if styles.BackgroundColor != nil {
		cardStyle.BackgroundColor = *styles.BackgroundColor
	}
	if styles.CornerRadius != nil {
		cardStyle.CornerRadius = *styles.CornerRadius
	}

	// Create hover style (slightly lighter)
	var hoverStyle *ui.CardStyle
	if styles.BackgroundColor != nil {
		hs := ui.CardStyle{
			BackgroundColor: lighten(*styles.BackgroundColor, 0.1),
			Padding:         styles.Padding,
		}
		if styles.CornerRadius != nil {
			hs.CornerRadius = *styles.CornerRadius
		}
		hoverStyle = &hs
	}

	card := ui.NewCard(container).WithStyle(cardStyle)
	if hoverStyle != nil {
		card.WithHoverStyle(*hoverStyle)
	}
	if b.ctx != nil && b.ctx.Window != nil {
		card.WithGraphicsWindow(b.ctx.Window)
	}

	// Apply fixed size if specified
	var fixedW, fixedH float32
	if styles.Width != nil {
		fixedW = *styles.Width
	}
	if styles.Height != nil {
		fixedH = *styles.Height
	}
	if fixedW > 0 || fixedH > 0 {
		card.WithFixedSize(fixedW, fixedH)
	}

	// Attach click handler
	if handlerName := n.getAttribute("data-onclick"); handlerName != "" {
		if handler, ok := b.doc.handlers[handlerName]; ok {
			if fn, ok := handler.(func()); ok {
				card.OnClick(fn)
			}
		}
	}

	return card
}

// buildInput builds an input widget based on type.
func (b *builder) buildInput(n *node) ui.Widget {
	inputType := n.getAttribute("type")
	if inputType == "" {
		inputType = "text"
	}

	switch inputType {
	case "checkbox":
		return b.buildCheckbox(n)
	default:
		return b.buildTextInput(n)
	}
}

// buildTextInput builds a TextInput widget.
func (b *builder) buildTextInput(n *node) ui.Widget {
	styles := ParseClasses(n.classes)

	input := ui.NewTextInput()

	// Set placeholder
	if placeholder := n.getAttribute("placeholder"); placeholder != "" {
		input.WithPlaceholder(placeholder)
	}

	// Build style
	inputStyle := ui.DefaultTextInputStyle()

	if styles.BackgroundColor != nil {
		inputStyle.BackgroundColor = *styles.BackgroundColor
		inputStyle.BackgroundColorFocused = lighten(*styles.BackgroundColor, 0.05)
	}

	if styles.TextColor != nil {
		inputStyle.TextColor = *styles.TextColor
	}

	if styles.TextSize != nil {
		inputStyle.TextSize = *styles.TextSize
	}

	if styles.CornerRadius != nil {
		inputStyle.CornerRadius = *styles.CornerRadius
	}

	inputStyle.Padding = styles.Padding

	if styles.Width != nil {
		inputStyle.MinWidth = *styles.Width
	}
	if styles.Height != nil {
		inputStyle.Height = *styles.Height
	}

	input.WithStyle(inputStyle)

	// Set initial value if provided
	bindName := n.getAttribute("data-bind")
	if bindName != "" {
		if value, ok := b.doc.values[bindName]; ok {
			input.SetText(value)
		}

		// Store binding
		b.doc.inputs[bindName] = &inputBinding{textInput: input}

		// Attach change handler
		if handler, ok := b.doc.handlers[bindName]; ok {
			if fn, ok := handler.(func(string)); ok {
				input.OnChange(fn)
			}
		}
	}

	// Also check data-onchange for explicit handler
	if handlerName := n.getAttribute("data-onchange"); handlerName != "" {
		if handler, ok := b.doc.handlers[handlerName]; ok {
			if fn, ok := handler.(func(string)); ok {
				input.OnChange(fn)
			}
		}
	}

	return input
}

// buildCheckbox builds a Checkbox widget.
func (b *builder) buildCheckbox(n *node) ui.Widget {
	label := n.getAttribute("label")
	if label == "" {
		// Look for adjacent label text
		label = ""
	}

	checkbox := ui.NewCheckbox(label)

	// Set initial checked state
	bindName := n.getAttribute("data-bind")
	if bindName != "" {
		// Store binding
		b.doc.inputs[bindName] = &inputBinding{checkbox: checkbox}

		// Attach change handler
		if handler, ok := b.doc.handlers[bindName]; ok {
			if fn, ok := handler.(func(bool)); ok {
				checkbox.OnChange(fn)
			}
		}
	}

	// Also check data-onchange for explicit handler
	if handlerName := n.getAttribute("data-onchange"); handlerName != "" {
		if handler, ok := b.doc.handlers[handlerName]; ok {
			if fn, ok := handler.(func(bool)); ok {
				checkbox.OnChange(fn)
			}
		}
	}

	return checkbox
}

// Color manipulation helpers

func lighten(c color.RGBA, amount float64) color.RGBA {
	return color.RGBA{
		R: clampByte(float64(c.R) + 255*amount),
		G: clampByte(float64(c.G) + 255*amount),
		B: clampByte(float64(c.B) + 255*amount),
		A: c.A,
	}
}

func darken(c color.RGBA, amount float64) color.RGBA {
	return color.RGBA{
		R: clampByte(float64(c.R) - 255*amount),
		G: clampByte(float64(c.G) - 255*amount),
		B: clampByte(float64(c.B) - 255*amount),
		A: c.A,
	}
}

func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// buildSVG builds an SVGImage widget from an <svg> element.
func (b *builder) buildSVG(n *node) ui.Widget {
	if b.ctx == nil || b.ctx.Window == nil {
		// Can't render SVG without a window
		return nil
	}

	// Serialize node back to SVG XML
	svgXML := serializeToSVG(n)

	// Parse SVG
	svg, err := graphics.LoadSVG(b.ctx.Window, []byte(svgXML))
	if err != nil {
		// Return empty widget on error
		return ui.NewSpacer()
	}

	// Get dimensions from attributes or viewBox
	var width, height float32 = 24, 24 // Default icon size

	if w := n.getAttribute("width"); w != "" {
		if v, err := strconv.ParseFloat(strings.TrimSuffix(w, "px"), 32); err == nil {
			width = float32(v)
		}
	}
	if h := n.getAttribute("height"); h != "" {
		if v, err := strconv.ParseFloat(strings.TrimSuffix(h, "px"), 32); err == nil {
			height = float32(v)
		}
	}

	// If no explicit dimensions, use viewBox
	if svg.Width() > 0 && svg.Height() > 0 {
		if n.getAttribute("width") == "" && n.getAttribute("height") == "" {
			width = svg.Width()
			height = svg.Height()
		}
	}

	styles := ParseClasses(n.classes)
	if styles.Width != nil {
		width = *styles.Width
	}
	if styles.Height != nil {
		height = *styles.Height
	}

	return ui.NewSVGImage(svg).WithSize(width, height)
}

// serializeToSVG converts a node tree back to SVG XML string.
func serializeToSVG(n *node) string {
	var sb strings.Builder
	serializeNode(&sb, n)
	return sb.String()
}

func serializeNode(sb *strings.Builder, n *node) {
	if n.isText() {
		sb.WriteString(n.text)
		return
	}

	sb.WriteString("<")
	sb.WriteString(n.tag)

	// Write attributes
	for name, value := range n.attributes {
		sb.WriteString(" ")
		sb.WriteString(name)
		sb.WriteString("=\"")
		sb.WriteString(escapeXML(value))
		sb.WriteString("\"")
	}

	// Self-closing tags for elements with no children
	if len(n.children) == 0 {
		sb.WriteString("/>")
		return
	}

	sb.WriteString(">")

	// Write children
	for _, child := range n.children {
		serializeNode(sb, child)
	}

	sb.WriteString("</")
	sb.WriteString(n.tag)
	sb.WriteString(">")
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
