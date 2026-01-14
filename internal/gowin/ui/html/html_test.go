package html

import (
	"testing"

	"github.com/tinyrange/cc/internal/gowin/ui"
)

func TestParseSimpleDiv(t *testing.T) {
	doc, err := Parse(`<div class="flex flex-col">Hello</div>`)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if doc == nil {
		t.Fatal("doc is nil")
	}
	if doc.root == nil {
		t.Fatal("root is nil")
	}
	if doc.root.tag != "div" {
		t.Errorf("expected tag 'div', got '%s'", doc.root.tag)
	}
}

func TestParseNestedElements(t *testing.T) {
	html := `
		<div class="flex flex-col gap-4 p-6">
			<h2 class="text-xl text-primary">Title</h2>
			<p class="text-secondary">Description</p>
		</div>
	`
	doc, err := Parse(html)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(doc.root.children) != 2 {
		t.Errorf("expected 2 children, got %d", len(doc.root.children))
	}
}

func TestParseButton(t *testing.T) {
	html := `<button data-onclick="save" class="bg-accent px-4 py-2 rounded">Save</button>`
	doc, err := Parse(html)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	doc.SetHandler("save", func() {
		// Handler registered
	})

	widget := doc.Render(nil)
	if widget == nil {
		t.Fatal("widget is nil")
	}

	// Check it's a button
	_, ok := widget.(*ui.Button)
	if !ok {
		t.Fatalf("expected *ui.Button, got %T", widget)
	}
}

func TestParseInput(t *testing.T) {
	html := `<input type="text" placeholder="Enter name" data-bind="name" />`
	doc, err := Parse(html)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	doc.SetValue("name", "initial")

	widget := doc.Render(nil)
	if widget == nil {
		t.Fatal("widget is nil")
	}

	// Check it's a TextInput
	_, ok := widget.(*ui.TextInput)
	if !ok {
		t.Fatalf("expected *ui.TextInput, got %T", widget)
	}
}

func TestParseSelfClosingTags(t *testing.T) {
	html := `<div><input type="text" /><br><input type="checkbox" /></div>`
	doc, err := Parse(html)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(doc.root.children) != 3 {
		t.Errorf("expected 3 children, got %d", len(doc.root.children))
	}
}

func TestParseComment(t *testing.T) {
	html := `<div><!-- This is a comment --><span>Text</span></div>`
	doc, err := Parse(html)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	// Comment should be skipped
	if len(doc.root.children) != 1 {
		t.Errorf("expected 1 child, got %d", len(doc.root.children))
	}
}

func TestClassSpacing(t *testing.T) {
	tests := []struct {
		class   string
		padding ui.EdgeInsets
	}{
		{"p-4", ui.All(16)},
		{"p-6", ui.All(24)},
		{"px-4", ui.EdgeInsets{Left: 16, Right: 16}},
		{"py-2", ui.EdgeInsets{Top: 8, Bottom: 8}},
	}

	for _, tt := range tests {
		styles := ParseClasses([]string{tt.class})
		if styles.Padding != tt.padding {
			t.Errorf("%s: expected padding %+v, got %+v", tt.class, tt.padding, styles.Padding)
		}
	}
}

func TestClassFlex(t *testing.T) {
	styles := ParseClasses([]string{"flex", "flex-row", "gap-4", "justify-between", "items-center"})

	if styles.Axis != ui.AxisHorizontal {
		t.Errorf("expected AxisHorizontal, got %v", styles.Axis)
	}
	if styles.Gap != 16 {
		t.Errorf("expected gap 16, got %v", styles.Gap)
	}
	if styles.MainAlign != ui.MainAxisSpaceBetween {
		t.Errorf("expected MainAxisSpaceBetween, got %v", styles.MainAlign)
	}
	if styles.CrossAlign != ui.CrossAxisCenter {
		t.Errorf("expected CrossAxisCenter, got %v", styles.CrossAlign)
	}
}

func TestClassColors(t *testing.T) {
	styles := ParseClasses([]string{"bg-accent", "text-primary"})

	if styles.BackgroundColor == nil {
		t.Error("expected BackgroundColor to be set")
	} else if *styles.BackgroundColor != colorAccent {
		t.Errorf("expected bg-accent color, got %+v", *styles.BackgroundColor)
	}

	if styles.TextColor == nil {
		t.Error("expected TextColor to be set")
	} else if *styles.TextColor != colorTextPrimary {
		t.Errorf("expected text-primary color, got %+v", *styles.TextColor)
	}
}

func TestClassTextSize(t *testing.T) {
	tests := []struct {
		class string
		size  float64
	}{
		{"text-xs", 12},
		{"text-sm", 14},
		{"text-base", 16},
		{"text-lg", 18},
		{"text-xl", 20},
		{"text-2xl", 24},
	}

	for _, tt := range tests {
		styles := ParseClasses([]string{tt.class})
		if styles.TextSize == nil {
			t.Errorf("%s: expected TextSize to be set", tt.class)
		} else if *styles.TextSize != tt.size {
			t.Errorf("%s: expected size %v, got %v", tt.class, tt.size, *styles.TextSize)
		}
	}
}

