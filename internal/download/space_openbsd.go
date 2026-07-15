//go:build openbsd

package download

import "golang.org/x/sys/unix"

func filesystemAvailableBytes(path string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	if stat.F_bavail <= 0 {
		return 0, nil
	}
	return uint64(stat.F_bavail) * uint64(stat.F_bsize), nil
}
