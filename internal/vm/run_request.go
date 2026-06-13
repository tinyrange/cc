package vm

import "j5.nz/cc/client"

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
