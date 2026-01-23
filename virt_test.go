package cc_test

import (
	"testing"

	cc "github.com/tinyrange/cc"
)

func TestNewOCIClient(t *testing.T) {
	client, err := cc.NewOCIClient()
	if err != nil {
		t.Fatalf("NewOCIClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewOCIClient() returned nil")
	}
}

func TestOptions(t *testing.T) {
	// Verify options implement the Option interface
	var _ cc.Option = cc.WithMemoryMB(256)
	var _ cc.Option = cc.WithEnv("FOO=bar")
	var _ cc.Option = cc.WithTimeout(0)
	var _ cc.Option = cc.WithWorkdir("/app")
	var _ cc.Option = cc.WithUser("1000")

	// Verify pull options implement the OCIPullOption interface
	var _ cc.OCIPullOption = cc.WithPlatform("linux", "amd64")
	var _ cc.OCIPullOption = cc.WithAuth("user", "pass")
	var _ cc.OCIPullOption = cc.WithPullPolicy(cc.PullAlways)
}

func TestPullPolicy(t *testing.T) {
	if cc.PullIfNotPresent != 0 {
		t.Error("PullIfNotPresent should be 0")
	}
	if cc.PullAlways != 1 {
		t.Error("PullAlways should be 1")
	}
	if cc.PullNever != 2 {
		t.Error("PullNever should be 2")
	}
}
