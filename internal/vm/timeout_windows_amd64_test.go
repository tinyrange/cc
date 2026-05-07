//go:build windows && amd64

package vm

import (
	"os"
	"strconv"
	"testing"
	"time"
)

func windowsBootTestTimeout(t *testing.T) time.Duration {
	t.Helper()
	const defaultTimeout = 90 * time.Second
	raw := os.Getenv("CCX3_WHP_BOOT_TIMEOUT_SECONDS")
	if raw == "" {
		return defaultTimeout
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		t.Fatalf("CCX3_WHP_BOOT_TIMEOUT_SECONDS=%q must be a positive integer", raw)
	}
	return time.Duration(seconds) * time.Second
}
