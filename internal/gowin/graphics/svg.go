package graphics

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"
	"strconv"
	"strings"
)

type SVG struct {
	viewBox viewBox
	mesh    Mesh
	groups  map[string]*svgGroup
}

type viewBox struct {
	minX float32
	minY float32
	w    float32
	h    float32
}

type svgGroup struct {
	mesh   Mesh
	bounds rect

	// centroid is the area-weighted centroid of all triangles in this group
	// in viewBox space.
	centroidX float32
	centroidY float32
	hasCenter bool
}

type rect struct {
	minX float32
	minY float32
	maxX float32
	maxY float32
	ok   bool
}

func (r *rect) addPoint(x, y float32) {
	if !r.ok {
		r.minX, r.maxX = x, x
		r.minY, r.maxY = y, y
		r.ok = true
		return
	}
	if x < r.minX {
		r.minX = x
	}
	if x > r.maxX {
		r.maxX = x
	}
	if y < r.minY {
		r.minY = y
	}
	if y > r.maxY {
		r.maxY = y
	}
}

type groupBuilder struct {
	verts   []Vertex
	indices []uint32
	bounds  rect

	areaSum float64
	cxSum   float64
	cySum   float64
}

func (gb *groupBuilder) addTri(a, b, c pt) {
	// Area is |cross|/2. Use area as weight for centroid.
	area2 := float64((b.x-a.x)*(c.y-a.y) - (b.y-a.y)*(c.x-a.x))
	if area2 < 0 {
		area2 = -area2
	}
	if area2 == 0 {
		return
	}
	area := area2 * 0.5
	cx := (float64(a.x) + float64(b.x) + float64(c.x)) / 3.0
	cy := (float64(a.y) + float64(b.y) + float64(c.y)) / 3.0
	gb.areaSum += area
	gb.cxSum += cx * area
	gb.cySum += cy * area
}

// Width returns the SVG viewBox width.
func (s *SVG) Width() float32 {
	if s == nil {
		return 0
	}
	return s.viewBox.w
}

// Height returns the SVG viewBox height.
func (s *SVG) Height() float32 {
	if s == nil {
		return 0
	}
	return s.viewBox.h
}

