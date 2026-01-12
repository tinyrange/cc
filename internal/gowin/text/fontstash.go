package text

import (
	"fmt"
	"io/ioutil"
	"unicode/utf8"
	"unsafe"

	glpkg "github.com/tinyrange/cc/internal/gowin/gl"
	"github.com/tinyrange/cc/internal/gowin/third_party/truetype"
)

const (
	textVertexShaderSource = `#version 150
in vec2 a_position;
in vec2 a_texCoord;
in vec4 a_color;

out vec2 v_texCoord;
out vec4 v_color;

uniform mat4 u_proj;

void main() {
	gl_Position = u_proj * vec4(a_position, 0.0, 1.0);
	v_texCoord = a_texCoord;
	v_color = a_color;
}`

	textFragmentShaderSource = `#version 150
in vec2 v_texCoord;
in vec4 v_color;

out vec4 fragColor;

uniform sampler2D u_texture;

void main() {
	// Sample red channel as alpha (GL_R8 texture)
	float alpha = texture(u_texture, v_texCoord).r;
	fragColor = vec4(v_color.rgb, v_color.a * alpha);
}`
)

const (
	HASH_LUT_SIZE uint = 256
	MAX_ROWS      int  = 128
	VERT_COUNT         = 6 * 128
	VERT_STRIDE        = 16
)

var idx int = 0

const (
	TTFONT_FILE int = iota + 1
	TTFONT_MEM
	BMFONT
)

type Stash struct {
	gl glpkg.OpenGL

	tw         int
	th         int
	itw        float64
	ith        float64
	emptyData  []byte
	ttTextures []*Texture
	bmTextures []*Texture
	fonts      []*Font
	drawing    bool
	yInverted  bool

	// GL3 resources
	shaderProgram  uint32
	vao            uint32
	vbo            uint32
	projUniform    int32
	viewportW      int32
	viewportH      int32
	scale          float32
	graphicsShader uint32
}

type Font struct {
	idx       int
	fType     int
	font      *truetype.FontInfo
	data      []byte
	glyphs    []*Glyph
	lut       [HASH_LUT_SIZE]int
	ascender  float64
	descender float64
	lineh     float64
}

type Row struct {
	x, y, h int16
}

type Texture struct {
	id     uint32
	rows   []*Row
	verts  [VERT_COUNT * 8]float32 // 8 floats per vertex: x, y, s, t, r, g, b, a
	nverts int
}

type Glyph struct {
	codepoint int
	size      int16
	texture   *Texture
	x0        int
	y0        int
	x1        int
	y1        int
	xadv      float64
	xoff      float64
	yoff      float64
	next      int
}

type Quad struct {
	x0, y0, s0, t0 float32
	x1, y1, s1, t1 float32
}

func hashint(a uint) uint {
	a += ^(a << 15)
	a ^= (a >> 10)
	a += (a << 3)
	a ^= (a >> 6)
	a += ^(a << 11)
	a ^= (a >> 16)
	return a
}

