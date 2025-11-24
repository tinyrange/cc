package arm64

import (
	"encoding/binary"
	"fmt"

	"github.com/tinyrange/cc/internal/asm"
)

type Context struct {
	text           []byte
	constData      []byte
	literalData    []byte
	constLocations map[asm.Variable]constantLocation
	literals       map[dataKey]int
	patches        []patch
	literalLoads   []literalLoadPatch
	labels         map[asm.Label]int
	branches       []branchPatch
	calls          []branchPatch
	bssSize        int
}

type constantLocation struct {
	section dataSection
	offset  int
}

type dataSection int

const (
	sectionLiteral dataSection = iota
	sectionConst
	sectionBSS
)

type patch struct {
	inText     bool
	pos        int
	target     constantLocation
	ptrSection dataSection
}

type literalLoadPatch struct {
	pos           int
	literalOffset int
	width         literalWidth
}

type literalWidth uint8

const (
	literal64 literalWidth = 8
	literal32 literalWidth = 4
	literal16 literalWidth = 2
	literal8  literalWidth = 1
)

type branchKind uint8

const (
	branchB branchKind = iota
	branchBL
	branchCond
)

type branchPatch struct {
	label asm.Label
	pos   int
	kind  branchKind
	cond  condition
}

func newContext() *Context {
	return &Context{
		constLocations: make(map[asm.Variable]constantLocation),
		labels:         make(map[asm.Label]int),
	}
}

func (c *Context) EmitBytes(data []byte) {
	c.text = append(c.text, data...)
}

func (c *Context) emit32(word uint32) int {
	pos := len(c.text)
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], word)
	c.text = append(c.text, buf[:]...)
	return pos
}

func (c *Context) SetLabel(label asm.Label) {
	c.labels[label] = len(c.text)
}

func (c *Context) GetLabel(label asm.Label) (int, bool) {
	pos, ok := c.labels[label]
	return pos, ok
}

func (c *Context) literalOffset(val asm.LiteralValue) int {
	key := dataKey{key: string(val.Data), zeroTerm: val.ZeroTerm}
	if offset, ok := c.literals[key]; ok {
		return offset
	}
	offset := len(c.literalData)
	c.literalData = append(c.literalData, val.Data...)
	if val.ZeroTerm {
		c.literalData = append(c.literalData, 0)
	}
	if c.literals == nil {
		c.literals = make(map[dataKey]int)
	}
	c.literals[key] = offset
	return offset
}

type dataKey struct {
	key      string
	zeroTerm bool
}

func (c *Context) AddConstant(target asm.Variable, data []byte) {
	offset := len(c.constData)
	if c.constLocations == nil {
		c.constLocations = make(map[asm.Variable]constantLocation)
	}
	c.constLocations[target] = constantLocation{
		section: sectionConst,
		offset:  offset,
	}
	c.constData = append(c.constData, data...)
}

func (c *Context) AddZeroConstant(target asm.Variable, size int) {
	if size < 0 {
		panic("arm64 asm: AddZeroConstant negative size")
	}
	const bssAlign = 16
	offset := alignTo(c.bssSize, bssAlign)
	if c.constLocations == nil {
		c.constLocations = make(map[asm.Variable]constantLocation)
	}
	c.constLocations[target] = constantLocation{
		section: sectionBSS,
		offset:  offset,
	}
	c.bssSize = offset + size
}

func (c *Context) ConstantLocation(v asm.Variable) (constantLocation, bool) {
	loc, ok := c.constLocations[v]
	return loc, ok
}

func (c *Context) appendTextPatch(pos int, target constantLocation) {
	c.patches = append(c.patches, patch{
		inText: true,
		pos:    pos,
		target: target,
	})
}

func (c *Context) appendDataPatch(pos int, section dataSection, target constantLocation) {
	c.patches = append(c.patches, patch{
		inText:     false,
		pos:        pos,
		target:     target,
		ptrSection: section,
	})
}

