package client

import (
	"encoding/json"
	"testing"
)

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

func TestPullImageRequestRoundTripsExperimentalImageControls(t *testing.T) {
	want := PullImageRequest{
		Source:         "registry.example.test/team/neuro:latest",
		Architecture:   "amd64",
		Refresh:        true,
		KeepCompressed: true,
		ActivateFrom:   "ndappx/neuro-staged",
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got PullImageRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Source != want.Source || got.Architecture != want.Architecture || !got.Refresh || !got.KeepCompressed || got.ActivateFrom != want.ActivateFrom {
		t.Fatalf("round-tripped request = %+v", got)
	}
}
