package guestinit

import (
	"runtime"
	"testing"
)

func TestReleasePayloadIsEmbedded(t *testing.T) {
	payload := embeddedPayload(runtime.GOARCH)
	if len(payload) < 4 || string(payload[:4]) != "\x7fELF" {
		t.Fatalf("embedded FreeBSD/%s guest init is not an ELF payload", runtime.GOARCH)
	}
}
