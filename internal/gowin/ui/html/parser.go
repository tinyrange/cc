package html

import (
	"strings"

	"golang.org/x/net/html"
)

// parse parses an HTML string into a node tree using golang.org/x/net/html.
func parse(input string) (*node, error) {
	// Wrap in a body tag to ensure proper parsing of fragments
	doc, err := html.Parse(strings.NewReader(input))
	if err != nil {
		return nil, err
	}

	// Find the body element and convert its children
	body := findBody(doc)
	if body == nil {
		return newElement("root"), nil
	}

	root := newElement("root")
	for child := body.FirstChild; child != nil; child = child.NextSibling {
		if n := convertNode(child); n != nil {
			root.appendChild(n)
		}
	}

	// If there's only one child, return it directly
	if len(root.children) == 1 {
		return root.children[0], nil
	}

	return root, nil
}

// findBody finds the body element in the parsed document.
func findBody(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "body" {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := findBody(child); found != nil {
			return found
		}
	}
	return nil
}

// convertNode converts an html.Node to our internal node type.
func convertNode(n *html.Node) *node {
	switch n.Type {
	case html.ElementNode:
		return convertElement(n)
	case html.TextNode:
		return convertText(n)
	default:
		return nil
	}
}

// convertElement converts an html.ElementNode to our internal node type.
func convertElement(n *html.Node) *node {
	elem := newElement(n.Data)

	// Convert attributes
	for _, attr := range n.Attr {
		elem.setAttribute(attr.Key, attr.Val)
	}

	// Convert children
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if childNode := convertNode(child); childNode != nil {
			elem.appendChild(childNode)
		}
	}

	return elem
}

// convertText converts an html.TextNode to our internal node type.
func convertText(n *html.Node) *node {
	// Skip nodes that are purely whitespace (newlines, indentation)
	if strings.TrimSpace(n.Data) == "" {
		return nil
	}
	// Preserve the text as-is to maintain newlines for code blocks
	// Just trim leading/trailing pure whitespace lines but keep internal structure
	text := n.Data
	return newText(text)
}
