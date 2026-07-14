//go:build openbsd

package guestagent

import "golang.org/x/sys/unix"

func archiveFilesystemCapacity(path string) (uint64, uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	var available uint64
	if stat.F_bavail > 0 {
		available = uint64(stat.F_bavail) * uint64(stat.F_bsize)
	}
	return available, stat.F_ffree, nil
}
