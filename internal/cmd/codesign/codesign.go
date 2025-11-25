// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package codesign provides basic functionalities for
// ad-hoc code signing of Mach-O files.
//
// This is not a general tool for code-signing. It is made
// specifically for the Go toolchain. It uses the same
// ad-hoc signing algorithm as the Darwin linker.
package main

import (
	"bytes"
	"crypto/sha256"
	"debug/macho"
	"encoding/binary"
	"flag"
	"fmt"
	"hash"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
)

// Code signature layout.
//
// The code signature is a block of bytes that contains
// a SuperBlob, which contains one or more Blobs. For ad-hoc
// signing, a single CodeDirectory Blob suffices.
//
// TODO(oxisto): adjust documentation
// A SuperBlob starts with its header (the binary representation
// of the SuperBlob struct), followed by a list of (in our case,
// one) Blobs (offset and size). A CodeDirectory Blob starts
// with its head (the binary representation of CodeDirectory struct),
// followed by the identifier (as a C string) and the hashes, at
// the corresponding offsets.
//
// The signature data must be included in the __LINKEDIT segment.
// In the Mach-O file header, an LC_CODE_SIGNATURE load command
// points to the data.

const (
	pageSizeBits = 12
	pageSize     = 1 << pageSizeBits
)

const LC_CODE_SIGNATURE = 0x1d

// Constants and struct layouts are from
// https://opensource.apple.com/source/xnu/xnu-4903.270.47/osfmk/kern/cs_blobs.h

const (
	CSMAGIC_REQUIREMENT               = 0xfade0c00 // single Requirement blob
	CSMAGIC_REQUIREMENTS              = 0xfade0c01 // Requirements vector (internal requirements)
	CSMAGIC_CODEDIRECTORY             = 0xfade0c02 // CodeDirectory blob
	CSMAGIC_EMBEDDED_SIGNATURE        = 0xfade0cc0 // embedded form of signature data
	CSMAGIC_EMBEDDED_ENTITLEMENTS     = 0xfade7171 // embedded entitlements
	CSMAGIC_EMBEDDED_DER_ENTITLEMENTS = 0xfade7172 // embedded DER entitlements
	CSMAGIC_DETACHED_SIGNATURE        = 0xfade0cc1 // multi-arch collection of embedded signatures
	CSMAGIC_BLOBWRAPPER               = 0xfade0b01 // blob wrapper used for the certificate

	CSSLOT_CODEDIRECTORY    = 0x00000 // slot index for CodeDirectory
	CSSLOT_REQUIREMENTS     = 0x00002 // slot index for requirements
	CSSLOT_ENTITLEMENTS     = 0x00005 // slot index for entitlements
	CSSLOT_DER_ENTITLEMENTS = 0x00007 // slot index for DER entitlements
	CSSLOT_SIGNATURESLOT    = 0x10000 // slot index for signature
)

const (
	CS_HASHTYPE_SHA1             = 1
	CS_HASHTYPE_SHA256           = 2
	CS_HASHTYPE_SHA256_TRUNCATED = 3
	CS_HASHTYPE_SHA384           = 4
)

const (
	CS_EXECSEG_MAIN_BINARY     = 0x1   // executable segment denotes main binary
	CS_EXECSEG_ALLOW_UNSIGNED  = 0x10  // allow unsigned pages (for debugging)
	CS_EXECSEG_DEBUGGER        = 0x20  // main binary is debugger
	CS_EXECSEG_JIT             = 0x40  // JIT enabled
	CS_EXECSEG_SKIP_LV         = 0x80  // skip library validation
	CS_EXECSEG_CAN_LOAD_CDHASH = 0x100 // can bless cdhash for execution
	CS_EXECSEG_CAN_EXEC_CDHASH = 0x200 // can execute blessed cdhash
)

type BlobIndex struct {
	typ    uint32 // type of entry
	offset uint32 // offset of entry
}

