//! Rust bindings for the cc virtualization library.
//!
//! This crate provides a safe Rust interface to cc's virtualization primitives,
//! allowing you to pull OCI images, create VM instances, and interact with them
//! through filesystem operations, command execution, and networking.
//!
//! # Example
//!
//! ```no_run
//! use cc::{OciClient, Instance, InstanceOptions};
//!
//! fn main() -> cc::Result<()> {
//!     // Initialize the library
//!     cc::init()?;
//!
//!     // Check for hypervisor support
//!     if cc::supports_hypervisor()? {
//!         println!("Hypervisor available!");
//!     }
//!
//!     // Pull an image and create an instance
//!     let client = OciClient::new()?;
//!     let source = client.pull("alpine:latest", None, None)?;
//!
//!     let opts = InstanceOptions {
//!         memory_mb: 512,
//!         cpus: 2,
//!         ..Default::default()
//!     };
//!
//!     let inst = Instance::new(source, Some(opts))?;
//!     println!("Instance ID: {}", inst.id());
//!
//!     // Run a command
//!     let output = inst.command("echo", &["Hello from Rust!"])?.output()?;
//!     println!("Output: {}", String::from_utf8_lossy(&output.stdout));
//!
//!     // File operations
//!     inst.write_file("/tmp/test.txt", b"Hello, World!", 0o644)?;
//!     let data = inst.read_file("/tmp/test.txt")?;
//!     println!("Read: {}", String::from_utf8_lossy(&data));
//!
//!     // Shutdown when done
//!     cc::shutdown();
//!
//!     Ok(())
//! }
//! ```
//!
//! # Code Signing
//!
//! The user's Rust binary does NOT need code signing. The libcc shared library
//! handles virtualization through cc-helper, which is already signed with the
//! necessary entitlements. Your application simply links against libcc.

pub mod client;
pub mod cmd;
pub mod error;
mod ffi;
pub mod file;
pub mod instance;
pub mod net;
pub mod snapshot;
pub mod types;

// Re-export main types at the crate root
pub use client::{CancelToken, InstanceSource, OciClient};
pub use cmd::Cmd;
pub use error::{Error, Result};
pub use file::File;
pub use instance::Instance;
pub use net::{Conn, Listener};
pub use snapshot::Snapshot;
pub use types::{
    flags, Capabilities, CommandOutput, DirEntry, DockerfileOptions, DownloadProgress, FileInfo,
    ImageConfig, InstanceOptions, MountConfig, PullOptions, PullPolicy, SeekWhence, SnapshotOptions,
};

use std::ffi::CStr;

/// API version constants.
pub mod version {
    /// API major version.
    pub const MAJOR: i32 = 0;
    /// API minor version.
    pub const MINOR: i32 = 1;
    /// API patch version.
    pub const PATCH: i32 = 0;
    /// Guest protocol version.
    pub const GUEST_PROTOCOL: i32 = 1;
}

/// Get the API version string (e.g., "0.1.0").
pub fn api_version() -> String {
    unsafe {
        let ptr = ffi::cc_api_version();
        if ptr.is_null() {
            return String::new();
        }
        let s = CStr::from_ptr(ptr).to_string_lossy().into_owned();
        ffi::cc_free_string(ptr as *mut i8);
        s
    }
}

/// Check if the runtime library is compatible with the given version.
///
/// Returns `true` if the library is compatible with code compiled against
/// the specified major.minor version.
pub fn api_version_compatible(major: i32, minor: i32) -> bool {
    unsafe { ffi::cc_api_version_compatible(major, minor) }
}

/// Initialize the library.
///
/// Must be called before any other function. Safe to call multiple times
/// (reference counted).
pub fn init() -> Result<()> {
    let code = unsafe { ffi::cc_init() };
    if code != ffi::CC_OK {
        return Err(Error::Unknown(format!("init failed with code {}", code)));
    }
    Ok(())
}

/// Shutdown the library and release all resources.
///
/// After shutdown, all handles become invalid and any function call
/// (except `init`) returns an error.
///
/// Reference counted: only shuts down when all `init` calls are balanced.
pub fn shutdown() {
    unsafe {
        ffi::cc_shutdown();
    }
}

/// Check if hypervisor is available on this system.
///
/// Returns `Ok(true)` if available, `Ok(false)` if not available.
/// Returns `Err` on other errors.
pub fn supports_hypervisor() -> Result<bool> {
    unsafe {
        let mut err = ffi::CcError::default();
        let code = ffi::cc_supports_hypervisor(&mut err);

        if code == ffi::CC_OK {
            return Ok(true);
        }

        if code == ffi::CC_ERR_HYPERVISOR_UNAVAILABLE {
            ffi::cc_error_free(&mut err);
            return Ok(false);
        }

        Err(ffi::error_from_cc(&mut err))
    }
}

/// Get the guest protocol version supported by this library.
pub fn guest_protocol_version() -> i32 {
    unsafe { ffi::cc_guest_protocol_version() }
}

/// Query system capabilities.
pub fn query_capabilities() -> Result<Capabilities> {
    unsafe {
        let mut caps = ffi::CcCapabilities::default();
        let mut err = ffi::CcError::default();

        let code = ffi::cc_query_capabilities(&mut caps, &mut err);
        ffi::check_error(code, &mut err)?;

        let architecture = if caps.architecture.is_null() {
            String::new()
        } else {
            let s = CStr::from_ptr(caps.architecture)
                .to_string_lossy()
                .into_owned();
            ffi::cc_free_string(caps.architecture as *mut i8);
            s
        };

        Ok(Capabilities {
            hypervisor_available: caps.hypervisor_available,
            max_memory_mb: caps.max_memory_mb,
            max_cpus: caps.max_cpus,
            architecture,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_api_version() {
        let version = api_version();
        assert_eq!(version, "0.1.0");
    }

    #[test]
    fn test_api_version_compatible() {
        assert!(api_version_compatible(0, 1));
        assert!(api_version_compatible(0, 0));
        assert!(!api_version_compatible(1, 0));
        assert!(!api_version_compatible(0, 99));
    }

    #[test]
    fn test_guest_protocol_version() {
        init().unwrap();
        let ver = guest_protocol_version();
        assert_eq!(ver, 1);
        shutdown();
    }
}
