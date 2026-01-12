package graphics

import (
	"image/color"
	"math"
)

// CornerRadius specifies the radius for each corner of a rounded rectangle.
type CornerRadius struct {
	TopLeft     float32
	TopRight    float32
	BottomRight float32
	BottomLeft  float32
}

// UniformRadius creates a CornerRadius with the same value for all corners.
func UniformRadius(r float32) CornerRadius {
	return CornerRadius{r, r, r, r}
}

// GradientDirection specifies the direction of a color gradient.
type GradientDirection int

const (
	GradientNone       GradientDirection = iota
	GradientVertical                     // Top to bottom
	GradientHorizontal                   // Left to right
	GradientDiagonalTL                   // Top-left to bottom-right (135deg)
	GradientDiagonalTR                   // Top-right to bottom-left (45deg)
)

// ColorStop defines a color at a specific position in a gradient.
type ColorStop struct {
	Position float32     // 0.0 to 1.0
	Color    color.Color
}

// ShapeStyle defines the visual appearance of a shape.
type ShapeStyle struct {
	// Fill color (used when GradientStops is empty)
	FillColor color.Color

	// Gradient colors (overrides FillColor if set)
	GradientDirection GradientDirection
	GradientStops     []ColorStop
}

// DefaultShapeStyle returns a white solid fill.
func DefaultShapeStyle() ShapeStyle {
	return ShapeStyle{
		FillColor: ColorWhite,
	}
}

// SegmentsForRadius returns appropriate tessellation quality based on radius.
// Larger radii need more segments for smooth curves.
func SegmentsForRadius(radius float32) int {
	// Roughly 1 segment per 4 pixels of arc length, clamped to [4, 24]
	segments := int(math.Ceil(float64(radius) * math.Pi / 8.0))
	if segments < 4 {
		return 4
	}
	if segments > 24 {
		return 24
	}
	return segments
}

// RoundedRectVertexCount returns the number of vertices needed for a rounded rect.
func RoundedRectVertexCount(segments int) int {
	// 4 corners * (segments + 1) perimeter vertices + 1 center vertex
	return 4*(segments+1) + 1
}

// RoundedRectIndexCount returns the number of indices needed for a rounded rect.
func RoundedRectIndexCount(segments int) int {
	// Triangle fan from center: 4 * (segments + 1) triangles
	// Each triangle = 3 indices
	return 4 * (segments + 1) * 3
}

// RoundedRectGeometry generates vertices and indices for a rounded rectangle.
// The rectangle has its top-left corner at (x, y) with the given dimensions.
func RoundedRectGeometry(
	x, y, width, height float32,
	radius CornerRadius,
	style ShapeStyle,
	segments int,
) ([]Vertex, []uint32) {
	// Clamp radii to prevent overlap
	maxRadiusH := width / 2
	maxRadiusV := height / 2
	maxRadius := minf(maxRadiusH, maxRadiusV)

	tl := clampf(radius.TopLeft, 0, maxRadius)
	tr := clampf(radius.TopRight, 0, maxRadius)
	br := clampf(radius.BottomRight, 0, maxRadius)
	bl := clampf(radius.BottomLeft, 0, maxRadius)

	// Color interpolation helper
	colorAt := func(px, py float32) [4]float32 {
		return interpolateColor(style, px, py, x, y, width, height)
	}

	verts := make([]Vertex, 0, RoundedRectVertexCount(segments))

	// Generate perimeter vertices going clockwise from top-left

	// Top-left corner arc (from left edge to top edge)
	cx, cy := x+tl, y+tl
	for i := 0; i <= segments; i++ {
		angle := math.Pi + float64(i)*math.Pi/2/float64(segments) // 180° to 270°
		px := cx + tl*float32(math.Cos(angle))
		py := cy + tl*float32(math.Sin(angle))
		c := colorAt(px, py)
		verts = append(verts, Vertex{X: px, Y: py, U: 0, V: 0, R: c[0], G: c[1], B: c[2], A: c[3]})
	}

	// Top-right corner arc (from top edge to right edge)
	cx, cy = x+width-tr, y+tr
	for i := 0; i <= segments; i++ {
		angle := 1.5*math.Pi + float64(i)*math.Pi/2/float64(segments) // 270° to 360°
		px := cx + tr*float32(math.Cos(angle))
		py := cy + tr*float32(math.Sin(angle))
		c := colorAt(px, py)
		verts = append(verts, Vertex{X: px, Y: py, U: 0, V: 0, R: c[0], G: c[1], B: c[2], A: c[3]})
	}

	// Bottom-right corner arc (from right edge to bottom edge)
	cx, cy = x+width-br, y+height-br
	for i := 0; i <= segments; i++ {
		angle := float64(i) * math.Pi / 2 / float64(segments) // 0° to 90°
		px := cx + br*float32(math.Cos(angle))
		py := cy + br*float32(math.Sin(angle))
		c := colorAt(px, py)
		verts = append(verts, Vertex{X: px, Y: py, U: 0, V: 0, R: c[0], G: c[1], B: c[2], A: c[3]})
	}

	// Bottom-left corner arc (from bottom edge to left edge)
	cx, cy = x+bl, y+height-bl
	for i := 0; i <= segments; i++ {
		angle := math.Pi/2 + float64(i)*math.Pi/2/float64(segments) // 90° to 180°
		px := cx + bl*float32(math.Cos(angle))
		py := cy + bl*float32(math.Sin(angle))
		c := colorAt(px, py)
		verts = append(verts, Vertex{X: px, Y: py, U: 0, V: 0, R: c[0], G: c[1], B: c[2], A: c[3]})
	}

	// Add center vertex for fan triangulation
	centerX := x + width/2
	centerY := y + height/2
	centerC := colorAt(centerX, centerY)
	centerIdx := uint32(len(verts))
	verts = append(verts, Vertex{
		X: centerX, Y: centerY, U: 0.5, V: 0.5,
		R: centerC[0], G: centerC[1], B: centerC[2], A: centerC[3],
	})

	// Generate indices as triangle fan from center
	n := uint32(len(verts) - 1) // number of perimeter vertices
	idxs := make([]uint32, 0, RoundedRectIndexCount(segments))
	for i := uint32(0); i < n; i++ {
		next := (i + 1) % n
		idxs = append(idxs, centerIdx, i, next)
	}

	return verts, idxs
}