func (b *BlobIndex) put(out []byte) []byte {
	out = put32be(out, b.typ)
	out = put32be(out, b.offset)
	return out
}

const blobIndexSize = 2 * 4

// SuperBlob is the outer most container that contains all blobs.
type SuperBlob struct {
	magic  uint32      // magic number
	length uint32      // total length of SuperBlob
	count  uint32      // number of index entries following
	index  []BlobIndex // index entries

	cdir  *CodeDirectory
	blobs map[uint32]*GenericBlob // map of blobs (without code directory), indexed by the slot index
}

func (s *SuperBlob) put(out []byte) []byte {
	out = put32be(out, s.magic)
	out = put32be(out, s.length)
	out = put32be(out, s.count)

	for _, b := range s.index {
		out = b.put(out)
	}

	return out
}

func (sb *SuperBlob) add(off *uint32, magic uint32, slot uint32, data []byte) (blob *GenericBlob) {
	blob = &GenericBlob{
		magic:  magic,
		length: genericBlobSize + uint32(len(data)),
		data:   data,
	}
	sb.blobs[slot] = blob
	sb.index = append(sb.index, BlobIndex{
		typ:    slot,
		offset: *off,
	})

	*off += blob.length
	return
}

// sbsize returns the header size of the SuperBlob, including its blob index
// structures, but without any blob data.
func sbsize(nblobs uint32) uint32 {
	return fixedSuperBlobSize + nblobs*blobIndexSize
}

const fixedSuperBlobSize = 3 * 4

type CodeDirectory struct {
	magic         uint32 // magic number (CSMAGIC_CODEDIRECTORY)
	length        uint32 // total length of CodeDirectory blob
	version       uint32 // compatibility version
	flags         uint32 // setup and mode flags
	hashOffset    uint32 // offset of hash slot element at index zero
	identOffset   uint32 // offset of identifier string
	nSpecialSlots uint32 // number of special hash slots
	nCodeSlots    uint32 // number of ordinary (code) hash slots
	codeLimit     uint32 // limit to main image signature range
	hashSize      uint8  // size of each hash in bytes
	hashType      uint8  // type of hash (cdHashType* constants)
	_pad1         uint8  // unused (must be zero)
	pageSize      uint8  // log2(page size in bytes); 0 => infinite
	_pad2         uint32 // unused (must be zero)
	scatterOffset uint32
	teamOffset    uint32
	_pad3         uint32
	codeLimit64   uint64
	execSegBase   uint64
	execSegLimit  uint64
	execSegFlags  uint64
	// data follows
}

func (c *CodeDirectory) put(out []byte) []byte {
	out = put32be(out, c.magic)
	out = put32be(out, c.length)
	out = put32be(out, c.version)
	out = put32be(out, c.flags)
	out = put32be(out, c.hashOffset)
	out = put32be(out, c.identOffset)
	out = put32be(out, c.nSpecialSlots)
	out = put32be(out, c.nCodeSlots)
	out = put32be(out, c.codeLimit)
	out = put8(out, c.hashSize)
	out = put8(out, c.hashType)
	out = put8(out, c._pad1)
	out = put8(out, c.pageSize)
	out = put32be(out, c._pad2)
	out = put32be(out, c.scatterOffset)
	out = put32be(out, c.teamOffset)
	out = put32be(out, c._pad3)
	out = put64be(out, c.codeLimit64)
	out = put64be(out, c.execSegBase)
	out = put64be(out, c.execSegLimit)
	out = put64be(out, c.execSegFlags)
	return out
}

const codeDirectorySize = 13*4 + 4*1 + 4*8 // without hashes and id

type GenericBlob struct {
	magic  uint32 // magic number
	length uint32 // total length of blob
	data   []byte // data
}

func (g *GenericBlob) put(out []byte) []byte {
	out = put32be(out, g.magic)
	out = put32be(out, g.length)
	out = puts(out, g.data)
	return out
}

