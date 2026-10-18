package nvme

import (
	"encoding/binary"
	"fmt"
	"testing"
)

type highMemory struct {
	base uint64
	data []byte
}

func (m *highMemory) SliceIPA(addr uint64, size int) ([]byte, error) {
	if size < 0 || addr < m.base || addr-m.base > uint64(len(m.data)) || uint64(size) > uint64(len(m.data))-(addr-m.base) {
		return nil, fmt.Errorf("unmapped DMA %#x+%d", addr, size)
	}
	return m.data[addr-m.base : addr-m.base+uint64(size)], nil
}
func (m *highMemory) ReadIPA(addr uint64, size int) ([]byte, error) { return m.SliceIPA(addr, size) }
func (m *highMemory) WriteIPA(addr uint64, b []byte) error {
	dst, err := m.SliceIPA(addr, len(b))
	if err == nil {
		copy(dst, b)
	}
	return err
}

func TestAdminQueuesAbove4GiB(t *testing.T) {
	for _, width := range []int{4, 8} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			mem := &highMemory{base: 0x400000000, data: make([]byte, 0x10000)}
			irq := &testIRQ{}
			c := NewController(newTestDisk(1 << 20))
			c.Attach(mem, irq)
			wr := func(off uint64, size int, value uint64) {
				t.Helper()
				if err := c.WriteMMIO(off, size, value); err != nil {
					t.Fatal(err)
				}
			}
			wr(regAQA, 4, 15|15<<16)
			for off, addr := range map[uint64]uint64{regASQ: mem.base + 0x1000, regACQ: mem.base + 0x2000} {
				wr(off, width, addr)
				if width == 4 {
					wr(off+4, 4, addr>>32)
				}
				got, err := c.ReadMMIO(off, 8)
				if err != nil || got != addr {
					t.Fatalf("register %#x = %#x, %v", off, got, err)
				}
			}
			wr(regCC, 4, 1)
			cmd := mem.data[0x1000:0x1040]
			cmd[0] = adminIdentify
			binary.LittleEndian.PutUint16(cmd[2:], 42)
			binary.LittleEndian.PutUint64(cmd[24:], mem.base+0x3000)
			binary.LittleEndian.PutUint32(cmd[40:], 1)
			wr(doorbellBase, 4, 1)
			if binary.LittleEndian.Uint16(mem.data[0x200c:]) != 42 || !irq.level {
				t.Fatal("high-memory completion missing")
			}
			if string(mem.data[0x3018:0x302c]) != "cc NVMe Block Device" {
				t.Fatal("identify DMA missing")
			}
			wr(doorbellBase+4, 4, 1)
			if irq.level {
				t.Fatal("completion interrupt not acknowledged")
			}
		})
	}
}
