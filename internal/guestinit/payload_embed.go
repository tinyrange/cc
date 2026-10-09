package guestinit

import "embed"

//go:embed payloads
var payloadFiles embed.FS

func init() {
	embeddedPayloads["amd64"], _ = payloadFiles.ReadFile("payloads/guest-init-linux-amd64")
	embeddedPayloads["arm64"], _ = payloadFiles.ReadFile("payloads/guest-init-linux-arm64")
}
