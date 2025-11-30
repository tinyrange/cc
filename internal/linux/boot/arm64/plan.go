package arm64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	dtbAlignment           = 0x8
	initrdAlignment        = 0x1000
	stackGuardBytes        = 0x2000
	fdtHeaderSize          = 0x28
	fdtVersion             = 17
	fdtLastCompVer         = 16
	fdtMagic        uint32 = 0xd00dfeed

	fdtBeginNode uint32 = 0x1
	fdtEndNode   uint32 = 0x2
	fdtProp      uint32 = 0x3
	fdtEnd       uint32 = 0x9
)

// BootOptions describes how the ARM64 kernel should be placed into guest RAM.
type BootOptions struct {
	Cmdline string

	Initrd        []byte
	InitrdGPA     uint64
	DeviceTreeGPA uint64
	StackTopGPA   uint64

	NumCPUs int
	UART    *UARTConfig
}

func (o BootOptions) withDefaults() BootOptions {
	out := o
	if out.NumCPUs <= 0 {
		out.NumCPUs = 1
	}
	return out
}

// UARTConfig describes an optional ns16550-compatible console.
type UARTConfig struct {
	Base     uint64
	Size     uint64
	ClockHz  uint32
	RegShift uint32
	BaudRate uint32
}

// BootPlan captures the derived addresses needed to enter the kernel.
type BootPlan struct {
	EntryGPA      uint64
	StackTopGPA   uint64
	DeviceTreeGPA uint64
}

// Prepare loads the kernel payload and supporting blobs into guest RAM and
// derives the state required to enter the kernel.
func (k *KernelImage) Prepare(vm hv.VirtualMachine, opts BootOptions) (*BootPlan, error) {
	if vm == nil || vm.MemorySize() == 0 {
		return nil, errors.New("arm64 prepare requires a virtual machine")
	}
	if k == nil || len(k.Payload()) == 0 {
		return nil, errors.New("arm64 kernel payload is empty")
	}

	opts = opts.withDefaults()

	memStart := vm.MemoryBase()
	memSize := vm.MemorySize()
	memEnd := memStart + memSize

	base := alignUp(memStart, imageLoadAlignment)
	loadAddr := base + k.Header.TextOffset
	if loadAddr < memStart {
		return nil, fmt.Errorf("arm64 kernel load address %#x below RAM base %#x", loadAddr, memStart)
	}

	payload := k.Payload()
	kernelEnd := loadAddr + uint64(len(payload))
	if kernelEnd > memEnd {
		return nil, fmt.Errorf("arm64 kernel [%#x, %#x) outside RAM [%#x, %#x)", loadAddr, kernelEnd, memStart, memEnd)
	}

	if err := writeGuest(vm, loadAddr, payload); err != nil {
		return nil, fmt.Errorf("write arm64 kernel payload: %w", err)
	}

	var initrdStart, initrdEnd uint64
	if len(opts.Initrd) > 0 {
		initrdStart = opts.InitrdGPA
		if initrdStart == 0 {
			initrdStart = alignUp(kernelEnd, initrdAlignment)
		}
		initrdEnd = initrdStart + uint64(len(opts.Initrd))
		if initrdStart < memStart || initrdEnd > memEnd {
			return nil, fmt.Errorf("initrd [%#x, %#x) outside RAM [%#x, %#x)", initrdStart, initrdEnd, memStart, memEnd)
		}
		if err := writeGuest(vm, initrdStart, opts.Initrd); err != nil {
			return nil, fmt.Errorf("write initrd: %w", err)
		}
	}

	dtbConfig := deviceTreeConfig{
		MemoryBase:  memStart,
		MemorySize:  memSize,
		NumCPUs:     opts.NumCPUs,
		Cmdline:     opts.Cmdline,
		InitrdStart: initrdStart,
		InitrdEnd:   initrdEnd,
		UART:        opts.UART,
	}
	dtb, err := buildDeviceTree(dtbConfig)
	if err != nil {
		return nil, fmt.Errorf("build device tree: %w", err)
	}

	dtbAddr := opts.DeviceTreeGPA
	if dtbAddr == 0 {
		allocBase := kernelEnd
		if initrdEnd > allocBase {
			allocBase = initrdEnd
		}
		dtbAddr = alignUp(allocBase, dtbAlignment)
	}
	dtbEnd := dtbAddr + uint64(len(dtb))
	if dtbAddr < memStart || dtbEnd > memEnd {
		return nil, fmt.Errorf("device tree [%#x, %#x) outside RAM [%#x, %#x)", dtbAddr, dtbEnd, memStart, memEnd)
	}
	if err := writeGuest(vm, dtbAddr, dtb); err != nil {
		return nil, fmt.Errorf("write device tree: %w", err)
	}

	stackTop := opts.StackTopGPA
	if stackTop == 0 {
		stackTop = alignDown(memEnd, 16)
	}
	if stackTop <= dtbEnd+stackGuardBytes {
		return nil, fmt.Errorf("stack top %#x overlaps device tree ending at %#x", stackTop, dtbEnd)
	}

	entry, err := k.Header.EntryPoint(base)
	if err != nil {
		return nil, fmt.Errorf("arm64 entry point: %w", err)
	}

	return &BootPlan{
		EntryGPA:      entry,
		StackTopGPA:   stackTop,
		DeviceTreeGPA: dtbAddr,
	}, nil
}

