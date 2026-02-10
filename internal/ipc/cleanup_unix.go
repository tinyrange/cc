//go:build !windows

package ipc

func removeSocketPlatform(path string) {
	removeSocketDefault(path)
}
