//go:build amd64

package guestinit

import _ "embed"

//go:embed guest-init-openbsd-amd64
var guestInitOpenBSD []byte

func init() {
	embeddedPayloads["amd64"] = guestInitOpenBSD
}
