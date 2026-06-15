package vm

// Boxed numeric types that implement MemoryRegion interface
// Use only when you need a single number as a memory region

type Uint8 [1]byte

// ReadAt implements MemoryRegion.
func (u *Uint8) ReadAt(p []byte, off int64) (n int, err error) {
	if len(p) == 0 || len(u) == 0 {
		return 0, nil
	}
	if off < 0 || off >= int64(len(u)) {
		return 0, nil
	}
	p[0] = u[off]
	return 1, nil
}

// Size implements MemoryRegion.
func (u *Uint8) Size() int64 {
	if len(u) == 0 {
		return 0
	}
	return int64(len(u))
}

// WriteAt implements MemoryRegion.
func (u *Uint8) WriteAt(p []byte, off int64) (n int, err error) {
	if len(p) == 0 || len(u) == 0 {
		return 0, nil
	}
	if off < 0 || off >= int64(len(u)) {
		return 0, nil
	}
	u[off] = p[0]
	return 1, nil
}

func (u *Uint8) Get() uint8 {
	if len(u) == 0 {
		return 0
	}
	return uint8(u[0])
}

func (u *Uint8) Set(value uint8) {
	if len(u) == 0 {
		return
	}
	u[0] = byte(value)
}

type Uint16 [2]byte

// ReadAt implements MemoryRegion.
func (u *Uint16) ReadAt(p []byte, off int64) (n int, err error) {
	if len(p) < 2 || len(u) < 2 {
		return 0, nil
	}
	if off < 0 || off >= int64(len(u)) {
		return 0, nil
	}
	p[0] = u[off]
	p[1] = u[off+1]
	return 2, nil
}

// Size implements MemoryRegion.
func (u *Uint16) Size() int64 {
	if len(u) == 0 {
		return 0
	}
	return int64(len(u))
}

// WriteAt implements MemoryRegion.
func (u *Uint16) WriteAt(p []byte, off int64) (n int, err error) {
	if len(p) < 2 || len(u) < 2 {
		return 0, nil
	}
	if off < 0 || off >= int64(len(u)) {
		return 0, nil
	}
	u[off] = p[0]
	u[off+1] = p[1]
	return 2, nil
}

func (u *Uint16) Get() uint16 {
	if len(u) < 2 {
		return 0
	}
	return uint16(u[0]) | (uint16(u[1]) << 8)
}

func (u *Uint16) Set(value uint16) {
	if len(u) < 2 {
		return
	}
	u[0] = byte(value)
	u[1] = byte(value >> 8)
}

type Uint32 [4]byte

// ReadAt implements MemoryRegion.
func (u *Uint32) ReadAt(p []byte, off int64) (n int, err error) {
	if len(p) < 4 || len(u) < 4 {
		return 0, nil
	}
	if off < 0 || off >= int64(len(u)) {
		return 0, nil
	}
	p[0] = u[off]
	p[1] = u[off+1]
	p[2] = u[off+2]
	p[3] = u[off+3]
	return 4, nil
}

// Size implements MemoryRegion.
func (u *Uint32) Size() int64 {
	if len(u) == 0 {
		return 0
	}
	return int64(len(u))
}

// WriteAt implements MemoryRegion.
func (u *Uint32) WriteAt(p []byte, off int64) (n int, err error) {
	if len(p) < 4 || len(u) < 4 {
		return 0, nil
	}
	if off < 0 || off >= int64(len(u)) {
		return 0, nil
	}
	u[off] = p[0]
	u[off+1] = p[1]
	u[off+2] = p[2]
	u[off+3] = p[3]
	return 4, nil
}

func (u *Uint32) Get() uint32 {
	if len(u) < 4 {
		return 0
	}
	return uint32(u[0]) | (uint32(u[1]) << 8) | (uint32(u[2]) << 16) | (uint32(u[3]) << 24)
}

func (u *Uint32) Set(value uint32) {
	if len(u) < 4 {
		return
	}
	u[0] = byte(value)
	u[1] = byte(value >> 8)
	u[2] = byte(value >> 16)
	u[3] = byte(value >> 24)
}

type Uint64 [8]byte

// ReadAt implements MemoryRegion.
func (u *Uint64) ReadAt(p []byte, off int64) (n int, err error) {
	if len(p) < 8 || len(u) < 8 {
		return 0, nil
	}
	if off < 0 || off >= int64(len(u)) {
		return 0, nil
	}
	p[0] = u[off]
	p[1] = u[off+1]
	p[2] = u[off+2]
	p[3] = u[off+3]
	p[4] = u[off+4]
	p[5] = u[off+5]
	p[6] = u[off+6]
	p[7] = u[off+7]
	return 8, nil
}

// Size implements MemoryRegion.
func (u *Uint64) Size() int64 {
	if len(u) == 0 {
		return 0
	}
	return int64(len(u))
}

// WriteAt implements MemoryRegion.
func (u *Uint64) WriteAt(p []byte, off int64) (n int, err error) {
	if len(p) < 8 || len(u) < 8 {
		return 0, nil
	}
	if off < 0 || off >= int64(len(u)) {
		return 0, nil
	}
	u[off] = p[0]
	u[off+1] = p[1]
	u[off+2] = p[2]
	u[off+3] = p[3]
	u[off+4] = p[4]
	u[off+5] = p[5]
	u[off+6] = p[6]
	u[off+7] = p[7]
	return 8, nil
}

func (u *Uint64) Get() uint64 {
	if len(u) < 8 {
		return 0
	}
	return uint64(u[0]) | (uint64(u[1]) << 8) | (uint64(u[2]) << 16) | (uint64(u[3]) << 24) |
		(uint64(u[4]) << 32) | (uint64(u[5]) << 40) | (uint64(u[6]) << 48) | (uint64(u[7]) << 56)
}

func (u *Uint64) Set(value uint64) {
	if len(u) < 8 {
		return
	}
	u[0] = byte(value)
	u[1] = byte(value >> 8)
	u[2] = byte(value >> 16)
	u[3] = byte(value >> 24)
	u[4] = byte(value >> 32)
	u[5] = byte(value >> 40)
	u[6] = byte(value >> 48)
	u[7] = byte(value >> 56)
}

// Verify interface compliance
var (
	_ MemoryRegion = &Uint8{}
	_ MemoryRegion = &Uint16{}
	_ MemoryRegion = &Uint32{}
	_ MemoryRegion = &Uint64{}
)
