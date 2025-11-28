package boot

import (
	"bufio"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	pagePresent = 1 << 0
	pageHuge    = 1 << 7
)

// StackFrame captures a single unwound frame from the guest kernel.
type StackFrame struct {
	Index  int
	PC     uint64
	Symbol string
	Offset uint64
}

// CaptureStackTrace walks the current guest stack and symbolises the frames using
// symbols from either vmlinux or (optionally) a System.map. The return slice always
// contains at least the current RIP when no fatal error occurs. maxFrames bounds
// the number of frames collected; if zero or negative a default of 16 is used.
// Symbol resolution assumes KASLR is disabled (for example by building without
// CONFIG_RANDOMIZE_BASE or booting with nokaslr).
func CaptureStackTrace(vcpu hv.VirtualCPU, vmlinux io.ReaderAt, systemMap io.ReaderAt, maxFrames int) ([]StackFrame, error) {
	if vcpu == nil {
		return nil, errors.New("vcpu is nil")
	}
	if vmlinux == nil && systemMap == nil {
		return nil, errors.New("both vmlinux and system map readers are nil")
	}
	if maxFrames <= 0 {
		maxFrames = 16
	}

	regs := map[hv.Register]hv.RegisterValue{
		hv.RegisterAMD64Rbp: hv.Register64(0),
		hv.RegisterAMD64Rip: hv.Register64(0),

		hv.RegisterAMD64Cr3: hv.Register64(0),
	}
	if err := vcpu.GetRegisters(regs); err != nil {
		return nil, fmt.Errorf("get registers: %w", err)
	}

	var symtab *symbolTable
	var err error
	var symErr error
	if vmlinux != nil {
		symtab, err = loadSymbolTable(vmlinux, readELFSymbols)
		if err != nil {
			symErr = fmt.Errorf("load vmlinux symbols: %w", err)
		}
	}
	if (symtab == nil || symErr != nil) && systemMap != nil {
		symtab, err = loadSymbolTable(systemMap, readSystemMapSymbols)
		if err != nil {
			if symErr != nil {
				return nil, fmt.Errorf("load system map symbols: %w (fallback after %v)", err, symErr)
			}
			return nil, fmt.Errorf("load system map symbols: %w", err)
		}
		symErr = nil
	}
	if symtab == nil {
		if symErr != nil {
			return nil, symErr
		}
		return nil, errors.New("no symbol source available")
	}

	walker := pageWalker{
		vm:  vcpu.VirtualMachine(),
		cr3: uint64(regs[hv.RegisterAMD64Cr3].(hv.Register64)),
	}

	var frames []StackFrame
	addFrame := func(addr uint64) {
		name, off, ok := symtab.lookup(addr)
		if !ok {
			name = "??"
		}
		frame := StackFrame{
			Index:  len(frames),
			PC:     addr,
			Symbol: name,
			Offset: off,
		}
		frames = append(frames, frame)
	}

	addFrame(uint64(regs[hv.RegisterAMD64Rip].(hv.Register64)))

	currentFP := uint64(regs[hv.RegisterAMD64Rbp].(hv.Register64))
	seen := 0
	var walkErr error
	for len(frames) < maxFrames {
		if currentFP == 0 {
			break
		}
		if !isCanonical(currentFP) {
			break
		}

		retAddr, err := walker.readUint64(currentFP + 8)
		if err != nil {
			walkErr = fmt.Errorf("read return address @%#x: %w", currentFP+8, err)
			break
		}
		prevFP, err := walker.readUint64(currentFP)
		if err != nil {
			walkErr = fmt.Errorf("read frame pointer @%#x: %w", currentFP, err)
			break
		}
		if retAddr == 0 || !isCanonical(retAddr) {
			break
		}
		addFrame(retAddr)
		if prevFP <= currentFP {
			break
		}
		if prevFP-currentFP > 0x100000 {
			walkErr = fmt.Errorf("frame pointer jump %#x -> %#x too large", currentFP, prevFP)
			break
		}
		currentFP = prevFP
		seen++
		if seen > maxFrames {
			break
		}
	}

	return frames, walkErr
}

type pageWalker struct {
	vm  hv.VirtualMachine
	cr3 uint64
}

func (w pageWalker) translate(virt uint64) (uint64, error) {
	if !isCanonical(virt) {
		return 0, fmt.Errorf("non-canonical virtual address %#x", virt)
	}

	pml4Base := w.cr3 &^ 0xFFF
	pml4Entry, err := w.readPhysUint64(pml4Base + ((virt>>39)&0x1FF)*8)
	if err != nil {
		return 0, fmt.Errorf("read PML4 entry: %w", err)
	}
	if pml4Entry&pagePresent == 0 {
		return 0, fmt.Errorf("PML4 entry not present for %#x", virt)
	}

	pdptBase := pml4Entry &^ 0xFFF
	pdptEntry, err := w.readPhysUint64(pdptBase + ((virt>>30)&0x1FF)*8)
	if err != nil {
		return 0, fmt.Errorf("read PDPT entry: %w", err)
	}
	if pdptEntry&pagePresent == 0 {
		return 0, fmt.Errorf("PDPT entry not present for %#x", virt)
	}
	if pdptEntry&pageHuge != 0 {
		offset := virt & ((1 << 30) - 1)
		return (pdptEntry &^ ((1 << 30) - 1)) + offset, nil
	}

	pdBase := pdptEntry &^ 0xFFF
	pdEntry, err := w.readPhysUint64(pdBase + ((virt>>21)&0x1FF)*8)
	if err != nil {
		return 0, fmt.Errorf("read PD entry: %w", err)
	}
	if pdEntry&pagePresent == 0 {
		return 0, fmt.Errorf("PD entry not present for %#x", virt)
	}
	if pdEntry&pageHuge != 0 {
		offset := virt & ((1 << 21) - 1)
		return (pdEntry &^ ((1 << 21) - 1)) + offset, nil
	}

	ptBase := pdEntry &^ 0xFFF
	ptEntry, err := w.readPhysUint64(ptBase + ((virt>>12)&0x1FF)*8)
	if err != nil {
		return 0, fmt.Errorf("read PT entry: %w", err)
	}
	if ptEntry&pagePresent == 0 {
		return 0, fmt.Errorf("page table entry not present for %#x", virt)
	}
	offset := virt & 0xFFF
	return (ptEntry &^ 0xFFF) + offset, nil
}

