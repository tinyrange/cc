//go:build netbsd

package main

import (
	"time"

	"j5.nz/cc/internal/managed/guestagent"
)

func main() {
	if err := guestagent.Run(guestagent.Options{Name: "netbsd"}); err != nil {
		guestagent.WriteConsole("ccx3-netbsd-init: " + err.Error() + "\n")
		for {
			time.Sleep(time.Hour)
		}
	}
}
