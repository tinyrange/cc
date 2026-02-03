//! Type definitions and enums.

/// Pull policy for image fetching.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum PullPolicy {
    /// Pull only if the image is not present locally.
    #[default]
    IfNotPresent,
    /// Always pull the latest image from the registry.
    Always,
    /// Never pull; fail if image is not present locally.
    Never,
}

impl From<PullPolicy> for i32 {
    fn from(policy: PullPolicy) -> i32 {
        match policy {
            PullPolicy::IfNotPresent => 0,
            PullPolicy::Always => 1,
            PullPolicy::Never => 2,
        }
    }
}

/// Seek origin for file operations.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum SeekWhence {
    /// Seek from beginning of file.
    #[default]
    Set,
    /// Seek from current position.
    Current,
    /// Seek from end of file.
    End,
}

impl From<SeekWhence> for i32 {
    fn from(whence: SeekWhence) -> i32 {
        match whence {
            SeekWhence::Set => 0,
            SeekWhence::Current => 1,
            SeekWhence::End => 2,
        }
    }
}

/// File open flags.
pub mod flags {
    /// Read only.
    pub const O_RDONLY: i32 = 0x0000;
    /// Write only.
    pub const O_WRONLY: i32 = 0x0001;
    /// Read and write.
    pub const O_RDWR: i32 = 0x0002;
    /// Append mode.
    pub const O_APPEND: i32 = 0x0008;
    /// Create if not exists.
    pub const O_CREATE: i32 = 0x0200;
    /// Truncate to zero.
    pub const O_TRUNC: i32 = 0x0400;
    /// Exclusive create (fail if exists).
    pub const O_EXCL: i32 = 0x0800;
}

/// Options for pulling images.
#[derive(Debug, Clone, Default)]
pub struct PullOptions {
    /// Target OS (e.g., "linux").
    pub platform_os: Option<String>,
    /// Target architecture (e.g., "amd64", "arm64").
    pub platform_arch: Option<String>,
    /// Registry username for authentication.
    pub username: Option<String>,
    /// Registry password for authentication.
    pub password: Option<String>,
    /// Pull policy.
    pub policy: PullPolicy,
}

/// Mount configuration for virtio-fs.
#[derive(Debug, Clone)]
pub struct MountConfig {
    /// Mount tag (guest uses: mount -t virtiofs <tag> /mnt).
    pub tag: String,
    /// Host directory (None for empty writable fs).
    pub host_path: Option<String>,
    /// Whether the mount is writable (default: read-only).
    pub writable: bool,
}

/// Options for creating an instance.
#[derive(Debug, Clone)]
pub struct InstanceOptions {
    /// Memory in MB (default: 256).
    pub memory_mb: u64,
    /// Number of vCPUs (default: 1).
    pub cpus: i32,
    /// Instance timeout in seconds (0 for no timeout).
    pub timeout_seconds: f64,
    /// User:group to run as (e.g., "1000:1000").
    pub user: Option<String>,
    /// Enable kernel dmesg output.
    pub enable_dmesg: bool,
    /// Mounts.
    pub mounts: Vec<MountConfig>,
}

impl Default for InstanceOptions {
    fn default() -> Self {
        Self {
            memory_mb: 256,
            cpus: 1,
            timeout_seconds: 0.0,
            user: None,
            enable_dmesg: false,
            mounts: Vec::new(),
        }
    }
}

/// File information structure.
#[derive(Debug, Clone)]
pub struct FileInfo {
    /// File name.
    pub name: String,
    /// File size in bytes.
    pub size: i64,
    /// File mode/permissions.
    pub mode: u32,
    /// Modification time as Unix timestamp (seconds).
    pub mod_time_unix: i64,
    /// Whether this is a directory.
    pub is_dir: bool,
    /// Whether this is a symbolic link.
    pub is_symlink: bool,
}

/// Directory entry.
#[derive(Debug, Clone)]
pub struct DirEntry {
    /// Entry name.
    pub name: String,
    /// Whether this is a directory.
    pub is_dir: bool,
    /// Entry mode/permissions.
    pub mode: u32,
}

/// OCI image configuration.
#[derive(Debug, Clone, Default)]
pub struct ImageConfig {
    /// Architecture (e.g., "amd64", "arm64").
    pub architecture: Option<String>,
    /// Environment variables as "KEY=VALUE" strings.
    pub env: Vec<String>,
    /// Working directory.
    pub working_dir: Option<String>,
    /// Entrypoint command.
    pub entrypoint: Vec<String>,
    /// Default command arguments.
    pub cmd: Vec<String>,
    /// User to run as.
    pub user: Option<String>,
}

/// System capabilities.
#[derive(Debug, Clone)]
pub struct Capabilities {
    /// Whether hypervisor is available.
    pub hypervisor_available: bool,
    /// Maximum memory in MB (0 if unknown).
    pub max_memory_mb: u64,
    /// Maximum number of CPUs (0 if unknown).
    pub max_cpus: i32,
    /// Architecture (e.g., "x86_64", "arm64").
    pub architecture: String,
}

/// Options for filesystem snapshots.
#[derive(Debug, Clone, Default)]
pub struct SnapshotOptions {
    /// Glob patterns to exclude.
    pub excludes: Vec<String>,
    /// Cache directory for layers.
    pub cache_dir: Option<String>,
}

/// Download progress information.
#[derive(Debug, Clone)]
pub struct DownloadProgress {
    /// Bytes downloaded so far.
    pub current: i64,
    /// Total bytes (-1 if unknown).
    pub total: i64,
    /// Current file being downloaded.
    pub filename: Option<String>,
    /// Current blob index (0-based).
    pub blob_index: i32,
    /// Total number of blobs.
    pub blob_count: i32,
    /// Download speed in bytes per second.
    pub bytes_per_second: f64,
    /// Estimated time remaining in seconds (-1 if unknown).
    pub eta_seconds: f64,
}

/// Command output.
#[derive(Debug, Clone)]
pub struct CommandOutput {
    /// Standard output.
    pub stdout: Vec<u8>,
    /// Exit code.
    pub exit_code: i32,
}
