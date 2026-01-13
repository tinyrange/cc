package html

// node represents a node in the HTML AST.
type node struct {
	tag        string
	classes    []string
	attributes map[string]string
	children   []*node
	text       string // For text nodes (tag == "")
	parent     *node
}

// newElement creates a new element node.
func newElement(tag string) *node {
	return &node{
		tag:        tag,
		attributes: make(map[string]string),
	}
}

// newText creates a new text node.
func newText(text string) *node {
	return &node{
		text: text,
	}
}

// isText returns true if this is a text node.
func (n *node) isText() bool {
	return n.tag == ""
}

// addClass adds a class to the node.
func (n *node) addClass(class string) {
	n.classes = append(n.classes, class)
}

// setAttribute sets an attribute on the node.
func (n *node) setAttribute(name, value string) {
	n.attributes[name] = value
	// Parse class attribute into classes slice
	if name == "class" {
		n.classes = splitClasses(value)
	}
}

// getAttribute gets an attribute value.
func (n *node) getAttribute(name string) string {
	return n.attributes[name]
}

// appendChild adds a child node.
func (n *node) appendChild(child *node) {
	child.parent = n
	n.children = append(n.children, child)
}

// hasClass checks if the node has a specific class.
func (n *node) hasClass(class string) bool {
	for _, c := range n.classes {
		if c == class {
			return true
		}
	}
	return false
}

// textContent returns all text content of the node and its descendants.
func (n *node) textContent() string {
	if n.isText() {
		return n.text
	}
	var result string
	for _, child := range n.children {
		result += child.textContent()
	}
	return result
}

// splitClasses splits a class attribute value into individual classes.
func splitClasses(value string) []string {
	var classes []string
	var current string
	for _, r := range value {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if current != "" {
				classes = append(classes, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		classes = append(classes, current)
	}
	return classes
}
