//go:build windows

package main

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
)

// enableVTProcessing enables Virtual Terminal processing on stdout so that
// ANSI escape sequences are interpreted by the Windows console rather than
// being displayed as raw text. This must be called before entering raw mode.
//
// Returns a cleanup function that restores the original console mode.
func enableVTProcessing() (restore func(), err error) {
	h := windows.Handle(os.Stdout.Fd())

	var originalMode uint32
	if err := windows.GetConsoleMode(h, &originalMode); err != nil {
		// Not a console (e.g. redirected to file) - no-op.
		return func() {}, nil
	}

	newMode := originalMode | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	if err := windows.SetConsoleMode(h, newMode); err != nil {
		return nil, err
	}

	return func() {
		_ = windows.SetConsoleMode(h, originalMode)
	}, nil
}

// cprFilterReader wraps an io.Reader to filter out Cursor Position Report (CPR)
// responses that the Windows console writes to stdin when VT input is enabled.
// CPR responses have the format: ESC [ <row> ; <col> R
//
// When a guest program sends a CPR query (ESC[6n), the Windows console responds
// with the cursor position. Without filtering, this response gets forwarded to
// the guest where it can be echoed or misinterpreted, causing terminal spam.
type cprFilterReader struct {
	r   io.Reader
	buf []byte // partial escape sequence buffer
}

// wrapStdinForVT wraps stdin to filter out CPR responses on Windows.
func wrapStdinForVT(r io.Reader) io.Reader {
	return &cprFilterReader{r: r}
}

func (f *cprFilterReader) Read(p []byte) (int, error) {
	// Read into a temporary buffer so we can filter.
	tmp := make([]byte, len(p))
	n, err := f.r.Read(tmp)
	if n == 0 {
		return 0, err
	}

	// Prepend any buffered partial sequence.
	data := append(f.buf, tmp[:n]...)
	f.buf = nil

	// Filter out CPR responses: ESC [ <digits> ; <digits> R
	out := make([]byte, 0, len(data))
	i := 0
	for i < len(data) {
		if data[i] == 0x1b { // ESC
			// Look for CPR pattern: ESC [ <digits> ; <digits> R
			j := i + 1
			if j < len(data) && data[j] == '[' {
				j++
				// Parse first number (row).
				for j < len(data) && data[j] >= '0' && data[j] <= '9' {
					j++
				}
				if j < len(data) && data[j] == ';' {
					j++
					// Parse second number (col).
					for j < len(data) && data[j] >= '0' && data[j] <= '9' {
						j++
					}
					if j < len(data) && data[j] == 'R' {
						// Found a complete CPR response - skip it.
						i = j + 1
						continue
					}
				}
				// Check if we might have a partial CPR at end of buffer.
				if j >= len(data) {
					// Buffer the partial sequence for next read.
					f.buf = append(f.buf, data[i:]...)
					break
				}
			} else if j >= len(data) {
				// ESC at end of buffer - might be start of CPR.
				f.buf = append(f.buf, data[i:]...)
				break
			}
		}
		out = append(out, data[i])
		i++
	}

	if len(out) == 0 {
		// All data was filtered out or buffered. Try another read.
		return f.Read(p)
	}

	copy(p, out)
	return len(out), err
}
