//go:build !windows

package main

// enableVTProcessing is a no-op on non-Windows platforms.
// Unix terminals generally support VT escape codes natively.
func enableVTProcessing() (restore func(), err error) {
	return func() {}, nil
}
