package term

import (
	"fmt"
	"image/color"
	"math/rand"
	"testing"
)

// Benchmark grid sizes to test.
var benchGridSizes = []struct {
	name string
	cols int
	rows int
}{
	{"Small_40x20", 40, 20},
	{"Medium_80x40", 80, 40},
	{"Large_120x60", 120, 60},
	{"XLarge_200x100", 200, 100},
}

// BenchmarkNewGrid measures grid creation time.
func BenchmarkNewGrid(b *testing.B) {
	for _, size := range benchGridSizes {
		b.Run(size.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = NewGrid(size.cols, size.rows)
			}
		})
	}
}

// BenchmarkCellAt measures cell read access time.
func BenchmarkCellAt(b *testing.B) {
	for _, size := range benchGridSizes {
		b.Run(size.name, func(b *testing.B) {
			g := NewGrid(size.cols, size.rows)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				x := i % size.cols
				y := (i / size.cols) % size.rows
				_ = g.CellAt(x, y)
			}
		})
	}
}

// BenchmarkSetCell measures cell write time.
func BenchmarkSetCell(b *testing.B) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	black := color.RGBA{R: 0, G: 0, B: 0, A: 255}

	for _, size := range benchGridSizes {
		b.Run(size.name, func(b *testing.B) {
			g := NewGrid(size.cols, size.rows)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				x := i % size.cols
				y := (i / size.cols) % size.rows
				content := string(rune('A' + (i % 26)))
				g.SetCell(x, y, content, 1, white, black, 0)
			}
		})
	}
}

// BenchmarkSetCellNoChange measures time when cell doesn't change.
func BenchmarkSetCellNoChange(b *testing.B) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	black := color.RGBA{R: 0, G: 0, B: 0, A: 255}

	for _, size := range benchGridSizes {
		b.Run(size.name, func(b *testing.B) {
			g := NewGrid(size.cols, size.rows)
			// Pre-fill with known content.
			for y := 0; y < size.rows; y++ {
				for x := 0; x < size.cols; x++ {
					g.SetCell(x, y, "A", 1, white, black, 0)
				}
			}
			g.ClearDirty()
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				x := i % size.cols
				y := (i / size.cols) % size.rows
				// Same content - should detect no change.
				g.SetCell(x, y, "A", 1, white, black, 0)
			}
		})
	}
}

// BenchmarkIsDirty measures dirty flag check time.
func BenchmarkIsDirty(b *testing.B) {
	for _, size := range benchGridSizes {
		b.Run(size.name, func(b *testing.B) {
			g := NewGrid(size.cols, size.rows)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				x := i % size.cols
				y := (i / size.cols) % size.rows
				_ = g.IsDirty(x, y)
			}
		})
	}
}

// BenchmarkMarkDirty measures marking a cell dirty.
func BenchmarkMarkDirty(b *testing.B) {
	for _, size := range benchGridSizes {
		b.Run(size.name, func(b *testing.B) {
			g := NewGrid(size.cols, size.rows)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				x := i % size.cols
				y := (i / size.cols) % size.rows
				g.MarkDirty(x, y)
			}
		})
	}
}

// BenchmarkClearDirty measures clearing all dirty flags.
func BenchmarkClearDirty(b *testing.B) {
	for _, size := range benchGridSizes {
		b.Run(size.name, func(b *testing.B) {
			g := NewGrid(size.cols, size.rows)
			g.MarkAllDirty()
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				g.ClearDirty()
			}
		})
	}
}

// BenchmarkMarkAllDirty measures marking all cells dirty.
func BenchmarkMarkAllDirty(b *testing.B) {
	for _, size := range benchGridSizes {
		b.Run(size.name, func(b *testing.B) {
			g := NewGrid(size.cols, size.rows)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				g.MarkAllDirty()
			}
		})
	}
}

// BenchmarkDirtyCount measures counting dirty cells.
func BenchmarkDirtyCount(b *testing.B) {
	dirtyPercentages := []int{0, 10, 50, 100}

	for _, size := range benchGridSizes {
		for _, pct := range dirtyPercentages {
			name := fmt.Sprintf("%s_%dpct_dirty", size.name, pct)
			b.Run(name, func(b *testing.B) {
				g := NewGrid(size.cols, size.rows)
				numDirty := (size.cols * size.rows * pct) / 100
				for i := 0; i < numDirty; i++ {
					g.MarkDirty(i%size.cols, i/size.cols)
				}
				b.ReportAllocs()
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					_ = g.DirtyCount()
				}
			})
		}
	}
}

// BenchmarkIterateAll measures full grid iteration.
func BenchmarkIterateAll(b *testing.B) {
	for _, size := range benchGridSizes {
		b.Run(size.name, func(b *testing.B) {
			g := NewGrid(size.cols, size.rows)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				g.IterateAll(func(x, y int, cell *Cell) {
					// Simulate minimal work.
					_ = cell.Content
				})
			}
		})
	}
}

// BenchmarkIterateDirty measures dirty-only iteration at various dirty levels.
func BenchmarkIterateDirty(b *testing.B) {
	dirtyPercentages := []int{1, 5, 10, 25, 50, 100}

	for _, size := range benchGridSizes {
		for _, pct := range dirtyPercentages {
			name := fmt.Sprintf("%s_%dpct_dirty", size.name, pct)
			b.Run(name, func(b *testing.B) {
				g := NewGrid(size.cols, size.rows)
				totalCells := size.cols * size.rows
				numDirty := (totalCells * pct) / 100
				if numDirty == 0 {
					numDirty = 1
				}

				// Spread dirty cells randomly.
				rng := rand.New(rand.NewSource(42))
				indices := rng.Perm(totalCells)[:numDirty]
				for _, idx := range indices {
					g.MarkDirty(idx%size.cols, idx/size.cols)
				}

				b.ReportAllocs()
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					g.IterateDirty(func(x, y int, cell *Cell) {
						_ = cell.Content
					})
				}
			})
		}
	}
}

