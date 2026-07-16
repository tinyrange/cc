//go:build amd64

package guestinit

import _ "embed"

//go:embed guest-init-freebsd-amd64
var guestInitFreeBSD []byte

func init() {
	embeddedPayloads["amd64"] = guestInitFreeBSD
}
