package fsimage

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path"
	"strings"
	"time"

	ffsimgvm "j5.nz/cc/internal/fsimage/vm"
	"j5.nz/cc/internal/imagefs"
)

const (
	defaultFFSSize = 64 << 20
	ffsPageSize    = 16 << 10

	ffsSectorSize = 512
	ffsBSize      = 16 << 10
	ffsFSize      = 2 << 10
	ffsFrag       = ffsBSize / ffsFSize
	ffsNSPF       = ffsFSize / ffsSectorSize
	ffsFSBTODB    = 2
	ffsInodeSize  = 128
	ffsInopb      = ffsBSize / ffsInodeSize
	ffsNindir     = ffsBSize / 4

	ffsSBOFF   = 8192
	ffsPartLBA = 1024
	ffsSBlkNo  = 8
	ffsCBlkNo  = 16
	ffsIBlkNo  = 24
	ffsDBlkNo  = 32
	ffsRootIno = 2
	ffsMaxName = 255

	ffsMagic = 0x011954
	cgMagic  = 0x090255
	dlMagic  = 0x82564557

	dtFIFO = 1
	dtCHR  = 2
	dtDIR  = 4
	dtBLK  = 6
	dtREG  = 8
	dtLNK  = 10
	dtSOCK = 12

	ifmt   = 0o170000
	ififo  = 0o010000
	ifchr  = 0o020000
	ifdir  = 0o040000
	ifblk  = 0o060000
	ifreg  = 0o100000
	iflnk  = 0o120000
	ifsock = 0o140000
)

type ffsStats struct {
	files uint64
	dirs  uint64
	links uint64
	devs  uint64
	bytes uint64
}

type ffsNode struct {
	name     string
	ino      uint32
	mode     uint16
	uid      uint32
	gid      uint32
	rdev     uint32
	modTime  time.Time
	size     uint64
	file     imagefs.File
	target   string
	parent   *ffsNode
	children []*ffsNode
	blocks   []uint32
	indirect uint32
}

type ffsBuilder struct {
	ctx       context.Context
	root      imagefs.Directory
	vm        *ffsimgvm.VirtualMemory
	size      int64
	fsOffset  int64
	fsSize    int64
	fsFrags   uint32
	nextIno   uint32
	nextFrag  uint32
	usedFrags []bool
	usedInos  []bool
	now       int64
}

func buildFFS(ctx context.Context, root imagefs.Directory, opts Options) (*ffsimgvm.VirtualMemory, error) {
	if root == nil {
		return nil, fmt.Errorf("root filesystem is required")
	}
	stats, err := scanFFS(ctx, root, "/")
	if err != nil {
		return nil, err
	}
	size := opts.SizeBytes
	if size == 0 {
		size = estimateFFSSize(stats, opts.ExtraBytes)
	}
	fsSize := roundUp(size, ffsBSize)
	if fsSize < ffsDBlkNo*ffsFSize+ffsBSize {
		fsSize = ffsDBlkNo*ffsFSize + ffsBSize
	}
	if fsSize/ffsFSize > math.MaxUint32 {
		return nil, fmt.Errorf("FFS image is too large: %d bytes", fsSize)
	}
	fsOffset := int64(ffsPartLBA * ffsSectorSize)
	totalSize := fsOffset + fsSize
	b := &ffsBuilder{
		ctx:       ctx,
		root:      root,
		vm:        ffsimgvm.NewVirtualMemory(totalSize, ffsPageSize),
		size:      totalSize,
		fsOffset:  fsOffset,
		fsSize:    fsSize,
		fsFrags:   uint32(fsSize / ffsFSize),
		nextIno:   ffsRootIno,
		nextFrag:  ffsDBlkNo,
		usedFrags: make([]bool, uint32(size/ffsFSize)),
		now:       time.Now().Unix(),
	}
	if !opts.DeterministicTime.IsZero() {
		b.now = opts.DeterministicTime.Unix()
	}
	inodesNeeded := uint32(stats.files + stats.dirs + stats.links + stats.devs + 8)
	ipg := uint32(roundUp(int64(inodesNeeded), ffsInopb))
	if ipg < ffsInopb {
		ipg = ffsInopb
	}
	inodeFrags := (ipg * ffsInodeSize) / ffsFSize
	if ffsIBlkNo+inodeFrags > ffsDBlkNo {
		return nil, fmt.Errorf("too many inodes for single-cylinder-group FFS image: %d", ipg)
	}
	b.usedInos = make([]bool, ipg)
	for frag := uint32(0); frag < ffsDBlkNo; frag++ {
		b.usedFrags[frag] = true
	}

	rootNode, err := b.collectDir(root, "/", nil, "")
	if err != nil {
		return nil, err
	}
	if err := b.assignData(rootNode); err != nil {
		return nil, err
	}
	if err := b.writeTree(rootNode); err != nil {
		return nil, err
	}
	b.writeMBR()
	b.writeDisklabel()
	b.writeSuperblocks(ipg)
	b.writeCylinderGroup(ipg, rootNode)
	return b.vm, nil
}

