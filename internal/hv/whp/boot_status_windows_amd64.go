//go:build windows && amd64

package whp

import "j5.nz/cc/client"

func emitManagedBootStatus(onEvent func(client.BootEvent) error, message string) error {
	if onEvent == nil {
		return nil
	}
	return onEvent(client.BootEvent{Kind: "status", Message: message})
}