// LoadSVG parses a minimal subset of SVG and uploads a compiled mesh to the GPU.
//
// Supported for now (enough for internal/assets/logo-color-white.svg):
// - <svg viewBox="minX minY w h">
// - <style> rules for "#id { fill: ... }" and "#id polygon { fill: ... }"
// - <g id="..."> grouping for style resolution
// - <polygon points="..." [fill="..."]>
func LoadSVG(win Window, data []byte) (*SVG, error) {
	if win == nil {
		return nil, fmt.Errorf("nil window")
	}

	dec := xml.NewDecoder(bytes.NewReader(data))

	var vb viewBox
	vb.w = 1
	vb.h = 1

	// Minimal CSS support: group defaults.
	groupFill := map[string]color.Color{}
	groupPolygonFill := map[string]color.Color{}

	var groupStack []string

	var vertices []Vertex
	var indices []uint32

	// Per-group mesh builders keyed by the nearest non-empty <g id="..."> in the stack.
	// (Empty key means "no group".)
	groupBuilders := map[string]*groupBuilder{}

	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "svg":
				for _, a := range t.Attr {
					if a.Name.Local == "viewBox" {
						minX, minY, w, h, ok := parseViewBox(a.Value)
						if ok {
							vb = viewBox{minX: minX, minY: minY, w: w, h: h}
						}
					}
				}
			case "g":
				var id string
				for _, a := range t.Attr {
					if a.Name.Local == "id" {
						id = strings.TrimSpace(a.Value)
						break
					}
				}
				groupStack = append(groupStack, id)
			case "style":
				var styleText string
				if err := dec.DecodeElement(&styleText, &t); err != nil {
					return nil, err
				}
				parseStyle(styleText, groupFill, groupPolygonFill)
				continue
			case "polygon":
				var pointsAttr string
				var fillAttr string
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "points":
						pointsAttr = a.Value
					case "fill":
						fillAttr = a.Value
					}
				}
				pts, ok := parsePoints(pointsAttr)
				if !ok || len(pts) < 3 {
					continue
				}

				fill := resolveFill(fillAttr, groupStack, groupFill, groupPolygonFill)
				addPolygon(&vertices, &indices, groupBuilders, groupStack, pts, fill)

			case "circle":
				var cx, cy, r float32
				var fillAttr string
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "cx":
						cx = parseFloat32(a.Value)
					case "cy":
						cy = parseFloat32(a.Value)
					case "r":
						r = parseFloat32(a.Value)
					case "fill":
						fillAttr = a.Value
					}
				}
				if r <= 0 {
					continue
				}
				pts := circleToPolygon(cx, cy, r, 24)
				fill := resolveFill(fillAttr, groupStack, groupFill, groupPolygonFill)
				addPolygon(&vertices, &indices, groupBuilders, groupStack, pts, fill)

			case "rect":
				var x, y, w, h, rx, ry float32
				var fillAttr string
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "x":
						x = parseFloat32(a.Value)
					case "y":
						y = parseFloat32(a.Value)
					case "width":
						w = parseFloat32(a.Value)
					case "height":
						h = parseFloat32(a.Value)
					case "rx":
						rx = parseFloat32(a.Value)
					case "ry":
						ry = parseFloat32(a.Value)
					case "fill":
						fillAttr = a.Value
					}
				}
				if w <= 0 || h <= 0 {
					continue
				}
				pts := rectToPolygon(x, y, w, h, rx, ry)
				fill := resolveFill(fillAttr, groupStack, groupFill, groupPolygonFill)
				addPolygon(&vertices, &indices, groupBuilders, groupStack, pts, fill)

			case "path":
				var dAttr string
				var fillAttr string
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "d":
						dAttr = a.Value
					case "fill":
						fillAttr = a.Value
					}
				}
				if dAttr == "" {
					continue
				}
				paths := parsePath(dAttr)
				fill := resolveFill(fillAttr, groupStack, groupFill, groupPolygonFill)
				for _, pts := range paths {
					if len(pts) >= 3 {
						addPolygon(&vertices, &indices, groupBuilders, groupStack, pts, fill)
					}
				}
			}

		case xml.EndElement:
			if t.Name.Local == "g" {
				if len(groupStack) > 0 {
					groupStack = groupStack[:len(groupStack)-1]
				}
			}
		}
	}

	mesh, err := win.NewMesh(vertices, indices, nil)
	if err != nil {
		return nil, err
	}

	groups := map[string]*svgGroup{}
	for gid, gb := range groupBuilders {
		if gid == "" || gb == nil || len(gb.verts) == 0 || len(gb.indices) == 0 {
			continue
		}
		gm, err := win.NewMesh(gb.verts, gb.indices, nil)
		if err != nil {
			return nil, err
		}
		g := &svgGroup{mesh: gm, bounds: gb.bounds}
		if gb.areaSum > 0 {
			g.centroidX = float32(gb.cxSum / gb.areaSum)
			g.centroidY = float32(gb.cySum / gb.areaSum)
			g.hasCenter = true
		}
		groups[gid] = g
	}

	return &SVG{
		viewBox: vb,
		mesh:    mesh,
		groups:  groups,
	}, nil
}

func (s *SVG) Draw(f Frame, x, y, w, h float32) {
	s.DrawWithOptions(f, x, y, w, h, DrawOptions{})
}

func (s *SVG) modelForDraw(x, y, w, h float32, local Mat4) (Mat4, bool) {
	if s == nil || w == 0 || h == 0 {
		return Mat4{}, false
	}
	vb := s.viewBox
	if vb.w == 0 || vb.h == 0 {
		return Mat4{}, false
	}

	// Map from viewBox space -> local SVG space with origin at (0,0).
	pre := TranslateMat4(-vb.minX, -vb.minY)

	// Map from local SVG space -> screen space.
	sx := w / vb.w
	sy := h / vb.h
	post := MulMat4(TranslateMat4(x, y), ScaleMat4(sx, sy))

	if local == (Mat4{}) {
		local = IdentityMat4()
	}

	// Final: v -> pre -> local -> post
	return MulMat4(post, MulMat4(local, pre)), true
}

// DrawWithOptions draws the full SVG mesh, applying opts.Model in SVG local space (after viewBox translation).
func (s *SVG) DrawWithOptions(f Frame, x, y, w, h float32, opts DrawOptions) {
	if s == nil || f == nil || s.mesh == nil {
		return
	}
	model, ok := s.modelForDraw(x, y, w, h, opts.Model)
	if !ok {
		return
	}
	f.RenderMesh(s.mesh, DrawOptions{Model: model})
}

// GroupBounds returns the viewBox-space bounds for a named <g id="..."> group.
func (s *SVG) GroupBounds(groupID string) (minX, minY, maxX, maxY float32, ok bool) {
	if s == nil || s.groups == nil {
		return 0, 0, 0, 0, false
	}
	g := s.groups[groupID]
	if g == nil || !g.bounds.ok {
		return 0, 0, 0, 0, false
	}
	return g.bounds.minX, g.bounds.minY, g.bounds.maxX, g.bounds.maxY, true
}