func scanFFS(ctx context.Context, dir imagefs.Directory, guestPath string) (ffsStats, error) {
	if err := ctx.Err(); err != nil {
		return ffsStats{}, err
	}
	stats := ffsStats{dirs: 1}
	children, err := dir.ReadDir()
	if err != nil {
		return ffsStats{}, fmt.Errorf("read %s: %w", guestPath, err)
	}
	for _, child := range sortedImageDirEnts(children) {
		if child.Name == "." || child.Name == ".." {
			continue
		}
		childPath := path.Join(guestPath, child.Name)
		entry, err := dir.Lookup(child.Name)
		if err != nil {
			return ffsStats{}, fmt.Errorf("lookup %s: %w", childPath, err)
		}
		switch {
		case entry.Dir != nil:
			childStats, err := scanFFS(ctx, entry.Dir, childPath)
			if err != nil {
				return ffsStats{}, err
			}
			stats.files += childStats.files
			stats.dirs += childStats.dirs
			stats.links += childStats.links
			stats.devs += childStats.devs
			stats.bytes += childStats.bytes
		case entry.Symlink != nil:
			stats.links++
			stats.bytes += uint64(len(entry.Symlink.Target()))
		case entry.File != nil:
			size, mode := entry.File.Stat()
			if mode&fs.ModeDevice != 0 || mode&fs.ModeCharDevice != 0 {
				stats.devs++
				continue
			}
			if mode.Type() != 0 {
				return ffsStats{}, fmt.Errorf("%s has unsupported file type %s for FFS filesystem", childPath, mode.Type())
			}
			stats.files++
			stats.bytes += size
		default:
			return ffsStats{}, fmt.Errorf("%s has no filesystem entry", childPath)
		}
	}
	return stats, nil
}

func (b *ffsBuilder) collectDir(dir imagefs.Directory, guestPath string, parent *ffsNode, name string) (*ffsNode, error) {
	if err := b.ctx.Err(); err != nil {
		return nil, err
	}
	mode := ifdir | uint16(dir.Stat().Perm())
	uid, gid := dir.Owner()
	node := &ffsNode{name: name, ino: b.allocIno(), mode: mode, uid: uid, gid: gid, parent: parent, modTime: dir.ModTime()}
	children, err := dir.ReadDir()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", guestPath, err)
	}
	for _, child := range sortedImageDirEnts(children) {
		if child.Name == "." || child.Name == ".." {
			continue
		}
		childPath := path.Join(guestPath, child.Name)
		entry, err := dir.Lookup(child.Name)
		if err != nil {
			return nil, fmt.Errorf("lookup %s: %w", childPath, err)
		}
		childNode, err := b.collectEntry(entry, childPath, node, child.Name)
		if err != nil {
			return nil, err
		}
		node.children = append(node.children, childNode)
	}
	return node, nil
}

