//go:build embed_guestinit && amd64

package guestinit

import _ "embed"

//go:embed guest-init-netbsd-amd64
var guestInitNetBSD []byte

func embeddedPayload(arch string) []byte {
	if arch == "amd64" {
		return guestInitNetBSD
	}
	return nil
}