func (c *Context) addPointerLiteral(target constantLocation) int {
	const literalAlign = 8
	aligned := alignTo(len(c.literalData), literalAlign)
	if aligned > len(c.literalData) {
		padding := aligned - len(c.literalData)
		c.literalData = append(c.literalData, make([]byte, padding)...)
	}
	offset := aligned
	c.literalData = append(c.literalData, make([]byte, 8)...)
	c.appendDataPatch(offset, sectionLiteral, target)
	return offset
}

func (c *Context) addLiteralLoad(pos int, literalOffset int, width literalWidth) {
	c.literalLoads = append(c.literalLoads, literalLoadPatch{
		pos:           pos,
		literalOffset: literalOffset,
		width:         width,
	})
}

func (c *Context) finalize() (asm.Program, error) {
	const align = 4
	if rem := len(c.text) % align; rem != 0 {
		padding := align - rem
		c.text = append(c.text, make([]byte, padding)...)
	}

	textLen := len(c.text)
	literalLen := len(c.literalData)
	constLen := len(c.constData)

	dataBase := len(c.text)
	finalData := make([]byte, 0, literalLen+constLen)
	finalData = append(finalData, c.literalData...)
	finalData = append(finalData, c.constData...)

	relocations := make([]int, 0, len(c.patches))

	sectionBase := func(section dataSection) (int, error) {
		switch section {
		case sectionLiteral:
			return 0, nil
		case sectionConst:
			return literalLen, nil
		case sectionBSS:
			return literalLen + constLen, nil
		default:
			return 0, fmt.Errorf("arm64 asm: unknown data section %d", section)
		}
	}

	absAddr := func(loc constantLocation) (int, error) {
		targetBase, err := sectionBase(loc.section)
		if err != nil {
			return 0, err
		}
		return dataBase + targetBase + loc.offset, nil
	}

	for _, p := range c.patches {
		addr, err := absAddr(p.target)
		if err != nil {
			return asm.Program{}, err
		}
		if p.inText {
			if p.pos+8 > len(c.text) {
				return asm.Program{}, fmt.Errorf("arm64 asm: text patch out of range")
			}
			binary.LittleEndian.PutUint64(c.text[p.pos:p.pos+8], uint64(addr))
			relocations = append(relocations, p.pos)
			continue
		}
		ptrBase, err := sectionBase(p.ptrSection)
		if err != nil {
			return asm.Program{}, err
		}
		if p.ptrSection == sectionBSS {
			return asm.Program{}, fmt.Errorf("arm64 asm: cannot place pointer in BSS section")
		}
		index := ptrBase + p.pos
		if index+8 > len(finalData) {
			return asm.Program{}, fmt.Errorf("arm64 asm: data patch out of range")
		}
		binary.LittleEndian.PutUint64(finalData[index:index+8], uint64(addr))
		relocations = append(relocations, textLen+index)
	}

	for _, patch := range c.literalLoads {
		if err := c.patchLiteralLoad(patch, dataBase, literalLen); err != nil {
			return asm.Program{}, err
		}
	}

	for _, br := range c.branches {
		if err := c.patchBranch(br); err != nil {
			return asm.Program{}, err
		}
	}
	for _, call := range c.calls {
		if err := c.patchBranch(call); err != nil {
			return asm.Program{}, err
		}
	}

	code := append(c.text, finalData...)
	return asm.NewProgram(code, relocations, c.bssSize), nil
}

