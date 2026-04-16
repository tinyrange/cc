package client

type ServerHello struct {
	Addr string `json:"addr"`
}

type ErrorResponse struct {
	Error string `json:"error"`
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
	Source string `json:"source"`
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