// CircleGeometry generates vertices/indices for a circle.
func CircleGeometry(cx, cy, radius float32, style ShapeStyle, segments int) ([]Vertex, []uint32) {
	d := radius * 2
	return RoundedRectGeometry(cx-radius, cy-radius, d, d, UniformRadius(radius), style, segments)
}

// PillGeometry generates vertices/indices for a pill/capsule shape.
func PillGeometry(x, y, width, height float32, style ShapeStyle, segments int) ([]Vertex, []uint32) {
	r := height / 2
	return RoundedRectGeometry(x, y, width, height, UniformRadius(r), style, segments)
}

// interpolateColor returns the RGBA color at a given point based on the style.
func interpolateColor(style ShapeStyle, px, py, rx, ry, rw, rh float32) [4]float32 {
	if style.GradientDirection == GradientNone || len(style.GradientStops) < 2 {
		return ColorToFloat32(style.FillColor)
	}

	var t float32
	switch style.GradientDirection {
	case GradientVertical:
		t = (py - ry) / rh
	case GradientHorizontal:
		t = (px - rx) / rw
	case GradientDiagonalTL:
		// 135deg: top-left (0,0) = 0.0, bottom-right (1,1) = 1.0
		t = ((px - rx) / rw + (py - ry) / rh) / 2
	case GradientDiagonalTR:
		// 45deg: top-right (1,0) = 0.0, bottom-left (0,1) = 1.0
		t = ((rw - (px - rx)) / rw + (py - ry) / rh) / 2
	}

	t = clampf(t, 0, 1)

	// Find surrounding stops
	stops := style.GradientStops
	var lower, upper ColorStop
	lower = stops[0]
	upper = stops[len(stops)-1]

	for i := 0; i < len(stops)-1; i++ {
		if t >= stops[i].Position && t <= stops[i+1].Position {
			lower = stops[i]
			upper = stops[i+1]
			break
		}
	}

	// Interpolate between stops
	if upper.Position == lower.Position {
		return ColorToFloat32(lower.Color)
	}

	factor := (t - lower.Position) / (upper.Position - lower.Position)
	lc := ColorToFloat32(lower.Color)
	uc := ColorToFloat32(upper.Color)

	return [4]float32{
		lc[0] + (uc[0]-lc[0])*factor,
		lc[1] + (uc[1]-lc[1])*factor,
		lc[2] + (uc[2]-lc[2])*factor,
		lc[3] + (uc[3]-lc[3])*factor,
	}
}

func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func max4f(a, b, c, d float32) float32 {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	if d > m {
		m = d
	}
	return m
}

// ShapeBuilder helps UI widgets manage dynamic shape meshes.
type ShapeBuilder struct {
	mesh     DynamicMesh
	segments int
}

// NewShapeBuilder creates a builder with pre-allocated capacity for a rounded rect.
func NewShapeBuilder(w Window, segments int) (*ShapeBuilder, error) {
	vCount := RoundedRectVertexCount(segments)
	iCount := RoundedRectIndexCount(segments)

	mesh, err := w.NewDynamicMesh(vCount, iCount, nil)
	if err != nil {
		return nil, err
	}

	return &ShapeBuilder{
		mesh:     mesh,
		segments: segments,
	}, nil
}

// UpdateRoundedRect updates the mesh with new rounded rect geometry.
func (b *ShapeBuilder) UpdateRoundedRect(
	x, y, width, height float32,
	radius CornerRadius,
	style ShapeStyle,
) {
	verts, idxs := RoundedRectGeometry(x, y, width, height, radius, style, b.segments)
	b.mesh.UpdateAllVertices(verts)
	b.mesh.UpdateIndices(idxs)
}

// UpdateCircle updates the mesh with circle geometry.
func (b *ShapeBuilder) UpdateCircle(cx, cy, radius float32, style ShapeStyle) {
	verts, idxs := CircleGeometry(cx, cy, radius, style, b.segments)
	b.mesh.UpdateAllVertices(verts)
	b.mesh.UpdateIndices(idxs)
}

// UpdatePill updates the mesh with pill/capsule geometry.
func (b *ShapeBuilder) UpdatePill(x, y, width, height float32, style ShapeStyle) {
	verts, idxs := PillGeometry(x, y, width, height, style, b.segments)
	b.mesh.UpdateAllVertices(verts)
	b.mesh.UpdateIndices(idxs)
}

// Mesh returns the underlying mesh for rendering.
func (b *ShapeBuilder) Mesh() Mesh {
	return b.mesh
}

// Segments returns the tessellation quality.
func (b *ShapeBuilder) Segments() int {
	return b.segments
}
