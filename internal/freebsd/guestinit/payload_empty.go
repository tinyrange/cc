//go:build !embed_guestinit || (!amd64 && !arm64)

package guestinit

func embeddedPayload(arch string) []byte {
	return nil
}
