//! Filesystem snapshots for VM instances.

use std::ffi::{CStr, CString};
use std::ptr;

use crate::client::InstanceSource;
use crate::error::{Error, Result};
use crate::ffi::{self, check_error, CcInstance, CcSnapshot};
use crate::types::SnapshotOptions;

/// A filesystem snapshot from a VM instance.
///
/// Snapshots can be used to create new instances from a known state.
///
/// # Example
///
/// ```no_run
/// # use cc::Instance;
/// # fn example(inst: &Instance) -> cc::Result<()> {
/// // Take a snapshot
/// let snapshot = inst.snapshot(None)?;
/// println!("Cache key: {}", snapshot.cache_key());
///
/// // Convert to instance source
/// let source = snapshot.as_source();
/// # Ok(())
/// # }
/// ```
pub struct Snapshot {
    handle: CcSnapshot,
}

impl Snapshot {
    /// Create a new Snapshot from a handle.
    ///
    /// # Safety
    ///
    /// The handle must be valid and not already owned by another Snapshot.
    pub(crate) fn from_handle(handle: CcSnapshot) -> Self {
        Self { handle }
    }

    /// Take a filesystem snapshot from an instance.
    ///
    /// Users should use [`Instance::snapshot()`](crate::Instance::snapshot) instead.
    pub(crate) fn new(inst_handle: CcInstance, options: Option<SnapshotOptions>) -> Result<Self> {
        unsafe {
            let opts = options.unwrap_or_default();

            // Build excludes array
            let excludes_c: Vec<CString> = opts
                .excludes
                .iter()
                .map(|s| CString::new(s.as_str()).expect("exclude contains null byte"))
                .collect();
            let mut excludes_ptrs: Vec<*const i8> = excludes_c.iter().map(|s| s.as_ptr()).collect();
            if !excludes_ptrs.is_empty() {
                excludes_ptrs.push(ptr::null());
            }

            let cache_dir_c = opts
                .cache_dir
                .as_ref()
                .map(|s| CString::new(s.as_str()).expect("cache_dir contains null byte"));

            let c_opts = ffi::CcSnapshotOptions {
                excludes: if excludes_ptrs.is_empty() {
                    ptr::null()
                } else {
                    excludes_ptrs.as_ptr()
                },
                exclude_count: opts.excludes.len(),
                cache_dir: cache_dir_c
                    .as_ref()
                    .map(|s| s.as_ptr())
                    .unwrap_or(ptr::null()),
            };

            let mut handle = CcSnapshot::invalid();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_fs_snapshot(inst_handle, &c_opts, &mut handle, &mut err);
            check_error(code, &mut err)?;

            Ok(Self { handle })
        }
    }

    /// Get the snapshot cache key.
    pub fn cache_key(&self) -> String {
        if !self.handle.is_valid() {
            return String::new();
        }

        unsafe {
            let ptr = ffi::cc_snapshot_cache_key(self.handle);
            if ptr.is_null() {
                return String::new();
            }
            let s = CStr::from_ptr(ptr).to_string_lossy().into_owned();
            ffi::cc_free_string(ptr);
            s
        }
    }

    /// Get the parent snapshot, if any.
    pub fn parent(&self) -> Option<Snapshot> {
        if !self.handle.is_valid() {
            return None;
        }

        unsafe {
            let parent_handle = ffi::cc_snapshot_parent(self.handle);
            if !parent_handle.is_valid() {
                return None;
            }
            Some(Snapshot {
                handle: parent_handle,
            })
        }
    }

    /// Convert the snapshot to an instance source.
    ///
    /// Note: This consumes the snapshot. The returned InstanceSource
    /// can be used to create new instances.
    pub fn as_source(self) -> InstanceSource {
        unsafe {
            let source_handle = ffi::cc_snapshot_as_source(self.handle);
            // Don't drop self - the handle is now owned by the source
            std::mem::forget(self);
            InstanceSource::from_handle(source_handle)
        }
    }

    /// Close the snapshot.
    pub fn close(&mut self) -> Result<()> {
        if !self.handle.is_valid() {
            return Ok(());
        }

        unsafe {
            let mut err = ffi::CcError::default();
            let code = ffi::cc_snapshot_close(self.handle, &mut err);
            self.handle = CcSnapshot::invalid();
            check_error(code, &mut err)
        }
    }
}

impl Drop for Snapshot {
    fn drop(&mut self) {
        if self.handle.is_valid() {
            unsafe {
                let mut err = ffi::CcError::default();
                ffi::cc_snapshot_close(self.handle, &mut err);
                // Ignore errors on drop
                if err.code != ffi::CC_OK {
                    ffi::cc_error_free(&mut err);
                }
            }
        }
    }
}

// Snapshot is Send + Sync
unsafe impl Send for Snapshot {}
unsafe impl Sync for Snapshot {}
