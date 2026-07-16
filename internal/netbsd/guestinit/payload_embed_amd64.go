//go:build amd64

package guestinit

import _ "embed"

//go:embed guest-init-netbsd-amd64
var guestInitNetBSD []byte

func init() {
	embeddedPayloads["amd64"] = guestInitNetBSD
}
