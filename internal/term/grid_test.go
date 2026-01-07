package term

import (
	"image/color"
	"testing"
)

// Test colors for use in tests.
var (
	testWhite = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	testBlack = color.RGBA{R: 0, G: 0, B: 0, A: 255}
	testRed   = color.RGBA{R: 255, G: 0, B: 0, A: 255}
	testGreen = color.RGBA{R: 0, G: 255, B: 0, A: 255}
	testBlue  = color.RGBA{R: 0, G: 0, B: 255, A: 255}
)

func TestNewGrid(t *testing.T) {
	tests := []struct {
		name     string
		cols     int
		rows     int
		wantCols int
		wantRows int
	}{
		{"normal", 80, 40, 80, 40},
		{"small", 10, 5, 10, 5},
		{"zero cols", 0, 40, 1, 40},
		{"zero rows", 80, 0, 80, 1},
		{"negative", -5, -10, 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGrid(tt.cols, tt.rows)
			cols, rows := g.Size()
			if cols != tt.wantCols || rows != tt.wantRows {
				t.Errorf("NewGrid(%d, %d).Size() = (%d, %d), want (%d, %d)",
					tt.cols, tt.rows, cols, rows, tt.wantCols, tt.wantRows)
			}
		})
	}
}

func TestGridCellAt(t *testing.T) {
	g := NewGrid(10, 10)

	// Valid cell access.
	cell := g.CellAt(5, 5)
	if cell == nil {
		t.Error("CellAt(5, 5) returned nil for valid coordinates")
	}

	// Out of bounds access.
	outOfBoundsCases := []struct {
		x, y int
	}{
		{-1, 5},
		{5, -1},
		{10, 5},
		{5, 10},
		{100, 100},
	}

	for _, tc := range outOfBoundsCases {
		if g.CellAt(tc.x, tc.y) != nil {
			t.Errorf("CellAt(%d, %d) should return nil for out of bounds", tc.x, tc.y)
		}
	}
}

func TestGridSetCell(t *testing.T) {
	g := NewGrid(10, 10)

	// Set a cell.
	changed := g.SetCell(5, 5, "A", 1, testWhite, testBlack, 0)
	if !changed {
		t.Error("SetCell should return true for first change")
	}

	// Verify cell content.
	cell := g.CellAt(5, 5)
	if cell == nil {
		t.Fatal("CellAt returned nil after SetCell")
	}
	if cell.Content != "A" {
		t.Errorf("cell.Content = %q, want %q", cell.Content, "A")
	}
	if cell.Width != 1 {
		t.Errorf("cell.Width = %d, want %d", cell.Width, 1)
	}

	// Set same value - should return false.
	changed = g.SetCell(5, 5, "A", 1, testWhite, testBlack, 0)
	if changed {
		t.Error("SetCell should return false for identical value")
	}

	// Set different value - should return true.
	changed = g.SetCell(5, 5, "B", 1, testWhite, testBlack, 0)
	if !changed {
		t.Error("SetCell should return true for different value")
	}

	// Set out of bounds - should return false.
	changed = g.SetCell(-1, 5, "X", 1, testWhite, testBlack, 0)
	if changed {
		t.Error("SetCell should return false for out of bounds coordinates")
	}
}

func TestGridDirtyTracking(t *testing.T) {
	g := NewGrid(10, 10)

	// Initially no dirty cells.
	if count := g.DirtyCount(); count != 0 {
		t.Errorf("DirtyCount() = %d, want 0 for new grid", count)
	}

	// Set a cell - should become dirty.
	g.SetCell(5, 5, "A", 1, testWhite, testBlack, 0)
	if !g.IsDirty(5, 5) {
		t.Error("Cell should be dirty after SetCell")
	}
	if count := g.DirtyCount(); count != 1 {
		t.Errorf("DirtyCount() = %d, want 1", count)
	}

	// Clear dirty flags.
	g.ClearDirty()
	if g.IsDirty(5, 5) {
		t.Error("Cell should not be dirty after ClearDirty")
	}
	if count := g.DirtyCount(); count != 0 {
		t.Errorf("DirtyCount() = %d, want 0 after ClearDirty", count)
	}

	// Mark all dirty.
	g.MarkAllDirty()
	expectedDirty := 10 * 10
	if count := g.DirtyCount(); count != expectedDirty {
		t.Errorf("DirtyCount() = %d, want %d after MarkAllDirty", count, expectedDirty)
	}
}

