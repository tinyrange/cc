package amd64vm

import "j5.nz/cc/internal/vmruntime"

const (
	RootFSBase = 0xd0004000
	RootFSSize = 0x1000
	RootFSIRQ  = 5

	VsockBase = 0xd0005000
	VsockSize = 0x1000
	VsockIRQ  = 6

	ShareFSBase = 0xd0006000
	ShareFSIRQ  = 7
	FSStride    = 0x1000

	RootFSTag   = vmruntime.RootFSTag
	EmulatorTag = vmruntime.EmulatorTag
	GuestCID    = vmruntime.GuestCID
	ControlPort = vmruntime.ControlPort
)
