//go:build linux

package virtio

import "syscall"

type statTimeKind int

const (
	statTimeAccess statTimeKind = iota
	statTimeModify
	statTimeChange
)

func statTimespecUnix(st *syscall.Stat_t, kind statTimeKind) (uint64, uint32) {
	switch kind {
	case statTimeAccess:
		return uint64(st.Atim.Sec), uint32(st.Atim.Nsec)
	case statTimeModify:
		return uint64(st.Mtim.Sec), uint32(st.Mtim.Nsec)
	default:
		return uint64(st.Ctim.Sec), uint32(st.Ctim.Nsec)
	}
}
