package client

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type ServerHello struct {
	Addr string `json:"addr"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type BootEvent struct {
	Kind    string        `json:"kind"`
	Message string        `json:"message,omitempty"`
	Data    string        `json:"data,omitempty"`
	Error   string        `json:"error,omitempty"`
	State   InstanceState `json:"state,omitempty"`
}

type KernelState struct {
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
}

type DownloadRequest struct {
	Source string `json:"source,omitempty"`
}

type ProgressEvent struct {
	Status   string  `json:"status"`
	Progress float64 `json:"progress,omitempty"`
	Blob     string  `json:"blob,omitempty"`
	Error    string  `json:"error,omitempty"`
}

type ImageState struct {
	Name       string `json:"name"`
	Source     string `json:"source,omitempty"`
	SourceKind string `json:"source_kind,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

type PullImageRequest struct {
	Source    string       `json:"-"`
	SourceRef *ImageSource `json:"-"`
	CacheDir  string       `json:"cache_dir,omitempty"`
}

type ImageSource struct {
	Type   string `json:"type"`
	Format string `json:"format,omitempty"`
	Mirror string `json:"mirror,omitempty"`
	Repo   string `json:"repo,omitempty"`
	Path   string `json:"path,omitempty"`
}

type CVMFSListRequest struct {
	Mirror   string `json:"mirror,omitempty"`
	Repo     string `json:"repo"`
	Path     string `json:"path,omitempty"`
	CacheDir string `json:"cache_dir,omitempty"`
}

type CVMFSDirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int64  `json:"size,omitempty"`
}

type CVMFSListResponse struct {
	Entries []CVMFSDirectoryEntry `json:"entries"`
}

type CVMFSReadRequest struct {
	Mirror   string `json:"mirror,omitempty"`
	Repo     string `json:"repo"`
	Path     string `json:"path"`
	Offset   int64  `json:"offset,omitempty"`
	Length   int64  `json:"length,omitempty"`
	CacheDir string `json:"cache_dir,omitempty"`
}

type CVMFSReadResponse struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset,omitempty"`
	Data   []byte `json:"data,omitempty"`
	EOF    bool   `json:"eof,omitempty"`
}

func (r PullImageRequest) MarshalJSON() ([]byte, error) {
	payload := map[string]any{}
	switch {
	case r.SourceRef != nil:
		payload["source"] = r.SourceRef
	case r.Source != "":
		payload["source"] = r.Source
	default:
		payload["source"] = ""
	}
	if r.CacheDir != "" {
		payload["cache_dir"] = r.CacheDir
	}
	return json.Marshal(payload)
}