// GroupCenter returns the area-weighted centroid for a named <g id="..."> group.
// The center is expressed in viewBox space.
func (s *SVG) GroupCenter(groupID string) (cx, cy float32, ok bool) {
	if s == nil || s.groups == nil {
		return 0, 0, false
	}
	g := s.groups[groupID]
	if g == nil {
		return 0, 0, false
	}
	if g.hasCenter {
		return g.centroidX, g.centroidY, true
	}
	// Fallback to bounds center if needed.
	if g.bounds.ok {
		return (g.bounds.minX + g.bounds.maxX) * 0.5, (g.bounds.minY + g.bounds.maxY) * 0.5, true
	}
	return 0, 0, false
}

// DrawGroupWithOptions draws a specific named group mesh, applying opts.Model after the SVG-to-viewport transform.
func (s *SVG) DrawGroupWithOptions(f Frame, groupID string, x, y, w, h float32, opts DrawOptions) {
	if s == nil || f == nil || s.groups == nil {
		return
	}
	g := s.groups[groupID]
	if g == nil || g.mesh == nil {
		return
	}
	model, ok := s.modelForDraw(x, y, w, h, opts.Model)
	if !ok {
		return
	}
	f.RenderMesh(g.mesh, DrawOptions{Model: model})
}

// DrawGroupRotated rotates a named group around its own bounds center (in viewBox space).
func (s *SVG) DrawGroupRotated(f Frame, groupID string, x, y, w, h float32, angleRad float32) {
	cxVB, cyVB, ok := s.GroupCenter(groupID)
	if !ok {
		s.DrawGroupWithOptions(f, groupID, x, y, w, h, DrawOptions{})
		return
	}
	// modelForDraw applies viewBox translation before local transforms, so the
	// rotation center should be expressed in post-viewBox local coordinates.
	cx := cxVB - s.viewBox.minX
	cy := cyVB - s.viewBox.minY
	rot := MulMat4(
		TranslateMat4(cx, cy),
		MulMat4(RotateZMat4(angleRad), TranslateMat4(-cx, -cy)),
	)
	s.DrawGroupWithOptions(f, groupID, x, y, w, h, DrawOptions{Model: rot})
}

type pt struct {
	x float32
	y float32
}

func parseViewBox(s string) (minX, minY, w, h float32, ok bool) {
	nums, ok := parseFloatList(s)
	if !ok || len(nums) < 4 {
		return 0, 0, 0, 0, false
	}
	return nums[0], nums[1], nums[2], nums[3], true
}

func parsePoints(s string) ([]pt, bool) {
	s = strings.ReplaceAll(s, ",", " ")
	fields := strings.Fields(s)
	if len(fields) < 6 || len(fields)%2 != 0 {
		return nil, false
	}
	pts := make([]pt, 0, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		x, err1 := strconv.ParseFloat(fields[i], 32)
		y, err2 := strconv.ParseFloat(fields[i+1], 32)
		if err1 != nil || err2 != nil {
			return nil, false
		}
		pts = append(pts, pt{x: float32(x), y: float32(y)})
	}
	return pts, true
}

func parseFloatList(s string) ([]float32, bool) {
	s = strings.ReplaceAll(s, ",", " ")
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil, false
	}
	out := make([]float32, 0, len(fields))
	for _, f := range fields {
		v, err := strconv.ParseFloat(f, 32)
		if err != nil {
			return nil, false
		}
		out = append(out, float32(v))
	}
	return out, true
}

func parseStyle(css string, groupFill map[string]color.Color, groupPolygonFill map[string]color.Color) {
	// Very small CSS parser: split by '}' and look for selector + fill decl.
	for _, chunk := range strings.Split(css, "}") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		parts := strings.SplitN(chunk, "{", 2)
		if len(parts) != 2 {
			continue
		}
		selector := strings.TrimSpace(parts[0])
		body := parts[1]

		fill, ok := findCSSFill(body)
		if !ok {
			continue
		}

		c, ok := parseFillColor(fill)
		if !ok {
			continue
		}

		// Support selector lists like:
		//   #inner-circle,
		//   #outer-circle polygon { fill: white; }
		//
		// And individual selectors:
		// - #id { fill: ... }
		// - #id polygon { fill: ... }
		for _, selRaw := range strings.Split(selector, ",") {
			selRaw = strings.TrimSpace(selRaw)
			if !strings.HasPrefix(selRaw, "#") {
				continue
			}
			sel := strings.TrimSpace(selRaw[1:])
			if strings.HasSuffix(sel, " polygon") {
				id := strings.TrimSpace(strings.TrimSuffix(sel, " polygon"))
				groupPolygonFill[id] = c
			} else {
				groupFill[sel] = c
			}
		}
	}
}