func (w pageWalker) readPhysUint64(phys uint64) (uint64, error) {
	data := make([]byte, 8)
	if _, err := w.vm.ReadAt(data, int64(phys)); err != nil {
		return 0, fmt.Errorf("read physical address %#x: %w", phys, err)
	}
	return binary.LittleEndian.Uint64(data), nil
}

func (w pageWalker) readUint64(virt uint64) (uint64, error) {
	phys, err := w.translate(virt)
	if err != nil {
		return 0, err
	}
	return w.readPhysUint64(phys)
}

func isCanonical(addr uint64) bool {
	sign := (addr >> 47) & 1
	if sign == 0 {
		return addr>>48 == 0
	}
	return (addr >> 48) == 0xFFFF
}

type symbol struct {
	addr uint64
	size uint64
	name string
}

type symbolTable struct {
	once sync.Once
	syms []symbol
	err  error
}

var symbolCache sync.Map // map[io.ReaderAt]*symbolTable

func loadSymbolTable(r io.ReaderAt, loader func(io.ReaderAt) ([]symbol, error)) (*symbolTable, error) {
	val, _ := symbolCache.LoadOrStore(r, &symbolTable{})
	tab := val.(*symbolTable)
	tab.once.Do(func() {
		tab.syms, tab.err = loader(r)
	})
	if tab.err != nil {
		return nil, tab.err
	}
	return tab, nil
}

func (t *symbolTable) lookup(addr uint64) (string, uint64, bool) {
	syms := t.syms
	if len(syms) == 0 {
		return "", 0, false
	}
	idx := sort.Search(len(syms), func(i int) bool {
		return syms[i].addr > addr
	})
	if idx == 0 {
		return "", 0, false
	}
	sym := syms[idx-1]
	if sym.size != 0 && addr >= sym.addr+sym.size {
		return "", 0, false
	}
	return sym.name, addr - sym.addr, true
}

func readELFSymbols(r io.ReaderAt) ([]symbol, error) {
	f, err := elf.NewFile(r)
	if err != nil {
		return nil, fmt.Errorf("open vmlinux: %w", err)
	}
	defer f.Close()

	raw, err := f.Symbols()
	if err != nil {
		return nil, fmt.Errorf("read symbols: %w", err)
	}

	funcs := make([]symbol, 0, len(raw))
	for _, sym := range raw {
		if sym.Section == elf.SHN_UNDEF || sym.Value == 0 {
			continue
		}
		typ := elf.ST_TYPE(sym.Info)
		if typ != elf.STT_FUNC && !(typ == elf.STT_NOTYPE && sym.Size != 0) {
			continue
		}
		funcs = append(funcs, symbol{
			addr: sym.Value,
			size: sym.Size,
			name: sym.Name,
		})
	}

	return finalizeSymbols(funcs)
}

func readSystemMapSymbols(r io.ReaderAt) ([]symbol, error) {
	reader := &readerAtStream{r: r}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	var funcs []symbol
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		addr, err := strconv.ParseUint(fields[0], 16, 64)
		if err != nil || addr == 0 {
			continue
		}
		if !isTextSymbolType(fields[1]) {
			continue
		}
		funcs = append(funcs, symbol{
			addr: addr,
			name: fields[2],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read System.map: %w", err)
	}
	return finalizeSymbols(funcs)
}

func finalizeSymbols(funcs []symbol) ([]symbol, error) {
	if len(funcs) == 0 {
		return nil, fmt.Errorf("no function symbols found")
	}

	sort.Slice(funcs, func(i, j int) bool {
		return funcs[i].addr < funcs[j].addr
	})

	for i := range funcs {
		if funcs[i].size == 0 {
			if i+1 < len(funcs) {
				next := funcs[i+1].addr
				if next > funcs[i].addr {
					funcs[i].size = next - funcs[i].addr
				}
			}
		}
	}
	return funcs, nil
}

func isTextSymbolType(field string) bool {
	if field == "" {
		return false
	}
	switch field[0] {
	case 't', 'T', 'w', 'W':
		return true
	default:
		return false
	}
}

type readerAtStream struct {
	r   io.ReaderAt
	off int64
}

func (r *readerAtStream) Read(p []byte) (int, error) {
	n, err := r.r.ReadAt(p, r.off)
	r.off += int64(n)
	return n, err
}

// ClearSymbolCache clears the memoised symbol tables. Intended for tests.
func ClearSymbolCache() {
	symbolCache.Range(func(key, value any) bool {
		symbolCache.Delete(key)
		return true
	})
}
