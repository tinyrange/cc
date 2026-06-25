package ccvmd

import (
	"net/http"

	internal "j5.nz/cc/internal/ccvmd"
)

type ServerOptions struct {
	Kind             string
	TokenPath        string
	RegisterHandlers func(*http.ServeMux)
	WrapHandler      func(http.Handler) http.Handler
}

func Main(args []string) {
	internal.Main(args)
}

func RunServer(args []string, opts ServerOptions) (bool, error) {
	return internal.RunServer(args, internal.ServerOptions{
		Kind:             opts.Kind,
		TokenPath:        opts.TokenPath,
		RegisterHandlers: opts.RegisterHandlers,
		WrapHandler:      opts.WrapHandler,
	})
}
