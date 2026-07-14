//go:build netbsd

package guestagent

import "golang.org/x/sys/unix"

func archiveFilesystemCapacity(path string) (uint64, uint64, error) {
	var stat unix.Statvfs_t
	if err := unix.Statvfs(path, &stat); err != nil {
		return 0, 0, err
	}
	return stat.Bavail * stat.Frsize, stat.Ffree, nil
}
