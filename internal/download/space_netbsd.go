//go:build netbsd

package download

import "golang.org/x/sys/unix"

func filesystemAvailableBytes(path string) (uint64, error) {
	var stat unix.Statvfs_t
	if err := unix.Statvfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * stat.Frsize, nil
}
