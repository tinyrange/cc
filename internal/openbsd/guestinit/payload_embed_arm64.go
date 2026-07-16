//go:build arm64

package guestinit

import _ "embed"

//go:embed guest-init-openbsd-arm64
var guestInitOpenBSD []byte

func init() {
	embeddedPayloads["arm64"] = guestInitOpenBSD
}
