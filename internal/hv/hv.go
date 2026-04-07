package hv

import (
	"fmt"
	"runtime"
)

func Supports() error {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return fmt.Errorf("unsupported host: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return nil
}