func TestGridMarkDirty(t *testing.T) {
	g := NewGrid(10, 10)

	// Mark a specific cell dirty.
	g.MarkDirty(3, 4)
	if !g.IsDirty(3, 4) {
		t.Error("Cell should be dirty after MarkDirty")
	}

	// Mark out of bounds - should not panic.
	g.MarkDirty(-1, 5)
	g.MarkDirty(5, -1)
	g.MarkDirty(100, 100)
}

func TestGridResize(t *testing.T) {
	g := NewGrid(10, 10)

	// Set some content.
	g.SetCell(0, 0, "A", 1, testWhite, testBlack, 0)
	g.SetCell(5, 5, "B", 1, testWhite, testBlack, 0)
	g.SetCell(9, 9, "C", 1, testWhite, testBlack, 0)
	g.ClearDirty()

	// Resize larger.
	g.Resize(20, 20)
	cols, rows := g.Size()
	if cols != 20 || rows != 20 {
		t.Errorf("Size() = (%d, %d), want (20, 20)", cols, rows)
	}

	// Content should be preserved.
	if cell := g.CellAt(0, 0); cell == nil || cell.Content != "A" {
		t.Error("Content at (0, 0) should be preserved after resize")
	}
	if cell := g.CellAt(5, 5); cell == nil || cell.Content != "B" {
		t.Error("Content at (5, 5) should be preserved after resize")
	}

	// Old cells should be dirty (positions changed).
	if !g.IsDirty(0, 0) {
		t.Error("Preserved cells should be marked dirty after resize")
	}

	// New cells should also be dirty.
	if !g.IsDirty(15, 15) {
		t.Error("New cells should be marked dirty after resize")
	}

	// Resize smaller.
	g.ClearDirty()
	g.Resize(5, 5)
	cols, rows = g.Size()
	if cols != 5 || rows != 5 {
		t.Errorf("Size() = (%d, %d), want (5, 5)", cols, rows)
	}

	// Content within bounds should be preserved.
	if cell := g.CellAt(0, 0); cell == nil || cell.Content != "A" {
		t.Error("Content at (0, 0) should be preserved after shrink")
	}

	// Content outside new bounds should be gone.
	if g.CellAt(9, 9) != nil {
		t.Error("CellAt(9, 9) should be nil after shrinking to 5x5")
	}
}

func TestGridCursor(t *testing.T) {
	g := NewGrid(10, 10)

	// Initial cursor position.
	x, y := g.CursorPosition()
	if x != -1 || y != -1 {
		t.Errorf("Initial cursor = (%d, %d), want (-1, -1)", x, y)
	}

	// Update cursor.
	g.UpdateCursor(5, 5)
	x, y = g.CursorPosition()
	if x != 5 || y != 5 {
		t.Errorf("Cursor after update = (%d, %d), want (5, 5)", x, y)
	}

	// New position should be dirty.
	if !g.IsDirty(5, 5) {
		t.Error("New cursor position should be dirty")
	}

	g.ClearDirty()

	// Move cursor - old and new positions should be dirty.
	g.UpdateCursor(3, 3)
	if !g.IsDirty(5, 5) {
		t.Error("Old cursor position should be dirty after move")
	}
	if !g.IsDirty(3, 3) {
		t.Error("New cursor position should be dirty after move")
	}
}

func TestGridIterateDirty(t *testing.T) {
	g := NewGrid(10, 10)

	// Set some dirty cells.
	g.SetCell(1, 1, "A", 1, testWhite, testBlack, 0)
	g.SetCell(2, 2, "B", 1, testWhite, testBlack, 0)
	g.SetCell(3, 3, "C", 1, testWhite, testBlack, 0)

	visited := make(map[string]bool)
	g.IterateDirty(func(x, y int, cell *Cell) {
		key := cell.Content
		visited[key] = true
	})

	for _, c := range []string{"A", "B", "C"} {
		if !visited[c] {
			t.Errorf("IterateDirty should have visited cell with content %q", c)
		}
	}

	if len(visited) != 3 {
		t.Errorf("IterateDirty visited %d cells, want 3", len(visited))
	}
}

func TestGridIterateAll(t *testing.T) {
	g := NewGrid(5, 5)

	count := 0
	g.IterateAll(func(x, y int, cell *Cell) {
		count++
	})

	expected := 5 * 5
	if count != expected {
		t.Errorf("IterateAll visited %d cells, want %d", count, expected)
	}
}

