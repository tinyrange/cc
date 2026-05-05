package amd64vm

import "j5.nz/cc/internal/vmruntime"

const (
	RootFSBase = 0xd0004000
	RootFSSize = 0x1000
	RootFSIRQ  = 16

	VsockBase = 0xd0005000
	VsockSize = 0x1000
	VsockIRQ  = 17

	RNGBase = 0xd0006000
	RNGSize = 0x1000
	RNGIRQ  = 18

	NetBase = 0xd0007000
	NetSize = 0x1000
	NetIRQ  = 19

	ShareFSBase = 0xd0008000
	ShareFSIRQ  = 20
	FSStride    = 0x1000

	RootFSTag   = vmruntime.RootFSTag
	EmulatorTag = vmruntime.EmulatorTag
	GuestCID    = vmruntime.GuestCID
	ControlPort = vmruntime.ControlPort
)