func New(gl glpkg.OpenGL, cachew, cacheh int) *Stash {
	stash := &Stash{}

	stash.gl = gl

	// Create data for clearing the textures
	stash.emptyData = make([]byte, cachew*cacheh)

	// Create first texture for the cache
	stash.tw = cachew
	stash.th = cacheh
	stash.itw = 1 / float64(cachew)
	stash.ith = 1 / float64(cacheh)
	stash.ttTextures = make([]*Texture, 1)
	stash.ttTextures[0] = &Texture{}
	gl.GenTextures(1, &stash.ttTextures[0].id)
	gl.BindTexture(glpkg.Texture2D, stash.ttTextures[0].id)
	// Use GL_R8 for single-channel alpha texture (OpenGL 3.0+)
	gl.TexImage2D(glpkg.Texture2D, 0, int32(glpkg.R8), int32(cachew), int32(cacheh),
		0, glpkg.Red, glpkg.UnsignedByte, unsafe.Pointer(&stash.emptyData[0]))
	gl.TexParameteri(glpkg.Texture2D, glpkg.TextureMinFilter, glpkg.Linear)
	gl.TexParameteri(glpkg.Texture2D, glpkg.TextureMagFilter, glpkg.Linear)
	gl.TexParameteri(glpkg.Texture2D, glpkg.TextureWrapS, glpkg.ClampToEdge)
	gl.TexParameteri(glpkg.Texture2D, glpkg.TextureWrapT, glpkg.ClampToEdge)

	// Create shader program
	program, err := createTextShaderProgram(gl, textVertexShaderSource, textFragmentShaderSource)
	if err != nil {
		// Return stash but shader will be nil - caller should handle error
		return stash
	}
	stash.shaderProgram = program
	stash.projUniform = gl.GetUniformLocation(program, "u_proj")

	// Create VAO and VBO
	var vao, vbo uint32
	gl.GenVertexArrays(1, &vao)
	gl.GenBuffers(1, &vbo)
	stash.vao = vao
	stash.vbo = vbo

	gl.BindVertexArray(vao)
	gl.BindBuffer(glpkg.ArrayBuffer, vbo)
	// Allocate buffer for VERT_COUNT vertices * 8 floats (2 pos + 2 tex + 4 color)
	gl.BufferData(glpkg.ArrayBuffer, VERT_COUNT*8*4, nil, glpkg.DynamicDraw)

	// Set up vertex attributes
	posLoc := gl.GetAttribLocation(program, "a_position")
	texLoc := gl.GetAttribLocation(program, "a_texCoord")
	colLoc := gl.GetAttribLocation(program, "a_color")
	gl.VertexAttribPointer(uint32(posLoc), 2, glpkg.Float, false, 8*4, 0)
	gl.EnableVertexAttribArray(uint32(posLoc))
	gl.VertexAttribPointer(uint32(texLoc), 2, glpkg.Float, false, 8*4, 8)
	gl.EnableVertexAttribArray(uint32(texLoc))
	gl.VertexAttribPointer(uint32(colLoc), 4, glpkg.Float, false, 8*4, 16)
	gl.EnableVertexAttribArray(uint32(colLoc))

	return stash
}

func createTextShaderProgram(gl glpkg.OpenGL, vertexSrc, fragmentSrc string) (uint32, error) {
	// Create and compile vertex shader
	vertexShader := gl.CreateShader(glpkg.VertexShader)
	gl.ShaderSource(vertexShader, vertexSrc)
	gl.CompileShader(vertexShader)
	var status int32
	gl.GetShaderiv(vertexShader, glpkg.CompileStatus, &status)
	if status == 0 {
		log := gl.GetShaderInfoLog(vertexShader)
		gl.DeleteShader(vertexShader)
		return 0, fmt.Errorf("text vertex shader compilation failed: %s", log)
	}

	// Create and compile fragment shader
	fragmentShader := gl.CreateShader(glpkg.FragmentShader)
	gl.ShaderSource(fragmentShader, fragmentSrc)
	gl.CompileShader(fragmentShader)
	gl.GetShaderiv(fragmentShader, glpkg.CompileStatus, &status)
	if status == 0 {
		log := gl.GetShaderInfoLog(fragmentShader)
		gl.DeleteShader(vertexShader)
		gl.DeleteShader(fragmentShader)
		return 0, fmt.Errorf("text fragment shader compilation failed: %s", log)
	}

	// Create program and link
	program := gl.CreateProgram()
	gl.AttachShader(program, vertexShader)
	gl.AttachShader(program, fragmentShader)
	gl.LinkProgram(program)
	gl.GetProgramiv(program, glpkg.LinkStatus, &status)
	if status == 0 {
		log := gl.GetProgramInfoLog(program)
		gl.DeleteShader(vertexShader)
		gl.DeleteShader(fragmentShader)
		gl.DeleteProgram(program)
		return 0, fmt.Errorf("text program linking failed: %s", log)
	}

	gl.DeleteShader(vertexShader)
	gl.DeleteShader(fragmentShader)

	return program, nil
}