func TestClassRounded(t *testing.T) {
	tests := []struct {
		class  string
		radius float32
	}{
		{"rounded", 4},
		{"rounded-sm", 4},
		{"rounded-md", 6},
		{"rounded-lg", 10},
		{"rounded-xl", 12},
		{"rounded-full", 9999},
	}

	for _, tt := range tests {
		styles := ParseClasses([]string{tt.class})
		if styles.CornerRadius == nil {
			t.Errorf("%s: expected CornerRadius to be set", tt.class)
		} else if *styles.CornerRadius != tt.radius {
			t.Errorf("%s: expected radius %v, got %v", tt.class, tt.radius, *styles.CornerRadius)
		}
	}
}

func TestGradientParsing(t *testing.T) {
	classes := []string{"w-12", "h-12", "rounded-xl", "bg-gradient-to-br", "from-mango-400", "to-mango-600"}
	styles := ParseClasses(classes)

	if styles.GradientDir != GradientToBottomRight {
		t.Errorf("expected GradientToBottomRight, got %v", styles.GradientDir)
	}
	if styles.GradientFrom == nil {
		t.Error("GradientFrom is nil")
	} else {
		t.Logf("GradientFrom: R=%d G=%d B=%d", styles.GradientFrom.R, styles.GradientFrom.G, styles.GradientFrom.B)
	}
	if styles.GradientTo == nil {
		t.Error("GradientTo is nil")
	} else {
		t.Logf("GradientTo: R=%d G=%d B=%d", styles.GradientTo.R, styles.GradientTo.G, styles.GradientTo.B)
	}
	if styles.CornerRadius == nil {
		t.Error("CornerRadius is nil")
	} else {
		t.Logf("CornerRadius: %v", *styles.CornerRadius)
	}
}

func TestDeleteDialogHTML(t *testing.T) {
	// Test the delete dialog pattern from the plan
	html := `
<div class="flex flex-col gap-5 p-6 bg-card rounded-lg">
    <h3 class="text-xl text-primary">Delete Bundle?</h3>
    <p class="text-secondary text-sm">This action cannot be undone.</p>
    <div class="flex flex-row gap-3 justify-end">
        <button data-onclick="cancel" class="bg-btn text-primary px-5 py-3 rounded-md">
            Cancel
        </button>
        <button data-onclick="delete" class="bg-danger text-dark px-5 py-3 rounded-md">
            Delete
        </button>
    </div>
</div>
`
	doc, err := Parse(html)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	cancelCalled := false
	deleteCalled := false

	doc.SetHandler("cancel", func() { cancelCalled = true })
	doc.SetHandler("delete", func() { deleteCalled = true })

	widget := doc.Render(nil)
	if widget == nil {
		t.Fatal("widget is nil")
	}

	// The widget should be a Card (because of rounded-lg)
	card, ok := widget.(*ui.Card)
	if !ok {
		t.Fatalf("expected *ui.Card, got %T", widget)
	}
	_ = card

	// Verify handlers were registered
	if doc.handlers["cancel"] == nil {
		t.Error("cancel handler not registered")
	}
	if doc.handlers["delete"] == nil {
		t.Error("delete handler not registered")
	}

	// Test handlers work (would need to traverse widget tree to find buttons)
	_ = cancelCalled
	_ = deleteCalled
}

func TestParseMarkdown(t *testing.T) {
	md := `# Test Heading

This is a paragraph.

## Code Example

` + "```" + `python
print("hello")
` + "```" + `

- Item 1
- Item 2

| Header | Value |
|--------|-------|
| A      | 1     |
`
	doc, err := ParseMarkdown(md)
	if err != nil {
		t.Fatalf("ParseMarkdown failed: %v", err)
	}
	if doc == nil {
		t.Fatal("doc is nil")
	}
	if doc.root == nil {
		t.Fatal("root is nil")
	}

	// Verify the root is a div wrapper
	if doc.root.tag != "div" {
		t.Errorf("expected root tag 'div', got '%s'", doc.root.tag)
	}

	// The parsed content should have children (h1, p, h2, pre, ul, table)
	if len(doc.root.children) < 4 {
		t.Errorf("expected at least 4 children, got %d", len(doc.root.children))
	}

	// Render to verify no panics
	widget := doc.Render(nil)
	if widget == nil {
		t.Fatal("rendered widget is nil")
	}
}

func TestParseMarkdownTable(t *testing.T) {
	md := `| Command | Description |
|---------|-------------|
| ls      | List files  |
| cd      | Change dir  |
`
	doc, err := ParseMarkdown(md)
	if err != nil {
		t.Fatalf("ParseMarkdown failed: %v", err)
	}

	// Render to verify table parsing works
	widget := doc.Render(nil)
	if widget == nil {
		t.Fatal("rendered widget is nil")
	}
}

func TestParseMarkdownCodeBlock(t *testing.T) {
	md := "```bash\necho hello\n```"
	doc, err := ParseMarkdown(md)
	if err != nil {
		t.Fatalf("ParseMarkdown failed: %v", err)
	}

	widget := doc.Render(nil)
	if widget == nil {
		t.Fatal("rendered widget is nil")
	}
}

func TestParseMarkdownLists(t *testing.T) {
	md := `- Item 1
- Item 2

1. First
2. Second
`
	doc, err := ParseMarkdown(md)
	if err != nil {
		t.Fatalf("ParseMarkdown failed: %v", err)
	}

	widget := doc.Render(nil)
	if widget == nil {
		t.Fatal("rendered widget is nil")
	}
}
