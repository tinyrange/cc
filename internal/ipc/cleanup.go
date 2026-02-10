package ipc

import "os"

// removeSocket removes a Unix domain socket file.
// On Windows, this may retry briefly since file locks can persist after socket close.
func removeSocket(path string) {
	removeSocketPlatform(path)
}

// removeSocketDefault is the standard removal (used on non-Windows).
func removeSocketDefault(path string) {
	os.Remove(path)
}
