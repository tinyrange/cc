//! Command execution for VM instances.

use std::ffi::{CStr, CString};
use std::ptr;

use crate::error::{Error, Result};
use crate::ffi::{self, check_error, CcCmd, CcInstance};
use crate::types::CommandOutput;

/// A command to run in a VM instance.
///
/// Commands can be configured with environment variables and working directory
/// before being executed.
///
/// # Example
///
/// ```no_run
/// # use cc::Instance;
/// # fn example(inst: &Instance) -> cc::Result<()> {
/// // Simple command
/// let output = inst.command("echo", &["Hello", "World"])?.output()?;
/// println!("Output: {}", String::from_utf8_lossy(&output.stdout));
///
/// // With environment and directory
/// let exit_code = inst.command("env", &[])?
///     .dir("/tmp")?
///     .env("MY_VAR", "my_value")?
///     .run()?;
/// # Ok(())
/// # }
/// ```
pub struct Cmd {
    handle: CcCmd,
    started: bool,
}

impl Cmd {
    /// Create a new command from an instance.
    ///
    /// Users should use [`Instance::command()`](crate::Instance::command) instead.
    pub(crate) fn new(inst_handle: CcInstance, name: &str, args: &[&str]) -> Result<Self> {
        unsafe {
            let name_c = CString::new(name).expect("name contains null byte");

            // Build NULL-terminated args array
            let args_c: Vec<CString> = args
                .iter()
                .map(|s| CString::new(*s).expect("arg contains null byte"))
                .collect();
            let mut args_ptrs: Vec<*const i8> = args_c.iter().map(|s| s.as_ptr()).collect();
            args_ptrs.push(ptr::null());

            let mut handle = CcCmd::invalid();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_cmd_new(
                inst_handle,
                name_c.as_ptr(),
                args_ptrs.as_ptr(),
                &mut handle,
                &mut err,
            );
            check_error(code, &mut err)?;

            Ok(Self {
                handle,
                started: false,
            })
        }
    }

    /// Create a command using the container's entrypoint.
    pub(crate) fn entrypoint(inst_handle: CcInstance, args: Option<&[&str]>) -> Result<Self> {
        unsafe {
            // Build args array if provided
            let args_c: Vec<CString>;
            let mut args_ptrs: Vec<*const i8>;
            let args_ptr = if let Some(args) = args {
                args_c = args
                    .iter()
                    .map(|s| CString::new(*s).expect("arg contains null byte"))
                    .collect();
                args_ptrs = args_c.iter().map(|s| s.as_ptr()).collect();
                args_ptrs.push(ptr::null());
                args_ptrs.as_ptr()
            } else {
                ptr::null()
            };

            let mut handle = CcCmd::invalid();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_cmd_entrypoint(inst_handle, args_ptr, &mut handle, &mut err);
            check_error(code, &mut err)?;

            Ok(Self {
                handle,
                started: false,
            })
        }
    }

    /// Set the working directory for the command.
    ///
    /// Returns self for method chaining.
    pub fn dir(self, dir: &str) -> Result<Self> {
        unsafe {
            let dir_c = CString::new(dir).expect("dir contains null byte");
            let mut err = ffi::CcError::default();

            let code = ffi::cc_cmd_set_dir(self.handle, dir_c.as_ptr(), &mut err);
            check_error(code, &mut err)?;

            Ok(self)
        }
    }

    /// Set an environment variable.
    ///
    /// Returns self for method chaining.
    pub fn env(self, key: &str, value: &str) -> Result<Self> {
        unsafe {
            let key_c = CString::new(key).expect("key contains null byte");
            let value_c = CString::new(value).expect("value contains null byte");
            let mut err = ffi::CcError::default();

            let code = ffi::cc_cmd_set_env(self.handle, key_c.as_ptr(), value_c.as_ptr(), &mut err);
            check_error(code, &mut err)?;

            Ok(self)
        }
    }

    /// Get an environment variable.
    pub fn get_env(&self, key: &str) -> Option<String> {
        unsafe {
            let key_c = CString::new(key).expect("key contains null byte");
            let ptr = ffi::cc_cmd_get_env(self.handle, key_c.as_ptr());
            if ptr.is_null() {
                return None;
            }
            let s = CStr::from_ptr(ptr).to_string_lossy().into_owned();
            ffi::cc_free_string(ptr);
            Some(s)
        }
    }

