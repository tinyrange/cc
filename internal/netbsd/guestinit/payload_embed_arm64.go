//go:build embed_guestinit && arm64

package guestinit

import _ "embed"

//go:embed guest-init-netbsd-arm64
var guestInitNetBSD []byte

func embeddedPayload(arch string) []byte {
	if arch == "arm64" {
		return guestInitNetBSD
	}
	return nil
}
