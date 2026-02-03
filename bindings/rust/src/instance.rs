//! VM instance management.

use std::ffi::{CStr, CString};
use std::ptr;

use crate::client::{CancelToken, InstanceSource};
use crate::cmd::Cmd;
use crate::error::{Error, Result};
use crate::ffi::{self, check_error, CcCancelToken, CcFile, CcInstance};
use crate::file::File;
use crate::net::Listener;
use crate::snapshot::Snapshot;
use crate::types::{DirEntry, FileInfo, InstanceOptions, SnapshotOptions};

/// A running VM instance.
///
/// Provides filesystem operations, command execution, and networking.
///
/// # Example
///
/// ```no_run
/// use cc::{Instance, InstanceOptions, OciClient};
///
/// let client = OciClient::new()?;
/// let source = client.pull("alpine:latest", None, None)?;
///
/// let opts = InstanceOptions {
///     memory_mb: 512,
///     cpus: 2,
///     ..Default::default()
/// };
///
/// let inst = Instance::new(source, Some(opts))?;
/// println!("Instance ID: {}", inst.id());
///
/// // Run a command
/// let output = inst.command("echo", &["Hello from Rust!"])?.output()?;
/// println!("Output: {}", String::from_utf8_lossy(&output.stdout));
///
/// // File operations
/// inst.write_file("/tmp/test.txt", b"Hello", 0o644)?;
/// let data = inst.read_file("/tmp/test.txt")?;
///
/// // Cleanup happens automatically on drop
/// # Ok::<(), cc::Error>(())
/// ```
pub struct Instance {
    handle: CcInstance,
}

impl Instance {
    /// Create and start a new VM instance from a source.
    ///
    /// # Arguments
    ///
    /// * `source` - The instance source (from `OciClient::pull()` etc.)
    /// * `options` - Instance configuration options
    pub fn new(source: InstanceSource, options: Option<InstanceOptions>) -> Result<Self> {
        unsafe {
            let opts = options.unwrap_or_default();

            // Build mounts array
            let mounts_c: Vec<CString> = opts
                .mounts
                .iter()
                .flat_map(|m| {
                    vec![
                        CString::new(m.tag.as_str()).expect("tag contains null byte"),
                        m.host_path
                            .as_ref()
                            .map(|s| CString::new(s.as_str()).expect("host_path contains null byte"))
                            .unwrap_or_else(|| CString::new("").unwrap()),
                    ]
                })
                .collect();

            let mount_configs: Vec<ffi::CcMountConfig> = opts
                .mounts
                .iter()
                .enumerate()
                .map(|(i, m)| ffi::CcMountConfig {
                    tag: mounts_c[i * 2].as_ptr(),
                    host_path: if m.host_path.is_some() {
                        mounts_c[i * 2 + 1].as_ptr()
                    } else {
                        ptr::null()
                    },
                    writable: m.writable,
                })
                .collect();

            let user_c = opts
                .user
                .as_ref()
                .map(|s| CString::new(s.as_str()).expect("user contains null byte"));

            let c_opts = ffi::CcInstanceOptions {
                memory_mb: opts.memory_mb,
                cpus: opts.cpus,
                timeout_seconds: opts.timeout_seconds,
                user: user_c.as_ref().map(|s| s.as_ptr()).unwrap_or(ptr::null()),
                enable_dmesg: opts.enable_dmesg,
                mounts: if mount_configs.is_empty() {
                    ptr::null()
                } else {
                    mount_configs.as_ptr()
                },
                mount_count: mount_configs.len(),
            };

            let mut handle = CcInstance::invalid();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_instance_new(source.handle(), &c_opts, &mut handle, &mut err);
            check_error(code, &mut err)?;

            // Note: We don't drop source here - the C library takes ownership
            // The source handle will be freed when the instance is closed
            std::mem::forget(source);

            Ok(Self { handle })
        }
    }

    /// Get the instance ID.
    pub fn id(&self) -> String {
        if !self.handle.is_valid() {
            return String::new();
        }

        unsafe {
            let ptr = ffi::cc_instance_id(self.handle);
            if ptr.is_null() {
                return String::new();
            }
            let s = CStr::from_ptr(ptr).to_string_lossy().into_owned();
            ffi::cc_free_string(ptr);
            s
        }
    }