// orthoMatrix creates an orthographic projection matrix (column-major)
func orthoMatrix(left, right, bottom, top, near, far float32) [16]float32 {
	// Column-major order
	return [16]float32{
		2.0 / (right - left), 0, 0, 0,
		0, 2.0 / (top - bottom), 0, 0,
		0, 0, -2.0 / (far - near), 0,
		-(right + left) / (right - left), -(top + bottom) / (top - bottom), -(far + near) / (far - near), 1,
	}
}

func (s *Stash) AddFontFromMemory(buffer []byte) (int, error) {
	fnt := &Font{}

	// Init hash lookup.
	for i := 0; i < int(HASH_LUT_SIZE); i++ {
		fnt.lut[i] = -1
	}

	fnt.data = buffer

	// Init truetype
	var err error
	fnt.font, err = truetype.InitFont(fnt.data, 0)
	if err != nil {
		return 0, err
	}

	// Store normalized line height. The real line height is calculated
	// by multiplying the lineh by font size.
	ascent, descent, lineGap := fnt.font.GetFontVMetrics()
	fh := float64(ascent - descent)
	fnt.ascender = float64(ascent) / fh
	fnt.descender = float64(descent) / fh
	fnt.lineh = (fh + float64(lineGap)) / fh

	fnt.idx = idx
	fnt.fType = TTFONT_MEM
	s.fonts = append([]*Font{fnt}, s.fonts...)

	idx++
	return idx - 1, nil
}

func (s *Stash) AddFont(path string) (int, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return 0, err
	}

	idx, err := s.AddFontFromMemory(data)
	if err != nil {
		return 0, err
	}
	s.fonts[0].fType = TTFONT_FILE

	return idx, nil
}