func (b *ffsBuilder) collectEntry(entry imagefs.Entry, guestPath string, parent *ffsNode, name string) (*ffsNode, error) {
	if len(name) > ffsMaxName {
		return nil, fmt.Errorf("%s name is too long for FFS", guestPath)
	}
	switch {
	case entry.Dir != nil:
		return b.collectDir(entry.Dir, guestPath, parent, name)
	case entry.Symlink != nil:
		uid, gid := entry.Symlink.Owner()
		target := entry.Symlink.Target()
		return &ffsNode{name: name, ino: b.allocIno(), mode: iflnk | uint16(entry.Symlink.Stat().Perm()), uid: uid, gid: gid, target: target, parent: parent, size: uint64(len(target)), modTime: entry.Symlink.ModTime()}, nil
	case entry.File != nil:
		size, mode := entry.File.Stat()
		uid, gid := entry.File.Owner()
		node := &ffsNode{name: name, ino: b.allocIno(), mode: goModeToFFS(mode), uid: uid, gid: gid, rdev: entry.File.RDev(), file: entry.File, parent: parent, size: size, modTime: entry.File.ModTime()}
		if node.mode&ifmt == ifreg && mode.Type() != 0 {
			return nil, fmt.Errorf("%s has unsupported file type %s for FFS filesystem", guestPath, mode.Type())
		}
		return node, nil
	default:
		return nil, fmt.Errorf("%s has no filesystem entry", guestPath)
	}
}

func (b *ffsBuilder) allocIno() uint32 {
	ino := b.nextIno
	b.nextIno++
	if int(ino) < len(b.usedInos) {
		b.usedInos[ino] = true
	}
	return ino
}

func (b *ffsBuilder) assignData(node *ffsNode) error {
	switch node.mode & ifmt {
	case ifdir:
		data := encodeFFSDir(node)
		node.size = uint64(len(data))
		node.blocks = b.allocBlocksForSize(uint64(len(data)))
	case ifreg:
		node.blocks = b.allocBlocksForSize(node.size)
	case iflnk:
		if len(node.target) > 60 {
			node.blocks = b.allocBlocksForSize(uint64(len(node.target)))
		}
	}
	if len(node.blocks) > 12 {
		node.indirect = b.allocBlock()
	}
	for _, child := range node.children {
		if err := b.assignData(child); err != nil {
			return err
		}
	}
	return nil
}

func (b *ffsBuilder) allocBlocksForSize(size uint64) []uint32 {
	if size == 0 {
		return nil
	}
	count := int(roundUp(int64(size), ffsBSize) / ffsBSize)
	blocks := make([]uint32, count)
	for i := range blocks {
		blocks[i] = b.allocBlock()
	}
	return blocks
}

func (b *ffsBuilder) allocBlock() uint32 {
	frag := roundUpFrag(b.nextFrag, ffsFrag)
	for {
		end := frag + ffsFrag
		if end > uint32(len(b.usedFrags)) {
			panic("FFS image is too small")
		}
		ok := true
		for i := frag; i < end; i++ {
			if b.usedFrags[i] {
				ok = false
				break
			}
		}
		if ok {
			for i := frag; i < end; i++ {
				b.usedFrags[i] = true
			}
			b.nextFrag = end
			return frag
		}
		frag += ffsFrag
	}
}

func (b *ffsBuilder) writeTree(node *ffsNode) error {
	if node.mode&ifmt == ifdir {
		if err := b.writeNodeData(node, encodeFFSDir(node)); err != nil {
			return err
		}
	}
	if node.mode&ifmt == ifreg {
		if err := b.writeFileData(node); err != nil {
			return err
		}
	}
	if node.mode&ifmt == iflnk && len(node.target) > 60 {
		if err := b.writeNodeData(node, []byte(node.target)); err != nil {
			return err
		}
	}
	b.writeInode(node)
	for _, child := range node.children {
		if err := b.writeTree(child); err != nil {
			return err
		}
	}
	return nil
}

