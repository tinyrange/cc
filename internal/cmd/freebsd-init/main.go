//go:build freebsd

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/creack/pty"
	"j5.nz/cc/internal/managed/guestagent"
)

type freeBSDPTY struct{}

func (freeBSDPTY) Open(cols, rows int) (*os.File, *os.File, error) {
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

func (freeBSDPTY) Resize(master *os.File, cols, rows int) error {
	return pty.Setsize(master, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func main() {
	if err := guestagent.Run(guestagent.Options{Name: "freebsd", PTY: freeBSDPTY{}}); err != nil {
		guestagent.WriteConsole("ccx3-freebsd-init: " + err.Error() + "\n")
		for {
			time.Sleep(time.Hour)
		}
	}
}
