package ccvmd

import (
	"context"
	"net/http"

	"j5.nz/cc/client"
	internal "j5.nz/cc/internal/ccvmd"
)

type ServerOptions struct {
	Kind             string
	TokenPath        string
	RegisterHandlers func(*http.ServeMux, RuntimeView)
	WrapHandler      func(http.Handler) http.Handler
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
		Kind:      opts.Kind,
		TokenPath: opts.TokenPath,
		RegisterHandlers: func(mux *http.ServeMux, runtime internal.RuntimeView) {
			if opts.RegisterHandlers != nil {
				opts.RegisterHandlers(mux, runtime)
			}
		},
		WrapHandler: opts.WrapHandler,
	})
}
