//go:build !darwin

package api

// EnsureExecutableIsSigned is a no-op on non-Darwin platforms.
// On macOS, this signs the executable with the hypervisor entitlement
// and re-execs if necessary.
func EnsureExecutableIsSigned() error {
	return nil
}
