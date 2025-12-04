//go:build linux

package kvm

type kvmClockData struct {
	Clock    uint64
	Flags    uint32
	Pad0     uint32
	Realtime uint64
	HostTSC  uint64
	Pad      [4]uint32
}

type kvmIRQChip struct {
	ChipID uint32
	Pad    uint32
	Chip   [512]byte
}

type kvmPitChannelState struct {
	Count         uint32
	LatchedCount  uint16
	CountLatched  uint8
	StatusLatched uint8
	Status        uint8
	ReadState     uint8
	WriteState    uint8
	WriteLatch    uint8
	RWMode        uint8
	Mode          uint8
	Bcd           uint8
	Gate          uint8
	CountLoadTime int64
}

type kvmPitState2 struct {
	Channels [3]kvmPitChannelState
	Flags    uint32
	Reserved [9]uint32
}

type kvmIRQLevel struct {
	IRQOrStatus uint32
	Level       uint32
}

type kvmMSI struct {
	AddressLo uint32
	AddressHi uint32
	Data      uint32
	Flags     uint32
	Devid     uint32
	Pad       [12]uint8
}
