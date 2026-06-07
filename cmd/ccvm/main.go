package main

import (
	"fmt"
	"os"

	"j5.nz/cc/internal/ccvmd"
	"j5.nz/cc/internal/guestinit"
)

func main() {
	if err := guestinit.RequireEmbedded(); err != nil {
		fmt.Fprintln(os.Stderr, "ccvm:", err)
		os.Exit(1)
	}
	ccvmd.Main(os.Args[1:])
}
