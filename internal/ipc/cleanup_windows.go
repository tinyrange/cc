//go:build windows

package ipc

import (
	"os"
	"time"
)

func removeSocketPlatform(path string) {
	// On Windows, file locks may persist briefly after the socket is closed.
	// Retry removal a few times with short delays.
	for i := 0; i < 5; i++ {
		err := os.Remove(path)
		if err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
