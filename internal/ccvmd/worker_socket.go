package ccvmd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

const workerSocketProbeTimeout = 250 * time.Millisecond

func prepareWorkerUnixSocket(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect worker control socket: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("worker control path already exists and is not a socket")
	}
	owned, err := workerSocketOwnedByCurrentUser(info)
	if err != nil {
		return fmt.Errorf("inspect worker control socket owner: %w", err)
	}
	if !owned {
		return fmt.Errorf("worker control socket is not owned by the current user")
	}

	conn, dialErr := net.DialTimeout("unix", path, workerSocketProbeTimeout)
	if dialErr == nil {
		_ = conn.Close()
		return fmt.Errorf("worker control socket is already active")
	}
	if errors.Is(dialErr, os.ErrNotExist) || errors.Is(dialErr, syscall.ENOENT) {
		return nil
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) {
		return fmt.Errorf("worker control socket could not be proven stale: %w", dialErr)
	}
	if err := removeWorkerSocketIfSame(path, info); err != nil {
		return fmt.Errorf("remove stale worker control socket: %w", err)
	}
	return nil
}

func ownedWorkerUnixSocketCleanup(path string) (func(), error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return nil, fmt.Errorf("worker control listener path is not a socket")
	}
	owned, err := workerSocketOwnedByCurrentUser(info)
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, fmt.Errorf("worker control listener is not owned by the current user")
	}
	return func() { _ = removeWorkerSocketIfSame(path, info) }, nil
}

func removeWorkerSocketIfSame(path string, expected os.FileInfo) error {
	current, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.Mode()&os.ModeSocket == 0 || !os.SameFile(expected, current) {
		return fmt.Errorf("worker control socket path changed ownership")
	}
	owned, err := workerSocketOwnedByCurrentUser(current)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("worker control socket is not owned by the current user")
	}
	return os.Remove(path)
}
