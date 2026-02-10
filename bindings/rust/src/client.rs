//! OCI client for pulling and managing container images.

use std::ffi::{CStr, CString};
use std::ptr;

use crate::error::Result;
use crate::ffi::{self, check_error, CcCancelToken, CcInstanceSource, CcOciClient, CcSnapshot};
use crate::snapshot::Snapshot;
use crate::types::{DockerfileOptions, ImageConfig, PullOptions};

/// An opaque reference to a pulled container image.
///
/// This struct represents a source that can be used to create VM instances.
/// It should not be created directly; instead use [`OciClient::pull()`],
/// [`OciClient::load_tar()`], or [`OciClient::load_dir()`].
pub struct InstanceSource {
    handle: CcInstanceSource,
}

impl InstanceSource {
    /// Create a new InstanceSource from a handle.
    ///
    /// # Safety
    ///
    /// The handle must be valid and not already owned by another InstanceSource.
    pub(crate) unsafe fn from_handle(handle: CcInstanceSource) -> Self {
        Self { handle }
    }

    /// Get the underlying handle.
    pub(crate) fn handle(&self) -> CcInstanceSource {
        self.handle
    }

    /// Get the image configuration.
    pub fn get_config(&self) -> Result<ImageConfig> {
        unsafe {
            let mut config_ptr: *mut ffi::CcImageConfig = ptr::null_mut();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_source_get_config(self.handle, &mut config_ptr, &mut err);
            check_error(code, &mut err)?;

            if config_ptr.is_null() {
                return Ok(ImageConfig::default());
            }

            let config = &*config_ptr;

            // Extract architecture
            let architecture = if config.architecture.is_null() {
                None
            } else {
                Some(CStr::from_ptr(config.architecture).to_string_lossy().into_owned())
            };

            // Extract env
            let mut env = Vec::new();
            if !config.env.is_null() && config.env_count > 0 {
                for i in 0..config.env_count {
                    let s = *config.env.add(i);
                    if !s.is_null() {
                        env.push(CStr::from_ptr(s).to_string_lossy().into_owned());
                    }
                }
            }

            // Extract working_dir
            let working_dir = if config.working_dir.is_null() {
                None
            } else {
                Some(CStr::from_ptr(config.working_dir).to_string_lossy().into_owned())
            };

            // Extract entrypoint
            let mut entrypoint = Vec::new();
            if !config.entrypoint.is_null() && config.entrypoint_count > 0 {
                for i in 0..config.entrypoint_count {
                    let s = *config.entrypoint.add(i);
                    if !s.is_null() {
                        entrypoint.push(CStr::from_ptr(s).to_string_lossy().into_owned());
                    }
                }
            }

            // Extract cmd
            let mut cmd = Vec::new();
            if !config.cmd.is_null() && config.cmd_count > 0 {
                for i in 0..config.cmd_count {
                    let s = *config.cmd.add(i);
                    if !s.is_null() {
                        cmd.push(CStr::from_ptr(s).to_string_lossy().into_owned());
                    }
                }
            }

            // Extract user
            let user = if config.user.is_null() {
                None
            } else {
                Some(CStr::from_ptr(config.user).to_string_lossy().into_owned())
            };

            // Free the C config
            ffi::cc_image_config_free(config_ptr);

            Ok(ImageConfig {
                architecture,
                env,
                working_dir,
                entrypoint,
                cmd,
                user,
            })
        }
    }
}

impl Drop for InstanceSource {
    fn drop(&mut self) {
        if self.handle.is_valid() {
            unsafe {
                ffi::cc_instance_source_free(self.handle);
            }
        }
    }
}

// InstanceSource is not Clone - each source owns its handle
// InstanceSource is Send + Sync if the underlying C library is thread-safe
unsafe impl Send for InstanceSource {}
unsafe impl Sync for InstanceSource {}

/// A cancellation token for long-running operations.
///
/// # Example
///
/// ```no_run
/// use cc::CancelToken;
///
/// let token = CancelToken::new();
///
/// // In another thread:
/// // token.cancel();
///
/// // Check if cancelled
/// if token.is_cancelled() {
///     println!("Operation was cancelled");
/// }
/// ```
pub struct CancelToken {
    handle: CcCancelToken,
}