func findCSSFill(body string) (string, bool) {
	// Split declarations by ';' and find fill: ...
	for _, decl := range strings.Split(body, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		kv := strings.SplitN(decl, ":", 2)
		if len(kv) != 2 {
			continue
		}
		prop := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if prop == "fill" {
			return val, true
		}
	}
	return "", false
}

func resolveFill(fillAttr string, groupStack []string, groupFill map[string]color.Color, groupPolygonFill map[string]color.Color) color.Color {
	if fillAttr != "" {
		if c, ok := parseFillColor(fillAttr); ok {
			return c
		}
	}

	// Prefer the nearest group rule.
	for i := len(groupStack) - 1; i >= 0; i-- {
		id := groupStack[i]
		if id == "" {
			continue
		}
		if c, ok := groupPolygonFill[id]; ok {
			return c
		}
		if c, ok := groupFill[id]; ok {
			return c
		}
	}

	return ColorBlack
}

func parseFillColor(s string) (color.Color, bool) {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	switch low {
	case "white":
		return ColorWhite, true
	case "black":
		return ColorBlack, true
	case "none":
		// Return transparent (no fill)
		return color.RGBA{0, 0, 0, 0}, true
	case "currentcolor":
		// Default to white for currentColor (no inheritance context)
		return ColorWhite, true
	}

	// hsl(H, S%, L%)
	if strings.HasPrefix(low, "hsl(") && strings.HasSuffix(low, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(low, "hsl("), ")")
		inner = strings.ReplaceAll(inner, "%", "")
		nums, ok := parseFloatList(inner)
		if !ok || len(nums) < 3 {
			return nil, false
		}
		h := float64(nums[0])
		sat := float64(nums[1]) / 100.0
		lit := float64(nums[2]) / 100.0
		r, g, b := hslToRGB(h, sat, lit)
		return color.RGBA{R: r, G: g, B: b, A: 255}, true
	}

	// Hex colors: #RGB, #RRGGBB
	if strings.HasPrefix(s, "#") {
		hex := s[1:]
		if len(hex) == 3 {
			// #RGB -> #RRGGBB
			r := hexDigit(hex[0])
			g := hexDigit(hex[1])
			b := hexDigit(hex[2])
			return color.RGBA{R: r | (r << 4), G: g | (g << 4), B: b | (b << 4), A: 255}, true
		}
		if len(hex) == 6 {
			r := hexDigit(hex[0])<<4 | hexDigit(hex[1])
			g := hexDigit(hex[2])<<4 | hexDigit(hex[3])
			b := hexDigit(hex[4])<<4 | hexDigit(hex[5])
			return color.RGBA{R: r, G: g, B: b, A: 255}, true
		}
	}

	return nil, false
}

func hexDigit(c byte) uint8 {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}

func hslToRGB(h, s, l float64) (r, g, b uint8) {
	h = math.Mod(h, 360.0)
	if h < 0 {
		h += 360.0
	}
	c := (1 - math.Abs(2*l-1)) * s
	hp := h / 60.0
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	var r1, g1, b1 float64
	switch {
	case 0 <= hp && hp < 1:
		r1, g1, b1 = c, x, 0
	case 1 <= hp && hp < 2:
		r1, g1, b1 = x, c, 0
	case 2 <= hp && hp < 3:
		r1, g1, b1 = 0, c, x
	case 3 <= hp && hp < 4:
		r1, g1, b1 = 0, x, c
	case 4 <= hp && hp < 5:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}
	m := l - c/2
	toByte := func(v float64) uint8 {
		v = (v + m) * 255.0
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return uint8(math.Round(v))
	}
	return toByte(r1), toByte(g1), toByte(b1)
}

