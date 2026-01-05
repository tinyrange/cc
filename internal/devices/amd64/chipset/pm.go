package chipset

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	pm1aEvtBase uint16 = 0x400
	pm1aEvtSize        = 4

	pm1aCntBase uint16 = 0x404
	pm1aCntSize        = 2

	pmTmrBase uint16 = 0x408
	pmTmrSize        = 4
)

// PM implements a minimal ACPI power management block (PM1a + PM timer).
type PM struct {
	pm1aStatus uint16
	pm1aCnt    uint16

	startTime time.Time

	// TODO: implement sleep state handling.
}

// NewPM builds a PM block stub.
func NewPM() *PM {
	return &PM{
		startTime: time.Now(),
	}
}

func (p *PM) Init(vm hv.VirtualMachine) error {
	_ = vm
	return nil
}

func (p *PM) IOPorts() []uint16 {
	ports := make([]uint16, 0, pm1aEvtSize+pm1aCntSize+pmTmrSize)
	for off := uint16(0); off < pm1aEvtSize; off++ {
		ports = append(ports, pm1aEvtBase+off)
	}
	for off := uint16(0); off < pm1aCntSize; off++ {
		ports = append(ports, pm1aCntBase+off)
	}
	for off := uint16(0); off < pmTmrSize; off++ {
		ports = append(ports, pmTmrBase+off)
	}
	return ports
}

func (p *PM) ReadIOPort(ctx hv.ExitContext, port uint16, data []byte) error {
	switch {
	case port >= pm1aEvtBase && port < pm1aEvtBase+pm1aEvtSize:
		return readUint16(port-pm1aEvtBase, p.pm1aStatus, data)
	case port >= pm1aCntBase && port < pm1aCntBase+pm1aCntSize:
		return readUint16(port-pm1aCntBase, p.pm1aCnt, data)
	case port >= pmTmrBase && port < pmTmrBase+pmTmrSize:
		return readUint32(port-pmTmrBase, p.pmTimerValue(), data)
	default:
		return fmt.Errorf("pm: invalid read port 0x%04x", port)
	}
}

func (p *PM) WriteIOPort(ctx hv.ExitContext, port uint16, data []byte) error {
	switch {
	case port >= pm1aEvtBase && port < pm1aEvtBase+pm1aEvtSize:
		return writeUint16(&p.pm1aStatus, port-pm1aEvtBase, data)
	case port >= pm1aCntBase && port < pm1aCntBase+pm1aCntSize:
		return writeUint16(&p.pm1aCnt, port-pm1aCntBase, data)
	case port == pm1aCntBase && len(data) == 2:
		// SLP_TYP/SLP_EN reside in PM1a control; ignore for now.
		return nil
	case port >= pmTmrBase && port < pmTmrBase+pmTmrSize:
		// PM timer is read-only in hardware; ignore writes.
		return nil
	default:
		return fmt.Errorf("pm: invalid write port 0x%04x", port)
	}
}

// pmTimerValue returns a simple monotonic counter at ~3.58MHz.
func (p *PM) pmTimerValue() uint32 {
	elapsed := time.Since(p.startTime)
	// 3_579_545 Hz PM timer frequency.
	ticks := uint64(elapsed.Nanoseconds()) * 3579545 / 1_000_000_000
	return uint32(ticks)
}

func readUint16(offset uint16, value uint16, data []byte) error {
	if len(data) != 1 && len(data) != 2 {
		return fmt.Errorf("pm: invalid read size %d", len(data))
	}
	buf := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf, value)
	copy(data, buf[offset:])
	return nil
}

func writeUint16(dst *uint16, offset uint16, data []byte) error {
	if len(data) != 1 && len(data) != 2 {
		return fmt.Errorf("pm: invalid write size %d", len(data))
	}
	buf := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf, *dst)
	copy(buf[offset:], data)
	*dst = binary.LittleEndian.Uint16(buf)
	return nil
}

func readUint32(offset uint16, value uint32, data []byte) error {
	if len(data) != 1 && len(data) != 2 && len(data) != 4 {
		return fmt.Errorf("pm: invalid read size %d", len(data))
	}
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, value)
	copy(data, buf[offset:])
	return nil
}

var (
	_ hv.Device          = (*PM)(nil)
	_ hv.X86IOPortDevice = (*PM)(nil)
)