func (s *Stash) GetGlyph(fnt *Font, codepoint int, isize int16) *Glyph {
	size := float64(isize) / 10

	// Find code point and size.
	h := hashint(uint(codepoint)) & (HASH_LUT_SIZE - 1)
	for i := fnt.lut[h]; i != -1; i = fnt.glyphs[i].next {
		if fnt.glyphs[i].codepoint == codepoint && (fnt.fType == BMFONT || fnt.glyphs[i].size == isize) {
			return fnt.glyphs[i]
		}
	}
	// Could not find glyph.

	// For bitmap fonts: ignore this glyph.
	if fnt.fType == BMFONT {
		return nil
	}

	// For truetype fonts: create this glyph.
	scale := fnt.font.ScaleForPixelHeight(size)
	g := fnt.font.FindGlyphIndex(codepoint)
	if g == 0 {
		// glyph not found
		return nil
	}
	advance, _ := fnt.font.GetGlyphHMetrics(g)
	x0, y0, x1, y1 := fnt.font.GetGlyphBitmapBox(g, scale, scale)
	gw := x1 - x0
	gh := y1 - y0

	// Check if glyph is larger than maximum texture size
	if gw >= s.tw || gh >= s.th {
		return nil
	}

	// Find texture and row where the glyph can be fit.
	rh := (int16(gh) + 7) & ^7
	var tt int
	texture := s.ttTextures[tt]
	const glyphPadding = 3 // Padding between glyphs to prevent texture bleeding

	var br *Row
	for br == nil {
		for i := range texture.rows {
			if texture.rows[i].h == rh && int(texture.rows[i].x)+gw+glyphPadding <= s.tw {
				br = texture.rows[i]
			}
		}

		// If no row is found, there are 3 possibilities:
		//  - add new row
		//  - try next texture
		//  - create new texture
		if br == nil {
			var py int16
			// Check that there is enough space.
			if len(texture.rows) > 0 {
				py = texture.rows[len(texture.rows)-1].y + texture.rows[len(texture.rows)-1].h + int16(glyphPadding)
				if int(py+rh) > s.th {
					if tt < len(s.ttTextures)-1 {
						tt++
						texture = s.ttTextures[tt]
					} else {
						// Create new texture
						texture = &Texture{}
						s.gl.GenTextures(1, &texture.id)
						s.gl.BindTexture(glpkg.Texture2D, texture.id)
						s.gl.TexImage2D(glpkg.Texture2D, 0, int32(glpkg.R8),
							int32(s.tw), int32(s.th), 0,
							glpkg.Red, glpkg.UnsignedByte,
							unsafe.Pointer(&s.emptyData[0]))
						s.gl.TexParameteri(glpkg.Texture2D, glpkg.TextureMinFilter, glpkg.Linear)
						s.gl.TexParameteri(glpkg.Texture2D, glpkg.TextureMagFilter, glpkg.Linear)
						s.gl.TexParameteri(glpkg.Texture2D, glpkg.TextureWrapS, glpkg.ClampToEdge)
						s.gl.TexParameteri(glpkg.Texture2D, glpkg.TextureWrapT, glpkg.ClampToEdge)
						s.ttTextures = append(s.ttTextures, texture)
					}
					continue
				}
			}
			// Init and add row
			br = &Row{
				x: 0,
				y: py,
				h: rh,
			}
			texture.rows = append(texture.rows, br)
		}
	}

	// Init glyph.
	glyph := &Glyph{
		codepoint: codepoint,
		size:      isize,
		texture:   texture,
		x0:        int(br.x),
		y0:        int(br.y),
		x1:        int(br.x) + gw,
		y1:        int(br.y) + gh,
		xadv:      scale * float64(advance),
		xoff:      float64(x0),
		yoff:      float64(y0),
		next:      0,
	}
	fnt.glyphs = append(fnt.glyphs, glyph)

	// Advance row location.
	br.x += int16(gw) + int16(glyphPadding)

	// Insert char to hash lookup.
	glyph.next = fnt.lut[h]
	fnt.lut[h] = len(fnt.glyphs) - 1

	// Rasterize
	bmp := make([]byte, gw*gh)
	bmp = fnt.font.MakeGlyphBitmap(bmp, gw, gh, gw, scale, scale, g)
	if len(bmp) > 0 {
		// Update texture
		s.gl.BindTexture(glpkg.Texture2D, texture.id)
		s.gl.PixelStorei(glpkg.UnpackAlignment, 1)
		s.gl.TexSubImage2D(glpkg.Texture2D, 0, int32(glyph.x0), int32(glyph.y0),
			int32(gw), int32(gh), glpkg.Red, glpkg.UnsignedByte,
			unsafe.Pointer(&bmp[0]))
	}

	return glyph
}

func (s *Stash) SetYInverted(inverted bool) {
	s.yInverted = inverted
}

func (s *Stash) SetViewport(width, height int32) {
	s.viewportW = width
	s.viewportH = height
}

func (s *Stash) SetScale(scale float32) {
	s.scale = scale
}

func (s *Stash) SetGraphicsShader(shader uint32) {
	s.graphicsShader = shader
}

func (s *Stash) GetQuad(fnt *Font, glyph *Glyph, isize int16, x, y float64) (float64, float64, *Quad) {
	q := &Quad{}
	scale := float64(1)

	if fnt.fType == BMFONT {
		scale = float64(isize) / float64(glyph.size*10)
	}

	// Use subpixel positioning for smoother text with GL_LINEAR filtering
	rx := x + scale*glyph.xoff
	ry := y - scale*glyph.yoff

	q.x0 = float32(rx)
	q.y0 = float32(ry)
	q.x1 = float32(float64(rx) + scale*float64(glyph.x1-glyph.x0))
	q.y1 = float32(float64(ry) - scale*float64(glyph.y1-glyph.y0))

	q.s0 = float32(float64(glyph.x0) * s.itw)
	q.t0 = float32(float64(glyph.y0) * s.ith)
	q.s1 = float32(float64(glyph.x1) * s.itw)
	q.t1 = float32(float64(glyph.y1) * s.ith)

	if s.yInverted {
		yOffset := float32(2 * y)
		q.y0 = yOffset - q.y0
		q.y1 = yOffset - q.y1
	}

	x += scale * glyph.xadv

	return x, y, q
}

