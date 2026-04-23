package arm64vm

import (
	bootarm64 "j5.nz/cc/internal/linux/boot/arm64"
	"j5.nz/cc/internal/vmruntime"
)

const (
	GICVersionDefault = bootarm64.GICVersionDefault
	GICVersionV2      = bootarm64.GICVersionV2
	GICVersionV3      = bootarm64.GICVersionV3

	MemoryBase        = 0xa0000000
	DefaultMemorySize = 512 << 20

	DefaultPStateBits   = bootarm64.DefaultPStateBits
	DefaultUARTBase     = bootarm64.DefaultUARTBase
	DefaultUARTSize     = bootarm64.DefaultUARTSize
	DefaultUARTRegShift = bootarm64.DefaultUARTRegShift

	GICDistributorMin  = 0x08000000
	GICDistributorSize = 0x00010000
	GICDistributorMax  = GICDistributorMin + GICDistributorSize

	GICRedistributorMin  = 0x080a0000
	GICRedistributorSize = 0x00020000
	GICRedistributorMax  = GICRedistributorMin + GICRedistributorSize

	ConsoleBase = 0x0a100000
	ConsoleSize = 0x1000
	ConsoleIRQ  = 40

	RootFSBase = 0x0a101000
	RootFSSize = 0x1000
	RootFSIRQ  = 41

	VsockBase = 0x0a102000
	VsockSize = 0x1000
	VsockIRQ  = 42

	ShareFSBase = 0x0a103000
	ShareFSIRQ  = 43
	FSStride    = 0x1000

	UARTSPI = 33

	RootFSTag   = vmruntime.RootFSTag
	EmulatorTag = vmruntime.EmulatorTag
	GuestCID    = vmruntime.GuestCID
	ControlPort = vmruntime.ControlPort
)
