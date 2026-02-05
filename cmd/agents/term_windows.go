//go:build windows

package main

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
)

// enableVTProcessing enables Windows Virtual Terminal (VT) processing for
// proper ANSI escape code handling in interactive mode.
func enableVTProcessing() (restore func(), err error) {
	stdin := windows.Handle(os.Stdin.Fd())
	stdout := windows.Handle(os.Stdout.Fd())

	var originalInMode, originalOutMode uint32

	// Get current console modes
	if err := windows.GetConsoleMode(stdin, &originalInMode); err != nil {
		return nil, err
	}
	if err := windows.GetConsoleMode(stdout, &originalOutMode); err != nil {
		return nil, err
	}

	// Enable VT processing on stdout (for escape codes)
	// ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x0004
	newOutMode := originalOutMode | 0x0004
	if err := windows.SetConsoleMode(stdout, newOutMode); err != nil {
		return nil, err
	}

	// Enable VT input on stdin
	// ENABLE_VIRTUAL_TERMINAL_INPUT = 0x0200
	newInMode := originalInMode | 0x0200
	if err := windows.SetConsoleMode(stdin, newInMode); err != nil {
		// Restore stdout mode on error
		windows.SetConsoleMode(stdout, originalOutMode)
		return nil, err
	}

	// Return a function to restore original modes
	restore = func() {
		windows.SetConsoleMode(stdin, originalInMode)
		windows.SetConsoleMode(stdout, originalOutMode)
	}

	return restore, nil
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
	// Retry loop to handle cases where all data is filtered out.
	for {
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

		if len(out) > 0 {
			copy(p, out)
			return len(out), err
		}
		// All data was filtered out. If there's an error, return it.
		// Otherwise, loop to try another read.
		if err != nil {
			return 0, err
		}
	}
}
