//go:build embed_guestinit && amd64

package guestinit

import _ "embed"

//go:embed guest-init-openbsd-amd64
var guestInitOpenBSD []byte

func embeddedPayload(arch string) []byte {
	if arch == "amd64" {
		return guestInitOpenBSD
	}
	return nil
}