impl CancelToken {
    /// Create a new cancellation token.
    pub fn new() -> Self {
        let handle = unsafe { ffi::cc_cancel_token_new() };
        Self { handle }
    }

    /// Get the underlying handle.
    pub(crate) fn handle(&self) -> CcCancelToken {
        self.handle
    }

    /// Cancel the token. All operations using this token will be cancelled.
    pub fn cancel(&self) {
        unsafe {
            ffi::cc_cancel_token_cancel(self.handle);
        }
    }

    /// Check if the token has been cancelled.
    pub fn is_cancelled(&self) -> bool {
        unsafe { ffi::cc_cancel_token_is_cancelled(self.handle) }
    }
}

impl Default for CancelToken {
    fn default() -> Self {
        Self::new()
    }
}

impl Drop for CancelToken {
    fn drop(&mut self) {
        if self.handle.is_valid() {
            unsafe {
                ffi::cc_cancel_token_free(self.handle);
            }
        }
    }
}

// CancelToken is thread-safe
unsafe impl Send for CancelToken {}
unsafe impl Sync for CancelToken {}

/// OCI client for pulling and managing container images.
///
/// # Example
///
/// ```no_run
/// use cc::OciClient;
///
/// let client = OciClient::new()?;
/// let source = client.pull("alpine:latest", None, None)?;
/// # Ok::<(), cc::Error>(())
/// ```
pub struct OciClient {
    handle: CcOciClient,
}

impl OciClient {
    /// Create a new OCI client with the default cache directory.
    pub fn new() -> Result<Self> {
        unsafe {
            let mut handle = CcOciClient::invalid();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_oci_client_new(&mut handle, &mut err);
            check_error(code, &mut err)?;

            Ok(Self { handle })
        }
    }

    /// Create a new OCI client with a custom cache directory.
    pub fn with_cache_dir(cache_dir: &str) -> Result<Self> {
        unsafe {
            let cache_dir_c = CString::new(cache_dir).expect("cache_dir contains null byte");
            let mut handle = CcOciClient::invalid();
            let mut err = ffi::CcError::default();

            let code =
                ffi::cc_oci_client_new_with_cache(cache_dir_c.as_ptr(), &mut handle, &mut err);
            check_error(code, &mut err)?;

            Ok(Self { handle })
        }
    }

    /// Get the cache directory path.
    pub fn cache_dir(&self) -> String {
        unsafe {
            let ptr = ffi::cc_oci_client_cache_dir(self.handle);
            if ptr.is_null() {
                return String::new();
            }
            let s = CStr::from_ptr(ptr).to_string_lossy().into_owned();
            ffi::cc_free_string(ptr);
            s
        }
    }

    /// Pull an OCI image from a registry.
    ///
    /// # Arguments
    ///
    /// * `image_ref` - Image reference (e.g., "alpine:latest", "docker.io/library/nginx:1.21")
    /// * `options` - Pull options (platform, auth, policy)
    /// * `cancel_token` - Optional cancellation token
    ///
    /// # Returns
    ///
    /// An [`InstanceSource`] that can be used to create instances.
    pub fn pull(
        &self,
        image_ref: &str,
        options: Option<PullOptions>,
        cancel_token: Option<&CancelToken>,
    ) -> Result<InstanceSource> {
        unsafe {
            let image_ref_c = CString::new(image_ref).expect("image_ref contains null byte");

            // Build options
            let opts = options.unwrap_or_default();
            let platform_os_c = opts.platform_os.as_ref().map(|s| CString::new(s.as_str()).unwrap());
            let platform_arch_c = opts.platform_arch.as_ref().map(|s| CString::new(s.as_str()).unwrap());
            let username_c = opts.username.as_ref().map(|s| CString::new(s.as_str()).unwrap());
            let password_c = opts.password.as_ref().map(|s| CString::new(s.as_str()).unwrap());

            let c_opts = ffi::CcPullOptions {
                platform_os: platform_os_c.as_ref().map(|s| s.as_ptr()).unwrap_or(ptr::null()),
                platform_arch: platform_arch_c.as_ref().map(|s| s.as_ptr()).unwrap_or(ptr::null()),
                username: username_c.as_ref().map(|s| s.as_ptr()).unwrap_or(ptr::null()),
                password: password_c.as_ref().map(|s| s.as_ptr()).unwrap_or(ptr::null()),
                policy: opts.policy.into(),
            };

            let cancel_handle = cancel_token
                .map(|t| t.handle())
                .unwrap_or(CcCancelToken::invalid());

            let mut source = CcInstanceSource::invalid();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_oci_client_pull(
                self.handle,
                image_ref_c.as_ptr(),
                &c_opts,
                None, // progress callback
                ptr::null_mut(),
                cancel_handle,
                &mut source,
                &mut err,
            );
            check_error(code, &mut err)?;

            Ok(InstanceSource::from_handle(source))
        }
    }

