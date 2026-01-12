package html

import (
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/ui"
)

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

	container := b.createFlexContainer(styles)

	// Apply background color
	if styles.BackgroundColor != nil {
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

	// If we have styling that needs a Card wrapper (corner radius, background), wrap it
	if styles.CornerRadius != nil && *styles.CornerRadius > 0 {
		cardStyle := ui.CardStyle{
			CornerRadius: *styles.CornerRadius,
		}
		if styles.BackgroundColor != nil {
			cardStyle.BackgroundColor = *styles.BackgroundColor
			container.WithBackground(nil) // Let card handle background
		}
		cardStyle.Padding = styles.Padding

		// Reset container padding since card handles it
		container.WithPadding(ui.EdgeInsets{})

		return ui.NewCard(container).WithStyle(cardStyle)
	}

	return container
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

	label := ui.NewLabel(text)

	// Apply heading size if not overridden by class
	if styles.TextSize == nil {
		if size, ok := headingSizes[n.tag]; ok {
			label.WithSize(size)
		}
	} else {
		label.WithSize(*styles.TextSize)
	}

	if styles.TextColor != nil {
		label.WithColor(*styles.TextColor)
	}

	return label
}

// buildButton builds a Button from a button element.
func (b *builder) buildButton(n *node) ui.Widget {
	text := n.textContent()
	styles := ParseClasses(n.classes)

	btn := ui.NewButton(text)

	// Build button style
	btnStyle := ui.DefaultButtonStyle()

	if styles.BackgroundColor != nil {
		btnStyle.BackgroundNormal = *styles.BackgroundColor
		// Generate hover/pressed variants
		btnStyle.BackgroundHovered = lighten(*styles.BackgroundColor, 0.1)
		btnStyle.BackgroundPressed = darken(*styles.BackgroundColor, 0.1)
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
