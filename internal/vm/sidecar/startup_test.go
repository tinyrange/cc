package sidecar

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadStartupHello(t *testing.T) {
	got, err := ReadStartupHello(bytes.NewBufferString(`{"addr":"127.0.0.1:1234"}`))
	if err != nil {
		t.Fatalf("ReadStartupHello: %v", err)
	}
	if got.Addr != "127.0.0.1:1234" {
		t.Fatalf("Addr = %q", got.Addr)
	}
}

func TestReadStartupHelloRejectsMalformedJSON(t *testing.T) {
	_, err := ReadStartupHello(bytes.NewBufferString(`{`))
	if err == nil || !strings.Contains(err.Error(), "read sidecar startup banner") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadStartupHelloRejectsErrorBanner(t *testing.T) {
	_, err := ReadStartupHello(bytes.NewBufferString(`{"kind":"error","detail":"no host support"}`))
	if err == nil || !strings.Contains(err.Error(), "sidecar ccvm failed to start: no host support") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadStartupHelloRejectsMissingAddress(t *testing.T) {
	_, err := ReadStartupHello(bytes.NewBufferString(`{"addr":"   "}`))
	if err == nil || !strings.Contains(err.Error(), "did not report an address") {
		t.Fatalf("err = %v", err)
	}
}
