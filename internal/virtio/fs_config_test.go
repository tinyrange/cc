package virtio

import "testing"

func TestVirtioFSKickPollingRequiresExplicitOptIn(t *testing.T) {
	t.Setenv("CCX3_VIRTIOFS_KICK_POLL", "")
	if resolveVirtioFSKickPoll() {
		t.Fatal("virtio-fs kick polling enabled without explicit opt-in")
	}

	t.Setenv("CCX3_VIRTIOFS_KICK_POLL", "true")
	if !resolveVirtioFSKickPoll() {
		t.Fatal("virtio-fs kick polling not enabled by explicit opt-in")
	}

	t.Setenv("CCX3_VIRTIOFS_KICK_POLL", "invalid")
	if resolveVirtioFSKickPoll() {
		t.Fatal("invalid virtio-fs kick polling setting enabled polling")
	}
}
