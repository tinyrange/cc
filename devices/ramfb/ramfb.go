// Package ramfb implements the fw_cfg DMA RAM framebuffer protocol. Guest
// memory and display capture are supplied by the caller; no host files or
// graphics libraries are required. Calls must be serialized with guest access.
package ramfb

import (
	"encoding/binary"
	"fmt"
	"image"
	"math/bits"

	"j5.nz/cc/display"
)

type Memory interface {
	ReadPhysical(address uint64, dst []byte) error
	WritePhysical(address uint64, src []byte) error
}

type Device struct {
	base       uint64
	memory     Memory
	selector   uint16
	offset     uint64
	dmaHigh    uint64
	config     [28]byte
	generation uint64
}

func New(base uint64, memory Memory) (*Device, error) {
	if base&0xfff != 0 || base > ^uint64(0)-4096 || memory == nil {
		return nil, fmt.Errorf("invalid RAMFB aperture or memory")
	}
	d := &Device{base: base, memory: memory}
	return d, nil
}

func (d *Device) Contains(a uint64, size int) bool {
	return size > 0 && size <= 8 && a >= d.base && a-d.base <= 4096-uint64(size)
}

func (d *Device) file() ([]byte, bool) {
	switch d.selector {
	case 0:
		return []byte("QEMU"), false
	case 1:
		return []byte{3, 0, 0, 0}, false
	case 0x19:
		b := make([]byte, 68)
		binary.BigEndian.PutUint32(b, 1)
		binary.BigEndian.PutUint32(b[4:], 28)
		binary.BigEndian.PutUint16(b[8:], 0x20)
		copy(b[12:], "etc/ramfb")
		return b, false
	case 0x20:
		return d.config[:], true
	default:
		return nil, false
	}
}

func (d *Device) Read(a uint64, size int) (uint64, error) {
	if !d.Contains(a, size) {
		return 0, fmt.Errorf("invalid fw_cfg read")
	}
	off := a - d.base
	if off >= 16 && off+uint64(size) <= 24 {
		var sig = [8]byte{'Q', 'E', 'M', 'U', ' ', 'C', 'F', 'G'}
		var v uint64
		for i := 0; i < size; i++ {
			v |= uint64(sig[int(off)-16+i]) << (8 * i)
		}
		return v, nil
	}
	if off == 0 && size == 1 {
		data, _ := d.file()
		var v byte
		if d.offset < uint64(len(data)) {
			v = data[d.offset]
		}
		d.offset++
		return uint64(v), nil
	}
	return 0, nil
}

func (d *Device) Write(a uint64, size int, v uint64) error {
	if !d.Contains(a, size) {
		return fmt.Errorf("invalid fw_cfg write")
	}
	switch {
	case a == d.base+8 && size == 2:
		d.selector = bits.ReverseBytes16(uint16(v))
		d.offset = 0
	case a == d.base+16 && size == 8:
		return d.dma(bits.ReverseBytes64(v))
	case a == d.base+16 && size == 4:
		d.dmaHigh = uint64(bits.ReverseBytes32(uint32(v))) << 32
	case a == d.base+20 && size == 4:
		return d.dma(d.dmaHigh | uint64(bits.ReverseBytes32(uint32(v))))
	}
	return nil
}

func (d *Device) dma(a uint64) error {
	var desc [16]byte
	if err := d.memory.ReadPhysical(a, desc[:]); err != nil {
		return err
	}
	control := binary.BigEndian.Uint32(desc[:])
	length := uint64(binary.BigEndian.Uint32(desc[4:]))
	target := binary.BigEndian.Uint64(desc[8:])
	if control&8 != 0 {
		d.selector = uint16(control >> 16)
		d.offset = 0
	}
	status := uint32(0)
	data, writable := d.file()
	if length > 1<<20 || target+length < target || control&0xffc0 != 0 {
		status = 1
	} else {
		switch control & 0x36 {
		case 0: // Selection only.
		case 4:
			d.offset += length
		case 2:
			b := make([]byte, length)
			if d.offset < uint64(len(data)) {
				copy(b, data[d.offset:])
			}
			if err := d.memory.WritePhysical(target, b); err != nil {
				status = 1
			} else {
				d.offset += length
			}
		case 16:
			if !writable || d.offset > uint64(len(data)) || length > uint64(len(data))-d.offset {
				status = 1
				break
			}
			b := make([]byte, length)
			if err := d.memory.ReadPhysical(target, b); err != nil {
				status = 1
				break
			}
			candidate := d.config
			copy(candidate[d.offset:], b)
			// The RAMFB configuration is published as one complete record.
			if d.offset == 0 && length == 28 {
				if err := validate(candidate); err != nil {
					status = 1
					break
				}
			}
			d.config = candidate
			d.offset += length
			d.generation++
		default:
			status = 1
		}
	}
	var result [4]byte
	binary.BigEndian.PutUint32(result[:], status)
	return d.memory.WritePhysical(a, result[:])
}

// Dimensions returns the guest's committed mode, or zero before configuration.
func (d *Device) Dimensions() (int, int) {
	if validate(d.config) != nil {
		return 0, 0
	}
	return int(binary.BigEndian.Uint32(d.config[16:])), int(binary.BigEndian.Uint32(d.config[20:]))
}

func validate(c [28]byte) error {
	address := binary.BigEndian.Uint64(c[:])
	format := binary.BigEndian.Uint32(c[8:])
	flags := binary.BigEndian.Uint32(c[12:])
	w, h, stride := uint64(binary.BigEndian.Uint32(c[16:])), uint64(binary.BigEndian.Uint32(c[20:])), uint64(binary.BigEndian.Uint32(c[24:]))
	if address == 0 || format != 0x34325258 || flags != 0 || w == 0 || h == 0 || w > 8192 || h > 8192 || stride < w*4 || stride > 1<<20 || stride*h > 256<<20 || address+stride*h < address {
		return fmt.Errorf("invalid XRGB8888 RAMFB configuration")
	}
	return nil
}

// Snapshot copies the configured guest framebuffer while the caller has paused
// guest execution. Pixels follow display.FramebufferUpdate's BGRX contract.
func (d *Device) Snapshot() (display.FramebufferUpdate, error) {
	return d.SnapshotInto(nil)
}

// SnapshotInto reuses caller-owned storage for repeated capture. The returned
// pixels alias storage when its capacity is sufficient; callers retain ownership.
// The guest must be stopped throughout the copy.
func (d *Device) SnapshotInto(storage []byte) (display.FramebufferUpdate, error) {
	if err := validate(d.config); err != nil {
		return display.FramebufferUpdate{}, err
	}
	a := binary.BigEndian.Uint64(d.config[:])
	w := int(binary.BigEndian.Uint32(d.config[16:]))
	h := int(binary.BigEndian.Uint32(d.config[20:]))
	stride := uint64(binary.BigEndian.Uint32(d.config[24:]))
	if cap(storage) < w*h*4 {
		storage = make([]byte, w*h*4)
	}
	pixels := storage[:w*h*4]
	if stride == uint64(w*4) {
		if err := d.memory.ReadPhysical(a, pixels); err != nil {
			return display.FramebufferUpdate{}, err
		}
	} else {
		for y := 0; y < h; y++ {
			if err := d.memory.ReadPhysical(a+uint64(y)*stride, pixels[y*w*4:(y+1)*w*4]); err != nil {
				return display.FramebufferUpdate{}, err
			}
		}
	}
	d.generation++
	return display.FramebufferUpdate{Width: w, Height: h, Generation: d.generation, Rect: image.Rect(0, 0, w, h), Pixels: pixels}, nil
}