func triangulate(pts []pt) ([]uint32, bool) {
	n := len(pts)
	if n < 3 {
		return nil, false
	}

	area := signedArea(pts)
	ccw := area > 0

	// Build initial index list.
	V := make([]int, n)
	for i := 0; i < n; i++ {
		V[i] = i
	}

	var out []uint32
	guard := 0
	for len(V) > 2 && guard < 10000 {
		guard++
		earFound := false
		for i := 0; i < len(V); i++ {
			i0 := V[(i+len(V)-1)%len(V)]
			i1 := V[i]
			i2 := V[(i+1)%len(V)]

			a, b, c := pts[i0], pts[i1], pts[i2]
			if !isConvex(a, b, c, ccw) {
				continue
			}

			// Check no other point lies inside the ear triangle.
			contains := false
			for _, j := range V {
				if j == i0 || j == i1 || j == i2 {
					continue
				}
				if pointInTri(pts[j], a, b, c) {
					contains = true
					break
				}
			}
			if contains {
				continue
			}

			out = append(out, uint32(i0), uint32(i1), uint32(i2))
			// Remove ear vertex.
			V = append(V[:i], V[i+1:]...)
			earFound = true
			break
		}
		if !earFound {
			return nil, false
		}
	}

	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func signedArea(pts []pt) float32 {
	var a float32
	for i := 0; i < len(pts); i++ {
		j := (i + 1) % len(pts)
		a += pts[i].x*pts[j].y - pts[j].x*pts[i].y
	}
	return a * 0.5
}

func isConvex(a, b, c pt, ccw bool) bool {
	cross := (b.x-a.x)*(c.y-a.y) - (b.y-a.y)*(c.x-a.x)
	if ccw {
		return cross > 0
	}
	return cross < 0
}

func pointInTri(p, a, b, c pt) bool {
	// Barycentric technique using sign of areas.
	sign := func(p1, p2, p3 pt) float32 {
		return (p1.x-p3.x)*(p2.y-p3.y) - (p2.x-p3.x)*(p1.y-p3.y)
	}
	d1 := sign(p, a, b)
	d2 := sign(p, b, c)
	d3 := sign(p, c, a)

	hasNeg := (d1 < 0) || (d2 < 0) || (d3 < 0)
	hasPos := (d1 > 0) || (d2 > 0) || (d3 > 0)
	return !(hasNeg && hasPos)
}

// addPolygon adds a filled polygon to the vertex/index buffers.
func addPolygon(vertices *[]Vertex, indices *[]uint32, groupBuilders map[string]*groupBuilder, groupStack []string, pts []pt, fill color.Color) {
	if len(pts) < 3 {
		return
	}

	rgba := ColorToFloat32(fill)
	base := uint32(len(*vertices))

	// Determine nearest group id.
	gid := ""
	for i := len(groupStack) - 1; i >= 0; i-- {
		if groupStack[i] != "" {
			gid = groupStack[i]
			break
		}
	}
	gb := groupBuilders[gid]
	if gb == nil {
		gb = &groupBuilder{}
		groupBuilders[gid] = gb
	}
	gbase := uint32(len(gb.verts))

	for _, p := range pts {
		gb.bounds.addPoint(p.x, p.y)
		*vertices = append(*vertices, Vertex{
			X: p.x, Y: p.y,
			U: 0, V: 0,
			R: rgba[0], G: rgba[1], B: rgba[2], A: rgba[3],
		})
		gb.verts = append(gb.verts, Vertex{
			X: p.x, Y: p.y,
			U: 0, V: 0,
			R: rgba[0], G: rgba[1], B: rgba[2], A: rgba[3],
		})
	}

	tris, ok := triangulate(pts)
	if !ok {
		// Best effort: fan triangulation (works for convex polygons).
		for i := 1; i+1 < len(pts); i++ {
			*indices = append(*indices, base+0, base+uint32(i), base+uint32(i+1))
			gb.indices = append(gb.indices, gbase+0, gbase+uint32(i), gbase+uint32(i+1))
			gb.addTri(pts[0], pts[i], pts[i+1])
		}
		return
	}
	for i := 0; i+2 < len(tris); i += 3 {
		i0, i1, i2 := tris[i], tris[i+1], tris[i+2]
		*indices = append(*indices, base+i0, base+i1, base+i2)
		gb.indices = append(gb.indices, gbase+i0, gbase+i1, gbase+i2)
		gb.addTri(pts[i0], pts[i1], pts[i2])
	}
}

// parseFloat32 parses a string as float32.
func parseFloat32(s string) float32 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
	if err != nil {
		return 0
	}
	return float32(v)
}

// circleToPolygon approximates a circle as a polygon.
func circleToPolygon(cx, cy, r float32, segments int) []pt {
	pts := make([]pt, segments)
	for i := 0; i < segments; i++ {
		angle := 2 * math.Pi * float64(i) / float64(segments)
		pts[i] = pt{
			x: cx + r*float32(math.Cos(angle)),
			y: cy + r*float32(math.Sin(angle)),
		}
	}
	return pts
}

