//go:build !darwin

package macos

func EnsureExecutableIsSigned() error {
	return nil
}
