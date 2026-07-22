//go:build freebsd || netbsd || openbsd

package guestagent

import "fmt"

func validateGuestUser(user string) error {
	if isRootUserRequest(user) {
		return nil
	}
	return fmt.Errorf("guest user %q is unsupported on BSD; only root is currently available", user)
}