func (b *ffsBuilder) writeFileData(node *ffsNode) error {
	remaining := int64(node.size)
	offset := uint64(0)
	buf := make([]byte, ffsBSize)
	for _, block := range node.blocks {
		n := int64(len(buf))
		if remaining < n {
			n = remaining
		}
		data, err := node.file.ReadAt(offset, uint32(n))
		if err != nil && err != io.EOF {
			return fmt.Errorf("read %s: %w", node.name, err)
		}
		clear(buf)
		copy(buf, data)
		if _, err := b.vm.WriteAt(buf, b.fsOffset+int64(block)*ffsFSize); err != nil {
			return err
		}
		offset += uint64(n)
		remaining -= n
	}
	return nil
}

func (b *ffsBuilder) writeNodeData(node *ffsNode, data []byte) error {
	for i, block := range node.blocks {
		start := i * ffsBSize
		end := start + ffsBSize
		if end > len(data) {
			end = len(data)
		}
		buf := make([]byte, ffsBSize)
		copy(buf, data[start:end])
		if _, err := b.vm.WriteAt(buf, b.fsOffset+int64(block)*ffsFSize); err != nil {
			return err
		}
	}
	return nil
}

func (b *ffsBuilder) writeInode(node *ffsNode) {
	var ino [ffsInodeSize]byte
	binary.LittleEndian.PutUint16(ino[0:2], node.mode)
	binary.LittleEndian.PutUint16(ino[2:4], uint16(b.linkCount(node)))
	binary.LittleEndian.PutUint64(ino[8:16], node.size)
	t := uint32(b.now)
	if !node.modTime.IsZero() {
		t = uint32(node.modTime.Unix())
	}
	binary.LittleEndian.PutUint32(ino[16:20], t)
	binary.LittleEndian.PutUint32(ino[24:28], t)
	binary.LittleEndian.PutUint32(ino[32:36], t)
	if node.mode&ifmt == iflnk && len(node.target) <= 60 {
		copy(ino[40:100], []byte(node.target))
	} else if node.mode&ifmt == ifchr || node.mode&ifmt == ifblk {
		binary.LittleEndian.PutUint32(ino[40:44], node.rdev)
	} else {
		for i := 0; i < len(node.blocks) && i < 12; i++ {
			binary.LittleEndian.PutUint32(ino[40+i*4:44+i*4], node.blocks[i])
		}
		if node.indirect != 0 {
			binary.LittleEndian.PutUint32(ino[88:92], node.indirect)
			var indirect [ffsBSize]byte
			for i, block := range node.blocks[12:] {
				binary.LittleEndian.PutUint32(indirect[i*4:i*4+4], block)
			}
			_, _ = b.vm.WriteAt(indirect[:], b.fsOffset+int64(node.indirect)*ffsFSize)
		}
	}
	binary.LittleEndian.PutUint32(ino[104:108], uint32(len(node.blocks)*ffsBSize/ffsSectorSize))
	binary.LittleEndian.PutUint32(ino[108:112], node.ino*1103515245+12345)
	binary.LittleEndian.PutUint32(ino[112:116], node.uid)
	binary.LittleEndian.PutUint32(ino[116:120], node.gid)
	_, _ = b.vm.WriteAt(ino[:], b.fsOffset+int64(ffsIBlkNo*ffsFSize+node.ino*ffsInodeSize))
}

func (b *ffsBuilder) linkCount(node *ffsNode) int {
	if node.mode&ifmt != ifdir {
		return 1
	}
	n := 2
	for _, child := range node.children {
		if child.mode&ifmt == ifdir {
			n++
		}
	}
	return n
}

