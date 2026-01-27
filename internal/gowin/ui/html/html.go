// Package html provides a minimal HTML renderer that generates gowin/ui widgets.
//
// It supports a subset of HTML elements and Tailwind CSS classes, mapping them
// to the existing widget system.
//
// Example usage:
//
//	doc, err := html.Parse(`
//	    <div class="flex flex-col gap-4 p-6 bg-card rounded-lg">
//	        <h2 class="text-xl text-primary">Title</h2>
//	        <button data-onclick="save" class="bg-accent px-4 py-2 rounded">Save</button>
//	    </div>
//	`)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	doc.SetHandler("save", func() { app.save() })
//	widget := doc.Render(ctx)
package html

import (
	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
	"github.com/tinyrange/cc/internal/gowin/ui"
)

// Document represents a parsed HTML document ready for rendering.
type Document struct {
	root     *node
	handlers map[string]any
	values   map[string]string
	inputs   map[string]*inputBinding
}

// inputBinding tracks an input widget for value get/set.
type inputBinding struct {
	textInput *ui.TextInput
	checkbox  *ui.Checkbox
}

// RenderContext provides dependencies for rendering.
type RenderContext struct {
	Window       graphics.Window
	TextRenderer *text.Renderer
}

// Parse parses an HTML string into a Document.
func Parse(html string) (*Document, error) {
	root, err := parse(html)
	if err != nil {
		return nil, err
	}

	return &Document{
		root:     root,
		handlers: make(map[string]any),
		values:   make(map[string]string),
		inputs:   make(map[string]*inputBinding),
	}, nil
}

// MustParse parses an HTML string and panics on error.
func MustParse(html string) *Document {
	doc, err := Parse(html)
	if err != nil {
		panic(err)
	}
	return doc
}

// SetHandler registers an event handler by name.
// Handler can be func() for onclick, or func(string) / func(bool) for onchange.
func (d *Document) SetHandler(name string, handler any) {
	d.handlers[name] = handler
}

// SetValue sets the value of a named input.
func (d *Document) SetValue(name string, value string) {
	d.values[name] = value
	if binding, ok := d.inputs[name]; ok {
		if binding.textInput != nil {
			binding.textInput.SetText(value)
		}
	}
}

// GetValue gets the current value of a named input.
func (d *Document) GetValue(name string) string {
	if binding, ok := d.inputs[name]; ok {
		if binding.textInput != nil {
			return binding.textInput.Text()
		}
	}
	return d.values[name]
}

// GetChecked gets the checked state of a named checkbox.
func (d *Document) GetChecked(name string) bool {
	if binding, ok := d.inputs[name]; ok {
		if binding.checkbox != nil {
			return binding.checkbox.IsChecked()
		}
	}
	return false
}

// SetChecked sets the checked state of a named checkbox.
func (d *Document) SetChecked(name string, checked bool) {
	if binding, ok := d.inputs[name]; ok {
		if binding.checkbox != nil {
			binding.checkbox.SetChecked(checked)
		}
	}
}

// Render converts the Document to a ui.Widget tree.
func (d *Document) Render(ctx *RenderContext) ui.Widget {
	// Clear previous bindings
	d.inputs = make(map[string]*inputBinding)

	b := &builder{
		doc: d,
		ctx: ctx,
	}
	return b.build(d.root)
}
