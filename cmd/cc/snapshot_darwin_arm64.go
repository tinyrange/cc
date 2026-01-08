//go:build darwin && arm64

package main

import (
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/hvf"
	"github.com/tinyrange/cc/internal/initx"
)

type hvfSnapshotIO struct{}

func (hvfSnapshotIO) SaveSnapshot(path string, snap hv.Snapshot) error {
	return hvf.SaveSnapshot(path, snap)
}

func (hvfSnapshotIO) LoadSnapshot(path string) (hv.Snapshot, error) {
	return hvf.LoadSnapshot(path)
}

func getSnapshotIO() initx.SnapshotIO {
	return hvfSnapshotIO{}
}