// ConfigureVCPU programs the first vCPU for entry into the Linux kernel.
func (p *BootPlan) ConfigureVCPU(vcpu hv.VirtualCPU) error {
	if p == nil {
		return errors.New("arm64 boot plan is nil")
	}
	if vcpu == nil {
		return errors.New("arm64 configure requires a vCPU")
	}
	if p.DeviceTreeGPA == 0 {
		return errors.New("arm64 device tree GPA is zero")
	}

	regs := map[hv.Register]hv.RegisterValue{
		hv.RegisterARM64Pc:     hv.Register64(p.EntryGPA),
		hv.RegisterARM64Sp:     hv.Register64(p.StackTopGPA),
		hv.RegisterARM64X0:     hv.Register64(p.DeviceTreeGPA),
		hv.RegisterARM64X1:     hv.Register64(0),
		hv.RegisterARM64X2:     hv.Register64(0),
		hv.RegisterARM64X3:     hv.Register64(0),
		hv.RegisterARM64Pstate: hv.Register64(defaultPstateBits),
	}
	if err := vcpu.SetRegisters(regs); err != nil {
		return fmt.Errorf("set arm64 registers: %w", err)
	}
	return nil
}

const (
	pstateModeEL1h    = 0x5
	pstateDF          = 0x200
	pstateAF          = 0x100
	pstateIF          = 0x80
	pstateFF          = 0x40
	defaultPstateBits = pstateModeEL1h | pstateDF | pstateAF | pstateIF | pstateFF
)

type deviceTreeConfig struct {
	MemoryBase  uint64
	MemorySize  uint64
	NumCPUs     int
	Cmdline     string
	InitrdStart uint64
	InitrdEnd   uint64
	UART        *UARTConfig
}

