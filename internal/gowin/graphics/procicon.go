package graphics

import (
	"hash/fnv"
	"image/color"
	"math"
)

// iconPalette contains Tokyo Night themed colors for procedural icons.
var iconPalette = []color.RGBA{
	{0x7a, 0xa2, 0xf7, 255}, // 0: Blue (accent)
	{0x7d, 0xcf, 0xff, 255}, // 1: Cyan
	{0x9e, 0xce, 0x6a, 255}, // 2: Green
	{0xe0, 0xaf, 0x68, 255}, // 3: Yellow/Orange
	{0xf7, 0x76, 0x8e, 255}, // 4: Pink/Red
	{0xbb, 0x9a, 0xf7, 255}, // 5: Purple
	{0x73, 0xda, 0xca, 255}, // 6: Teal
	{0xff, 0x9e, 0x64, 255}, // 7: Orange
	{0x2a, 0xc3, 0xde, 255}, // 8: Light blue
	{0xc0, 0xca, 0xf5, 255}, // 9: Lavender
	{0x41, 0x48, 0x68, 255}, // 10: Dark purple-gray
	{0x3d, 0x59, 0xa1, 255}, // 11: Dark blue
	{0x56, 0x5f, 0x89, 255}, // 12: Muted purple
	{0x1a, 0x9c, 0x9c, 255}, // 13: Dark teal
	{0xc5, 0x8a, 0x4c, 255}, // 14: Brown/amber
	{0x94, 0x7c, 0xb4, 255}, // 15: Muted violet
}

// Shape types for procedural icons.
const (
	shapeCircle = iota
	shapeSquare
	shapeTriangle
	shapeHexagon
	shapeDiamond
	shapePentagon
	shapePill
	shapeOctagon
)

// PolygonGeometry generates vertices and indices for a regular polygon.
// cx, cy: center position
// radius: distance from center to vertices
// sides: number of sides (3 = triangle, 6 = hexagon, etc.)
// rotation: rotation angle in radians
// style: fill color/gradient
func PolygonGeometry(cx, cy, radius float32, sides int, rotation float32, style ShapeStyle) ([]Vertex, []uint32) {
	if sides < 3 {
		sides = 3
	}

	verts := make([]Vertex, sides+1)

	// Generate perimeter vertices
	for i := 0; i < sides; i++ {
		angle := rotation + float32(i)*2*math.Pi/float32(sides) - math.Pi/2 // Start at top
		px := cx + radius*float32(math.Cos(float64(angle)))
		py := cy + radius*float32(math.Sin(float64(angle)))
		c := ColorToFloat32(style.FillColor)
		verts[i] = Vertex{X: px, Y: py, R: c[0], G: c[1], B: c[2], A: c[3]}
	}

	// Center vertex
	c := ColorToFloat32(style.FillColor)
	verts[sides] = Vertex{X: cx, Y: cy, R: c[0], G: c[1], B: c[2], A: c[3]}

	// Fan triangulation from center
	idxs := make([]uint32, 0, sides*3)
	centerIdx := uint32(sides)
	for i := 0; i < sides; i++ {
		next := (i + 1) % sides
		idxs = append(idxs, centerIdx, uint32(i), uint32(next))
	}

	return verts, idxs
}

// RingGeometry generates vertices and indices for a ring (donut shape).
func RingGeometry(cx, cy, outerRadius, innerRadius float32, style ShapeStyle, segments int) ([]Vertex, []uint32) {
	if segments < 8 {
		segments = 8
	}

	verts := make([]Vertex, segments*2)
	c := ColorToFloat32(style.FillColor)

	// Generate outer and inner ring vertices
	for i := 0; i < segments; i++ {
		angle := float32(i) * 2 * math.Pi / float32(segments)
		cos := float32(math.Cos(float64(angle)))
		sin := float32(math.Sin(float64(angle)))

		// Outer vertex
		verts[i*2] = Vertex{
			X: cx + outerRadius*cos,
			Y: cy + outerRadius*sin,
			R: c[0], G: c[1], B: c[2], A: c[3],
		}
		// Inner vertex
		verts[i*2+1] = Vertex{
			X: cx + innerRadius*cos,
			Y: cy + innerRadius*sin,
			R: c[0], G: c[1], B: c[2], A: c[3],
		}
	}

	// Generate indices for triangle strip between inner and outer rings
	idxs := make([]uint32, 0, segments*6)
	for i := 0; i < segments; i++ {
		next := (i + 1) % segments
		outer := uint32(i * 2)
		inner := uint32(i*2 + 1)
		nextOuter := uint32(next * 2)
		nextInner := uint32(next*2 + 1)

		// Two triangles per segment
		idxs = append(idxs, outer, inner, nextOuter)
		idxs = append(idxs, nextOuter, inner, nextInner)
	}

	return verts, idxs
}