func (s *Stash) FlushDraw() {
	if s.shaderProgram == 0 {
		// Shader not initialized; avoid unbounded vertex accumulation.
		for _, t := range s.ttTextures {
			t.nverts = 0
		}
		for _, t := range s.bmTextures {
			t.nverts = 0
		}
		return // Shader not initialized
	}

	// Compute orthographic projection matrix
	// Use viewport size if set, otherwise use a default
	// The viewport size should already be scaled (divided by scale factor)
	width := float32(s.viewportW)
	height := float32(s.viewportH)
	if width == 0 {
		width = 800 // default
	}
	if height == 0 {
		height = 600 // default
	}
	proj := orthoMatrix(0, width, height, 0, -1, 1)

	// Save current shader program to restore later
	// Note: We can't easily query the current program in GL3, so we'll rely on
	// the graphics system to reset it in prepareFrame() each frame
	s.gl.UseProgram(s.shaderProgram)
	s.gl.UniformMatrix4fv(s.projUniform, 1, false, &proj[0])
	s.gl.BindVertexArray(s.vao)

	i := 0
	texture := s.ttTextures[i]
	tt := true
	for {
		if texture.nverts > 0 {
			s.gl.ActiveTexture(glpkg.Texture0)
			s.gl.BindTexture(glpkg.Texture2D, texture.id)
			texUniform := s.gl.GetUniformLocation(s.shaderProgram, "u_texture")
			s.gl.Uniform1i(texUniform, 0)

			// texture.nverts is a *vertex count* (4 verts per quad), not a quad count.
			numQuads := texture.nverts / 4
			vertexCount := numQuads * 6                // 6 vertices per quad (2 triangles)
			vertices := make([]float32, vertexCount*8) // 8 floats per vertex

			vidx := 0
			for q := 0; q < numQuads; q++ {
				base := q * 4
				// corners in order: 0,1,2,3
				v0 := base + 0
				v1 := base + 1
				v2 := base + 2
				v3 := base + 3

				emit := func(v int) {
					base := v * 8
					vertices[vidx+0] = texture.verts[base+0] // x
					vertices[vidx+1] = texture.verts[base+1] // y
					vertices[vidx+2] = texture.verts[base+2] // s
					vertices[vidx+3] = texture.verts[base+3] // t
					vertices[vidx+4] = texture.verts[base+4] // r
					vertices[vidx+5] = texture.verts[base+5] // g
					vertices[vidx+6] = texture.verts[base+6] // b
					vertices[vidx+7] = texture.verts[base+7] // a
					vidx += 8
				}

				// Vertex order in texture.verts is:
				// 0: (x0,y0) 1:(x1,y0) 2:(x1,y1) 3:(x0,y1)
				// So triangles should be (0,1,2) and (0,2,3).
				emit(v0)
				emit(v1)
				emit(v2)
				emit(v0)
				emit(v2)
				emit(v3)
			}

			s.gl.BindBuffer(glpkg.ArrayBuffer, s.vbo)
			s.gl.BufferSubData(glpkg.ArrayBuffer, 0, len(vertices)*4, unsafe.Pointer(&vertices[0]))

			s.gl.DrawArrays(glpkg.Triangles, 0, int32(vertexCount))
			texture.nverts = 0
		}
		if tt {
			if i < len(s.ttTextures)-1 {
				i++
				texture = s.ttTextures[i]
			} else {
				i = 0
				if len(s.bmTextures) > 0 {
					texture = s.bmTextures[i]
					tt = false
				} else {
					break
				}
			}
		} else {
			if i < len(s.bmTextures)-1 {
				i++
				texture = s.bmTextures[i]
			} else {
				break
			}
		}
	}

	// Restore graphics shader program after all text rendering is complete
	if s.graphicsShader != 0 {
		s.gl.UseProgram(s.graphicsShader)
	}
}

