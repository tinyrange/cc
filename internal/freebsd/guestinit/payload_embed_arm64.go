//go:build arm64

package guestinit

import _ "embed"

//go:embed guest-init-freebsd-arm64
var guestInitFreeBSD []byte

func init() {
	embeddedPayloads["arm64"] = guestInitFreeBSD
}
