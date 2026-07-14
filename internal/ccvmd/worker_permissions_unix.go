//go:build !windows

package ccvmd

import (
	"fmt"
	"os"
	"syscall"
)

func validateWorkerControlDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("worker control parent is not a real directory")
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("worker control parent permissions are %04o, want 0700", info.Mode().Perm())
	}
	owned, err := workerControlPathOwnedByCurrentUser(info)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("worker control parent is not owned by the current user")
	}
	return nil
}

func secureWorkerControlSocket(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("worker control endpoint is not a socket")
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("worker control socket permissions are %04o, want 0600", info.Mode().Perm())
	}
	owned, err := workerControlPathOwnedByCurrentUser(info)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("worker control socket is not owned by the current user")
	}
	return nil
}

func workerControlPathOwnedByCurrentUser(info os.FileInfo) (bool, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("worker control path has unsupported stat metadata")
	}
	return stat.Uid == uint32(os.Geteuid()), nil
}
