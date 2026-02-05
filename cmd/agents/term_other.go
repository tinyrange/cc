//go:build !windows

package main

import "io"

// enableVTProcessing is a no-op on non-Windows platforms.
// Unix terminals generally support VT escape codes natively.
func enableVTProcessing() (restore func(), err error) {
	return func() {}, nil
}

// wrapStdinForVT is a no-op on non-Windows platforms.
// CPR filtering is only needed on Windows where the console auto-responds to CPR queries.
func wrapStdinForVT(r io.Reader) io.Reader {
	return r
}
