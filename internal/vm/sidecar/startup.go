package sidecar

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"j5.nz/cc/client"
)

func ReadStartupHello(r io.Reader) (client.ServerHello, error) {
	var hello client.ServerHello
	if err := json.NewDecoder(r).Decode(&hello); err != nil {
		return client.ServerHello{}, fmt.Errorf("read sidecar startup banner: %w", err)
	}
	if hello.Error != "" || hello.Kind == "error" {
		detail := firstNonEmpty(hello.Detail, hello.Error, "unknown startup error")
		return client.ServerHello{}, fmt.Errorf("sidecar ccvm failed to start: %s", detail)
	}
	if strings.TrimSpace(hello.Addr) == "" {
		return client.ServerHello{}, fmt.Errorf("sidecar ccvm did not report an address")
	}
	return hello, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
