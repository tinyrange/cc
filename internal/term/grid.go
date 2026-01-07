package term

import (
	"image/color"
)

// Cell represents a single terminal cell with cached state for dirty tracking.
type Cell struct {
	Content string
	Width   int
	Fg      color.Color
	Bg      color.Color
	Attrs   uint8
}

// equals compares two cells for equality to detect changes.
// We compare content, width, attrs, and color values.
func (c *Cell) equals(other *Cell) bool {
	if c.Content != other.Content || c.Width != other.Width || c.Attrs != other.Attrs {
		return false
	}
	return colorEquals(c.Fg, other.Fg) && colorEquals(c.Bg, other.Bg)
}

// colorEquals compares two colors for equality.
func colorEquals(a, b color.Color) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

// Grid manages a grid of terminal cells with dirty tracking for incremental updates.
type Grid struct {
	cells []Cell
	dirty []bool
	cols  int
	rows  int

	// Track cursor for dirty marking.
	cursorX, cursorY int

	// Statistics for benchmarking/debugging.
	stats GridStats
}

// GridStats tracks grid update statistics.
type GridStats struct {
	TotalCells  int
	DirtyCells  int
	SyncCalls   int
	FullRedraws int
}

// NewGrid creates a new grid with the given dimensions.
func NewGrid(cols, rows int) *Grid {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	size := cols * rows
	return &Grid{
		cells:   make([]Cell, size),
		dirty:   make([]bool, size),
		cols:    cols,
		rows:    rows,
		cursorX: -1,
		cursorY: -1,
	}
}

// Size returns the grid dimensions.
func (g *Grid) Size() (cols, rows int) {
	return g.cols, g.rows
}

// Resize changes the grid dimensions, preserving content where possible.
func (g *Grid) Resize(cols, rows int) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if cols == g.cols && rows == g.rows {
		return
	}

	newSize := cols * rows
	newCells := make([]Cell, newSize)
	newDirty := make([]bool, newSize)

	// Copy existing content where it overlaps.
	minCols := g.cols
	if cols < minCols {
		minCols = cols
	}
	minRows := g.rows
	if rows < minRows {
		minRows = rows
	}

	for y := 0; y < minRows; y++ {
		for x := 0; x < minCols; x++ {
			oldIdx := y*g.cols + x
			newIdx := y*cols + x
			newCells[newIdx] = g.cells[oldIdx]
			// Mark as dirty since geometry positions change on resize.
			newDirty[newIdx] = true
		}
	}

	// Mark all new cells as dirty.
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			if y >= minRows || x >= minCols {
				newDirty[y*cols+x] = true
			}
		}
	}

	g.cells = newCells
	g.dirty = newDirty
	g.cols = cols
	g.rows = rows
	g.stats.FullRedraws++
}

// CellAt returns a pointer to the cell at (x, y), or nil if out of bounds.
func (g *Grid) CellAt(x, y int) *Cell {
	if x < 0 || x >= g.cols || y < 0 || y >= g.rows {
		return nil
	}
	return &g.cells[y*g.cols+x]
}

// SetCell updates a cell and marks it dirty if it changed.
// Returns true if the cell was actually modified.
func (g *Grid) SetCell(x, y int, content string, width int, fg, bg color.Color, attrs uint8) bool {
	if x < 0 || x >= g.cols || y < 0 || y >= g.rows {
		return false
	}
	idx := y*g.cols + x

	newCell := Cell{
		Content: content,
		Width:   width,
		Fg:      fg,
		Bg:      bg,
		Attrs:   attrs,
	}

	if g.cells[idx].equals(&newCell) {
		return false
	}

	g.cells[idx] = newCell
	g.dirty[idx] = true
	return true
}

// IsDirty returns true if the cell at (x, y) needs re-rendering.
func (g *Grid) IsDirty(x, y int) bool {
	if x < 0 || x >= g.cols || y < 0 || y >= g.rows {
		return false
	}
	return g.dirty[y*g.cols+x]
}

// MarkDirty marks a cell as needing re-rendering.
func (g *Grid) MarkDirty(x, y int) {
	if x < 0 || x >= g.cols || y < 0 || y >= g.rows {
		return
	}
	g.dirty[y*g.cols+x] = true
}

