package fslayer

import (
	"crypto/sha256"
	"encoding/hex"
)

// DeriveKey derives a cache key from a parent key and an operation key.
// The resulting key is deterministic and uniquely identifies the state
// after applying the operation to the parent state.
func DeriveKey(parentKey string, opKey string) string {
	h := sha256.New()
	h.Write([]byte(parentKey))
	h.Write([]byte{0}) // Separator
	h.Write([]byte(opKey))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// BaseKey generates a cache key for a base image.
func BaseKey(imageRef string, arch string) string {
	h := sha256.New()
	h.Write([]byte("base:"))
	h.Write([]byte(imageRef))
	h.Write([]byte{0})
	h.Write([]byte(arch))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// SnapshotOpKey generates an operation key for a snapshot with a layer hash.
func SnapshotOpKey(layerHash string) string {
	return "snapshot:" + layerHash
}

// RunOpKey generates an operation key for a Run operation.
func RunOpKey(cmd []string, env []string, workDir string) string {
	h := sha256.New()
	h.Write([]byte("run:"))
	for _, c := range cmd {
		h.Write([]byte(c))
		h.Write([]byte{0})
	}
	h.Write([]byte{1}) // Separator between cmd and env
	for _, e := range env {
		h.Write([]byte(e))
		h.Write([]byte{0})
	}
	h.Write([]byte{1}) // Separator
	h.Write([]byte(workDir))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// CopyOpKey generates an operation key for a Copy operation.
func CopyOpKey(src, dst string, contentHash string) string {
	h := sha256.New()
	h.Write([]byte("copy:"))
	h.Write([]byte(src))
	h.Write([]byte{0})
	h.Write([]byte(dst))
	h.Write([]byte{0})
	h.Write([]byte(contentHash))
	return hex.EncodeToString(h.Sum(nil))[:32]
}