// BenchmarkGetDirtyRegions measures dirty region detection.
func BenchmarkGetDirtyRegions(b *testing.B) {
	dirtyPatterns := []struct {
		name  string
		setup func(g *Grid, cols, rows int)
	}{
		{"sparse_random", func(g *Grid, cols, rows int) {
			rng := rand.New(rand.NewSource(42))
			for i := 0; i < (cols*rows)/10; i++ {
				g.MarkDirty(rng.Intn(cols), rng.Intn(rows))
			}
		}},
		{"one_row", func(g *Grid, cols, rows int) {
			for x := 0; x < cols; x++ {
				g.MarkDirty(x, rows/2)
			}
		}},
		{"scattered_blocks", func(g *Grid, cols, rows int) {
			// 4x4 blocks in corners.
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 4; dx++ {
					g.MarkDirty(dx, dy)
					g.MarkDirty(cols-1-dx, dy)
					g.MarkDirty(dx, rows-1-dy)
					g.MarkDirty(cols-1-dx, rows-1-dy)
				}
			}
		}},
		{"full_grid", func(g *Grid, cols, rows int) {
			g.MarkAllDirty()
		}},
	}

	for _, size := range benchGridSizes {
		for _, pattern := range dirtyPatterns {
			name := fmt.Sprintf("%s/%s", size.name, pattern.name)
			b.Run(name, func(b *testing.B) {
				g := NewGrid(size.cols, size.rows)
				pattern.setup(g, size.cols, size.rows)
				b.ReportAllocs()
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					_ = g.GetDirtyRegions()
				}
			})
		}
	}
}

// BenchmarkResize measures grid resize time.
func BenchmarkResize(b *testing.B) {
	resizeOps := []struct {
		name     string
		fromCols int
		fromRows int
		toCols   int
		toRows   int
	}{
		{"grow_small", 40, 20, 80, 40},
		{"grow_medium", 80, 40, 120, 60},
		{"shrink_small", 80, 40, 40, 20},
		{"same_size", 80, 40, 80, 40},
	}

	for _, op := range resizeOps {
		b.Run(op.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				g := NewGrid(op.fromCols, op.fromRows)
				g.Resize(op.toCols, op.toRows)
			}
		})
	}
}

// BenchmarkUpdateCursor measures cursor update performance.
func BenchmarkUpdateCursor(b *testing.B) {
	for _, size := range benchGridSizes {
		b.Run(size.name, func(b *testing.B) {
			g := NewGrid(size.cols, size.rows)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				x := i % size.cols
				y := (i / size.cols) % size.rows
				g.UpdateCursor(x, y)
			}
		})
	}
}

// BenchmarkFullFrameSync simulates a complete frame sync operation.
func BenchmarkFullFrameSync(b *testing.B) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	black := color.RGBA{R: 0, G: 0, B: 0, A: 255}

	changeRates := []int{0, 1, 5, 10, 25, 50, 100}

	for _, size := range benchGridSizes {
		for _, changeRate := range changeRates {
			name := fmt.Sprintf("%s/%dpct_change", size.name, changeRate)
			b.Run(name, func(b *testing.B) {
				g := NewGrid(size.cols, size.rows)

				// Fill with initial content.
				for y := 0; y < size.rows; y++ {
					for x := 0; x < size.cols; x++ {
						g.SetCell(x, y, "X", 1, white, black, 0)
					}
				}
				g.ClearDirty()

				totalCells := size.cols * size.rows
				numChanges := (totalCells * changeRate) / 100

				b.ReportAllocs()
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					// Simulate frame sync: update some cells.
					for c := 0; c < numChanges; c++ {
						idx := (i*numChanges + c) % totalCells
						x := idx % size.cols
						y := idx / size.cols
						content := string(rune('A' + (c % 26)))
						g.SetCell(x, y, content, 1, white, black, 0)
					}

					// Update cursor.
					g.UpdateCursor(i%size.cols, (i/size.cols)%size.rows)

					// Clear dirty for next frame.
					g.ClearDirty()
				}
			})
		}
	}
}

// BenchmarkColorEquals measures color comparison.
func BenchmarkColorEquals(b *testing.B) {
	white1 := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	white2 := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	black := color.RGBA{R: 0, G: 0, B: 0, A: 255}

	b.Run("same", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = colorEquals(white1, white2)
		}
	})

	b.Run("different", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = colorEquals(white1, black)
		}
	})

	b.Run("nil_nil", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = colorEquals(nil, nil)
		}
	})
}

// BenchmarkCellEquals measures cell comparison.
func BenchmarkCellEquals(b *testing.B) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	black := color.RGBA{R: 0, G: 0, B: 0, A: 255}

	cell1 := &Cell{Content: "A", Width: 1, Fg: white, Bg: black, Attrs: 0}
	cell2 := &Cell{Content: "A", Width: 1, Fg: white, Bg: black, Attrs: 0}
	cell3 := &Cell{Content: "B", Width: 1, Fg: white, Bg: black, Attrs: 0}

	b.Run("equal", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = cell1.equals(cell2)
		}
	})

	b.Run("different_early_exit", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = cell1.equals(cell3)
		}
	})
}
