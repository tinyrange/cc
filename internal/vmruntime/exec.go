package vmruntime

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	"j5.nz/cc/client"
)

type ManagedExecRequest struct {
	ID        string   `json:"id"`
	Command   []string `json:"command"`
	Env       []string `json:"env,omitempty"`
	RootDir   string   `json:"root_dir,omitempty"`
	Path      string   `json:"path,omitempty"`
	Directory bool     `json:"directory,omitempty"`
	WorkDir   string   `json:"workdir,omitempty"`
	User      string   `json:"user,omitempty"`
	Stdin     []byte   `json:"stdin,omitempty"`
	TTY       bool     `json:"tty,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	Signal    string   `json:"signal,omitempty"`
	Cols      int      `json:"cols,omitempty"`
	Rows      int      `json:"rows,omitempty"`
}

func HasManagedExecBegin(text, id string) bool {
	return strings.Contains(text, CommandBeginMarker+id)
}

func HasManagedExecFirstByte(text, id string) bool {
	return strings.Contains(text, CommandOutputMarker+id+":") ||
		strings.Contains(text, CommandErrorMarker+id+":") ||
		strings.Contains(text, CommandExitMarkerPref+id+":")
}

func ExtractManagedExecResult(serial, id string, dmesg bool) (int, string, *client.ResourceUsage, bool) {
	beginMarker := CommandBeginMarker + id
	outputPrefix := CommandOutputMarker + id + ":"
	errorPrefix := CommandErrorMarker + id + ":"
	usagePrefix := CommandUsageMarker + id + ":"
	exitPrefix := CommandExitMarkerPref + id + ":"

	begin := strings.Index(serial, beginMarker)
	if begin == -1 {
		return 0, "", nil, false
	}

	var output bytes.Buffer
	var usage *client.ResourceUsage
	lines := strings.Split(serial[begin:], "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, outputPrefix); idx >= 0 {
			encoded := line[idx+len(outputPrefix):]
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				continue
			}
			output.Write(data)
			continue
		}
		if idx := strings.Index(line, errorPrefix); idx >= 0 {
			encoded := line[idx+len(errorPrefix):]
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				continue
			}
			output.Write(data)
			continue
		}
		if idx := strings.Index(line, usagePrefix); idx >= 0 {
			encoded := line[idx+len(usagePrefix):]
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				continue
			}
			var parsed client.ResourceUsage
			if err := json.Unmarshal(data, &parsed); err == nil {
				usage = &parsed
			}
			continue
		}
		if idx := strings.Index(line, exitPrefix); idx >= 0 {
			code, err := strconv.Atoi(strings.TrimSpace(line[idx+len(exitPrefix):]))
			if err != nil {
				return 0, "", nil, false
			}
			if dmesg {
				return code, strings.TrimRight(serial[begin:], "\r\n"), usage, true
			}
			return code, strings.TrimRight(output.String(), "\r\n"), usage, true
		}
	}
	return 0, "", nil, false
}

func ParseManagedExecEventLine(line, id string) (client.ExecEvent, bool, bool, error) {
	beginMarker := CommandBeginMarker + id
	stdoutPrefix := CommandOutputMarker + id + ":"
	stderrPrefix := CommandErrorMarker + id + ":"
	exitPrefix := CommandExitMarkerPref + id + ":"

	switch {
	case line == beginMarker:
		return client.ExecEvent{}, false, false, nil
	case strings.HasPrefix(line, stdoutPrefix):
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, stdoutPrefix))
		if err != nil {
			return client.ExecEvent{}, false, false, nil
		}
		return client.ExecEvent{Kind: "stdout", Stream: "stdout", Output: string(data), Data: data}, false, true, nil
	case strings.HasPrefix(line, stderrPrefix):
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, stderrPrefix))
		if err != nil {
			return client.ExecEvent{}, false, false, nil
		}
		return client.ExecEvent{Kind: "stderr", Stream: "stderr", Output: string(data), Data: data}, false, true, nil
	case strings.HasPrefix(line, exitPrefix):
		code, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, exitPrefix)))
		if err != nil {
			return client.ExecEvent{}, false, false, err
		}
		return client.ExecEvent{Kind: "exit", ExitCode: code}, true, true, nil
	default:
		return client.ExecEvent{}, false, false, nil
	}
}
