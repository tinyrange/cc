//go:build !darwin || !arm64

package main

import "github.com/tinyrange/cc/internal/initx"

func getSnapshotIO() initx.SnapshotIO {
	return initx.GetSnapshotIO()
}