func (g *GenericBlob) digest(h hash.Hash) []byte {
	h.Reset()
	b := []byte{}
	b = binary.BigEndian.AppendUint32(b, g.magic)
	b = binary.BigEndian.AppendUint32(b, g.length)
	b = append(b, g.data...)
	h.Write(b)
	return h.Sum(nil)
}

const genericBlobSize = 2 * 4 // without data

// CodeSigCmd is Mach-O LC_CODE_SIGNATURE load command.
type CodeSigCmd struct {
	Cmd      uint32 // LC_CODE_SIGNATURE
	Cmdsize  uint32 // sizeof this command (16)
	Dataoff  uint32 // file offset of data in __LINKEDIT segment
	Datasize uint32 // file size of data in __LINKEDIT segment
}

// ReadSuperBlob reads out an existing SuperBlob from a code signature
// and fills the Options struct, which contains information about the
// identifier as well as any existing entitlements.
func (c *CodeSigCmd) ReadSuperBlob(r io.ReaderAt) (sb *SuperBlob, opts Options, err error) {
	in := make([]byte, c.Datasize)
	if _, err = r.ReadAt(in, int64(c.Dataoff)); err != nil {
		return nil, opts, err
	}

	inp := in

	// read SuperBlob
	sb = &SuperBlob{}
	sb.magic, inp = read32be(inp)
	sb.length, inp = read32be(inp)
	sb.count, inp = read32be(inp)
	sb.blobs = make(map[uint32]*GenericBlob, sb.count-1)

	for i := 0; i < int(sb.count); i++ {
		// read BlobIndex
		idx := BlobIndex{}
		idx.typ, inp = read32be(inp)
		idx.offset, inp = read32be(inp)
		sb.index = append(sb.index, idx)
	}

	// read CodeDirectory
	sb.cdir = &CodeDirectory{}
	sb.cdir.magic, inp = read32be(inp)
	sb.cdir.length, inp = read32be(inp)
	sb.cdir.version, inp = read32be(inp)
	sb.cdir.flags, inp = read32be(inp)
	sb.cdir.hashOffset, inp = read32be(inp)
	sb.cdir.identOffset, inp = read32be(inp)
	sb.cdir.nSpecialSlots, inp = read32be(inp)
	sb.cdir.nCodeSlots, inp = read32be(inp)
	sb.cdir.codeLimit, inp = read32be(inp)
	sb.cdir.hashSize, inp = read8(inp)
	sb.cdir.hashType, inp = read8(inp)
	sb.cdir._pad1, inp = read8(inp)
	sb.cdir.pageSize, inp = read8(inp)
	sb.cdir._pad2, inp = read32be(inp)
	sb.cdir.scatterOffset, inp = read32be(inp)
	sb.cdir.teamOffset, inp = read32be(inp)
	sb.cdir._pad3, inp = read32be(inp)
	sb.cdir.codeLimit64, inp = read64be(inp)
	sb.cdir.execSegBase, inp = read64be(inp)
	sb.cdir.execSegLimit, inp = read64be(inp)
	sb.cdir.execSegFlags, inp = read64be(inp)

	identEnd := sb.cdir.hashOffset - sb.cdir.nSpecialSlots*uint32(sb.cdir.hashSize)
	id := make([]byte, identEnd-sb.cdir.identOffset)
	inp = read(inp, id)

	opts.ID = string(id[:len(id)-1])

	hashes := make([]byte, (sb.cdir.nCodeSlots+sb.cdir.nSpecialSlots)*uint32(sb.cdir.hashSize))
	inp = read(inp, hashes)

	// read remaining blobs
	for i := 0; i < int(sb.count-1); i++ {
		blob := &GenericBlob{}
		blob.magic, inp = read32be(inp)
		blob.length, inp = read32be(inp)
		blob.data = make([]byte, blob.length-genericBlobSize)
		inp = read(inp, blob.data)

		if blob.magic == CSMAGIC_EMBEDDED_ENTITLEMENTS {
			opts.Entitlements = blob.data
			sb.blobs[CSSLOT_ENTITLEMENTS] = blob
		} else if blob.magic == CSMAGIC_EMBEDDED_DER_ENTITLEMENTS {
			opts.DEREntitlements = blob.data
			sb.blobs[CSSLOT_DER_ENTITLEMENTS] = blob
		} else if blob.magic == CSMAGIC_REQUIREMENTS {
			sb.blobs[CSSLOT_REQUIREMENTS] = blob
		} else if blob.magic == CSMAGIC_BLOBWRAPPER {
			sb.blobs[CSSLOT_SIGNATURESLOT] = blob
		}
	}

	return nil, opts, nil
}

