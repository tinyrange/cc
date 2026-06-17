package vm

import (
	"fmt"
	"strings"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/imagefs"
)

type managedExecResolver struct {
	root           imagefs.Directory
	baseEnv        []string
	defaultWorkDir string
	defaultRootDir string
	missingRootErr string
	env            func(base, overrides []string, replace bool) []string
	user           func(string) (string, error)
	markResolved   bool
}

func resolveManagedExecRequest(req client.ExecRequest, resolver managedExecResolver) (client.ExecRequest, error) {
	env := append([]string(nil), req.Env...)
	if resolver.env != nil {
		env = resolver.env(resolver.baseEnv, req.Env, req.ReplaceEnv)
	}
	user := req.User
	if resolver.user != nil {
		resolved, err := resolver.user(req.User)
		if err != nil {
			return client.ExecRequest{}, err
		}
		user = resolved
	}
	command := append([]string(nil), req.Command...)
	if !req.SkipResolve {
		if resolver.root == nil {
			if strings.TrimSpace(resolver.missingRootErr) != "" {
				return client.ExecRequest{}, fmt.Errorf("%s", resolver.missingRootErr)
			}
			return client.ExecRequest{}, fmt.Errorf("running instance does not have a root filesystem")
		}
		resolved, err := imagefs.ResolveCommand(resolver.root, req.Command, env)
		if err != nil {
			return client.ExecRequest{}, err
		}
		command = resolved
	}
	workDir := req.WorkDir
	if workDir == "" {
		workDir = resolver.defaultWorkDir
	}
	if workDir == "" {
		workDir = "/"
	}
	if !strings.HasPrefix(workDir, "/") {
		return client.ExecRequest{}, fmt.Errorf("workdir must be absolute")
	}
	rootDir := req.RootDir
	if rootDir == "" {
		rootDir = resolver.defaultRootDir
	}
	skipResolve := req.SkipResolve
	if resolver.markResolved {
		skipResolve = true
	}
	return client.ExecRequest{
		Kind:        req.Kind,
		Command:     command,
		Env:         env,
		RootDir:     rootDir,
		Path:        req.Path,
		Directory:   req.Directory,
		ReplaceEnv:  req.ReplaceEnv,
		SkipResolve: skipResolve,
		WorkDir:     workDir,
		User:        user,
		Stdin:       append([]byte(nil), req.Stdin...),
		TTY:         req.TTY,
		ControlFD:   req.ControlFD,
		Cols:        req.Cols,
		Rows:        req.Rows,
	}, nil
}

func controlExecRequest(req client.ExecRequest, defaultWorkDir string) client.ExecRequest {
	workDir := req.WorkDir
	if workDir == "" {
		workDir = defaultWorkDir
	}
	return client.ExecRequest{
		Kind:      req.Kind,
		RootDir:   req.RootDir,
		Path:      req.Path,
		Directory: req.Directory,
		WorkDir:   workDir,
		User:      req.User,
		Stdin:     append([]byte(nil), req.Stdin...),
	}
}
