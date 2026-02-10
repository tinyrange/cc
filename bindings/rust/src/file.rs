//! File operations for VM instances.

use std::ffi::CStr;
use std::io::{self, Read, Write};
use std::ptr;

use crate::error::{Error, Result};
use crate::ffi::{self, check_error, CcFile};
use crate::types::{FileInfo, SeekWhence};

/// A file handle for reading/writing files in a VM instance.
///
/// Supports RAII pattern for automatic cleanup.
///
/// # Example
///
/// ```no_run
/// use std::io::{Read, Write};
/// # use cc::Instance;
/// # fn example(inst: &Instance) -> Result<(), Box<dyn std::error::Error>> {
/// // Open for reading
/// let mut file = inst.open("/etc/hosts")?;
/// let mut contents = String::new();
/// file.read_to_string(&mut contents)?;
///
/// // Create and write
/// let mut file = inst.create("/tmp/test.txt")?;
/// file.write_all(b"Hello, World!")?;
/// # Ok(())
/// # }
/// ```
pub struct File {
    handle: CcFile,
    path: String,
}

impl File {
    /// Create a new File from a handle.
    ///
    /// # Safety
    ///
    /// The handle must be valid and not already owned by another File.
    pub(crate) unsafe fn from_handle(handle: CcFile, path: String) -> Self {
        Self { handle, path }
    }

    /// Get the file name.
    pub fn name(&self) -> String {
        if !self.handle.is_valid() {
            return self.path.clone();
        }

        unsafe {
            let ptr = ffi::cc_file_name(self.handle);
            if ptr.is_null() {
                return self.path.clone();
            }
            let s = CStr::from_ptr(ptr).to_string_lossy().into_owned();
            ffi::cc_free_string(ptr);
            s
        }
    }

    /// Read up to `buf.len()` bytes from the file.
    ///
    /// Returns the number of bytes read.
    pub fn read_bytes(&mut self, buf: &mut [u8]) -> Result<usize> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let mut n: usize = 0;
            let mut err = ffi::CcError::default();

            let code = ffi::cc_file_read(
                self.handle,
                buf.as_mut_ptr(),
                buf.len(),
                &mut n,
                &mut err,
            );
            check_error(code, &mut err)?;

            Ok(n)
        }
    }

    /// Write data to the file.
    ///
    /// Returns the number of bytes written.
    pub fn write_bytes(&mut self, buf: &[u8]) -> Result<usize> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let mut n: usize = 0;
            let mut err = ffi::CcError::default();

            let code = ffi::cc_file_write(self.handle, buf.as_ptr(), buf.len(), &mut n, &mut err);
            check_error(code, &mut err)?;

            Ok(n)
        }
    }

    /// Seek to a position in the file.
    ///
    /// Returns the new file position.
    pub fn seek(&mut self, offset: i64, whence: SeekWhence) -> Result<i64> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let mut new_offset: i64 = 0;
            let mut err = ffi::CcError::default();

            let code =
                ffi::cc_file_seek(self.handle, offset, whence.into(), &mut new_offset, &mut err);
            check_error(code, &mut err)?;

            Ok(new_offset)
        }
    }

    /// Sync the file to disk.
    pub fn sync(&mut self) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let mut err = ffi::CcError::default();
            let code = ffi::cc_file_sync(self.handle, &mut err);
            check_error(code, &mut err)
        }
    }

    /// Truncate the file to the given size.
    pub fn truncate(&mut self, size: i64) -> Result<()> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let mut err = ffi::CcError::default();
            let code = ffi::cc_file_truncate(self.handle, size, &mut err);
            check_error(code, &mut err)
        }
    }

    /// Get file information.
    pub fn stat(&self) -> Result<FileInfo> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let mut info = ffi::CcFileInfo::default();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_file_stat(self.handle, &mut info, &mut err);
            check_error(code, &mut err)?;

            let name = if info.name.is_null() {
                String::new()
            } else {
                let s = CStr::from_ptr(info.name).to_string_lossy().into_owned();
                s
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

    /// Close the file.
    ///
    /// This is called automatically on drop, but can be called explicitly
    /// to handle any errors that may occur during close.
    pub fn close(&mut self) -> Result<()> {
        if !self.handle.is_valid() {
            return Ok(());
        }

        unsafe {
            let mut err = ffi::CcError::default();
            let code = ffi::cc_file_close(self.handle, &mut err);
            self.handle = CcFile::invalid();
            check_error(code, &mut err)
        }
    }
}

impl Drop for File {
    fn drop(&mut self) {
        if self.handle.is_valid() {
            unsafe {
                let mut err = ffi::CcError::default();
                ffi::cc_file_close(self.handle, &mut err);
                // Ignore errors on drop
                if err.code != ffi::CC_OK {
                    ffi::cc_error_free(&mut err);
                }
            }
        }
    }
}

// Implement std::io::Read
impl Read for File {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        self.read_bytes(buf).map_err(|e| io::Error::new(io::ErrorKind::Other, e))
    }
}

// Implement std::io::Write
impl Write for File {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.write_bytes(buf).map_err(|e| io::Error::new(io::ErrorKind::Other, e))
    }

    fn flush(&mut self) -> io::Result<()> {
        self.sync().map_err(|e| io::Error::new(io::ErrorKind::Other, e))
    }
}

// Implement std::io::Seek
impl io::Seek for File {
    fn seek(&mut self, pos: io::SeekFrom) -> io::Result<u64> {
        let (offset, whence) = match pos {
            io::SeekFrom::Start(n) => (n as i64, SeekWhence::Set),
            io::SeekFrom::Current(n) => (n, SeekWhence::Current),
            io::SeekFrom::End(n) => (n, SeekWhence::End),
        };

        self.seek(offset, whence)
            .map(|n| n as u64)
            .map_err(|e| io::Error::new(io::ErrorKind::Other, e))
    }
}

// File is Send + Sync if the underlying C library is thread-safe
unsafe impl Send for File {}
unsafe impl Sync for File {}