// rectToPolygon converts a rect to a polygon, with optional rounded corners.
func rectToPolygon(x, y, w, h, rx, ry float32) []pt {
	if rx <= 0 && ry <= 0 {
		// Simple rectangle
		return []pt{
			{x, y},
			{x + w, y},
			{x + w, y + h},
			{x, y + h},
		}
	}

	// Rounded rectangle
	if rx <= 0 {
		rx = ry
	}
	if ry <= 0 {
		ry = rx
	}
	// Clamp radii
	maxRx := w / 2
	maxRy := h / 2
	if rx > maxRx {
		rx = maxRx
	}
	if ry > maxRy {
		ry = maxRy
	}

	var pts []pt
	segments := 8 // Segments per corner

	// Top-left corner
	for i := 0; i <= segments; i++ {
		angle := math.Pi + float64(i)*math.Pi/2/float64(segments)
		pts = append(pts, pt{
			x: x + rx + rx*float32(math.Cos(angle)),
			y: y + ry + ry*float32(math.Sin(angle)),
		})
	}

	// Top-right corner
	for i := 0; i <= segments; i++ {
		angle := 1.5*math.Pi + float64(i)*math.Pi/2/float64(segments)
		pts = append(pts, pt{
			x: x + w - rx + rx*float32(math.Cos(angle)),
			y: y + ry + ry*float32(math.Sin(angle)),
		})
	}

	// Bottom-right corner
	for i := 0; i <= segments; i++ {
		angle := float64(i) * math.Pi / 2 / float64(segments)
		pts = append(pts, pt{
			x: x + w - rx + rx*float32(math.Cos(angle)),
			y: y + h - ry + ry*float32(math.Sin(angle)),
		})
	}

	// Bottom-left corner
	for i := 0; i <= segments; i++ {
		angle := math.Pi/2 + float64(i)*math.Pi/2/float64(segments)
		pts = append(pts, pt{
			x: x + rx + rx*float32(math.Cos(angle)),
			y: y + h - ry + ry*float32(math.Sin(angle)),
		})
	}

	return pts
}

// parsePath parses an SVG path "d" attribute and returns a list of subpaths as point lists.
// Supports: M, L, H, V, C, S, Q, T, A, Z (both absolute and relative)
func parsePath(d string) [][]pt {
	var paths [][]pt
	var current []pt
	var curX, curY float32
	var startX, startY float32 // Start of current subpath for Z command
	var lastCtrlX, lastCtrlY float32
	var lastCmd byte

	tokens := tokenizePath(d)
	i := 0

	for i < len(tokens) {
		tok := tokens[i]
		i++

		if len(tok) == 0 {
			continue
		}

		cmd := tok[0]
		if (cmd >= 'A' && cmd <= 'Z') || (cmd >= 'a' && cmd <= 'z') {
			// It's a command
		} else {
			// No command, repeat the last command
			i-- // Unread this token
			cmd = lastCmd
			// For M, subsequent coordinates are treated as L
			if cmd == 'M' {
				cmd = 'L'
			} else if cmd == 'm' {
				cmd = 'l'
			}
		}

		relative := cmd >= 'a' && cmd <= 'z'
		cmdUpper := cmd
		if relative {
			cmdUpper = cmd - 32
		}

		switch cmdUpper {
		case 'M': // moveto
			if i >= len(tokens) {
				break
			}
			x, y := parseCoordPair(tokens, &i)
			if relative {
				x += curX
				y += curY
			}
			// Start new subpath
			if len(current) > 0 {
				paths = append(paths, current)
			}
			current = []pt{{x, y}}
			curX, curY = x, y
			startX, startY = x, y
			lastCmd = cmd

		case 'L': // lineto
			for i < len(tokens) && isNumber(tokens[i]) {
				x, y := parseCoordPair(tokens, &i)
				if relative {
					x += curX
					y += curY
				}
				current = append(current, pt{x, y})
				curX, curY = x, y
			}
			lastCmd = cmd

		case 'H': // horizontal lineto
			for i < len(tokens) && isNumber(tokens[i]) {
				x := parseFloat32(tokens[i])
				i++
				if relative {
					x += curX
				}
				current = append(current, pt{x, curY})
				curX = x
			}
			lastCmd = cmd

		case 'V': // vertical lineto
			for i < len(tokens) && isNumber(tokens[i]) {
				y := parseFloat32(tokens[i])
				i++
				if relative {
					y += curY
				}
				current = append(current, pt{curX, y})
				curY = y
			}
			lastCmd = cmd

		case 'C': // cubic bezier
			for i+5 < len(tokens) && isNumber(tokens[i]) {
				x1, y1 := parseCoordPair(tokens, &i)
				x2, y2 := parseCoordPair(tokens, &i)
				x, y := parseCoordPair(tokens, &i)
				if relative {
					x1 += curX
					y1 += curY
					x2 += curX
					y2 += curY
					x += curX
					y += curY
				}
				pts := cubicBezier(curX, curY, x1, y1, x2, y2, x, y, 10)
				current = append(current, pts[1:]...)
				curX, curY = x, y
				lastCtrlX, lastCtrlY = x2, y2
			}
			lastCmd = cmd

		case 'S': // smooth cubic bezier
			for i+3 < len(tokens) && isNumber(tokens[i]) {
				// First control point is reflection of last control point
				x1 := 2*curX - lastCtrlX
				y1 := 2*curY - lastCtrlY
				x2, y2 := parseCoordPair(tokens, &i)
				x, y := parseCoordPair(tokens, &i)
				if relative {
					x2 += curX
					y2 += curY
					x += curX
					y += curY
				}
				pts := cubicBezier(curX, curY, x1, y1, x2, y2, x, y, 10)
				current = append(current, pts[1:]...)
				curX, curY = x, y
				lastCtrlX, lastCtrlY = x2, y2
			}
			lastCmd = cmd

		case 'Q': // quadratic bezier
			for i+3 < len(tokens) && isNumber(tokens[i]) {
				x1, y1 := parseCoordPair(tokens, &i)
				x, y := parseCoordPair(tokens, &i)
				if relative {
					x1 += curX
					y1 += curY
					x += curX
					y += curY
				}
				pts := quadBezier(curX, curY, x1, y1, x, y, 10)
				current = append(current, pts[1:]...)
				curX, curY = x, y
				lastCtrlX, lastCtrlY = x1, y1
			}
			lastCmd = cmd

		case 'T': // smooth quadratic bezier
			for i+1 < len(tokens) && isNumber(tokens[i]) {
				// Control point is reflection of last control point
				x1 := 2*curX - lastCtrlX
				y1 := 2*curY - lastCtrlY
				x, y := parseCoordPair(tokens, &i)
				if relative {
					x += curX
					y += curY
				}
				pts := quadBezier(curX, curY, x1, y1, x, y, 10)
				current = append(current, pts[1:]...)
				curX, curY = x, y
				lastCtrlX, lastCtrlY = x1, y1
			}
			lastCmd = cmd

		case 'A': // arc
			for i+6 < len(tokens) && isNumber(tokens[i]) {
				rx := parseFloat32(tokens[i])
				i++
				ry := parseFloat32(tokens[i])
				i++
				xRot := parseFloat32(tokens[i])
				i++
				largeArc := parseFloat32(tokens[i]) != 0
				i++
				sweep := parseFloat32(tokens[i]) != 0
				i++
				x, y := parseCoordPair(tokens, &i)
				if relative {
					x += curX
					y += curY
				}
				pts := arcToBezier(curX, curY, rx, ry, xRot, largeArc, sweep, x, y)
				current = append(current, pts...)
				curX, curY = x, y
			}
			lastCmd = cmd

		case 'Z': // closepath
			if len(current) > 0 {
				// Close to the start of the subpath
				if curX != startX || curY != startY {
					current = append(current, pt{startX, startY})
				}
				paths = append(paths, current)
				current = nil
			}
			curX, curY = startX, startY
			lastCmd = cmd
		}
	}

	if len(current) > 0 {
		paths = append(paths, current)
	}

	return paths
}