func (c *Context) patchLiteralLoad(p literalLoadPatch, dataBase, literalLen int) error {
	if p.pos+4 > len(c.text) {
		return fmt.Errorf("arm64 asm: literal load patch out of range")
	}
	literalAddr := dataBase + p.literalOffset
	pc := p.pos
	rel := literalAddr - pc
	if rel%4 != 0 {
		return fmt.Errorf("arm64 asm: literal load offset must be multiple of 4")
	}
	switch p.width {
	case literal64:
		if rel < -(1<<20) || rel >= (1<<20) {
			return fmt.Errorf("arm64 asm: literal out of range")
		}
		word := binary.LittleEndian.Uint32(c.text[p.pos : p.pos+4])
		imm := uint32((rel >> 2) & 0x7FFFF)
		word = (word &^ (0x7FFFF << 5)) | (imm << 5)
		binary.LittleEndian.PutUint32(c.text[p.pos:p.pos+4], word)
	case literal32:
		if rel < -(1<<20) || rel >= (1<<20) {
			return fmt.Errorf("arm64 asm: literal out of range")
		}
		word := binary.LittleEndian.Uint32(c.text[p.pos : p.pos+4])
		imm := uint32((rel >> 2) & 0x7FFFF)
		word = (word &^ (0x7FFFF << 5)) | (imm << 5)
		binary.LittleEndian.PutUint32(c.text[p.pos:p.pos+4], word)
	default:
		return fmt.Errorf("arm64 asm: unsupported literal width %d", p.width)
	}
	return nil
}

func (c *Context) patchBranch(p branchPatch) error {
	target, ok := c.labels[p.label]
	if !ok {
		return fmt.Errorf("arm64 asm: undefined label %q", p.label)
	}
	rel := target - p.pos
	switch p.kind {
	case branchB, branchBL:
		if rel%4 != 0 {
			return fmt.Errorf("arm64 asm: branch offset must be multiple of 4")
		}
		imm := rel / 4
		if imm < minBranchImm || imm > maxBranchImm {
			return fmt.Errorf("arm64 asm: branch target out of range")
		}
		word := binary.LittleEndian.Uint32(c.text[p.pos : p.pos+4])
		word = (word &^ ((1 << 26) - 1)) | (uint32(imm) & 0x03FFFFFF)
		binary.LittleEndian.PutUint32(c.text[p.pos:p.pos+4], word)
	case branchCond:
		if rel%4 != 0 {
			return fmt.Errorf("arm64 asm: branch offset must be multiple of 4")
		}
		imm := rel / 4
		if imm < -(1<<18) || imm >= (1<<18) {
			return fmt.Errorf("arm64 asm: conditional branch out of range")
		}
		word := binary.LittleEndian.Uint32(c.text[p.pos : p.pos+4])
		word = (word &^ (0x7FFFF << 5)) | (uint32(imm)&0x7FFFF)<<5
		word = (word &^ 0xF) | uint32(p.cond&0xF)
		binary.LittleEndian.PutUint32(c.text[p.pos:p.pos+4], word)
	default:
		return fmt.Errorf("arm64 asm: unsupported branch kind %d", p.kind)
	}
	return nil
}

const (
	minBranchImm = -(1 << 25)
	maxBranchImm = (1 << 25) - 1
)

func (c *Context) emitBranch(label asm.Label) {
	pos := c.emit32(0x14000000)
	c.branches = append(c.branches, branchPatch{
		label: label,
		pos:   pos,
		kind:  branchB,
	})
}

func (c *Context) emitCall(label asm.Label) {
	pos := c.emit32(0x94000000)
	c.calls = append(c.calls, branchPatch{
		label: label,
		pos:   pos,
		kind:  branchBL,
	})
}

func (c *Context) emitCondBranch(label asm.Label, cond condition) {
	pos := c.emit32(0x54000000 | uint32(cond&0xF))
	c.branches = append(c.branches, branchPatch{
		label: label,
		pos:   pos,
		kind:  branchCond,
		cond:  cond,
	})
}

func alignTo(value, align int) int {
	if align <= 0 {
		return value
	}
	if rem := value % align; rem != 0 {
		return value + (align - rem)
	}
	return value
}

// condition defines the condition code field used by conditional branches.
type condition uint8

const (
	condEQ condition = 0x0
	condNE condition = 0x1
	condCS condition = 0x2
	condCC condition = 0x3
	condMI condition = 0x4
	condPL condition = 0x5
	condVS condition = 0x6
	condVC condition = 0x7
	condHI condition = 0x8
	condLS condition = 0x9
	condGE condition = 0xA
	condLT condition = 0xB
	condGT condition = 0xC
	condLE condition = 0xD
	condAL condition = 0xE
)