// Options can be supplied to configure the signing process.
type Options struct {
	// ID is the identifier used for signing
	ID string
	// Entitlements are optional entitlements that can be embedded into the code
	// signature. They need to be in the XML-based property list format
	Entitlements []byte
	// DEREntitlements are entitlements in the DER format
	DEREntitlements []byte
}

func FindCodeSigCmd(f *macho.File) (CodeSigCmd, bool) {
	get32 := f.ByteOrder.Uint32
	for _, l := range f.Loads {
		data := l.Raw()
		cmd := get32(data)
		if cmd == LC_CODE_SIGNATURE {
			return CodeSigCmd{
				cmd,
				get32(data[4:]),
				get32(data[8:]),
				get32(data[12:]),
			}, true
		}
	}
	return CodeSigCmd{}, false
}

func put32be(b []byte, x uint32) []byte { binary.BigEndian.PutUint32(b, x); return b[4:] }
func put64be(b []byte, x uint64) []byte { binary.BigEndian.PutUint64(b, x); return b[8:] }
func put8(b []byte, x uint8) []byte     { b[0] = x; return b[1:] }
func puts(b, s []byte) []byte           { n := copy(b, s); return b[n:] }

func read32be(b []byte) (uint32, []byte) { x := binary.BigEndian.Uint32(b); return x, b[4:] }
func read64be(b []byte) (uint64, []byte) { x := binary.BigEndian.Uint64(b); return x, b[8:] }
func read8(b []byte) (uint8, []byte)     { x := b[0]; return x, b[1:] }
func read(b, s []byte) []byte            { n := copy(s, b); return b[n:] }

// Size computes the size of the code signature.
// id is the identifier used for signing (a field in CodeDirectory blob, which
// has no significance in ad-hoc signing).
// entitlements are optional entitlements.
func Size(codeSize int64, opts Options) (sz int64) {
	// number of regular slots, based on the code size
	nslots := (codeSize + pageSize - 1) / pageSize

	// number of special slots (only the entitlement currently, if specified)
	nspecial := int64(0)
	if opts.DEREntitlements != nil {
		nspecial = CSSLOT_DER_ENTITLEMENTS
	} else if opts.Entitlements != nil {
		nspecial = CSSLOT_ENTITLEMENTS
	} else {
		nspecial = CSSLOT_REQUIREMENTS
	}

	nblobs := uint32(3)
	if opts.Entitlements != nil {
		nblobs++
	}
	if opts.DEREntitlements != nil {
		nblobs++
	}

	// calculate offset based on fixed size and variable parts
	sz = int64(sbsize(nblobs)) // super blob + blob index per blob

	// code directory
	sz += cdsize(nslots, nspecial, opts.ID)

	// requirements
	sz += int64(genericBlobSize) // generic blob for requirements
	sz += 4                      // empty requirements, future use

	// XML entitlements
	if opts.Entitlements != nil {
		sz += int64(genericBlobSize) // generic blob for entitlements
		sz += int64(len(opts.Entitlements))
	}

	// DER entitlements
	if opts.DEREntitlements != nil {
		sz += int64(genericBlobSize) // generic blob for entitlements
		sz += int64(len(opts.DEREntitlements))
	}

	// (empty) certificate blob wrapper
	sz += int64(genericBlobSize) // generic blob for empty certificate

	return sz
}

