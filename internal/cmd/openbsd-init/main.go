//go:build openbsd

package main

import (
	"time"

	"j5.nz/cc/internal/managed/guestagent"
)

func main() {
	if err := guestagent.Run(guestagent.Options{Name: "openbsd"}); err != nil {
		guestagent.WriteConsole("ccx3-openbsd-init: " + err.Error() + "\n")
		for {
			time.Sleep(time.Hour)
		}
	}
}