// MarkAllDirty marks all cells as dirty (for full redraw).
func (g *Grid) MarkAllDirty() {
	for i := range g.dirty {
		g.dirty[i] = true
	}
	g.stats.FullRedraws++
}

// ClearDirty clears all dirty flags.
func (g *Grid) ClearDirty() {
	for i := range g.dirty {
		g.dirty[i] = false
	}
}

// DirtyCount returns the number of dirty cells.
func (g *Grid) DirtyCount() int {
	count := 0
	for _, d := range g.dirty {
		if d {
			count++
		}
	}
	return count
}

// UpdateCursor marks old and new cursor positions as dirty.
func (g *Grid) UpdateCursor(newX, newY int) {
	// Mark old cursor position dirty.
	if g.cursorX >= 0 && g.cursorY >= 0 {
		g.MarkDirty(g.cursorX, g.cursorY)
	}
	// Mark new cursor position dirty.
	if newX >= 0 && newY >= 0 {
		g.MarkDirty(newX, newY)
	}
	g.cursorX = newX
	g.cursorY = newY
}

// CursorPosition returns the current cursor position.
func (g *Grid) CursorPosition() (x, y int) {
	return g.cursorX, g.cursorY
}

// Stats returns current grid statistics.
func (g *Grid) Stats() GridStats {
	g.stats.TotalCells = g.cols * g.rows
	g.stats.DirtyCells = g.DirtyCount()
	return g.stats
}

// ResetStats resets the statistics counters.
func (g *Grid) ResetStats() {
	g.stats = GridStats{}
}

// IterateDirty calls fn for each dirty cell with its coordinates.
// This is useful for incremental rendering.
func (g *Grid) IterateDirty(fn func(x, y int, cell *Cell)) {
	for y := 0; y < g.rows; y++ {
		for x := 0; x < g.cols; x++ {
			idx := y*g.cols + x
			if g.dirty[idx] {
				fn(x, y, &g.cells[idx])
			}
		}
	}
}

// IterateAll calls fn for each cell regardless of dirty state.
func (g *Grid) IterateAll(fn func(x, y int, cell *Cell)) {
	for y := 0; y < g.rows; y++ {
		for x := 0; x < g.cols; x++ {
			fn(x, y, &g.cells[y*g.cols+x])
		}
	}
}

// DirtyRegion represents a rectangular region of dirty cells.
type DirtyRegion struct {
	X, Y          int
	Width, Height int
}

// GetDirtyRegions returns a list of dirty regions for batch processing.
// Adjacent dirty cells are merged into larger regions to reduce draw calls.
func (g *Grid) GetDirtyRegions() []DirtyRegion {
	if g.DirtyCount() == 0 {
		return nil
	}

	// Simple implementation: return individual dirty cells as regions.
	// For more optimization, this could merge adjacent dirty cells.
	var regions []DirtyRegion
	for y := 0; y < g.rows; y++ {
		x := 0
		for x < g.cols {
			idx := y*g.cols + x
			if !g.dirty[idx] {
				x++
				continue
			}

			// Found a dirty cell, scan for adjacent dirty cells in this row.
			startX := x
			for x < g.cols && g.dirty[y*g.cols+x] {
				x++
			}

			regions = append(regions, DirtyRegion{
				X:      startX,
				Y:      y,
				Width:  x - startX,
				Height: 1,
			})
		}
	}

	return regions
}

// EmulatorReader provides read access to VT emulator cell data.
type EmulatorReader interface {
	Width() int
	Height() int
	CellAt(x, y int) interface {
		GetContent() string
		GetWidth() int
		GetFg() color.Color
		GetBg() color.Color
		GetAttrs() uint8
	}
	CursorPosition() struct{ X, Y int }
}

// VTCell wraps the charmbracelet/vt Cell type for our interface.
type VTCell struct {
	Content string
	Width   int
	Style   struct {
		Fg    color.Color
		Bg    color.Color
		Attrs uint8
	}
}

func (c *VTCell) GetContent() string { return c.Content }
func (c *VTCell) GetWidth() int      { return c.Width }
func (c *VTCell) GetFg() color.Color { return c.Style.Fg }
func (c *VTCell) GetBg() color.Color { return c.Style.Bg }
func (c *VTCell) GetAttrs() uint8    { return uint8(c.Style.Attrs) }
