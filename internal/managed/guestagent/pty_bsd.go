//go:build freebsd || netbsd || openbsd

package guestagent

import (
	"fmt"
	"os"

	"github.com/creack/pty"
)

// BSDPTY provides the managed guest agent with a real pseudoterminal on the
// BSD guests supported by cc.
type BSDPTY struct{}

func (BSDPTY) Open(cols, rows int) (*os.File, *os.File, error) {
	master, slave, err := pty.Open()
	if err != nil {
		return nil, nil, err
	}
	if cols > 0 && rows > 0 {
		if err := pty.Setsize(master, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
			_ = master.Close()
			_ = slave.Close()
			return nil, nil, fmt.Errorf("set initial winsize: %w", err)
		}
	}
	return master, slave, nil
}

func (BSDPTY) Resize(master *os.File, cols, rows int) error {
	return pty.Setsize(master, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}
