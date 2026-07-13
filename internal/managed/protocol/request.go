package protocol

const (
	ReadyMarker         = "__CCX3_READY__"
	BeginMarkerPrefix   = "__CCX3_BEGIN__:"
	OutputMarkerPrefix  = "__CCX3_OUT__:"
	ErrorMarkerPrefix   = "__CCX3_ERR__:"
	ControlMarkerPrefix = "__CCX3_CTL__:"
	UsageMarkerPrefix   = "__CCX3_USAGE__:"
	ExitMarkerPrefix    = "__CCX3_EXIT__:"
	TimingMarkerPrefix  = "__CCX3_TIMING__:"
)

type ManagedExecRequest struct {
	ID                      string   `json:"id"`
	Command                 []string `json:"command"`
	Env                     []string `json:"env,omitempty"`
	RootDir                 string   `json:"root_dir,omitempty"`
	Path                    string   `json:"path,omitempty"`
	Directory               bool     `json:"directory,omitempty"`
	ReplaceEnv              bool     `json:"replace_env,omitempty"`
	SkipResolve             bool     `json:"skip_resolve,omitempty"`
	WorkDir                 string   `json:"workdir,omitempty"`
	User                    string   `json:"user,omitempty"`
	Stdin                   []byte   `json:"stdin,omitempty"`
	TTY                     bool     `json:"tty,omitempty"`
	ControlFD               bool     `json:"control_fd,omitempty"`
	Kind                    string   `json:"kind,omitempty"`
	Signal                  string   `json:"signal,omitempty"`
	Cols                    int      `json:"cols,omitempty"`
	Rows                    int      `json:"rows,omitempty"`
	ArchiveMaxEntries       int      `json:"archive_max_entries,omitempty"`
	ArchiveMaxFileBytes     int64    `json:"archive_max_file_bytes,omitempty"`
	ArchiveMaxExpandedBytes int64    `json:"archive_max_expanded_bytes,omitempty"`
	ArchiveTimeoutSeconds   float64  `json:"archive_timeout_seconds,omitempty"`
}