// tokenizePath splits a path "d" attribute into tokens.
func tokenizePath(d string) []string {
	var tokens []string
	var current strings.Builder

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	for i := 0; i < len(d); i++ {
		c := d[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',':
			flush()
		case (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z'):
			flush()
			tokens = append(tokens, string(c))
		case c == '-':
			// Minus can start a new number or be part of exponent
			if current.Len() > 0 {
				lastC := current.String()[current.Len()-1]
				if lastC != 'e' && lastC != 'E' {
					flush()
				}
			}
			current.WriteByte(c)
		case c == '.':
			// Check if we already have a dot
			if strings.Contains(current.String(), ".") {
				flush()
			}
			current.WriteByte(c)
		default:
			current.WriteByte(c)
		}
	}
	flush()

	return tokens
}

func isNumber(s string) bool {
	if len(s) == 0 {
		return false
	}
	c := s[0]
	return (c >= '0' && c <= '9') || c == '-' || c == '.' || c == '+'
}

func parseCoordPair(tokens []string, i *int) (float32, float32) {
	if *i >= len(tokens) {
		return 0, 0
	}
	x := parseFloat32(tokens[*i])
	*i++
	if *i >= len(tokens) {
		return x, 0
	}
	y := parseFloat32(tokens[*i])
	*i++
	return x, y
}

// cubicBezier generates points along a cubic Bezier curve.
func cubicBezier(x0, y0, x1, y1, x2, y2, x3, y3 float32, segments int) []pt {
	pts := make([]pt, segments+1)
	for i := 0; i <= segments; i++ {
		t := float32(i) / float32(segments)
		t2 := t * t
		t3 := t2 * t
		mt := 1 - t
		mt2 := mt * mt
		mt3 := mt2 * mt

		pts[i] = pt{
			x: mt3*x0 + 3*mt2*t*x1 + 3*mt*t2*x2 + t3*x3,
			y: mt3*y0 + 3*mt2*t*y1 + 3*mt*t2*y2 + t3*y3,
		}
	}
	return pts
}

// quadBezier generates points along a quadratic Bezier curve.
func quadBezier(x0, y0, x1, y1, x2, y2 float32, segments int) []pt {
	pts := make([]pt, segments+1)
	for i := 0; i <= segments; i++ {
		t := float32(i) / float32(segments)
		mt := 1 - t

		pts[i] = pt{
			x: mt*mt*x0 + 2*mt*t*x1 + t*t*x2,
			y: mt*mt*y0 + 2*mt*t*y1 + t*t*y2,
		}
	}
	return pts
}

// arcToBezier converts an SVG arc to a series of points.
func arcToBezier(x1, y1, rx, ry, xRot float32, largeArc, sweep bool, x2, y2 float32) []pt {
	// Handle degenerate cases
	if rx == 0 || ry == 0 {
		return []pt{{x2, y2}}
	}
	if x1 == x2 && y1 == y2 {
		return nil
	}

	// Ensure radii are positive
	if rx < 0 {
		rx = -rx
	}
	if ry < 0 {
		ry = -ry
	}

	// Convert rotation to radians
	phi := float64(xRot) * math.Pi / 180.0
	cosPhi := float32(math.Cos(phi))
	sinPhi := float32(math.Sin(phi))

	// Step 1: Compute (x1', y1')
	dx := (x1 - x2) / 2
	dy := (y1 - y2) / 2
	x1p := cosPhi*dx + sinPhi*dy
	y1p := -sinPhi*dx + cosPhi*dy

	// Correct radii if needed
	lambda := (x1p*x1p)/(rx*rx) + (y1p*y1p)/(ry*ry)
	if lambda > 1 {
		sqrtLambda := float32(math.Sqrt(float64(lambda)))
		rx *= sqrtLambda
		ry *= sqrtLambda
	}

	// Step 2: Compute (cx', cy')
	rxSq := rx * rx
	rySq := ry * ry
	x1pSq := x1p * x1p
	y1pSq := y1p * y1p

	denom := rxSq*y1pSq + rySq*x1pSq
	if denom == 0 {
		return []pt{{x2, y2}}
	}

	num := rxSq*rySq - denom
	if num < 0 {
		num = 0
	}
	sq := float32(math.Sqrt(float64(num / denom)))
	if largeArc == sweep {
		sq = -sq
	}

	cxp := sq * rx * y1p / ry
	cyp := -sq * ry * x1p / rx

	// Step 3: Compute (cx, cy)
	cx := cosPhi*cxp - sinPhi*cyp + (x1+x2)/2
	cy := sinPhi*cxp + cosPhi*cyp + (y1+y2)/2

	// Step 4: Compute angles
	ux := (x1p - cxp) / rx
	uy := (y1p - cyp) / ry
	vx := (-x1p - cxp) / rx
	vy := (-y1p - cyp) / ry

	theta1 := vectorAngle(1, 0, ux, uy)
	dTheta := vectorAngle(ux, uy, vx, vy)

	if !sweep && dTheta > 0 {
		dTheta -= 2 * math.Pi
	} else if sweep && dTheta < 0 {
		dTheta += 2 * math.Pi
	}

	// Generate points along the arc
	segments := int(math.Ceil(math.Abs(float64(dTheta)) / (math.Pi / 4)))
	if segments < 1 {
		segments = 1
	}

	pts := make([]pt, 0, segments)
	for i := 1; i <= segments; i++ {
		t := float64(i) / float64(segments)
		angle := float64(theta1) + t*float64(dTheta)
		x := float32(math.Cos(angle))*rx*cosPhi - float32(math.Sin(angle))*ry*sinPhi + cx
		y := float32(math.Cos(angle))*rx*sinPhi + float32(math.Sin(angle))*ry*cosPhi + cy
		pts = append(pts, pt{x, y})
	}

	return pts
}

func vectorAngle(ux, uy, vx, vy float32) float32 {
	dot := ux*vx + uy*vy
	lenU := float32(math.Sqrt(float64(ux*ux + uy*uy)))
	lenV := float32(math.Sqrt(float64(vx*vx + vy*vy)))
	if lenU == 0 || lenV == 0 {
		return 0
	}
	cos := dot / (lenU * lenV)
	if cos < -1 {
		cos = -1
	}
	if cos > 1 {
		cos = 1
	}
	angle := float32(math.Acos(float64(cos)))
	if ux*vy-uy*vx < 0 {
		angle = -angle
	}
	return angle
}
