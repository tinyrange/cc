package term

import (
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/graphics"
)

// BackgroundBuffer manages a persistent vertex buffer for terminal cell backgrounds.
// It supports efficient partial updates for dirty cells, reducing GPU draw calls
// from O(cells) to O(1).
type BackgroundBuffer struct {
	mesh     graphics.DynamicMesh
	vertices []graphics.Vertex
	indices  []uint32
	cols     int
	rows     int
	win      graphics.Window
	tex      graphics.Texture

	// Cached layout parameters.
	cellW, cellH     float32
	originX, originY float32
	padX, padY       float32

	// Track if indices have been uploaded.
	indicesUploaded bool

	// Track if a full rebuild is needed (after resize or initial creation).
	needsFullRebuild bool
}

// NewBackgroundBuffer creates a new background buffer for the given grid dimensions.
func NewBackgroundBuffer(win graphics.Window, tex graphics.Texture, cols, rows int) (*BackgroundBuffer, error) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}

	cellCount := cols * rows
	vertexCount := cellCount * 6 // 6 vertices per cell (2 triangles)
	indexCount := cellCount * 6  // 6 indices per cell

	mesh, err := win.NewDynamicMesh(vertexCount, indexCount, tex)
	if err != nil {
		return nil, err
	}

	b := &BackgroundBuffer{
		mesh:             mesh,
		vertices:         make([]graphics.Vertex, vertexCount),
		indices:          make([]uint32, indexCount),
		cols:             cols,
		rows:             rows,
		win:              win,
		tex:              tex,
		needsFullRebuild: true,
	}

	// Pre-compute indices (static pattern for all cells).
	b.buildIndices()

	return b, nil
}

// buildIndices generates the index buffer for all cells.
// Each cell uses 6 vertices forming 2 triangles.
func (b *BackgroundBuffer) buildIndices() {
	for i := 0; i < b.cols*b.rows; i++ {
		base := uint32(i * 6)
		off := i * 6
		// Triangle 1: 0, 1, 2
		b.indices[off+0] = base + 0
		b.indices[off+1] = base + 1
		b.indices[off+2] = base + 2
		// Triangle 2: 3, 4, 5
		b.indices[off+3] = base + 3
		b.indices[off+4] = base + 4
		b.indices[off+5] = base + 5
	}
	b.indicesUploaded = false
}

// SetLayout updates the layout parameters used to calculate vertex positions.
func (b *BackgroundBuffer) SetLayout(originX, originY, padX, padY, cellW, cellH float32) {
	b.originX = originX
	b.originY = originY
	b.padX = padX
	b.padY = padY
	b.cellW = cellW
	b.cellH = cellH
}

// NeedsFullRebuild returns true if a full buffer rebuild is required.
func (b *BackgroundBuffer) NeedsFullRebuild() bool {
	return b.needsFullRebuild
}

// ClearFullRebuild clears the full rebuild flag after a rebuild.
func (b *BackgroundBuffer) ClearFullRebuild() {
	b.needsFullRebuild = false
}

// Resize changes the buffer capacity to match new grid dimensions.
func (b *BackgroundBuffer) Resize(cols, rows int) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}

	if cols == b.cols && rows == b.rows {
		return
	}

	cellCount := cols * rows
	vertexCount := cellCount * 6
	indexCount := cellCount * 6

	// Resize GPU buffers.
	b.mesh.Resize(vertexCount, indexCount)

	// Resize CPU-side arrays.
	b.vertices = make([]graphics.Vertex, vertexCount)
	b.indices = make([]uint32, indexCount)
	b.cols = cols
	b.rows = rows

	// Rebuild indices for new size.
	b.buildIndices()

	// Mark for full rebuild since GPU buffers were recreated empty.
	b.needsFullRebuild = true
}

// UpdateCell updates the vertices for a single cell.
func (b *BackgroundBuffer) UpdateCell(x, y, width int, bg color.Color) {
	if x < 0 || x >= b.cols || y < 0 || y >= b.rows {
		return
	}

	x0 := b.originX + b.padX + float32(x)*b.cellW
	y0 := b.originY + b.padY + float32(y)*b.cellH
	w := float32(width) * b.cellW
	h := b.cellH

	rgba := graphics.ColorToFloat32(bg)

	// Calculate vertex buffer offset for this cell.
	base := (y*b.cols + x) * 6

	// Triangle 1: top-left, top-right, bottom-left
	b.vertices[base+0] = graphics.Vertex{X: x0, Y: y0, U: 0, V: 0, R: rgba[0], G: rgba[1], B: rgba[2], A: rgba[3]}
	b.vertices[base+1] = graphics.Vertex{X: x0 + w, Y: y0, U: 1, V: 0, R: rgba[0], G: rgba[1], B: rgba[2], A: rgba[3]}
	b.vertices[base+2] = graphics.Vertex{X: x0, Y: y0 + h, U: 0, V: 1, R: rgba[0], G: rgba[1], B: rgba[2], A: rgba[3]}
	// Triangle 2: top-right, bottom-right, bottom-left
	b.vertices[base+3] = graphics.Vertex{X: x0 + w, Y: y0, U: 1, V: 0, R: rgba[0], G: rgba[1], B: rgba[2], A: rgba[3]}
	b.vertices[base+4] = graphics.Vertex{X: x0 + w, Y: y0 + h, U: 1, V: 1, R: rgba[0], G: rgba[1], B: rgba[2], A: rgba[3]}
	b.vertices[base+5] = graphics.Vertex{X: x0, Y: y0 + h, U: 0, V: 1, R: rgba[0], G: rgba[1], B: rgba[2], A: rgba[3]}
}