func (s *Stash) BeginDraw() {
	if s.drawing {
		s.FlushDraw()
	}
	s.drawing = true
}

func (s *Stash) EndDraw() {
	if !s.drawing {
		return
	}
	s.FlushDraw()
	s.drawing = false
}

func (s *Stash) GetFontByIdx(idx int) *Font {
	for _, f := range s.fonts {
		if f.idx == idx {
			return f
		}
	}
	return nil
}

func (stash *Stash) GetAdvance(idx int, size float64, s string) float64 {
	isize := int16(size * 10)

	var fnt *Font
	for _, f := range stash.fonts {
		if f.idx == idx {
			fnt = f
			break
		}
	}
	if fnt == nil {
		return 0
	}
	if fnt.fType != BMFONT && len(fnt.data) == 0 {
		return 0
	}

	x := float64(0)
	scale := fnt.font.ScaleForPixelHeight(size)
	prevGlyph := -1 // Track previous glyph for kerning

	b := []byte(s)
	for len(b) > 0 {
		r, runeSize := utf8.DecodeRune(b)
		glyph := stash.GetGlyph(fnt, int(r), isize)
		if glyph == nil {
			b = b[runeSize:]
			prevGlyph = -1
			continue
		}

		// Apply kerning if we have a previous glyph
		if prevGlyph >= 0 && fnt.fType != BMFONT {
			currGlyph := fnt.font.FindGlyphIndex(int(r))
			kern := fnt.font.GetGlyphKernAdvance(prevGlyph, currGlyph)
			x += float64(kern) * scale
			prevGlyph = currGlyph
		} else {
			prevGlyph = fnt.font.FindGlyphIndex(int(r))
		}

		x, _, _ = stash.GetQuad(fnt, glyph, isize, x, 0)
		b = b[runeSize:]
	}

	return x
}

func (stash *Stash) DrawText(idx int, size, x, y float64, s string, color [4]float32) (nextX float64) {
	isize := int16(size * 10)

	var fnt *Font
	for _, f := range stash.fonts {
		if f.idx == idx {
			fnt = f
			break
		}
	}
	if fnt == nil {
		return 0
	}
	if fnt.fType != BMFONT && len(fnt.data) == 0 {
		return 0
	}

	// Store the initial x position for newline handling
	startX := x
	// Get line height for newline handling
	_, _, lineHeight := stash.VMetrics(idx, size)
	// Get scale for kerning calculations
	scale := fnt.font.ScaleForPixelHeight(size)
	prevGlyph := -1 // Track previous glyph for kerning

	var q *Quad

	b := []byte(s)
	for len(b) > 0 {
		r, runeSize := utf8.DecodeRune(b)

		// Handle newline character
		if r == '\n' {
			x = startX
			if stash.yInverted {
				y += lineHeight
			} else {
				y -= lineHeight
			}
			b = b[runeSize:]
			prevGlyph = -1 // Reset kerning on newline
			continue
		}

		glyph := stash.GetGlyph(fnt, int(r), isize)
		if glyph == nil {
			b = b[runeSize:]
			prevGlyph = -1
			continue
		}

		// Apply kerning if we have a previous glyph
		if prevGlyph >= 0 && fnt.fType != BMFONT {
			currGlyph := fnt.font.FindGlyphIndex(int(r))
			kern := fnt.font.GetGlyphKernAdvance(prevGlyph, currGlyph)
			x += float64(kern) * scale
			prevGlyph = currGlyph
		} else {
			prevGlyph = fnt.font.FindGlyphIndex(int(r))
		}
		texture := glyph.texture
		// We emit 4 vertices per glyph quad into texture.verts (8 floats each).
		// When rendering, each quad expands to 6 vertices (2 triangles).
		// VBO capacity is VERT_COUNT*8 floats = VERT_COUNT vertices.
		// Max stored vertices = VERT_COUNT * 4 / 6 to fit after expansion.
		maxStoredVerts := (VERT_COUNT * 4) / 6
		if texture.nverts+4 > maxStoredVerts {
			stash.FlushDraw()
		}

		x, y, q = stash.GetQuad(fnt, glyph, isize, x, y)

		// Vertex 0: top-left
		base := texture.nverts * 8
		texture.verts[base+0] = q.x0
		texture.verts[base+1] = q.y0
		texture.verts[base+2] = q.s0
		texture.verts[base+3] = q.t0
		texture.verts[base+4] = color[0]
		texture.verts[base+5] = color[1]
		texture.verts[base+6] = color[2]
		texture.verts[base+7] = color[3]
		texture.nverts++

		// Vertex 1: top-right
		base = texture.nverts * 8
		texture.verts[base+0] = q.x1
		texture.verts[base+1] = q.y0
		texture.verts[base+2] = q.s1
		texture.verts[base+3] = q.t0
		texture.verts[base+4] = color[0]
		texture.verts[base+5] = color[1]
		texture.verts[base+6] = color[2]
		texture.verts[base+7] = color[3]
		texture.nverts++

		// Vertex 2: bottom-right
		base = texture.nverts * 8
		texture.verts[base+0] = q.x1
		texture.verts[base+1] = q.y1
		texture.verts[base+2] = q.s1
		texture.verts[base+3] = q.t1
		texture.verts[base+4] = color[0]
		texture.verts[base+5] = color[1]
		texture.verts[base+6] = color[2]
		texture.verts[base+7] = color[3]
		texture.nverts++

		// Vertex 3: bottom-left
		base = texture.nverts * 8
		texture.verts[base+0] = q.x0
		texture.verts[base+1] = q.y1
		texture.verts[base+2] = q.s0
		texture.verts[base+3] = q.t1
		texture.verts[base+4] = color[0]
		texture.verts[base+5] = color[1]
		texture.verts[base+6] = color[2]
		texture.verts[base+7] = color[3]
		texture.nverts++
		b = b[runeSize:]
	}

	return x
}