// ProceduralIcon generates deterministic geometric icons from a name hash.
type ProceduralIcon struct {
	hash   uint64
	mesh   DynamicMesh
	width  float32
	height float32
}

// NewProceduralIcon creates a procedural icon generator for the given name.
func NewProceduralIcon(name string) *ProceduralIcon {
	h := fnv.New64a()
	h.Write([]byte(name))
	return &ProceduralIcon{
		hash: h.Sum64(),
	}
}

// Hash returns the computed hash value.
func (p *ProceduralIcon) Hash() uint64 {
	return p.hash
}

// extractBits extracts n bits starting at position start from the hash.
func (p *ProceduralIcon) extractBits(start, n int) int {
	mask := uint64((1 << n) - 1)
	return int((p.hash >> start) & mask)
}

// Generate creates the icon geometry for the given dimensions.
func (p *ProceduralIcon) Generate(width, height float32) ([]Vertex, []uint32) {
	// Extract parameters from hash bits
	primaryShape := p.extractBits(0, 3)
	secondaryShape := p.extractBits(3, 3)
	primaryColorIdx := p.extractBits(6, 4)
	secondaryColorIdx := p.extractBits(10, 4)
	rotationBits := p.extractBits(14, 4)
	arrangement := p.extractBits(18, 3)
	scaleBits := p.extractBits(21, 3)

	// Calculate derived values
	rotation := float32(rotationBits) * math.Pi / 8 // 0 to 2*pi in 16 steps
	scale := 0.4 + float32(scaleBits)*0.05          // 0.4 to 0.75

	// Center and size
	cx, cy := width/2, height/2
	baseRadius := minf(width, height) / 2 * scale

	primaryColor := iconPalette[primaryColorIdx%len(iconPalette)]
	secondaryColor := iconPalette[secondaryColorIdx%len(iconPalette)]

	var allVerts []Vertex
	var allIdxs []uint32

	switch arrangement {
	case 0: // Single centered shape
		v, i := p.generateShape(primaryShape, cx, cy, baseRadius, rotation, primaryColor)
		allVerts, allIdxs = v, i

	case 1: // Two shapes layered (larger behind, smaller in front)
		v1, i1 := p.generateShape(secondaryShape, cx, cy, baseRadius, rotation, secondaryColor)
		v2, i2 := p.generateShape(primaryShape, cx, cy, baseRadius*0.6, rotation, primaryColor)
		allVerts, allIdxs = mergeGeometry(v1, i1, v2, i2)

	case 2: // Two shapes side by side
		offset := baseRadius * 0.6
		v1, i1 := p.generateShape(primaryShape, cx-offset, cy, baseRadius*0.7, rotation, primaryColor)
		v2, i2 := p.generateShape(secondaryShape, cx+offset, cy, baseRadius*0.7, rotation, secondaryColor)
		allVerts, allIdxs = mergeGeometry(v1, i1, v2, i2)

	case 3: // Shape with ring
		v1, i1 := RingGeometry(cx, cy, baseRadius, baseRadius*0.75, ShapeStyle{FillColor: secondaryColor}, 24)
		v2, i2 := p.generateShape(primaryShape, cx, cy, baseRadius*0.6, rotation, primaryColor)
		allVerts, allIdxs = mergeGeometry(v1, i1, v2, i2)

	case 4: // Three shapes in triangle pattern
		r := baseRadius * 0.45
		offset := baseRadius * 0.5
		angles := []float32{-math.Pi / 2, math.Pi / 6, 5 * math.Pi / 6}
		v, i := p.generateShape(primaryShape, cx+offset*float32(math.Cos(float64(angles[0]))), cy+offset*float32(math.Sin(float64(angles[0]))), r, rotation, primaryColor)
		allVerts, allIdxs = v, i
		for j := 1; j < 3; j++ {
			px := cx + offset*float32(math.Cos(float64(angles[j])))
			py := cy + offset*float32(math.Sin(float64(angles[j])))
			col := primaryColor
			if j == 1 {
				col = secondaryColor
			}
			v2, i2 := p.generateShape(primaryShape, px, py, r, rotation, col)
			allVerts, allIdxs = mergeGeometry(allVerts, allIdxs, v2, i2)
		}

	case 5: // Four shapes in corners
		r := baseRadius * 0.4
		offset := baseRadius * 0.55
		positions := [][2]float32{{-1, -1}, {1, -1}, {-1, 1}, {1, 1}}
		colors := []color.RGBA{primaryColor, secondaryColor, secondaryColor, primaryColor}
		for j, pos := range positions {
			px := cx + pos[0]*offset
			py := cy + pos[1]*offset
			v, i := p.generateShape(primaryShape, px, py, r, rotation, colors[j])
			if j == 0 {
				allVerts, allIdxs = v, i
			} else {
				allVerts, allIdxs = mergeGeometry(allVerts, allIdxs, v, i)
			}
		}

	case 6: // Concentric shapes (two different shape types)
		v1, i1 := p.generateShape(secondaryShape, cx, cy, baseRadius, rotation, secondaryColor)
		v2, i2 := p.generateShape(primaryShape, cx, cy, baseRadius*0.5, rotation+math.Pi/float32(3+primaryShape), primaryColor)
		allVerts, allIdxs = mergeGeometry(v1, i1, v2, i2)

	case 7: // Single shape with gradient (using two-tone effect)
		// Primary shape larger, secondary smaller offset creates depth illusion
		v1, i1 := p.generateShape(primaryShape, cx+2, cy+2, baseRadius, rotation, darkenColor(primaryColor, 0.5))
		v2, i2 := p.generateShape(primaryShape, cx, cy, baseRadius, rotation, primaryColor)
		allVerts, allIdxs = mergeGeometry(v1, i1, v2, i2)

	default:
		v, i := p.generateShape(primaryShape, cx, cy, baseRadius, rotation, primaryColor)
		allVerts, allIdxs = v, i
	}

	return allVerts, allIdxs
}

