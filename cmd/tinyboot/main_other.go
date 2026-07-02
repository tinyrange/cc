//go:build (!darwin || !arm64) && (!windows || !amd64)

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "tinyboot: no boot backend is available for this host")
	os.Exit(1)
}