    /// Load an image from a local tar file (docker save format).
    ///
    /// # Arguments
    ///
    /// * `tar_path` - Path to the tar file
    /// * `options` - Optional pull options
    ///
    /// # Returns
    ///
    /// An [`InstanceSource`] that can be used to create instances.
    pub fn load_tar(&self, tar_path: &str, options: Option<PullOptions>) -> Result<InstanceSource> {
        unsafe {
            let tar_path_c = CString::new(tar_path).expect("tar_path contains null byte");

            // Build options if provided
            let opts = options.unwrap_or_default();
            let platform_os_c = opts.platform_os.as_ref().map(|s| CString::new(s.as_str()).unwrap());
            let platform_arch_c = opts.platform_arch.as_ref().map(|s| CString::new(s.as_str()).unwrap());

            let c_opts = ffi::CcPullOptions {
                platform_os: platform_os_c.as_ref().map(|s| s.as_ptr()).unwrap_or(ptr::null()),
                platform_arch: platform_arch_c.as_ref().map(|s| s.as_ptr()).unwrap_or(ptr::null()),
                username: ptr::null(),
                password: ptr::null(),
                policy: opts.policy.into(),
            };

            let mut source = CcInstanceSource::invalid();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_oci_client_load_tar(
                self.handle,
                tar_path_c.as_ptr(),
                &c_opts,
                &mut source,
                &mut err,
            );
            check_error(code, &mut err)?;

            Ok(InstanceSource::from_handle(source))
        }
    }

    /// Load an image from a prebaked directory.
    ///
    /// # Arguments
    ///
    /// * `dir_path` - Path to the directory
    /// * `options` - Optional pull options
    ///
    /// # Returns
    ///
    /// An [`InstanceSource`] that can be used to create instances.
    pub fn load_dir(&self, dir_path: &str, options: Option<PullOptions>) -> Result<InstanceSource> {
        unsafe {
            let dir_path_c = CString::new(dir_path).expect("dir_path contains null byte");

            // Build options if provided
            let opts = options.unwrap_or_default();
            let platform_os_c = opts.platform_os.as_ref().map(|s| CString::new(s.as_str()).unwrap());
            let platform_arch_c = opts.platform_arch.as_ref().map(|s| CString::new(s.as_str()).unwrap());

            let c_opts = ffi::CcPullOptions {
                platform_os: platform_os_c.as_ref().map(|s| s.as_ptr()).unwrap_or(ptr::null()),
                platform_arch: platform_arch_c.as_ref().map(|s| s.as_ptr()).unwrap_or(ptr::null()),
                username: ptr::null(),
                password: ptr::null(),
                policy: opts.policy.into(),
            };

            let mut source = CcInstanceSource::invalid();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_oci_client_load_dir(
                self.handle,
                dir_path_c.as_ptr(),
                &c_opts,
                &mut source,
                &mut err,
            );
            check_error(code, &mut err)?;

            Ok(InstanceSource::from_handle(source))
        }
    }