// generateShape creates geometry for a specific shape type.
func (p *ProceduralIcon) generateShape(shapeType int, cx, cy, radius, rotation float32, c color.RGBA) ([]Vertex, []uint32) {
	style := ShapeStyle{FillColor: c}
	segments := SegmentsForRadius(radius)

	switch shapeType {
	case shapeCircle:
		return CircleGeometry(cx, cy, radius, style, segments)
	case shapeSquare:
		return PolygonGeometry(cx, cy, radius, 4, rotation+math.Pi/4, style)
	case shapeTriangle:
		return PolygonGeometry(cx, cy, radius, 3, rotation, style)
	case shapeHexagon:
		return PolygonGeometry(cx, cy, radius, 6, rotation, style)
	case shapeDiamond:
		return PolygonGeometry(cx, cy, radius, 4, rotation, style)
	case shapePentagon:
		return PolygonGeometry(cx, cy, radius, 5, rotation, style)
	case shapePill:
		w := radius * 2
		h := radius
		return PillGeometry(cx-radius, cy-radius/2, w, h, style, segments)
	case shapeOctagon:
		return PolygonGeometry(cx, cy, radius, 8, rotation, style)
	default:
		return CircleGeometry(cx, cy, radius, style, segments)
	}
}

// mergeGeometry combines two sets of vertices and indices.
func mergeGeometry(v1 []Vertex, i1 []uint32, v2 []Vertex, i2 []uint32) ([]Vertex, []uint32) {
	offset := uint32(len(v1))
	verts := append(v1, v2...)
	idxs := make([]uint32, len(i1)+len(i2))
	copy(idxs, i1)
	for j, idx := range i2 {
		idxs[len(i1)+j] = idx + offset
	}
	return verts, idxs
}

// darkenColor reduces the brightness of a color.
func darkenColor(c color.RGBA, factor float32) color.RGBA {
	return color.RGBA{
		R: uint8(float32(c.R) * factor),
		G: uint8(float32(c.G) * factor),
		B: uint8(float32(c.B) * factor),
		A: c.A,
	}
}

// Initialize creates the GPU mesh (call once after graphics.Window is available).
func (p *ProceduralIcon) Initialize(w Window, width, height float32) error {
	verts, idxs := p.Generate(width, height)

	mesh, err := w.NewDynamicMesh(len(verts)*2, len(idxs)*2, nil)
	if err != nil {
		return err
	}
	mesh.UpdateAllVertices(verts)
	mesh.UpdateIndices(idxs)

	p.mesh = mesh
	p.width = width
	p.height = height
	return nil
}

// Draw renders the icon at the specified position.
func (p *ProceduralIcon) Draw(f Frame, x, y float32) {
	if p.mesh == nil {
		return
	}
	model := TranslateMat4(x, y)
	f.RenderMesh(p.mesh, DrawOptions{Model: model})
}

