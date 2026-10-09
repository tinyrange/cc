//go:build amd64

package guestinit

import "embed"

//go:embed payloads
var payloadFiles embed.FS

func init() {
	embeddedPayloads["amd64"], _ = payloadFiles.ReadFile("payloads/guest-init-netbsd-amd64")
}
