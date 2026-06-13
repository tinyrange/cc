package client

import "testing"

func TestPullImageRequestSourceStringRootFSTar(t *testing.T) {
	req := PullImageRequest{SourceRef: &ImageSource{
		Type: "rootfs-tar",
		Path: "https://example.test/rootfs.tar.xz",
	}}
	source, err := req.SourceString()
	if err != nil {
		t.Fatalf("SourceString: %v", err)
	}
	if source != "rootfs-tar:https://example.test/rootfs.tar.xz" {
		t.Fatalf("source = %q", source)
	}
}
