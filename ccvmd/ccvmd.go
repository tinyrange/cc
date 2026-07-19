package ccvmd

import (
	"context"
	"crypto/tls"
	"net/http"

	"j5.nz/cc/client"
	internal "j5.nz/cc/internal/ccvmd"
)

type ServerOptions struct {
	Kind string
	// TokenPath is caller-owned. RunServer advertises it but does not remove it.
	TokenPath              string
	Authentication         *ServerAuthentication
	Persistent             bool
	OnStartup              func(client.ServerHello) error
	RegisterHandlers       func(*http.ServeMux, RuntimeView)
	WrapHandler            func(http.Handler) http.Handler
	NormalizeCreateRequest func(*client.CreateInstanceRequest, RuntimeView) error
	NormalizeStartRequest  func(*client.StartInstanceRequest, RuntimeView) error
	NormalizeRunRequest    func(*client.RunRequest, RuntimeView) error
}

// ServerAuthentication is a validated transport authentication policy for a
// ccvmd listener. Construct one with NewMutualTLSAuthentication.
type ServerAuthentication struct {
	internal *internal.ServerAuthentication
}

// NewMutualTLSAuthentication validates and wraps a TLS configuration that
// requires a verified client certificate on every connection.
func NewMutualTLSAuthentication(config *tls.Config) (*ServerAuthentication, error) {
	authentication, err := internal.NewMutualTLSAuthentication(config)
	if err != nil {
		return nil, err
	}
	return &ServerAuthentication{internal: authentication}, nil
}

type ListenerSecurityError = internal.ListenerSecurityError
type ListenerSecurityReason = internal.ListenerSecurityReason

const (
	ListenerSecurityInvalidAddress               = internal.ListenerSecurityInvalidAddress
	ListenerSecurityRemoteAuthenticationRequired = internal.ListenerSecurityRemoteAuthenticationRequired
	ListenerSecurityInvalidMutualTLS             = internal.ListenerSecurityInvalidMutualTLS
	ListenerSecurityConflictingAuthentication    = internal.ListenerSecurityConflictingAuthentication
)

type RuntimeView interface {
	InstanceStatuses() []client.InstanceState
	RunStreamIn(context.Context, string, client.RunRequest, <-chan client.ExecInput, func(client.ExecEvent) error) error
	ShutdownInstance(context.Context, string) error
	AllowServiceProxyPort(context.Context, string, int) error
	SetInstanceBalloon(string, uint64) error
}

func Main(args []string) {
	internal.Main(args)
}

func RunServer(args []string, opts ServerOptions) (bool, error) {
	return internal.RunServer(args, internal.ServerOptions{
		Kind:      opts.Kind,
		TokenPath: opts.TokenPath,
		Authentication: func() *internal.ServerAuthentication {
			if opts.Authentication == nil {
				return nil
			}
			return opts.Authentication.internal
		}(),
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
