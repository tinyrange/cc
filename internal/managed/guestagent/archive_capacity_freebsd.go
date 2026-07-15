//go:build freebsd

package guestagent

import "golang.org/x/sys/unix"

func archiveFilesystemCapacity(path string) (uint64, uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	var available, entries uint64
	if stat.Bavail > 0 {
		available = uint64(stat.Bavail) * stat.Bsize
	}
	if stat.Ffree > 0 {
		entries = uint64(stat.Ffree)
	}
	return available, entries, nil
}
