//go:build netbsd

package main

import (
	"fmt"
	"os"
	"time"

	"j5.nz/cc/internal/managed/capturerelay"
	"j5.nz/cc/internal/managed/guestagent"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--ccx3-capture-relay" {
		if err := capturerelay.Run(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "ccx3-netbsd-init: capture relay:", err)
			os.Exit(1)
		}
		return
	}
	if err := guestagent.Run(guestagent.Options{Name: "netbsd", PTY: guestagent.BSDPTY{}}); err != nil {
		guestagent.WriteConsole("ccx3-netbsd-init: " + err.Error() + "\n")
		for {
			time.Sleep(time.Hour)
		}
	}
}
