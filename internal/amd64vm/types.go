package amd64vm

import "j5.nz/cc/internal/vmruntime"

const (
	RootFSTag   = vmruntime.RootFSTag
	EmulatorTag = vmruntime.EmulatorTag
	GuestCID    = vmruntime.GuestCID
	ControlPort = vmruntime.ControlPort
)

type DirectoryShare = vmruntime.DirectoryShare
type RunRequest = vmruntime.RunRequest
type RunResult = vmruntime.RunResult
