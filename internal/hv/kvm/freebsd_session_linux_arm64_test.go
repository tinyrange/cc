//go:build linux && arm64

package kvm

import (
	"testing"
)

type freeBSDArm64ManagedTestRoot struct{}

func (freeBSDArm64ManagedTestRoot) ReadAt([]byte, int64) (int, error)  { return 0, nil }
func (freeBSDArm64ManagedTestRoot) WriteAt([]byte, int64) (int, error) { return 0, nil }
func (freeBSDArm64ManagedTestRoot) Size() int64                        { return 512 }

func TestFreeBSDArm64BlockDisablesSizeMax(t *testing.T) {
	block := newFreeBSDArm64Block(freeBSDArm64ManagedTestRoot{})

	if !block.DisableSizeMax {
		t.Fatal("FreeBSD arm64 block should disable SIZE_MAX")
	}
}
