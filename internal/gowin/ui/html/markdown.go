package html

import (
	"bytes"
	"fmt"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// ParseMarkdown converts markdown to HTML, then parses to Document.
// It uses GitHub Flavored Markdown (GFM) which includes table support.
func ParseMarkdown(md string) (*Document, error) {
	return ParseMarkdownWithMaxWidth(md, 0)
}

// ParseMarkdownWithMaxWidth converts markdown to HTML with a maximum width constraint.
// If maxWidth is 0, no max width is applied.
func ParseMarkdownWithMaxWidth(md string, maxWidth float32) (*Document, error) {
	// Create goldmark with GFM extensions (tables, strikethrough, etc.)
	converter := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
	)

	var buf bytes.Buffer
	if err := converter.Convert([]byte(md), &buf); err != nil {
		return nil, err
	}

	// Wrap in a styled container for consistent rendering
	var styleAttr string
	if maxWidth > 0 {
		styleAttr = fmt.Sprintf(` style="width: %.0fpx"`, maxWidth)
	}
	html := `<div class="flex flex-col gap-4 p-6"` + styleAttr + `>` + buf.String() + `</div>`
	return Parse(html)
}

// MustParseMarkdown parses markdown and panics on error.
func MustParseMarkdown(md string) *Document {
	doc, err := ParseMarkdown(md)
	if err != nil {
		panic(err)
	}
	return doc
}
