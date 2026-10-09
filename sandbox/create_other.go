//go:build !linux || !amd64

package sandbox

import (
	"context"
	"errors"
)

type Runtime struct{}

func NewRuntime(string) (*Runtime, error) {
	return nil, errors.New("embedded sandbox requires Linux amd64 with KVM")
}
func (*Runtime) Create(context.Context, Config, func(Progress)) (*Sandbox, error) {
	return nil, errors.New("embedded sandbox requires Linux amd64 with KVM")
}
