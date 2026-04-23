//go:build darwin

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
		return uint64(st.Atimespec.Sec), uint32(st.Atimespec.Nsec)
	case statTimeModify:
		return uint64(st.Mtimespec.Sec), uint32(st.Mtimespec.Nsec)
	default:
		return uint64(st.Ctimespec.Sec), uint32(st.Ctimespec.Nsec)
	}
}
