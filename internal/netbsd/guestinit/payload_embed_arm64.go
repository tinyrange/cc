//go:build arm64

package guestinit

import _ "embed"

//go:embed guest-init-netbsd-arm64
var guestInitNetBSD []byte

func init() {
	embeddedPayloads["arm64"] = guestInitNetBSD
}
