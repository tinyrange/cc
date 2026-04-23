package vmruntime

import (
	"bytes"
	"encoding/base64"
	"strconv"
	"strings"
)

type ManagedExecRequest struct {
	ID      string   `json:"id"`
	Command []string `json:"command"`
	Env     []string `json:"env,omitempty"`
	WorkDir string   `json:"workdir,omitempty"`
	Stdin   []byte   `json:"stdin,omitempty"`
	TTY     bool     `json:"tty,omitempty"`
	Kind    string   `json:"kind,omitempty"`
	Signal  string   `json:"signal,omitempty"`
	Cols    int      `json:"cols,omitempty"`
	Rows    int      `json:"rows,omitempty"`
}

func HasManagedExecBegin(text, id string) bool {
	return strings.Contains(text, CommandBeginMarker+id)
}

func HasManagedExecFirstByte(text, id string) bool {
	return strings.Contains(text, CommandOutputMarker+id+":") ||
		strings.Contains(text, CommandErrorMarker+id+":") ||
		strings.Contains(text, CommandExitMarkerPref+id+":")
}

func ExtractManagedExecResult(serial, id string, dmesg bool) (int, string, bool) {
	beginMarker := CommandBeginMarker + id
	outputPrefix := CommandOutputMarker + id + ":"
	errorPrefix := CommandErrorMarker + id + ":"
	exitPrefix := CommandExitMarkerPref + id + ":"

	begin := strings.Index(serial, beginMarker)
	if begin == -1 {
		return 0, "", false
	}

	var output bytes.Buffer
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
		if idx := strings.Index(line, exitPrefix); idx >= 0 {
			code, err := strconv.Atoi(strings.TrimSpace(line[idx+len(exitPrefix):]))
			if err != nil {
				return 0, "", false
			}
			if dmesg {
				return code, strings.TrimRight(serial[begin:], "\r\n"), true
			}
			return code, strings.TrimRight(output.String(), "\r\n"), true
		}
	}
	return 0, "", false
}
