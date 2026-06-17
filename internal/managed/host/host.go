package host

import (
	"context"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/managed/rootartifact"
	managedsession "j5.nz/cc/internal/managed/session"
)

type StartRequest struct {
	Spec        machine.Spec
	Artifact    rootartifact.Artifact
	Attachments any
}

type VMM interface {
	Start(context.Context, StartRequest, func(client.BootEvent) error) (managedsession.Session, error)
}
