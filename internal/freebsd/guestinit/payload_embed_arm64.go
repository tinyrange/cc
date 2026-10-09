//go:build arm64

package guestinit

import "embed"

//go:embed payloads
var payloadFiles embed.FS

func init() {
	embeddedPayloads["arm64"], _ = payloadFiles.ReadFile("payloads/guest-init-freebsd-arm64")
}
