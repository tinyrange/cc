package oci

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"j5.nz/cc/internal/download"
	"strings"
)

func verifyManifestSHA256(body []byte, expected string) error {
	if !strings.HasPrefix(expected, "sha256:") || len(expected) != 71 {
		return fmt.Errorf("unsupported manifest digest %q", expected)
	}
	sum := sha256.Sum256(body)
	actual := "sha256:" + hex.EncodeToString(sum[:])
	if !strings.EqualFold(expected, actual) {
		return &download.DigestError{Expected: expected, Actual: actual}
	}
	return nil
}