// UpdateCellTransparent sets a cell to fully transparent (alpha=0).
// Used for cells with the default background color.
func (b *BackgroundBuffer) UpdateCellTransparent(x, y, width int) {
	if x < 0 || x >= b.cols || y < 0 || y >= b.rows {
		return
	}

	x0 := b.originX + b.padX + float32(x)*b.cellW
	y0 := b.originY + b.padY + float32(y)*b.cellH
	w := float32(width) * b.cellW
	h := b.cellH

	base := (y*b.cols + x) * 6

	// Set all vertices with alpha=0 (transparent).
	b.vertices[base+0] = graphics.Vertex{X: x0, Y: y0, U: 0, V: 0, R: 0, G: 0, B: 0, A: 0}
	b.vertices[base+1] = graphics.Vertex{X: x0 + w, Y: y0, U: 1, V: 0, R: 0, G: 0, B: 0, A: 0}
	b.vertices[base+2] = graphics.Vertex{X: x0, Y: y0 + h, U: 0, V: 1, R: 0, G: 0, B: 0, A: 0}
	b.vertices[base+3] = graphics.Vertex{X: x0 + w, Y: y0, U: 1, V: 0, R: 0, G: 0, B: 0, A: 0}
	b.vertices[base+4] = graphics.Vertex{X: x0 + w, Y: y0 + h, U: 1, V: 1, R: 0, G: 0, B: 0, A: 0}
	b.vertices[base+5] = graphics.Vertex{X: x0, Y: y0 + h, U: 0, V: 1, R: 0, G: 0, B: 0, A: 0}
}

// UpdateDirty updates vertices for all dirty cells in the grid.
func (b *BackgroundBuffer) UpdateDirty(grid *Grid, bgDefault color.Color) {
	// Track ranges of dirty vertices for efficient upload.
	minDirtyIdx := -1
	maxDirtyIdx := -1

	grid.IterateDirty(func(x, y int, cell *Cell) {
		width := max(cell.Width, 1)

		if cell.Bg == nil || colorEquals(cell.Bg, bgDefault) {
			// Cell has default background - make transparent.
			b.UpdateCellTransparent(x, y, width)
		} else {
			// Cell has non-default background.
			b.UpdateCell(x, y, width, cell.Bg)
		}

		// Track dirty range.
		base := (y*b.cols + x) * 6
		if minDirtyIdx < 0 || base < minDirtyIdx {
			minDirtyIdx = base
		}
		endIdx := base + 6
		if endIdx > maxDirtyIdx {
			maxDirtyIdx = endIdx
		}
	})

	// Upload dirty vertex range to GPU.
	if minDirtyIdx >= 0 && maxDirtyIdx > minDirtyIdx {
		b.mesh.UpdateVertices(minDirtyIdx, b.vertices[minDirtyIdx:maxDirtyIdx])
	}
}

// UpdateAll rebuilds all vertices and uploads to GPU.
// Call this after SetLayout or Resize.
func (b *BackgroundBuffer) UpdateAll(grid *Grid, bgDefault color.Color) {
	grid.IterateAll(func(x, y int, cell *Cell) {
		width := max(cell.Width, 1)

		if cell.Bg == nil || colorEquals(cell.Bg, bgDefault) {
			b.UpdateCellTransparent(x, y, width)
		} else {
			b.UpdateCell(x, y, width, cell.Bg)
		}
	})

	// Upload all vertices.
	b.mesh.UpdateAllVertices(b.vertices)

	// Upload indices if not done yet.
	if !b.indicesUploaded {
		b.mesh.UpdateIndices(b.indices)
		b.indicesUploaded = true
	}
}

// Render draws all backgrounds in a single draw call.
func (b *BackgroundBuffer) Render(f graphics.Frame) {
	// Upload indices on first render if not already done.
	if !b.indicesUploaded {
		b.mesh.UpdateIndices(b.indices)
		b.indicesUploaded = true
	}

	f.RenderMesh(b.mesh, graphics.DrawOptions{})
}