    /// Check if the instance is still running.
    pub fn is_running(&self) -> bool {
        if !self.handle.is_valid() {
            return false;
        }
        unsafe { ffi::cc_instance_is_running(self.handle) }
    }

    /// Wait for the instance to terminate.
    pub fn wait(&self, cancel_token: Option<&CancelToken>) -> Result<()> {
        if !self.handle.is_valid() {
            return Ok(());
        }

        unsafe {
            let cancel_handle = cancel_token
                .map(|t| t.handle())
                .unwrap_or(CcCancelToken::invalid());

            let mut err = ffi::CcError::default();
            let code = ffi::cc_instance_wait(self.handle, cancel_handle, &mut err);
            check_error(code, &mut err)
        }
    }

    /// Set the console size for interactive mode.
    pub fn set_console_size(&self, cols: i32, rows: i32) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let mut err = ffi::CcError::default();
            let code = ffi::cc_instance_set_console_size(self.handle, cols, rows, &mut err);
            check_error(code, &mut err)
        }
    }

    /// Enable or disable network access.
    pub fn set_network_enabled(&self, enabled: bool) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let mut err = ffi::CcError::default();
            let code = ffi::cc_instance_set_network_enabled(self.handle, enabled, &mut err);
            check_error(code, &mut err)
        }
    }

    /// Close the instance and release resources.
    ///
    /// This is called automatically on drop, but can be called explicitly
    /// to handle any errors that may occur during close.
    pub fn close(&mut self) -> Result<()> {
        if !self.handle.is_valid() {
            return Ok(());
        }

        unsafe {
            let mut err = ffi::CcError::default();
            let code = ffi::cc_instance_close(self.handle, &mut err);
            self.handle = CcInstance::invalid();
            check_error(code, &mut err)
        }
    }

    // ========== Filesystem Operations ==========

    /// Open a file for reading.
    pub fn open(&self, path: &str) -> Result<File> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut handle = CcFile::invalid();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_open(self.handle, path_c.as_ptr(), &mut handle, &mut err);
            check_error(code, &mut err)?;

            Ok(File::from_handle(handle, path.to_string()))
        }
    }

    /// Create or truncate a file.
    pub fn create(&self, path: &str) -> Result<File> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut handle = CcFile::invalid();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_create(self.handle, path_c.as_ptr(), &mut handle, &mut err);
            check_error(code, &mut err)?;

            Ok(File::from_handle(handle, path.to_string()))
        }
    }

    /// Open a file with specific flags and permissions.
    pub fn open_file(&self, path: &str, flags: i32, mode: u32) -> Result<File> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut handle = CcFile::invalid();
            let mut err = ffi::CcError::default();

            let code =
                ffi::cc_fs_open_file(self.handle, path_c.as_ptr(), flags, mode, &mut handle, &mut err);
            check_error(code, &mut err)?;

            Ok(File::from_handle(handle, path.to_string()))
        }
    }

    /// Read entire file contents.
    pub fn read_file(&self, path: &str) -> Result<Vec<u8>> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut data_ptr: *mut u8 = ptr::null_mut();
            let mut len: usize = 0;
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_read_file(
                self.handle,
                path_c.as_ptr(),
                &mut data_ptr,
                &mut len,
                &mut err,
            );
            check_error(code, &mut err)?;

            if data_ptr.is_null() || len == 0 {
                return Ok(Vec::new());
            }

            let data = std::slice::from_raw_parts(data_ptr, len).to_vec();
            ffi::cc_free_bytes(data_ptr);
            Ok(data)
        }
    }

    /// Write entire file contents.
    pub fn write_file(&self, path: &str, data: &[u8], mode: u32) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_write_file(
                self.handle,
                path_c.as_ptr(),
                data.as_ptr(),
                data.len(),
                mode,
                &mut err,
            );
            check_error(code, &mut err)
        }
    }

    /// Get file information by path.
    pub fn stat(&self, path: &str) -> Result<FileInfo> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut info = ffi::CcFileInfo::default();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_stat(self.handle, path_c.as_ptr(), &mut info, &mut err);
            check_error(code, &mut err)?;

            let name = if info.name.is_null() {
                String::new()
            } else {
                CStr::from_ptr(info.name).to_string_lossy().into_owned()
            };

            let result = FileInfo {
                name,
                size: info.size,
                mode: info.mode,
                mod_time_unix: info.mod_time_unix,
                is_dir: info.is_dir,
                is_symlink: info.is_symlink,
            };

            ffi::cc_file_info_free(&mut info);

            Ok(result)
        }
    }

    /// Get file information (don't follow symlinks).
    pub fn lstat(&self, path: &str) -> Result<FileInfo> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut info = ffi::CcFileInfo::default();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_lstat(self.handle, path_c.as_ptr(), &mut info, &mut err);
            check_error(code, &mut err)?;

            let name = if info.name.is_null() {
                String::new()
            } else {
                CStr::from_ptr(info.name).to_string_lossy().into_owned()
            };

            let result = FileInfo {
                name,
                size: info.size,
                mode: info.mode,
                mod_time_unix: info.mod_time_unix,
                is_dir: info.is_dir,
                is_symlink: info.is_symlink,
            };

            ffi::cc_file_info_free(&mut info);

            Ok(result)
        }
    }

    /// Remove a file.
    pub fn remove(&self, path: &str) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_remove(self.handle, path_c.as_ptr(), &mut err);
            check_error(code, &mut err)
        }
    }

    /// Remove a file or directory recursively.
    pub fn remove_all(&self, path: &str) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_remove_all(self.handle, path_c.as_ptr(), &mut err);
            check_error(code, &mut err)
        }
    }

    /// Create a directory.
    pub fn mkdir(&self, path: &str, mode: u32) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_mkdir(self.handle, path_c.as_ptr(), mode, &mut err);
            check_error(code, &mut err)
        }
    }

    /// Create a directory and all parent directories.
    pub fn mkdir_all(&self, path: &str, mode: u32) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_mkdir_all(self.handle, path_c.as_ptr(), mode, &mut err);
            check_error(code, &mut err)
        }
    }

    /// Rename a file or directory.
    pub fn rename(&self, old_path: &str, new_path: &str) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let old_path_c = CString::new(old_path).expect("old_path contains null byte");
            let new_path_c = CString::new(new_path).expect("new_path contains null byte");
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_rename(
                self.handle,
                old_path_c.as_ptr(),
                new_path_c.as_ptr(),
                &mut err,
            );
            check_error(code, &mut err)
        }
    }

    /// Create a symbolic link.
    pub fn symlink(&self, target: &str, link_path: &str) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let target_c = CString::new(target).expect("target contains null byte");
            let link_path_c = CString::new(link_path).expect("link_path contains null byte");
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_symlink(
                self.handle,
                target_c.as_ptr(),
                link_path_c.as_ptr(),
                &mut err,
            );
            check_error(code, &mut err)
        }
    }

    /// Read a symbolic link target.
    pub fn readlink(&self, path: &str) -> Result<String> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut target: *mut i8 = ptr::null_mut();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_readlink(self.handle, path_c.as_ptr(), &mut target, &mut err);
            check_error(code, &mut err)?;

            if target.is_null() {
                return Ok(String::new());
            }

            let s = CStr::from_ptr(target).to_string_lossy().into_owned();
            ffi::cc_free_string(target);
            Ok(s)
        }
    }

    /// Read directory contents.
    pub fn read_dir(&self, path: &str) -> Result<Vec<DirEntry>> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut entries_ptr: *mut ffi::CcDirEntry = ptr::null_mut();
            let mut count: usize = 0;
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_read_dir(
                self.handle,
                path_c.as_ptr(),
                &mut entries_ptr,
                &mut count,
                &mut err,
            );
            check_error(code, &mut err)?;

            let mut entries = Vec::with_capacity(count);
            if !entries_ptr.is_null() && count > 0 {
                for i in 0..count {
                    let e = &*entries_ptr.add(i);
                    let name = if e.name.is_null() {
                        String::new()
                    } else {
                        CStr::from_ptr(e.name).to_string_lossy().into_owned()
                    };
                    entries.push(DirEntry {
                        name,
                        is_dir: e.is_dir,
                        mode: e.mode,
                    });
                }
                ffi::cc_dir_entries_free(entries_ptr, count);
            }

            Ok(entries)
        }
    }

    /// Change file permissions.
    pub fn chmod(&self, path: &str, mode: u32) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_chmod(self.handle, path_c.as_ptr(), mode, &mut err);
            check_error(code, &mut err)
        }
    }

    /// Change file owner.
    pub fn chown(&self, path: &str, uid: i32, gid: i32) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_chown(self.handle, path_c.as_ptr(), uid, gid, &mut err);
            check_error(code, &mut err)
        }
    }

    /// Change file access and modification times.
    pub fn chtimes(&self, path: &str, atime_unix: i64, mtime_unix: i64) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let path_c = CString::new(path).expect("path contains null byte");
            let mut err = ffi::CcError::default();

            let code =
                ffi::cc_fs_chtimes(self.handle, path_c.as_ptr(), atime_unix, mtime_unix, &mut err);
            check_error(code, &mut err)
        }
    }

    // ========== Command Execution ==========

    /// Create a command to run in the instance.
    ///
    /// # Arguments
    ///
    /// * `name` - Command name (e.g., "echo", "/bin/sh")
    /// * `args` - Command arguments
    ///
    /// # Returns
    ///
    /// A [`Cmd`] that can be configured and executed.
    pub fn command(&self, name: &str, args: &[&str]) -> Result<Cmd> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        Cmd::new(self.handle, name, args)
    }

    /// Create a command using the container's entrypoint.
    pub fn entrypoint_command(&self, args: Option<&[&str]>) -> Result<Cmd> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        Cmd::entrypoint(self.handle, args)
    }

    /// Replace the init process with the specified command.
    ///
    /// This is a terminal operation - the instance cannot be used for
    /// other operations after this.
    pub fn exec(&self, name: &str, args: &[&str]) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let name_c = CString::new(name).expect("name contains null byte");

            // Build NULL-terminated args array
            let args_c: Vec<CString> = args
                .iter()
                .map(|s| CString::new(*s).expect("arg contains null byte"))
                .collect();
            let mut args_ptrs: Vec<*const i8> = args_c.iter().map(|s| s.as_ptr()).collect();
            args_ptrs.push(ptr::null());

            let mut err = ffi::CcError::default();

            let code = ffi::cc_instance_exec(
                self.handle,
                name_c.as_ptr(),
                args_ptrs.as_ptr(),
                &mut err,
            );
            check_error(code, &mut err)
        }
    }

    // ========== Networking ==========

    /// Listen for connections on the guest network.
    ///
    /// # Arguments
    ///
    /// * `network` - Network type ("tcp", "tcp4")
    /// * `address` - Address to listen on (e.g., ":8080", "0.0.0.0:80")
    ///
    /// # Returns
    ///
    /// A [`Listener`] that can accept connections.
    pub fn listen(&self, network: &str, address: &str) -> Result<Listener> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        Listener::new(self.handle, network, address)
    }

    // ========== Snapshots ==========

    /// Take a filesystem snapshot.
    ///
    /// # Arguments
    ///
    /// * `options` - Snapshot options (excludes, cache_dir)
    ///
    /// # Returns
    ///
    /// A [`Snapshot`] that can be used to create new instances.
    pub fn snapshot(&self, options: Option<SnapshotOptions>) -> Result<Snapshot> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        Snapshot::new(self.handle, options)
    }
}

impl Drop for Instance {
    fn drop(&mut self) {
        if self.handle.is_valid() {
            unsafe {
                let mut err = ffi::CcError::default();
                ffi::cc_instance_close(self.handle, &mut err);
                // Ignore errors on drop
                if err.code != ffi::CC_OK {
                    ffi::cc_error_free(&mut err);
                }
            }
        }
    }
}

// Instance is Send + Sync if the underlying C library is thread-safe
unsafe impl Send for Instance {}
unsafe impl Sync for Instance {}