func cdsize(nslots, nspecial int64, id string) (sz int64) {
	// fixed size
	sz = codeDirectorySize

	sz += int64(len(id) + 1)                // includes a null byte for termination
	sz += (nslots + nspecial) * sha256.Size // size of hashes

	return sz
}

// Sign generates an ad-hoc code signature and writes it to out.
// out must have length of Size(codeSize, opts).
// data is the file content without the signature, of size codeSize.
// textOff and textSize is the file offset and size of the text segment.
// isMain is true if this is a main executable.
// id is the identifier used for signing (a field in CodeDirectory blob, which
// has no significance in ad-hoc signing).
func Sign(out []byte, data io.Reader, codeSize, textOff, textSize int64, isMain bool, opts Options) {
	// number of regular slots, based on the code size
	nslots := (codeSize + pageSize - 1) / pageSize

	// number of special slots (only the entitlement currently, if specified)
	nspecial := int64(0)
	if opts.DEREntitlements != nil {
		nspecial = CSSLOT_DER_ENTITLEMENTS
	} else if opts.Entitlements != nil {
		nspecial = CSSLOT_ENTITLEMENTS
	} else {
		nspecial = CSSLOT_REQUIREMENTS
	}

	off := uint32(0)
	idOff := int64(codeDirectorySize)
	hashOff := idOff + int64(len(opts.ID)+1) + nspecial*sha256.Size
	sz := len(out)

	nblobs := uint32(3)
	if opts.Entitlements != nil {
		nblobs++
	}
	if opts.DEREntitlements != nil {
		nblobs++
	}

	// prepare blobs
	sb := SuperBlob{
		magic:  CSMAGIC_EMBEDDED_SIGNATURE,
		length: uint32(sz),
		count:  nblobs,
		blobs:  make(map[uint32]*GenericBlob, nblobs),
	}
	off += sbsize(nblobs)

	// code directory
	sb.cdir = &CodeDirectory{
		magic:         CSMAGIC_CODEDIRECTORY,
		length:        uint32(cdsize(nslots, nspecial, opts.ID)),
		version:       0x20400,
		flags:         0x20002, // adhoc | linkerSigned
		hashOffset:    uint32(hashOff),
		identOffset:   uint32(idOff),
		nSpecialSlots: uint32(nspecial),
		nCodeSlots:    uint32(nslots),
		codeLimit:     uint32(codeSize),
		hashSize:      sha256.Size,
		hashType:      CS_HASHTYPE_SHA256,
		pageSize:      uint8(pageSizeBits),
		execSegBase:   uint64(textOff),
		execSegLimit:  uint64(textSize),
	}
	if isMain {
		sb.cdir.execSegFlags = CS_EXECSEG_MAIN_BINARY
	}
	sb.index = append(sb.index, BlobIndex{
		typ:    CSSLOT_CODEDIRECTORY,
		offset: off,
	})
	off += sb.cdir.length

	// (empty) requirements
	sb.add(&off, CSMAGIC_REQUIREMENTS, CSSLOT_REQUIREMENTS, []byte{0, 0, 0, 0}) // empty requirements

	// entitlements blob index
	if opts.Entitlements != nil {
		sb.add(&off, CSMAGIC_EMBEDDED_ENTITLEMENTS, CSSLOT_ENTITLEMENTS, []byte(opts.Entitlements))
	}

	// DER entitlements blob index
	if opts.DEREntitlements != nil {
		sb.add(&off, CSMAGIC_EMBEDDED_DER_ENTITLEMENTS, CSSLOT_DER_ENTITLEMENTS, []byte(opts.DEREntitlements))
	}

	// (empty) certificate blob wrapper for future use. we are using an ad-hoc
	// certificate, therefore this block is empty
	sb.add(&off, CSMAGIC_BLOBWRAPPER, CSSLOT_SIGNATURESLOT, nil)

	// start emitting
	outp := out
	outp = sb.put(outp)

	// output the code directory, including identifier
	outp = sb.cdir.put(outp)
	outp = puts(outp, []byte(opts.ID+"\000"))

	// emit special slots (empty for now) in reverse order, so that we arrive at
	// index "0" for the regular hashes.
	h := sha256.New()
	for i := -int(sb.cdir.nSpecialSlots); i < 0; i++ {
		blob := sb.blobs[uint32(-i)]
		if blob != nil {
			outp = puts(outp, blob.digest(h))
		} else {
			outp = puts(outp, make([]byte, sb.cdir.hashSize))
		}
	}

	// emit hashes
	var buf [pageSize]byte
	p := 0
	for p < int(codeSize) {
		n, err := io.ReadFull(data, buf[:])
		if err == io.EOF {
			break
		}
		if err != nil && err != io.ErrUnexpectedEOF {
			panic(err)
		}
		if p+n > int(codeSize) {
			n = int(codeSize) - p
		}
		p += n
		h.Reset()
		h.Write(buf[:n])
		b := h.Sum(nil)
		outp = puts(outp, b[:])
	}

	// emit remaining blobs sorted by slot index
	// TODO(oxisto): can we use maps.Values instead?
	slots := make([]int, 0, len(sb.blobs))
	for s := range sb.blobs {
		slots = append(slots, int(s))
	}
	sort.Ints(slots)
	for _, s := range slots {
		outp = sb.blobs[uint32(s)].put(outp)
	}
}