func (b *ffsBuilder) writeSuperblocks(ipg uint32) {
	var sb [8192]byte
	fsSize := b.fsFrags
	dsize := fsSize - ffsDBlkNo
	nbfree, nffree := b.freeSummary()
	ndir := uint32(countDirsFromInodes(b.usedInos))
	put32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(sb[off:off+4], v) }
	put64 := func(off int, v uint64) { binary.LittleEndian.PutUint64(sb[off:off+8], v) }
	put32(8, ffsSBlkNo)
	put32(12, ffsCBlkNo)
	put32(16, ffsIBlkNo)
	put32(20, ffsDBlkNo)
	put32(28, 0xffffffff)
	put32(32, uint32(b.now))
	put32(36, fsSize)
	put32(40, dsize)
	put32(44, 1)
	put32(48, ffsBSize)
	put32(52, ffsFSize)
	put32(56, ffsFrag)
	put32(68, 60)
	put32(72, uint32(^uint32(ffsBSize-1)))
	put32(76, uint32(^uint32(ffsFSize-1)))
	put32(80, 14)
	put32(84, 11)
	put32(88, 1)
	put32(92, ffsBSize)
	put32(96, 3)
	put32(100, ffsFSBTODB)
	put32(104, 8192)
	put32(116, ffsNindir)
	put32(120, ffsInopb)
	put32(124, ffsNSPF)
	put32(128, 0)
	put32(132, 64)
	put32(136, 1)
	put32(140, 0)
	put32(144, uint32(b.now))
	put32(148, uint32(uint64(b.now)^0x5a5a5a5a))
	put32(152, ffsDBlkNo)
	put32(156, 8192)
	put32(160, ffsBSize)
	put32(164, 1)
	put32(168, 64)
	put32(172, 64)
	put32(176, (fsSize+63)/64)
	put32(180, 1)
	put32(184, 16)
	put32(188, ipg)
	put32(192, fsSize)
	put32(196, ndir)
	put32(200, nbfree)
	put32(204, uint32(len(b.usedInos))-uint32(countUsedInos(b.usedInos)))
	put32(208, nffree)
	sb[212] = 0
	sb[213] = 1
	copy(sb[216:216+1], "/")
	put32(808, 0)
	put32(840, 1)
	put32(844, ffsBSize)
	put64(1000, 8192)
	put64(1008, uint64(ndir))
	put64(1016, uint64(nbfree))
	put64(1024, uint64(len(b.usedInos)-countUsedInos(b.usedInos)))
	put64(1032, uint64(nffree))
	put64(1072, uint64(b.now))
	put64(1080, uint64(fsSize))
	put64(1088, uint64(dsize))
	put64(1096, ffsDBlkNo)
	put32(1196, 16384)
	put32(1200, 64)
	put32(1312, 0)
	put32(1316, 0)
	put32(1320, 60)
	put32(1324, 2)
	put64(1328, 0x000400400402ffff)
	put64(1336, uint64(ffsBSize-1))
	put64(1344, uint64(ffsFSize-1))
	put32(1352, 0)
	put32(1356, 1)
	put32(1360, 1)
	put32(1364, 0)
	put32(1368, 0)
	put32(1372, ffsMagic)
	_, _ = b.vm.WriteAt(sb[:], b.fsOffset+ffsSBOFF)
	_, _ = b.vm.WriteAt(sb[:], b.fsOffset+int64(ffsSBlkNo*ffsFSize))
}

