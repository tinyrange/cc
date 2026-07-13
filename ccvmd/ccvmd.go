package ccvmd

import (
	"context"
	"net/http"

	"j5.nz/cc/client"
	internal "j5.nz/cc/internal/ccvmd"
)

type ServerOptions struct {
	Kind string
	// TokenPath is caller-owned. RunServer advertises it but does not remove it.
	TokenPath              string
	Persistent             bool
	OnStartup              func(client.ServerHello) error
	RegisterHandlers       func(*http.ServeMux, RuntimeView)
	WrapHandler            func(http.Handler) http.Handler
	NormalizeCreateRequest func(*client.CreateInstanceRequest, RuntimeView) error
	NormalizeStartRequest  func(*client.StartInstanceRequest, RuntimeView) error
	NormalizeRunRequest    func(*client.RunRequest, RuntimeView) error
}

type RuntimeView interface {
	InstanceStatuses() []client.InstanceState
	RunStreamIn(context.Context, string, client.RunRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
	ShutdownInstance(context.Context, string) error
}

func Main(args []string) {
	internal.Main(args)
}

func RunServer(args []string, opts ServerOptions) (bool, error) {
	return internal.RunServer(args, internal.ServerOptions{
		Kind:       opts.Kind,
		TokenPath:  opts.TokenPath,
		Persistent: opts.Persistent,
		OnStartup:  opts.OnStartup,
		NormalizeCreateRequest: func(req *client.CreateInstanceRequest, runtime internal.RuntimeView) error {
			if opts.NormalizeCreateRequest == nil {
				return nil
			}
			return opts.NormalizeCreateRequest(req, runtimeViewAdapter{runtime})
		},
		NormalizeStartRequest: func(req *client.StartInstanceRequest, runtime internal.RuntimeView) error {
			if opts.NormalizeStartRequest == nil {
				return nil
			}
			return opts.NormalizeStartRequest(req, runtimeViewAdapter{runtime})
		},
		NormalizeRunRequest: func(req *client.RunRequest, runtime internal.RuntimeView) error {
			if opts.NormalizeRunRequest == nil {
				return nil
			}
			return opts.NormalizeRunRequest(req, runtimeViewAdapter{runtime})
		},
		RegisterHandlers: func(mux *http.ServeMux, runtime internal.RuntimeView) {
			if opts.RegisterHandlers != nil {
				opts.RegisterHandlers(mux, runtime)
			}
		},
		WrapHandler: opts.WrapHandler,
	})
}

type runtimeViewAdapter struct {
	internal.RuntimeView
}
