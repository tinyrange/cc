package main

import (
	"fmt"
	"os"
	"runtime"

	"j5.nz/cc/internal/ccvmd"
	freebsdguestinit "j5.nz/cc/internal/freebsd/guestinit"
	"j5.nz/cc/internal/guestinit"
	netbsdguestinit "j5.nz/cc/internal/netbsd/guestinit"
	openbsdguestinit "j5.nz/cc/internal/openbsd/guestinit"
)

func main() {
	if err := guestinit.RequireEmbedded(); err != nil {
		fmt.Fprintln(os.Stderr, "ccvm:", err)
		os.Exit(1)
	}
	for _, check := range []func(string) error{
		openbsdguestinit.RequireEmbeddedForArch,
		freebsdguestinit.RequireEmbeddedForArch,
		netbsdguestinit.RequireEmbeddedForArch,
	} {
		if err := check(runtime.GOARCH); err != nil {
			fmt.Fprintln(os.Stderr, "ccvm:", err)
			os.Exit(1)
		}
	}
	ccvmd.Main(os.Args[1:])
}