func (b *ffsBuilder) writeCylinderGroup(ipg uint32, rootNode *ffsNode) {
	var cg [ffsBSize]byte
	nbfree, nffree := b.freeSummary()
	ndir := uint32(countDirNodes(rootNode))
	put32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(cg[off:off+4], v) }
	put16 := func(off int, v uint16) { binary.LittleEndian.PutUint16(cg[off:off+2], v) }
	put32(4, cgMagic)
	put32(8, uint32(b.now))
	put16(16, 1)
	put16(18, uint16(ipg/ffsInopb))
	put32(20, b.fsFrags)
	put32(24, ndir)
	put32(28, nbfree)
	put32(32, ipg-uint32(countUsedInos(b.usedInos)))
	put32(36, nffree)
	put32(40, b.nextFrag)
	put32(44, b.nextFrag)
	put32(48, b.nextIno)
	btotoff := uint32(0xa8)
	boff := uint32(0xac)
	iusedoff := uint32(0xae)
	freeoff := iusedoff + uint32((ipg+7)/8)
	if freeoff%2 != 0 {
		freeoff++
	}
	nextfreeoff := freeoff + uint32((b.fsFrags+7)/8)
	put32(80, btotoff)
	put32(84, boff)
	put32(88, iusedoff)
	put32(92, freeoff)
	put32(96, nextfreeoff)
	put32(112, ipg/ffsInopb)
	put32(116, ipg/ffsInopb)
	put32(136, uint32(b.now))
	for ino, used := range b.usedInos {
		if used {
			cg[iusedoff+uint32(ino/8)] |= 1 << uint(ino%8)
		}
	}
	for frag := uint32(0); frag < b.fsFrags; frag++ {
		if !b.usedFrags[frag] {
			cg[freeoff+frag/8] |= 1 << uint(frag%8)
		}
	}
	binary.LittleEndian.PutUint32(cg[btotoff:btotoff+4], nbfree)
	binary.LittleEndian.PutUint16(cg[boff:boff+2], uint16(nbfree))
	_, _ = b.vm.WriteAt(cg[:], b.fsOffset+int64(ffsCBlkNo*ffsFSize))
}

func (b *ffsBuilder) writeDisklabel() {
	var sec [ffsSectorSize]byte
	put32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(sec[off:off+4], v) }
	put16 := func(off int, v uint16) { binary.LittleEndian.PutUint16(sec[off:off+2], v) }
	totalSectors := uint32(b.size / ffsSectorSize)
	fsSectors := uint32(b.fsSize / ffsSectorSize)
	put32(0, dlMagic)
	put16(4, 12)
	copy(sec[8:24], []byte("vnd device"))
	copy(sec[24:40], []byte("fictitious"))
	put32(40, ffsSectorSize)
	put32(44, 100)
	put32(48, 1)
	put32(52, (totalSectors+99)/100)
	put32(56, 100)
	put32(60, totalSectors)
	put32(80, ffsPartLBA)
	put32(84, totalSectors)
	put32(92, totalSectors)
	put16(114, 1)
	put32(132, dlMagic)
	put16(138, 16)
	part := 148
	put32(part, fsSectors)
	put32(part+4, ffsPartLBA)
	sec[part+12] = 7
	sec[part+13] = byte(((15 - 13) << 3) | 4)
	put16(part+14, 16)
	cpart := 148 + 2*16
	put32(cpart, totalSectors)
	put32(cpart+4, 0)
	for off := 0; off < 148+16*16; off += 2 {
		if off == 136 {
			continue
		}
		put16(136, binary.LittleEndian.Uint16(sec[136:138])^binary.LittleEndian.Uint16(sec[off:off+2]))
	}
	_, _ = b.vm.WriteAt(sec[:], b.fsOffset+ffsSectorSize)
}

func (b *ffsBuilder) writeMBR() {
	var mbr [ffsSectorSize]byte
	sectors := uint32(b.fsSize / ffsSectorSize)
	part := mbr[446+3*16 : 446+4*16]
	part[0] = 0x80
	part[1] = 0x00
	part[2] = 0x01
	part[3] = 0x10
	part[4] = 0xa6
	part[5] = 0xfe
	part[6] = 0xff
	part[7] = 0xff
	binary.LittleEndian.PutUint32(part[8:12], ffsPartLBA)
	binary.LittleEndian.PutUint32(part[12:16], sectors)
	mbr[510] = 0x55
	mbr[511] = 0xaa
	_, _ = b.vm.WriteAt(mbr[:], 0)
}

