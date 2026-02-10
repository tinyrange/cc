//go:build !windows

package ipc

func socketPath() string {
	return defaultSocketPath()
}