// GradientStop represents a color stop in a gradient.
type GradientStop struct {
	Position float32     // 0.0 to 1.0
	R, G, B, A float32
}

// DrawTextGradient draws text with a horizontal gradient applied.
// The gradient colors are interpolated across the text based on character position.
func (stash *Stash) DrawTextGradient(idx int, size, x, y float64, s string, stops []GradientStop, textWidth float64) (nextX float64) {
	if len(stops) < 2 {
		// Need at least 2 stops for a gradient
		return stash.DrawText(idx, size, x, y, s, [4]float32{1, 1, 1, 1})
	}

	isize := int16(size * 10)

	var fnt *Font
	for _, f := range stash.fonts {
		if f.idx == idx {
			fnt = f
			break
		}
	}
	if fnt == nil {
		return 0
	}
	if fnt.fType != BMFONT && len(fnt.data) == 0 {
		return 0
	}

	// Store the initial x position
	startX := x
	// Get line height for newline handling
	_, _, lineHeight := stash.VMetrics(idx, size)
	// Get scale for kerning calculations
	scale := fnt.font.ScaleForPixelHeight(size)
	prevGlyph := -1

	var q *Quad

	b := []byte(s)
	for len(b) > 0 {
		r, runeSize := utf8.DecodeRune(b)

		// Handle newline character
		if r == '\n' {
			x = startX
			if stash.yInverted {
				y += lineHeight
			} else {
				y -= lineHeight
			}
			b = b[runeSize:]
			prevGlyph = -1
			continue
		}

		glyph := stash.GetGlyph(fnt, int(r), isize)
		if glyph == nil {
			b = b[runeSize:]
			prevGlyph = -1
			continue
		}

		// Apply kerning if we have a previous glyph
		if prevGlyph >= 0 && fnt.fType != BMFONT {
			currGlyph := fnt.font.FindGlyphIndex(int(r))
			kern := fnt.font.GetGlyphKernAdvance(prevGlyph, currGlyph)
			x += float64(kern) * scale
			prevGlyph = currGlyph
		} else {
			prevGlyph = fnt.font.FindGlyphIndex(int(r))
		}

		texture := glyph.texture
		maxStoredVerts := (VERT_COUNT * 4) / 6
		if texture.nverts+4 > maxStoredVerts {
			stash.FlushDraw()
		}

		// Calculate position along gradient (0.0 to 1.0)
		t := float32(0)
		if textWidth > 0 {
			t = float32((x - startX) / textWidth)
			if t > 1 {
				t = 1
			}
		}

		// Interpolate color from gradient stops
		color := interpolateGradient(stops, t)

		x, y, q = stash.GetQuad(fnt, glyph, isize, x, y)

		// Vertex 0: top-left
		base := texture.nverts * 8
		texture.verts[base+0] = q.x0
		texture.verts[base+1] = q.y0
		texture.verts[base+2] = q.s0
		texture.verts[base+3] = q.t0
		texture.verts[base+4] = color[0]
		texture.verts[base+5] = color[1]
		texture.verts[base+6] = color[2]
		texture.verts[base+7] = color[3]
		texture.nverts++

		// Vertex 1: top-right
		base = texture.nverts * 8
		texture.verts[base+0] = q.x1
		texture.verts[base+1] = q.y0
		texture.verts[base+2] = q.s1
		texture.verts[base+3] = q.t0
		texture.verts[base+4] = color[0]
		texture.verts[base+5] = color[1]
		texture.verts[base+6] = color[2]
		texture.verts[base+7] = color[3]
		texture.nverts++

		// Vertex 2: bottom-right
		base = texture.nverts * 8
		texture.verts[base+0] = q.x1
		texture.verts[base+1] = q.y1
		texture.verts[base+2] = q.s1
		texture.verts[base+3] = q.t1
		texture.verts[base+4] = color[0]
		texture.verts[base+5] = color[1]
		texture.verts[base+6] = color[2]
		texture.verts[base+7] = color[3]
		texture.nverts++

		// Vertex 3: bottom-left
		base = texture.nverts * 8
		texture.verts[base+0] = q.x0
		texture.verts[base+1] = q.y1
		texture.verts[base+2] = q.s0
		texture.verts[base+3] = q.t1
		texture.verts[base+4] = color[0]
		texture.verts[base+5] = color[1]
		texture.verts[base+6] = color[2]
		texture.verts[base+7] = color[3]
		texture.nverts++

		b = b[runeSize:]
	}

	return x
}

