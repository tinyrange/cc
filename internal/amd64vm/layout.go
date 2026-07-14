package amd64vm

import "j5.nz/cc/internal/vmruntime"

const (
	RootFSBase = 0xd0004000
	RootFSSize = 0x1000
	RootFSIRQ  = 5

	VsockBase = 0xd0005000
	VsockSize = 0x1000
	VsockIRQ  = 6

	RNGBase = 0xd0006000
	RNGSize = 0x1000
	RNGIRQ  = 7

	NetBase = 0xd0007000
	NetSize = 0x1000
	NetIRQ  = 8

	ShareFSBase = 0xd0008000
	ShareFSIRQ  = 9
	FSStride    = 0x1000

	SnapshotBase = 0xd0009000
	SnapshotSize = 0x1000

	BalloonBase = 0xd000a000
	BalloonSize = 0x1000
	BalloonIRQ  = 10

	RootFSTag   = vmruntime.RootFSTag
	EmulatorTag = vmruntime.EmulatorTag
	GuestCID    = vmruntime.GuestCID
	ControlPort = vmruntime.ControlPort
)