func TestGridGetDirtyRegions(t *testing.T) {
	g := NewGrid(10, 10)

	// No dirty cells.
	regions := g.GetDirtyRegions()
	if len(regions) != 0 {
		t.Errorf("GetDirtyRegions() = %d regions, want 0 for clean grid", len(regions))
	}

	// Set some consecutive dirty cells in a row.
	g.SetCell(2, 3, "A", 1, testWhite, testBlack, 0)
	g.SetCell(3, 3, "B", 1, testWhite, testBlack, 0)
	g.SetCell(4, 3, "C", 1, testWhite, testBlack, 0)

	regions = g.GetDirtyRegions()

	// Should merge into one region.
	if len(regions) != 1 {
		t.Errorf("GetDirtyRegions() = %d regions, want 1 for consecutive cells", len(regions))
	}

	if len(regions) > 0 {
		r := regions[0]
		if r.X != 2 || r.Y != 3 || r.Width != 3 || r.Height != 1 {
			t.Errorf("Region = {X:%d, Y:%d, W:%d, H:%d}, want {X:2, Y:3, W:3, H:1}",
				r.X, r.Y, r.Width, r.Height)
		}
	}

	// Add non-consecutive dirty cells.
	g.SetCell(7, 3, "D", 1, testWhite, testBlack, 0)

	regions = g.GetDirtyRegions()
	if len(regions) != 2 {
		t.Errorf("GetDirtyRegions() = %d regions, want 2 for non-consecutive cells", len(regions))
	}
}

func TestColorEquals(t *testing.T) {
	tests := []struct {
		name string
		a    color.Color
		b    color.Color
		want bool
	}{
		{"both nil", nil, nil, true},
		{"a nil", nil, testWhite, false},
		{"b nil", testWhite, nil, false},
		{"same", testWhite, testWhite, true},
		{"different", testWhite, testBlack, false},
		{"same values different instances",
			color.RGBA{R: 255, G: 255, B: 255, A: 255},
			color.RGBA{R: 255, G: 255, B: 255, A: 255},
			true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := colorEquals(tt.a, tt.b); got != tt.want {
				t.Errorf("colorEquals() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCellEquals(t *testing.T) {
	cell1 := &Cell{Content: "A", Width: 1, Fg: testWhite, Bg: testBlack, Attrs: 0}
	cell2 := &Cell{Content: "A", Width: 1, Fg: testWhite, Bg: testBlack, Attrs: 0}
	cell3 := &Cell{Content: "B", Width: 1, Fg: testWhite, Bg: testBlack, Attrs: 0}
	cell4 := &Cell{Content: "A", Width: 2, Fg: testWhite, Bg: testBlack, Attrs: 0}
	cell5 := &Cell{Content: "A", Width: 1, Fg: testRed, Bg: testBlack, Attrs: 0}

	if !cell1.equals(cell2) {
		t.Error("Identical cells should be equal")
	}
	if cell1.equals(cell3) {
		t.Error("Cells with different content should not be equal")
	}
	if cell1.equals(cell4) {
		t.Error("Cells with different width should not be equal")
	}
	if cell1.equals(cell5) {
		t.Error("Cells with different fg color should not be equal")
	}
}

func TestGridStats(t *testing.T) {
	g := NewGrid(10, 10)

	stats := g.Stats()
	if stats.TotalCells != 100 {
		t.Errorf("TotalCells = %d, want 100", stats.TotalCells)
	}
	if stats.DirtyCells != 0 {
		t.Errorf("DirtyCells = %d, want 0", stats.DirtyCells)
	}

	g.SetCell(0, 0, "A", 1, testWhite, testBlack, 0)
	g.SetCell(1, 1, "B", 1, testWhite, testBlack, 0)

	stats = g.Stats()
	if stats.DirtyCells != 2 {
		t.Errorf("DirtyCells = %d, want 2", stats.DirtyCells)
	}

	g.MarkAllDirty()
	stats = g.Stats()
	if stats.FullRedraws != 1 {
		t.Errorf("FullRedraws = %d, want 1", stats.FullRedraws)
	}

	g.ResetStats()
	stats = g.Stats()
	if stats.FullRedraws != 0 {
		t.Errorf("FullRedraws after reset = %d, want 0", stats.FullRedraws)
	}
}