func buildDeviceTree(cfg deviceTreeConfig) ([]byte, error) {
	if cfg.MemorySize == 0 {
		return nil, errors.New("device tree requires non-zero RAM size")
	}
	if cfg.NumCPUs <= 0 {
		return nil, errors.New("device tree requires at least one CPU")
	}

	b := newFDTBuilder()

	b.beginNode("")
	b.propU32("#address-cells", 2)
	b.propU32("#size-cells", 2)
	b.propStrings("compatible", "tinyrange,cc-arm64", "tinyrange,cc")
	b.propStrings("model", "tinyrange-cc")

	b.beginNode("cpus")
	b.propU32("#address-cells", 2)
	b.propU32("#size-cells", 0)
	for cpu := 0; cpu < cfg.NumCPUs; cpu++ {
		name := fmt.Sprintf("cpu@%d", cpu)
		b.beginNode(name)
		b.propStrings("device_type", "cpu")
		b.propStrings("compatible", "arm,armv8")
		b.propU64("reg", uint64(cpu))
		b.propStrings("enable-method", "psci")
		b.endNode()
	}
	b.endNode()

	memNodeName := fmt.Sprintf("memory@%x", cfg.MemoryBase)
	b.beginNode(memNodeName)
	b.propStrings("device_type", "memory")
	b.propU64("reg", cfg.MemoryBase, cfg.MemorySize)
	b.endNode()

	stdoutAlias := ""
	stdoutBaud := uint32(0)
	if cfg.UART != nil {
		if cfg.UART.Size == 0 {
			return nil, errors.New("uart config requires non-zero size")
		}
		serialNodeName := fmt.Sprintf("serial@%x", cfg.UART.Base)
		serialPath := fmt.Sprintf("/%s", serialNodeName)
		b.beginNode(serialNodeName)
		b.propStrings("compatible", "ns16550a")
		b.propU64("reg", cfg.UART.Base, cfg.UART.Size)
		if cfg.UART.ClockHz != 0 {
			b.propU32("clock-frequency", cfg.UART.ClockHz)
		}
		if cfg.UART.RegShift != 0 {
			b.propU32("reg-shift", cfg.UART.RegShift)
		}
		b.propU32("reg-io-width", 1)
		b.propStrings("status", "okay")
		b.endNode()

		b.beginNode("aliases")
		b.propStrings("serial0", serialPath)
		b.endNode()

		stdoutAlias = "serial0"
		stdoutBaud = cfg.UART.BaudRate
	}

	b.beginNode("chosen")
	if cfg.Cmdline != "" {
		b.propStrings("bootargs", cfg.Cmdline)
	}
	if cfg.InitrdEnd > cfg.InitrdStart {
		b.propU64("linux,initrd-start", cfg.InitrdStart)
		b.propU64("linux,initrd-end", cfg.InitrdEnd)
	}
	if stdoutAlias != "" {
		baud := stdoutBaud
		if baud == 0 {
			baud = 115200
		}
		stdout := fmt.Sprintf("%s:%dn8", stdoutAlias, baud)
		b.propStrings("stdout-path", stdout)
		b.propStrings("linux,stdout-path", stdout)
	}
	b.endNode()

	b.beginNode("psci")
	b.propStrings("compatible", "arm,psci-0.2", "arm,psci")
	b.propStrings("method", "hvc")
	b.endNode()

	b.endNode() // root

	return b.finish()
}

type fdtBuilder struct {
	structBuf  bytes.Buffer
	strings    bytes.Buffer
	stringsOff map[string]uint32
}

func newFDTBuilder() *fdtBuilder {
	return &fdtBuilder{stringsOff: make(map[string]uint32)}
}

func (b *fdtBuilder) beginNode(name string) {
	b.writeToken(fdtBeginNode)
	b.structBuf.WriteString(name)
	b.structBuf.WriteByte(0)
	b.padStruct()
}

func (b *fdtBuilder) endNode() {
	b.writeToken(fdtEndNode)
}

func (b *fdtBuilder) propStrings(name string, values ...string) {
	var buf bytes.Buffer
	for _, v := range values {
		buf.WriteString(v)
		buf.WriteByte(0)
	}
	b.property(name, buf.Bytes())
}

func (b *fdtBuilder) propU32(name string, values ...uint32) {
	data := make([]byte, 0, len(values)*4)
	for _, v := range values {
		var tmp [4]byte
		binary.BigEndian.PutUint32(tmp[:], v)
		data = append(data, tmp[:]...)
	}
	b.property(name, data)
}