func (r *PullImageRequest) UnmarshalJSON(data []byte) error {
	var raw struct {
		Source   json.RawMessage `json:"source"`
		CacheDir string          `json:"cache_dir,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Source = ""
	r.SourceRef = nil
	r.CacheDir = raw.CacheDir
	if len(raw.Source) == 0 || string(raw.Source) == "null" {
		return nil
	}
	var sourceString string
	if err := json.Unmarshal(raw.Source, &sourceString); err == nil {
		r.Source = sourceString
		return nil
	}
	var sourceRef ImageSource
	if err := json.Unmarshal(raw.Source, &sourceRef); err != nil {
		return fmt.Errorf("decode source: %w", err)
	}
	r.SourceRef = &sourceRef
	return nil
}

func (r PullImageRequest) SourceString() (string, error) {
	if strings.TrimSpace(r.Source) != "" {
		return r.Source, nil
	}
	if r.SourceRef == nil {
		return "", fmt.Errorf("image source is required")
	}
	switch strings.ToLower(strings.TrimSpace(r.SourceRef.Type)) {
	case "cvmfs":
		repo := strings.TrimSpace(r.SourceRef.Repo)
		if repo == "" {
			return "", fmt.Errorf("cvmfs repo is required")
		}
		pathValue := strings.TrimSpace(r.SourceRef.Path)
		if pathValue == "" {
			pathValue = "/"
		}
		mirror := strings.TrimRight(strings.TrimSpace(r.SourceRef.Mirror), "/")
		if mirror == "" {
			return fmt.Sprintf("cvmfs://%s%s", repo, ensureAbsolutePath(pathValue)), nil
		}
		mirror = ensureCVMFSMirrorPath(mirror)
		return fmt.Sprintf("%s/%s%s", mirror, repo, ensureAbsolutePath(pathValue)), nil
	case "simg":
		if strings.TrimSpace(r.SourceRef.Path) == "" {
			return "", fmt.Errorf("simg path is required")
		}
		return r.SourceRef.Path, nil
	case "oci":
		if strings.TrimSpace(r.SourceRef.Path) == "" {
			return "", fmt.Errorf("oci path is required")
		}
		return r.SourceRef.Path, nil
	default:
		return "", fmt.Errorf("unsupported source type %q", r.SourceRef.Type)
	}
}

func ensureAbsolutePath(value string) string {
	if value == "" {
		return "/"
	}
	if strings.HasPrefix(value, "/") {
		return value
	}
	return "/" + value
}

func ensureCVMFSMirrorPath(mirror string) string {
	mirror = strings.TrimRight(strings.TrimSpace(mirror), "/")
	u, err := url.Parse(mirror)
	if err != nil {
		if !strings.HasSuffix(mirror, "/cvmfs") {
			return mirror + "/cvmfs"
		}
		return mirror
	}
	if !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/cvmfs") {
		u.Path = strings.TrimRight(u.Path, "/") + "/cvmfs"
	}
	return strings.TrimRight(u.String(), "/")
}

type ShareMount struct {
	Source   string `json:"source"`
	Mount    string `json:"mount"`
	Writable bool   `json:"writable,omitempty"`
}

type VMSupportedResponse struct {
	Supported bool   `json:"supported"`
	Error     string `json:"error,omitempty"`
}

type CreateInstanceRequest struct {
	Image    string       `json:"image"`
	Shares   []ShareMount `json:"shares,omitempty"`
	MemoryMB uint64       `json:"memory_mb,omitempty"`
	CPUs     int          `json:"cpus,omitempty"`
	Dmesg    bool         `json:"dmesg,omitempty"`
}

type InstanceState struct {
	Status    string `json:"status"`
	Image     string `json:"image,omitempty"`
	MemoryMB  uint64 `json:"memory_mb,omitempty"`
	CPUs      int    `json:"cpus,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

type RunRequest struct {
	Image    string       `json:"image"`
	Shares   []ShareMount `json:"shares,omitempty"`
	Command  []string     `json:"command,omitempty"`
	Env      []string     `json:"env,omitempty"`
	WorkDir  string       `json:"workdir,omitempty"`
	User     string       `json:"user,omitempty"`
	Stdin    []byte       `json:"stdin,omitempty"`
	TTY      bool         `json:"tty,omitempty"`
	Cols     int          `json:"cols,omitempty"`
	Rows     int          `json:"rows,omitempty"`
	MemoryMB uint64       `json:"memory_mb,omitempty"`
	CPUs     int          `json:"cpus,omitempty"`
	Dmesg    bool         `json:"dmesg,omitempty"`
}

type ExecResponse struct {
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output,omitempty"`
}

type StartVMRequest = CreateInstanceRequest
type VMState = InstanceState
type RunVMResponse = ExecResponse

type ExecRequest struct {
	Command []string `json:"command"`
	Env     []string `json:"env,omitempty"`
	WorkDir string   `json:"workdir,omitempty"`
	User    string   `json:"user,omitempty"`
	Stdin   []byte   `json:"stdin,omitempty"`
	TTY     bool     `json:"tty,omitempty"`
	Cols    int      `json:"cols,omitempty"`
	Rows    int      `json:"rows,omitempty"`
}

type ExecInput struct {
	Kind   string `json:"kind"`
	Input  string `json:"input,omitempty"`
	Data   []byte `json:"data,omitempty"`
	Signal string `json:"signal,omitempty"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
}

type ExecEvent struct {
	Kind     string `json:"kind"`
	Stream   string `json:"stream,omitempty"`
	Output   string `json:"output,omitempty"`
	Data     []byte `json:"data,omitempty"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
}
