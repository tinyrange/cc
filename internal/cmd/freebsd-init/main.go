//go:build freebsd

package main

import (
	"time"

	"j5.nz/cc/internal/managed/guestagent"
)

func main() {
	if err := guestagent.Run(guestagent.Options{Name: "freebsd", PTY: guestagent.BSDPTY{}}); err != nil {
		guestagent.WriteConsole("ccx3-freebsd-init: " + err.Error() + "\n")
		for {
			time.Sleep(time.Hour)
		}
	}
}
