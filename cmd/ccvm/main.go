package main

import (
	"os"

	"j5.nz/cc/internal/ccvmd"
)

func main() {
	ccvmd.Main(os.Args[1:])
}
