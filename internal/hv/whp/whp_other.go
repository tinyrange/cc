//go:build !(windows && amd64)

package whp

import "fmt"

func Supports() error {
	return fmt.Errorf("whp unavailable: requires windows/amd64")
}
