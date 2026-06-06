package client

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type ServerHello struct {
	Addr   string `json:"addr,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Error  string `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type WatchdogActivityState struct {
	CVMFS WatchdogActivityCounter `json:"cvmfs"`
}

type WatchdogActivityCounter struct {
	Events           uint64  `json:"events"`
	Bytes            int64   `json:"bytes"`
	LastActivityUnix int64   `json:"last_activity_unix,omitempty"`
	SecondsSinceLast float64 `json:"seconds_since_last,omitempty"`
}

type WatchdogLeaseRequest struct {
	LeaseID        string  `json:"lease_id,omitempty"`
	TimeoutSeconds float64 `json:"timeout_seconds,omitempty"`
}

type WatchdogLeaseResponse struct {
	LeaseID        string  `json:"lease_id"`
	TimeoutSeconds float64 `json:"timeout_seconds"`
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

type ImageMetadataState struct {
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	SourceKind   string   `json:"source_kind,omitempty"`
	Architecture string   `json:"architecture,omitempty"`
	Env          []string `json:"env,omitempty"`
	Error        string   `json:"error,omitempty"`
}

type EmulatorState struct {
	Status   string `json:"status"`
	Path     string `json:"path,omitempty"`
	Required bool   `json:"required"`
	Error    string `json:"error,omitempty"`
}

type DownloadRequest struct {
	Source string `json:"source,omitempty"`
}

type ProgressEvent struct {
	Status             string  `json:"status"`
	Artifact           string  `json:"artifact,omitempty"`
	Progress           float64 `json:"progress,omitempty"`
	BytesDownloaded    int64   `json:"bytes_downloaded,omitempty"`
	BytesTotal         int64   `json:"bytes_total,omitempty"`
	FilesDownloaded    int64   `json:"files_downloaded,omitempty"`
	FilesTotal         int64   `json:"files_total,omitempty"`
	RateBytesPerSecond float64 `json:"rate_bytes_per_second,omitempty"`
	ETASeconds         float64 `json:"eta_seconds,omitempty"`
	Blob               string  `json:"blob,omitempty"`
	Error              string  `json:"error,omitempty"`
}

type ImageState struct {
	Name       string `json:"name"`
	Source     string `json:"source,omitempty"`
	SourceKind string `json:"source_kind,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

type SaveImageRequest struct {
	Name  string `json:"name"`
	Image string `json:"image,omitempty"`
}

type PullImageRequest struct {
	Source          string       `json:"-"`
	SourceRef       *ImageSource `json:"-"`
	Architecture    string       `json:"architecture,omitempty"`
	CacheDir        string       `json:"cache_dir,omitempty"`
	Prefetch        bool         `json:"prefetch,omitempty"`
	PrefetchWorkers int          `json:"prefetch_workers,omitempty"`
}

type ImageSource struct {
	Type    string   `json:"type"`
	Format  string   `json:"format,omitempty"`
	Mirror  string   `json:"mirror,omitempty"`
	Mirrors []string `json:"mirrors,omitempty"`
	Repo    string   `json:"repo,omitempty"`
	Path    string   `json:"path,omitempty"`
}

type CVMFSListRequest struct {
	Mirror   string   `json:"mirror,omitempty"`
	Mirrors  []string `json:"mirrors,omitempty"`
	Repo     string   `json:"repo"`
	Path     string   `json:"path,omitempty"`
	CacheDir string   `json:"cache_dir,omitempty"`
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
	Mirror   string   `json:"mirror,omitempty"`
	Mirrors  []string `json:"mirrors,omitempty"`
	Repo     string   `json:"repo"`
	Path     string   `json:"path"`
	Offset   int64    `json:"offset,omitempty"`
	Length   int64    `json:"length,omitempty"`
	CacheDir string   `json:"cache_dir,omitempty"`
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
	if r.Architecture != "" {
		payload["architecture"] = r.Architecture
	}
	if r.Prefetch {
		payload["prefetch"] = true
	}
	if r.PrefetchWorkers > 0 {
		payload["prefetch_workers"] = r.PrefetchWorkers
	}
	return json.Marshal(payload)
}

func (r *PullImageRequest) UnmarshalJSON(data []byte) error {
	var raw struct {
		Source          json.RawMessage `json:"source"`
		Architecture    string          `json:"architecture,omitempty"`
		CacheDir        string          `json:"cache_dir,omitempty"`
		Prefetch        bool            `json:"prefetch,omitempty"`
		PrefetchWorkers int             `json:"prefetch_workers,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Source = ""
	r.SourceRef = nil
	r.Architecture = raw.Architecture
	r.CacheDir = raw.CacheDir
	r.Prefetch = raw.Prefetch
	r.PrefetchWorkers = raw.PrefetchWorkers
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
	case "docker-archive":
		if strings.TrimSpace(r.SourceRef.Path) == "" {
			return "", fmt.Errorf("docker archive path is required")
		}
		return "docker-archive:" + r.SourceRef.Path, nil
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
	MapOwner bool   `json:"map_owner,omitempty"`
	OwnerUID uint32 `json:"owner_uid,omitempty"`
	OwnerGID uint32 `json:"owner_gid,omitempty"`
	Cache    string `json:"cache,omitempty"`
}

type NetworkConfig struct {
	Enabled       bool          `json:"enabled,omitempty"`
	AllowInternet bool          `json:"allow_internet,omitempty"`
	HostDNSName   string        `json:"host_dns_name,omitempty"`
	PortForwards  []PortForward `json:"port_forwards,omitempty"`
}

type PortForward struct {
	Protocol  string `json:"protocol,omitempty"`
	HostAddr  string `json:"host_addr,omitempty"`
	HostPort  int    `json:"host_port,omitempty"`
	GuestAddr string `json:"guest_addr,omitempty"`
	GuestPort int    `json:"guest_port,omitempty"`
}

type VMSupportedResponse struct {
	Supported bool   `json:"supported"`
	Error     string `json:"error,omitempty"`
}

type CapabilitiesResponse struct {
	Host                   string   `json:"host"`
	Backend                string   `json:"backend,omitempty"`
	VMSupported            bool     `json:"vm_supported"`
	VMError                string   `json:"vm_error,omitempty"`
	MaxInstances           int      `json:"max_instances,omitempty"`
	SnapshotClasses        []string `json:"snapshot_classes,omitempty"`
	NetworkModes           []string `json:"network_modes,omitempty"`
	ShareConsistency       []string `json:"share_consistency,omitempty"`
	ResourceLimits         []string `json:"resource_limits,omitempty"`
	SupportsMultiImageExec bool     `json:"supports_multi_image_exec"`
	SupportsNestedVirt     bool     `json:"supports_nested_virtualization"`
	RequiresPrivilegedCCX3 bool     `json:"requires_privileged_ccx3"`
	Notes                  []string `json:"notes,omitempty"`
}

type CreateInstanceRequest struct {
	ID             string         `json:"id,omitempty"`
	Image          string         `json:"image"`
	Shares         []ShareMount   `json:"shares,omitempty"`
	Network        *NetworkConfig `json:"network,omitempty"`
	KernelModules  []string       `json:"kernel_modules,omitempty"`
	MemoryMB       uint64         `json:"memory_mb,omitempty"`
	CPUs           int            `json:"cpus,omitempty"`
	NestedVirt     bool           `json:"nested_virtualization,omitempty"`
	Dmesg          bool           `json:"dmesg,omitempty"`
	TimeoutSeconds float64        `json:"timeout_seconds,omitempty"`
}

type StartInstanceRequest struct {
	ID             string         `json:"id,omitempty"`
	Image          string         `json:"image,omitempty"`
	Network        *NetworkConfig `json:"network,omitempty"`
	KernelModules  []string       `json:"kernel_modules,omitempty"`
	MemoryMB       uint64         `json:"memory_mb,omitempty"`
	CPUs           int            `json:"cpus,omitempty"`
	NestedVirt     bool           `json:"nested_virtualization,omitempty"`
	Dmesg          bool           `json:"dmesg,omitempty"`
	TimeoutSeconds float64        `json:"timeout_seconds,omitempty"`
}

type InstanceState struct {
	ID          string `json:"id,omitempty"`
	Status      string `json:"status"`
	Image       string `json:"image,omitempty"`
	MemoryMB    uint64 `json:"memory_mb,omitempty"`
	CPUs        int    `json:"cpus,omitempty"`
	NestedVirt  bool   `json:"nested_virtualization,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	NetworkIPv4 string `json:"network_ipv4,omitempty"`
	Error       string `json:"error,omitempty"`
}

type ConsoleHistoryResponse struct {
	History string `json:"history"`
}

type RunRequest struct {
	ID             string         `json:"id,omitempty"`
	Image          string         `json:"image"`
	Shares         []ShareMount   `json:"shares,omitempty"`
	Network        *NetworkConfig `json:"network,omitempty"`
	KernelModules  []string       `json:"kernel_modules,omitempty"`
	Command        []string       `json:"command,omitempty"`
	Env            []string       `json:"env,omitempty"`
	RootDir        string         `json:"root_dir,omitempty"`
	ReplaceEnv     bool           `json:"replace_env,omitempty"`
	WorkDir        string         `json:"workdir,omitempty"`
	User           string         `json:"user,omitempty"`
	Stdin          []byte         `json:"stdin,omitempty"`
	TTY            bool           `json:"tty,omitempty"`
	Cols           int            `json:"cols,omitempty"`
	Rows           int            `json:"rows,omitempty"`
	MemoryMB       uint64         `json:"memory_mb,omitempty"`
	CPUs           int            `json:"cpus,omitempty"`
	NestedVirt     bool           `json:"nested_virtualization,omitempty"`
	Dmesg          bool           `json:"dmesg,omitempty"`
	TimeoutSeconds float64        `json:"timeout_seconds,omitempty"`
}

type ExecResponse struct {
	ExitCode int            `json:"exit_code"`
	Output   string         `json:"output,omitempty"`
	Usage    *ResourceUsage `json:"usage,omitempty"`
}

type ResourceUsage struct {
	WallSeconds   float64 `json:"wall_seconds,omitempty"`
	UserSeconds   float64 `json:"user_seconds,omitempty"`
	SystemSeconds float64 `json:"system_seconds,omitempty"`
	CPUSeconds    float64 `json:"cpu_seconds,omitempty"`
	MaxRSSBytes   uint64  `json:"max_rss_bytes,omitempty"`
	MemoryBytes   uint64  `json:"memory_bytes,omitempty"`
}

type StartVMRequest = CreateInstanceRequest
type VMState = InstanceState
type RunVMResponse = ExecResponse

type ExecRequest struct {
	ID          string   `json:"id,omitempty"`
	Command     []string `json:"command"`
	Env         []string `json:"env,omitempty"`
	RootDir     string   `json:"root_dir,omitempty"`
	ReplaceEnv  bool     `json:"replace_env,omitempty"`
	SkipResolve bool     `json:"skip_resolve,omitempty"`
	WorkDir     string   `json:"workdir,omitempty"`
	User        string   `json:"user,omitempty"`
	Stdin       []byte   `json:"stdin,omitempty"`
	TTY         bool     `json:"tty,omitempty"`
	Cols        int      `json:"cols,omitempty"`
	Rows        int      `json:"rows,omitempty"`
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
