package vm

import (
	"j5.nz/cc/client"
	"j5.nz/cc/internal/vmruntime"
)

func runExecRequest(req client.RunRequest) client.ExecRequest {
	return client.ExecRequest{
		Command:    append([]string(nil), req.Command...),
		Env:        append([]string(nil), req.Env...),
		RootDir:    req.RootDir,
		ReplaceEnv: req.ReplaceEnv,
		WorkDir:    req.WorkDir,
		User:       req.User,
		Stdin:      append([]byte(nil), req.Stdin...),
		TTY:        req.TTY,
		ControlFD:  req.ControlFD,
		Cols:       req.Cols,
		Rows:       req.Rows,
	}
}

func mergeImageRunEnv(base, overrides []string, _ bool) []string {
	return vmruntime.WithDefaultEnv(vmruntime.MergeEnv(base, overrides))
}
