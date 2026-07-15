package cachepath

import (
	"fmt"
	"os"
)

// EnsurePrivateRoot creates or repairs a cache root so only its owner can
// traverse it. Files within the root may retain content-oriented modes because
// the private root is the access-control boundary.
func EnsurePrivateRoot(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("cache root %q is a symbolic link", path)
		}
		if !info.IsDir() {
			return fmt.Errorf("cache root %q is not a directory", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("make cache root %q private: %w", path, err)
	}
	return nil
}