const (
	machHeaderSize32 = 7 * 4
	machHeaderSize64 = 8 * 4
	maxUint32        = ^uint32(0)
)

var maxIntValue = int64(^uint(0) >> 1)

func main() {
	binPath := flag.String("bin", "", "path to the Mach-O binary to sign")
	entPath := flag.String("entitlements", "", "path to the XML entitlements plist")
	idOverride := flag.String("id", "", "override the CodeDirectory identifier")
	derEntPath := flag.String("der-entitlements", "", "optional path to DER entitlements data")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s -bin <mach-o> -entitlements <plist> [flags]\n", filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
	flag.Parse()

	if *binPath == "" && flag.NArg() > 0 {
		*binPath = flag.Arg(0)
	}
	if *entPath == "" && flag.NArg() > 1 {
		*entPath = flag.Arg(1)
	}
	if *binPath == "" || *entPath == "" {
		flag.Usage()
		os.Exit(2)
	}

	entitlements, err := os.ReadFile(*entPath)
	if err != nil {
		log.Fatalf("read entitlements: %v", err)
	}
	var derEnt []byte
	if *derEntPath != "" {
		derEnt, err = os.ReadFile(*derEntPath)
		if err != nil {
			log.Fatalf("read DER entitlements: %v", err)
		}
	}

	machoFile, err := os.OpenFile(*binPath, os.O_RDWR, 0)
	if err != nil {
		log.Fatalf("open Mach-O binary: %v", err)
	}
	defer machoFile.Close()

	mf, err := macho.NewFile(machoFile)
	if err != nil {
		log.Fatalf("parse Mach-O binary: %v", err)
	}
	defer mf.Close()

	sigCmd, ok := FindCodeSigCmd(mf)
	if !ok {
		log.Fatalf("Mach-O binary does not contain an LC_CODE_SIGNATURE load command")
	}
	if sigCmd.Dataoff == 0 {
		log.Fatalf("LC_CODE_SIGNATURE load command reserves no space for signature data")
	}

	_, opts, err := sigCmd.ReadSuperBlob(machoFile)
	if err != nil {
		log.Fatalf("read existing code signature: %v", err)
	}
	if *idOverride != "" {
		opts.ID = *idOverride
	}
	if opts.ID == "" {
		opts.ID = filepath.Base(*binPath)
	}
	opts.Entitlements = entitlements
	if len(derEnt) != 0 {
		opts.DEREntitlements = derEnt
	}

	textOff, textSize, err := textSegmentInfo(mf)
	if err != nil {
		log.Fatalf("locate __TEXT segment: %v", err)
	}

	codeSize := int64(sigCmd.Dataoff)
	if codeSize <= 0 {
		log.Fatalf("invalid LC_CODE_SIGNATURE data offset %d", sigCmd.Dataoff)
	}

	sigSize := Size(codeSize, opts)
	if sigSize <= 0 {
		log.Fatalf("computed signature size is invalid (%d)", sigSize)
	}
	if sigSize > maxIntValue {
		log.Fatalf("code signature too large (%d bytes)", sigSize)
	}
	if sigSize > int64(maxUint32) {
		log.Fatalf("code signature size (%d bytes) exceeds uint32 limit", sigSize)
	}
	if codeSize > int64(maxUint32) {
		log.Fatalf("code section size (%d bytes) exceeds uint32 limit", codeSize)
	}

	fileInfo, err := machoFile.Stat()
	if err != nil {
		log.Fatalf("stat Mach-O binary: %v", err)
	}

	linkedit := mf.Segment("__LINKEDIT")
	if linkedit == nil {
		log.Fatalf("Mach-O binary is missing the __LINKEDIT segment")
	}

	oldSigSize := int64(sigCmd.Datasize)
	sigStart := int64(sigCmd.Dataoff)
	oldSigEnd := sigStart + oldSigSize
	newSigEnd := sigStart + sigSize
	fileEnd := fileInfo.Size()

	if sigSize != oldSigSize && oldSigEnd != fileEnd {
		log.Fatalf("code signature (%d bytes) is not located at the end of file, cannot resize", oldSigSize)
	}

	linkeditEnd := int64(linkedit.Offset) + int64(linkedit.Filesz)
	if linkeditEnd < oldSigEnd {
		log.Fatalf("__LINKEDIT segment too small: end=%d, signature end=%d", linkeditEnd, oldSigEnd)
	}
	if sigSize != oldSigSize && linkeditEnd != oldSigEnd {
		log.Fatalf("code signature is not the tail of __LINKEDIT (segment end=%d, signature end=%d)", linkeditEnd, oldSigEnd)
	}

	delta := sigSize - oldSigSize
	if delta != 0 {
		newFilesz := int64(linkedit.Filesz) + delta
		newMemsz := int64(linkedit.Memsz) + delta
		if newFilesz < 0 || newMemsz < 0 {
			log.Fatalf("__LINKEDIT size would become negative after resize (filesz=%d, memsz=%d, delta=%d)", linkedit.Filesz, linkedit.Memsz, delta)
		}
		if err := rewriteLoadCommands(machoFile, mf.FileHeader, mf.ByteOrder, uint32(sigStart), uint32(sigSize), uint64(newFilesz), uint64(newMemsz)); err != nil {
			log.Fatalf("update load commands: %v", err)
		}
	}

	switch {
	case delta > 0:
		if err := machoFile.Truncate(newSigEnd); err != nil {
			log.Fatalf("extend Mach-O binary: %v", err)
		}
	case delta < 0 && oldSigEnd == fileEnd:
		if err := machoFile.Truncate(newSigEnd); err != nil {
			log.Fatalf("shrink Mach-O binary: %v", err)
		}
	}

	reader := io.NewSectionReader(machoFile, 0, codeSize)
	sigBuf := make([]byte, sigSize)
	Sign(sigBuf, reader, codeSize, textOff, textSize, mf.Type == macho.TypeExec, opts)

	if _, err := machoFile.WriteAt(sigBuf, sigStart); err != nil {
		log.Fatalf("write code signature: %v", err)
	}
	if delta < 0 && oldSigEnd != fileEnd {
		if err := zeroRange(machoFile, sigStart+sigSize, oldSigSize-sigSize); err != nil {
			log.Fatalf("clear unused signature bytes: %v", err)
		}
	}

	slog.Info("code signing completed",
		"path", *binPath,
		"entitlements", *entPath,
	)
}