    /// Export an instance source to a directory.
    ///
    /// # Arguments
    ///
    /// * `source` - The instance source to export
    /// * `dir_path` - Path to the output directory
    pub fn export_dir(&self, source: &InstanceSource, dir_path: &str) -> Result<()> {
        unsafe {
            let dir_path_c = CString::new(dir_path).expect("dir_path contains null byte");
            let mut err = ffi::CcError::default();

            let code = ffi::cc_oci_client_export_dir(
                self.handle,
                source.handle(),
                dir_path_c.as_ptr(),
                &mut err,
            );
            check_error(code, &mut err)
        }
    }

    /// Build an image from Dockerfile content.
    ///
    /// # Arguments
    ///
    /// * `dockerfile` - Dockerfile content as bytes
    /// * `options` - Build options including cache_dir (required)
    /// * `cancel_token` - Optional cancellation token
    ///
    /// # Returns
    ///
    /// A [`Snapshot`] that can be used to create instances.
    ///
    /// # Example
    ///
    /// ```no_run
    /// use cc::{OciClient, DockerfileOptions};
    ///
    /// let client = OciClient::new()?;
    /// let dockerfile = b"FROM alpine:3.19\nRUN apk add --no-cache curl";
    /// let options = DockerfileOptions {
    ///     cache_dir: "/tmp/cache".to_string(),
    ///     ..Default::default()
    /// };
    /// let snapshot = client.build_dockerfile(dockerfile, options, None)?;
    /// # Ok::<(), cc::Error>(())
    /// ```
    pub fn build_dockerfile(
        &self,
        dockerfile: &[u8],
        options: DockerfileOptions,
        cancel_token: Option<&CancelToken>,
    ) -> Result<Snapshot> {
        unsafe {
            // Build options struct
            let cache_dir_c = CString::new(options.cache_dir.as_str())
                .expect("cache_dir contains null byte");
            let context_dir_c = options
                .context_dir
                .as_ref()
                .map(|s| CString::new(s.as_str()).expect("context_dir contains null byte"));

            // Build args array
            let build_args_keys: Vec<CString> = options
                .build_args
                .keys()
                .map(|k| CString::new(k.as_str()).expect("build_arg key contains null byte"))
                .collect();
            let build_args_values: Vec<CString> = options
                .build_args
                .values()
                .map(|v| CString::new(v.as_str()).expect("build_arg value contains null byte"))
                .collect();

            let build_args_c: Vec<ffi::CcBuildArg> = build_args_keys
                .iter()
                .zip(build_args_values.iter())
                .map(|(k, v)| ffi::CcBuildArg {
                    key: k.as_ptr(),
                    value: v.as_ptr(),
                })
                .collect();

            let c_opts = ffi::CcDockerfileOptions {
                context_dir: context_dir_c
                    .as_ref()
                    .map(|s| s.as_ptr())
                    .unwrap_or(ptr::null()),
                cache_dir: cache_dir_c.as_ptr(),
                build_args: if build_args_c.is_empty() {
                    ptr::null()
                } else {
                    build_args_c.as_ptr()
                },
                build_arg_count: build_args_c.len(),
            };

            let cancel_handle = cancel_token
                .map(|t| t.handle())
                .unwrap_or(CcCancelToken::invalid());

            let mut snapshot_handle = CcSnapshot::invalid();
            let mut err = ffi::CcError::default();

            ffi::cc_build_dockerfile_source(
                self.handle,
                dockerfile.as_ptr(),
                dockerfile.len(),
                &c_opts,
                cancel_handle,
                &mut snapshot_handle,
                &mut err,
            );

            // Check for errors (function returns void, check error struct)
            if err.code != ffi::CC_OK {
                check_error(err.code, &mut err)?;
            }

            Ok(Snapshot::from_handle(snapshot_handle))
        }
    }
}

impl Drop for OciClient {
    fn drop(&mut self) {
        if self.handle.is_valid() {
            unsafe {
                ffi::cc_oci_client_free(self.handle);
            }
        }
    }
}

// OciClient is Send + Sync if the underlying C library is thread-safe
unsafe impl Send for OciClient {}
unsafe impl Sync for OciClient {}
