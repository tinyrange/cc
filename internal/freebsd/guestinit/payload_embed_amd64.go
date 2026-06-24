//go:build embed_guestinit && amd64

package guestinit

import _ "embed"

//go:embed guest-init-freebsd-amd64
var guestInitFreeBSD []byte

func embeddedPayload(arch string) []byte {
	if arch == "amd64" {
		return guestInitFreeBSD
	}
	return nil
}
