//go:build embed_guestinit && arm64

package guestinit

import _ "embed"

//go:embed guest-init-freebsd-arm64
var guestInitFreeBSD []byte

func embeddedPayload(arch string) []byte {
	if arch == "arm64" {
		return guestInitFreeBSD
	}
	return nil
}