func textSegmentInfo(f *macho.File) (int64, int64, error) {
	seg := f.Segment("__TEXT")
	if seg == nil {
		return 0, 0, fmt.Errorf("missing __TEXT segment")
	}
	return int64(seg.Offset), int64(seg.Filesz), nil
}

func rewriteLoadCommands(file *os.File, hdr macho.FileHeader, order binary.ByteOrder, sigOffset, sigSize uint32, linkeditFilesz, linkeditMemsz uint64) error {
	headerSize, err := machoHeaderSize(hdr.Magic)
	if err != nil {
		return err
	}
	if int64(hdr.Cmdsz) > maxIntValue {
		return fmt.Errorf("load commands too large (%d bytes)", hdr.Cmdsz)
	}

	loadBytes := make([]byte, hdr.Cmdsz)
	if _, err := file.ReadAt(loadBytes, headerSize); err != nil {
		return fmt.Errorf("read load commands: %w", err)
	}

	var sawCodeSig bool
	var sawLinkedit bool
	for off := uint32(0); off < hdr.Cmdsz; {
		if off+8 > hdr.Cmdsz {
			return fmt.Errorf("truncated load command at offset %d", off)
		}
		cmd := order.Uint32(loadBytes[off:])
		cmdsize := order.Uint32(loadBytes[off+4:])
		if cmdsize < 8 || off+cmdsize > hdr.Cmdsz {
			return fmt.Errorf("invalid load command size %d at offset %d", cmdsize, off)
		}

		switch macho.LoadCmd(cmd) {
		case LC_CODE_SIGNATURE:
			order.PutUint32(loadBytes[off+8:], sigOffset)
			order.PutUint32(loadBytes[off+12:], sigSize)
			sawCodeSig = true

		case macho.LoadCmdSegment:
			if parseSegmentName(loadBytes[off+8:off+24]) == "__LINKEDIT" {
				if linkeditFilesz > uint64(maxUint32) || linkeditMemsz > uint64(maxUint32) {
					return fmt.Errorf("__LINKEDIT sizes exceed 32-bit limit")
				}
				order.PutUint32(loadBytes[off+28:], uint32(linkeditMemsz))
				order.PutUint32(loadBytes[off+36:], uint32(linkeditFilesz))
				sawLinkedit = true
			}

		case macho.LoadCmdSegment64:
			if parseSegmentName(loadBytes[off+8:off+24]) == "__LINKEDIT" {
				order.PutUint64(loadBytes[off+32:], linkeditMemsz)
				order.PutUint64(loadBytes[off+48:], linkeditFilesz)
				sawLinkedit = true
			}
		}

		off += cmdsize
	}

	if !sawCodeSig {
		return fmt.Errorf("LC_CODE_SIGNATURE load command not found")
	}
	if !sawLinkedit {
		return fmt.Errorf("__LINKEDIT segment load command not found")
	}

	if _, err := file.WriteAt(loadBytes, headerSize); err != nil {
		return fmt.Errorf("write load commands: %w", err)
	}

	return nil
}

func machoHeaderSize(magic uint32) (int64, error) {
	switch magic {
	case macho.Magic64:
		return machHeaderSize64, nil
	case macho.Magic32:
		return machHeaderSize32, nil
	default:
		return 0, fmt.Errorf("unsupported Mach-O magic %#x", magic)
	}
}

func zeroRange(f *os.File, off int64, n int64) error {
	if n <= 0 {
		return nil
	}
	buf := make([]byte, pageSize)
	for n > 0 {
		chunk := int64(len(buf))
		if chunk > n {
			chunk = n
		}
		if _, err := f.WriteAt(buf[:chunk], off); err != nil {
			return err
		}
		off += chunk
		n -= chunk
	}
	return nil
}

func parseSegmentName(b []byte) string {
	if idx := bytes.IndexByte(b, 0); idx >= 0 {
		b = b[:idx]
	}
	return string(b)
}
