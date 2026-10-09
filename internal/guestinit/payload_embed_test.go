package guestinit

import (
	"context"
	"testing"
)

func TestReleasePayloadsAreEmbedded(t *testing.T) {
	for _, arch := range []string{"arm64", "amd64"} {
		if len(embeddedPayload(arch)) == 0 {
			if _, err := BuildForArch(context.Background(), "", arch); err == nil {
				t.Fatal("missing payload accepted")
			}
			continue
		}
		if err := validateGuestInitPayload(arch, embeddedPayload(arch)); err != nil {
			t.Errorf("%s: %v", arch, err)
		}
	}
}
