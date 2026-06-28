//go:build embed_guestinit && arm64

package guestinit

import _ "embed"

//go:embed guest-init-openbsd-arm64
var guestInitOpenBSD []byte

func embeddedPayload(arch string) []byte {
	if arch == "arm64" {
		return guestInitOpenBSD
	}
	return nil
}
