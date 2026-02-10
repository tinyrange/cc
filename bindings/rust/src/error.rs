//! Error types for the cc crate.

use thiserror::Error;

/// Result type alias for cc operations.
pub type Result<T> = std::result::Result<T, Error>;

/// Error type for cc operations.
#[derive(Error, Debug)]
pub enum Error {
    /// Handle is NULL, zero, or already freed.
    #[error("invalid handle")]
    InvalidHandle,

    /// Function argument is invalid.
    #[error("invalid argument: {0}")]
    InvalidArgument(String),

    /// Instance has terminated.
    #[error("instance not running")]
    NotRunning,

    /// Resource was already closed.
    #[error("already closed")]
    AlreadyClosed,

    /// Operation exceeded time limit.
    #[error("timeout")]
    Timeout,

    /// No hypervisor support on this system.
    #[error("hypervisor unavailable: {0}")]
    HypervisorUnavailable(String),

    /// Filesystem I/O error (local to guest).
    #[error("I/O error: {message}")]
    Io {
        /// Error message.
        message: String,
        /// Operation that failed.
        op: Option<String>,
        /// Path involved.
        path: Option<String>,
    },

    /// Network error (DNS, TCP connect, etc.).
    #[error("network error: {0}")]
    Network(String),

    /// Operation was cancelled via cancel token.
    #[error("cancelled")]
    Cancelled,

    /// Unknown error.
    #[error("unknown error: {0}")]
    Unknown(String),
}

impl Error {
    /// Check if this is a hypervisor unavailable error.
    pub fn is_hypervisor_unavailable(&self) -> bool {
        matches!(self, Error::HypervisorUnavailable(_))
    }

    /// Check if this is a timeout error.
    pub fn is_timeout(&self) -> bool {
        matches!(self, Error::Timeout)
    }

    /// Check if this is a cancelled error.
    pub fn is_cancelled(&self) -> bool {
        matches!(self, Error::Cancelled)
    }

    /// Check if this is an I/O error.
    pub fn is_io(&self) -> bool {
        matches!(self, Error::Io { .. })
    }
}