    /// Get all environment variables as "KEY=VALUE" strings.
    pub fn environ(&self) -> Result<Vec<String>> {
        unsafe {
            let mut env_ptr: *mut *mut i8 = ptr::null_mut();
            let mut count: usize = 0;
            let mut err = ffi::CcError::default();

            let code = ffi::cc_cmd_environ(self.handle, &mut env_ptr, &mut count, &mut err);
            check_error(code, &mut err)?;

            let mut result = Vec::with_capacity(count);
            if !env_ptr.is_null() && count > 0 {
                for i in 0..count {
                    let s = *env_ptr.add(i);
                    if !s.is_null() {
                        result.push(CStr::from_ptr(s).to_string_lossy().into_owned());
                        ffi::cc_free_string(s);
                    }
                }
                ffi::cc_free_bytes(env_ptr as *mut u8);
            }

            Ok(result)
        }
    }

    /// Start the command (non-blocking).
    ///
    /// The command runs asynchronously. Use [`wait()`](Self::wait) to wait for completion.
    pub fn start(&mut self) -> Result<()> {
        if self.started {
            return Err(Error::InvalidArgument("command already started".to_string()));
        }

        unsafe {
            let mut err = ffi::CcError::default();
            let code = ffi::cc_cmd_start(self.handle, &mut err);
            check_error(code, &mut err)?;
            self.started = true;
            Ok(())
        }
    }

    /// Wait for the command to complete.
    ///
    /// Returns the exit code.
    pub fn wait(&mut self) -> Result<i32> {
        if !self.started {
            return Err(Error::InvalidArgument("command not started".to_string()));
        }

        unsafe {
            let mut exit_code: i32 = 0;
            let mut err = ffi::CcError::default();

            let code = ffi::cc_cmd_wait(self.handle, &mut exit_code, &mut err);
            check_error(code, &mut err)?;

            Ok(exit_code)
        }
    }

    /// Run the command and wait for completion.
    ///
    /// Returns the exit code.
    pub fn run(mut self) -> Result<i32> {
        unsafe {
            let mut exit_code: i32 = 0;
            let mut err = ffi::CcError::default();

            let code = ffi::cc_cmd_run(self.handle, &mut exit_code, &mut err);
            self.started = true; // Mark as started so we don't try to free
            check_error(code, &mut err)?;

            Ok(exit_code)
        }
    }

    /// Run the command and capture stdout.
    ///
    /// Returns the output including stdout and exit code.
    pub fn output(mut self) -> Result<CommandOutput> {
        unsafe {
            let mut out_ptr: *mut u8 = ptr::null_mut();
            let mut len: usize = 0;
            let mut exit_code: i32 = 0;
            let mut err = ffi::CcError::default();

            let code = ffi::cc_cmd_output(
                self.handle,
                &mut out_ptr,
                &mut len,
                &mut exit_code,
                &mut err,
            );
            self.started = true;
            check_error(code, &mut err)?;

            let stdout = if !out_ptr.is_null() && len > 0 {
                let data = std::slice::from_raw_parts(out_ptr, len).to_vec();
                ffi::cc_free_bytes(out_ptr);
                data
            } else {
                Vec::new()
            };

            Ok(CommandOutput { stdout, exit_code })
        }
    }

    /// Run the command and capture stdout + stderr.
    ///
    /// Returns the combined output and exit code.
    pub fn combined_output(mut self) -> Result<CommandOutput> {
        unsafe {
            let mut out_ptr: *mut u8 = ptr::null_mut();
            let mut len: usize = 0;
            let mut exit_code: i32 = 0;
            let mut err = ffi::CcError::default();

            let code = ffi::cc_cmd_combined_output(
                self.handle,
                &mut out_ptr,
                &mut len,
                &mut exit_code,
                &mut err,
            );
            self.started = true;
            check_error(code, &mut err)?;

            let stdout = if !out_ptr.is_null() && len > 0 {
                let data = std::slice::from_raw_parts(out_ptr, len).to_vec();
                ffi::cc_free_bytes(out_ptr);
                data
            } else {
                Vec::new()
            };

            Ok(CommandOutput { stdout, exit_code })
        }
    }

    /// Get the exit code (after wait/run).
    pub fn exit_code(&self) -> i32 {
        unsafe { ffi::cc_cmd_exit_code(self.handle) }
    }

    /// Kill a started command and release resources.
    ///
    /// Safe to call on commands that have already completed.
    pub fn kill(mut self) -> Result<()> {
        unsafe {
            let mut err = ffi::CcError::default();
            let code = ffi::cc_cmd_kill(self.handle, &mut err);
            self.handle = CcCmd::invalid(); // Mark as freed
            check_error(code, &mut err)
        }
    }
}

impl Drop for Cmd {
    fn drop(&mut self) {
        if self.handle.is_valid() && !self.started {
            // Only free if not started (started commands are freed by wait/run/output)
            unsafe {
                ffi::cc_cmd_free(self.handle);
            }
        }
    }
}

// Cmd is Send + Sync if the underlying C library is thread-safe
unsafe impl Send for Cmd {}
unsafe impl Sync for Cmd {}