// interpolateGradient returns the color at position t (0.0-1.0) in the gradient.
func interpolateGradient(stops []GradientStop, t float32) [4]float32 {
	if t <= stops[0].Position {
		return [4]float32{stops[0].R, stops[0].G, stops[0].B, stops[0].A}
	}
	if t >= stops[len(stops)-1].Position {
		s := stops[len(stops)-1]
		return [4]float32{s.R, s.G, s.B, s.A}
	}

	// Find surrounding stops
	var lower, upper GradientStop
	for i := 0; i < len(stops)-1; i++ {
		if t >= stops[i].Position && t <= stops[i+1].Position {
			lower = stops[i]
			upper = stops[i+1]
			break
		}
	}

	// Interpolate between stops
	if upper.Position == lower.Position {
		return [4]float32{lower.R, lower.G, lower.B, lower.A}
	}

	factor := (t - lower.Position) / (upper.Position - lower.Position)
	return [4]float32{
		lower.R + (upper.R-lower.R)*factor,
		lower.G + (upper.G-lower.G)*factor,
		lower.B + (upper.B-lower.B)*factor,
		lower.A + (upper.A-lower.A)*factor,
	}
}

func (s *Stash) VMetrics(idx int, size float64) (float64, float64, float64) {
	var fnt *Font
	for _, f := range s.fonts {
		if f.idx == idx {
			fnt = f
			break
		}
	}
	if fnt == nil {
		return 0, 0, 0
	}
	if fnt.fType != BMFONT && len(fnt.data) == 0 {
		return 0, 0, 0
	}
	return fnt.ascender * size, fnt.descender * size, fnt.lineh * size
}
