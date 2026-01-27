//go:build !windows

package main

import "io"

// enableVTProcessing is a no-op on non-Windows platforms where VT processing
// is natively supported.
func enableVTProcessing() (restore func(), err error) {
	return func() {}, nil
}

// wrapStdinForVT returns the reader unchanged on non-Windows platforms.
// On Windows, this would filter out CPR responses.
func wrapStdinForVT(r io.Reader) io.Reader {
	return r
}
