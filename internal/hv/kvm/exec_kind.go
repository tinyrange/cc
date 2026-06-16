package kvm

import "j5.nz/cc/client"

func execRequestKind(kind string) string {
	if kind == "" {
		return "exec"
	}
	return kind
}

func emitManagedBootStatus(onEvent func(client.BootEvent) error, message string) error {
	if onEvent == nil {
		return nil
	}
	return onEvent(client.BootEvent{Kind: "status", Message: message})
}