func (b *fdtBuilder) propU64(name string, values ...uint64) {
	data := make([]byte, 0, len(values)*8)
	for _, v := range values {
		var tmp [8]byte
		binary.BigEndian.PutUint64(tmp[:], v)
		data = append(data, tmp[:]...)
	}
	b.property(name, data)
}

func (b *fdtBuilder) property(name string, value []byte) {
	b.writeToken(fdtProp)
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], uint32(len(value)))
	b.structBuf.Write(tmp[:])
	binary.BigEndian.PutUint32(tmp[:], b.stringOffset(name))
	b.structBuf.Write(tmp[:])
	b.structBuf.Write(value)
	b.padStruct()
}

func (b *fdtBuilder) finish() ([]byte, error) {
	b.writeToken(fdtEnd)
	b.padStruct()

	structBytes := b.structBuf.Bytes()
	stringsBytes := b.strings.Bytes()

	memReserve := make([]byte, 16) // single terminating entry

	offMemReserve := fdtHeaderSize
	offStruct := offMemReserve + len(memReserve)
	offStrings := offStruct + len(structBytes)
	totalSize := offStrings + len(stringsBytes)

	blob := make([]byte, totalSize)
	header := blob[:fdtHeaderSize]
	binary.BigEndian.PutUint32(header[0:4], fdtMagic)
	binary.BigEndian.PutUint32(header[4:8], uint32(totalSize))
	binary.BigEndian.PutUint32(header[8:12], uint32(offStruct))
	binary.BigEndian.PutUint32(header[12:16], uint32(offStrings))
	binary.BigEndian.PutUint32(header[16:20], uint32(offMemReserve))
	binary.BigEndian.PutUint32(header[20:24], fdtVersion)
	binary.BigEndian.PutUint32(header[24:28], fdtLastCompVer)
	binary.BigEndian.PutUint32(header[28:32], 0) // boot_cpuid_phys
	binary.BigEndian.PutUint32(header[32:36], uint32(len(stringsBytes)))
	binary.BigEndian.PutUint32(header[36:40], uint32(len(structBytes)))

	copy(blob[offMemReserve:], memReserve)
	copy(blob[offStruct:], structBytes)
	copy(blob[offStrings:], stringsBytes)

	return blob, nil
}

func (b *fdtBuilder) stringOffset(name string) uint32 {
	if off, ok := b.stringsOff[name]; ok {
		return off
	}
	off := uint32(b.strings.Len())
	b.strings.WriteString(name)
	b.strings.WriteByte(0)
	b.stringsOff[name] = off
	return off
}

func (b *fdtBuilder) writeToken(token uint32) {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], token)
	b.structBuf.Write(tmp[:])
}

func (b *fdtBuilder) padStruct() {
	for b.structBuf.Len()%4 != 0 {
		b.structBuf.WriteByte(0)
	}
}

func alignUp(value, align uint64) uint64 {
	if align == 0 {
		return value
	}
	mask := align - 1
	return (value + mask) &^ mask
}

func alignDown(value, align uint64) uint64 {
	if align == 0 {
		return value
	}
	mask := align - 1
	return value &^ mask
}

func writeGuest(vm hv.VirtualMachine, guestAddr uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	memStart := vm.MemoryBase()
	memEnd := memStart + vm.MemorySize()
	if guestAddr < memStart || guestAddr+uint64(len(data)) > memEnd {
		return fmt.Errorf("guest address range [%#x, %#x) outside RAM [%#x, %#x)", guestAddr, guestAddr+uint64(len(data)), memStart, memEnd)
	}
	if guestAddr > math.MaxInt64 {
		return fmt.Errorf("guest address %#x out of host range", guestAddr)
	}
	if _, err := vm.WriteAt(data, int64(guestAddr)); err != nil {
		return fmt.Errorf("write guest memory at %#x: %w", guestAddr, err)
	}
	return nil
}
