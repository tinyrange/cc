package ipc

import (
	"os"
	"runtime"
	"strings"
	"sync"
)

var (
	useHelperOnce sync.Once
	useHelperVal  bool
)

// UseHelper returns true if the IPC helper should be used for instance operations.
// By default:
// - macOS: true (codesigning required for hypervisor access)
// - Other platforms: false (library can access hypervisor directly)
//
// This can be overridden by environment variables:
// - CC_USE_HELPER=1 or CC_USE_HELPER=true: Force helper usage
// - CC_USE_HELPER=0 or CC_USE_HELPER=false: Force direct execution
func UseHelper() bool {
	useHelperOnce.Do(func() {
		// Check environment variable override
		if env := os.Getenv("CC_USE_HELPER"); env != "" {
			switch strings.ToLower(env) {
			case "1", "true", "yes", "on":
				useHelperVal = true
				return
			case "0", "false", "no", "off":
				useHelperVal = false
				return
			}
		}

		// Default: use helper on macOS, direct on other platforms
		useHelperVal = runtime.GOOS == "darwin"
	})
	return useHelperVal
}

// ResetUseHelper resets the cached UseHelper value. This is only for testing.
func ResetUseHelper() {
	useHelperOnce = sync.Once{}
	useHelperVal = false
}
