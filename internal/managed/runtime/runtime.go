package runtime

import (
	"context"
	"fmt"
	"strings"

	"j5.nz/cc/client"
	managedguest "j5.nz/cc/internal/managed/guest"
	managedhost "j5.nz/cc/internal/managed/host"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/managed/rootartifact"
	managedsession "j5.nz/cc/internal/managed/session"
)

type StartRequest struct {
	Profile     managedguest.Profile
	Host        managedhost.VMM
	Spec        machine.Spec
	Artifact    rootartifact.Artifact
	Attachments any
}

type Started struct {
	Profile  managedguest.Profile
	Spec     machine.Spec
	Artifact rootartifact.Artifact
	Session  managedsession.Session
}

type Service struct{}

func (Service) Start(ctx context.Context, req StartRequest, onEvent func(client.BootEvent) error) (Started, error) {
	if req.Host == nil {
		_ = req.Artifact.Close()
		return Started{}, fmt.Errorf("managed runtime host is not configured")
	}
	if strings.TrimSpace(req.Profile.Name) == "" {
		_ = req.Artifact.Close()
		return Started{}, fmt.Errorf("managed runtime guest profile is not configured")
	}
	spec := req.Spec
	if spec.Guest == "" {
		spec.Guest = req.Profile.Name
	} else if !strings.EqualFold(strings.TrimSpace(spec.Guest), strings.TrimSpace(req.Profile.Name)) {
		_ = req.Artifact.Close()
		return Started{}, fmt.Errorf("managed runtime guest %q does not match profile %q", spec.Guest, req.Profile.Name)
	}
	session, err := req.Host.Start(ctx, managedhost.StartRequest{
		Spec:        spec,
		Artifact:    req.Artifact,
		Attachments: req.Attachments,
	}, onEvent)
	if err != nil {
		_ = req.Artifact.Close()
		return Started{}, err
	}
	return Started{
		Profile:  req.Profile,
		Spec:     spec,
		Artifact: req.Artifact,
		Session:  session,
	}, nil
}