func (b *ffsBuilder) freeSummary() (uint32, uint32) {
	var nbfree, nffree uint32
	for frag := uint32(ffsDBlkNo); frag+ffsFrag <= b.fsFrags; frag += ffsFrag {
		free := true
		for i := uint32(0); i < ffsFrag; i++ {
			if b.usedFrags[frag+i] {
				free = false
				break
			}
		}
		if free {
			nbfree++
		} else {
			for i := uint32(0); i < ffsFrag; i++ {
				if !b.usedFrags[frag+i] {
					nffree++
				}
			}
		}
	}
	return nbfree, nffree
}

func encodeFFSDir(node *ffsNode) []byte {
	type ent struct {
		ino  uint32
		typ  uint8
		name string
	}
	parentIno := node.ino
	if node.parent != nil {
		parentIno = node.parent.ino
	}
	entries := []ent{{node.ino, dtDIR, "."}, {parentIno, dtDIR, ".."}}
	for _, child := range node.children {
		entries = append(entries, ent{child.ino, ffsDirType(child.mode), child.name})
	}
	total := 0
	for _, e := range entries {
		total += directSize(len(e.name))
	}
	size := int(roundUp(int64(total), ffsSectorSize))
	if size < ffsSectorSize {
		size = ffsSectorSize
	}
	buf := make([]byte, size)
	off := 0
	for i, e := range entries {
		reclen := directSize(len(e.name))
		if i == len(entries)-1 {
			reclen = size - off
		}
		binary.LittleEndian.PutUint32(buf[off:off+4], e.ino)
		binary.LittleEndian.PutUint16(buf[off+4:off+6], uint16(reclen))
		buf[off+6] = e.typ
		buf[off+7] = byte(len(e.name))
		copy(buf[off+8:], e.name)
		off += reclen
	}
	return buf
}

func directSize(nameLen int) int {
	return (8 + nameLen + 1 + 3) &^ 3
}

func ffsDirType(mode uint16) uint8 {
	switch mode & ifmt {
	case ifdir:
		return dtDIR
	case iflnk:
		return dtLNK
	case ifchr:
		return dtCHR
	case ifblk:
		return dtBLK
	case ififo:
		return dtFIFO
	case ifsock:
		return dtSOCK
	default:
		return dtREG
	}
}

func goModeToFFS(mode fs.FileMode) uint16 {
	perm := uint16(mode.Perm())
	switch {
	case mode&fs.ModeDevice != 0 && mode&fs.ModeCharDevice != 0:
		return ifchr | perm
	case mode&fs.ModeDevice != 0:
		return ifblk | perm
	case mode&fs.ModeNamedPipe != 0:
		return ififo | perm
	case mode&fs.ModeSocket != 0:
		return ifsock | perm
	default:
		return ifreg | perm
	}
}

func roundUpFrag(v, frag uint32) uint32 {
	return ((v + frag - 1) / frag) * frag
}

func countUsedInos(inos []bool) int {
	n := 0
	for _, used := range inos {
		if used {
			n++
		}
	}
	return n
}

func countDirsFromInodes(inos []bool) int {
	if len(inos) > ffsRootIno && inos[ffsRootIno] {
		return 1
	}
	return 0
}

func countDirNodes(node *ffsNode) int {
	n := 0
	if node.mode&ifmt == ifdir {
		n++
	}
	for _, child := range node.children {
		n += countDirNodes(child)
	}
	return n
}

func estimateFFSSize(stats ffsStats, extra int64) int64 {
	size := int64(stats.bytes)
	size += int64(stats.files+stats.dirs+stats.links+stats.devs) * ffsBSize
	size += size / 4
	size += 16 << 20
	size += extra
	if size < defaultFFSSize {
		size = defaultFFSSize
	}
	return size
}

func ffsCleanPath(value string) string {
	return path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(value), "/"))
}
