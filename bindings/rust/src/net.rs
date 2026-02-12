//! Networking operations for VM instances.

use std::ffi::{CStr, CString};

use crate::error::{Error, Result};
use crate::ffi::{self, check_error, CcConn, CcInstance, CcListener};

/// A network listener for accepting connections.
///
/// # Example
///
/// ```no_run
/// # use cc::Instance;
/// # fn example(inst: &Instance) -> cc::Result<()> {
/// let listener = inst.listen("tcp", ":8080")?;
/// println!("Listening on {}", listener.addr());
///
/// let conn = listener.accept()?;
/// // Handle connection...
/// # Ok(())
/// # }
/// ```
pub struct Listener {
    handle: CcListener,
}

impl Listener {
    /// Create a new Listener from an instance.
    ///
    /// Users should use [`Instance::listen()`](crate::Instance::listen) instead.
    pub(crate) fn new(inst_handle: CcInstance, network: &str, address: &str) -> Result<Self> {
        unsafe {
            let network_c = CString::new(network).expect("network contains null byte");
            let address_c = CString::new(address).expect("address contains null byte");

            let mut handle = CcListener::invalid();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_net_listen(
                inst_handle,
                network_c.as_ptr(),
                address_c.as_ptr(),
                &mut handle,
                &mut err,
            );
            check_error(code, &mut err)?;

            Ok(Self { handle })
        }
    }

    /// Get the listener address.
    pub fn addr(&self) -> String {
        if !self.handle.is_valid() {
            return String::new();
        }

        unsafe {
            let ptr = ffi::cc_listener_addr(self.handle);
            if ptr.is_null() {
                return String::new();
            }
            let s = CStr::from_ptr(ptr).to_string_lossy().into_owned();
            ffi::cc_free_string(ptr);
            s
        }
    }

    /// Accept a connection from a client.
    pub fn accept(&self) -> Result<Conn> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let mut conn = CcConn::invalid();
            let mut err = ffi::CcError::default();

            let code = ffi::cc_listener_accept(self.handle, &mut conn, &mut err);
            check_error(code, &mut err)?;

            Ok(Conn { handle: conn })
        }
    }

    /// Close the listener.
    pub fn close(&mut self) -> Result<()> {
        if !self.handle.is_valid() {
            return Ok(());
        }

        unsafe {
            let mut err = ffi::CcError::default();
            let code = ffi::cc_listener_close(self.handle, &mut err);
            self.handle = CcListener::invalid();
            check_error(code, &mut err)
        }
    }
}

impl Drop for Listener {
    fn drop(&mut self) {
        if self.handle.is_valid() {
            unsafe {
                let mut err = ffi::CcError::default();
                ffi::cc_listener_close(self.handle, &mut err);
                // Ignore errors on drop
                if err.code != ffi::CC_OK {
                    ffi::cc_error_free(&mut err);
                }
            }
        }
    }
}

// Listener is Send + Sync
unsafe impl Send for Listener {}
unsafe impl Sync for Listener {}

/// A network connection.
///
/// # Example
///
/// ```no_run
/// # use cc::net::Conn;
/// # fn example(mut conn: Conn) -> cc::Result<()> {
/// // Read data
/// let mut buf = [0u8; 1024];
/// let n = conn.read(&mut buf)?;
///
/// // Write data
/// conn.write(b"Hello")?;
/// # Ok(())
/// # }
/// ```
pub struct Conn {
    handle: CcConn,
}

impl Conn {
    /// Create a Conn from a raw handle.
    pub(crate) fn from_handle(handle: CcConn) -> Self {
        Self { handle }
    }

    /// Get the local address.
    pub fn local_addr(&self) -> String {
        if !self.handle.is_valid() {
            return String::new();
        }

        unsafe {
            let ptr = ffi::cc_conn_local_addr(self.handle);
            if ptr.is_null() {
                return String::new();
            }
            let s = CStr::from_ptr(ptr).to_string_lossy().into_owned();
            ffi::cc_free_string(ptr);
            s
        }
    }

    /// Get the remote address.
    pub fn remote_addr(&self) -> String {
        if !self.handle.is_valid() {
            return String::new();
        }

        unsafe {
            let ptr = ffi::cc_conn_remote_addr(self.handle);
            if ptr.is_null() {
                return String::new();
            }
            let s = CStr::from_ptr(ptr).to_string_lossy().into_owned();
            ffi::cc_free_string(ptr);
            s
        }
    }

    /// Read up to `buf.len()` bytes from the connection.
    ///
    /// Returns the number of bytes read.
    pub fn read(&mut self, buf: &mut [u8]) -> Result<usize> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let mut n: usize = 0;
            let mut err = ffi::CcError::default();

            let code =
                ffi::cc_conn_read(self.handle, buf.as_mut_ptr(), buf.len(), &mut n, &mut err);
            check_error(code, &mut err)?;

            Ok(n)
        }
    }

    /// Write data to the connection.
    ///
    /// Returns the number of bytes written.
    pub fn write(&mut self, buf: &[u8]) -> Result<usize> {
        if !self.handle.is_valid() {
            return Err(Error::AlreadyClosed);
        }

        unsafe {
            let mut n: usize = 0;
            let mut err = ffi::CcError::default();

            let code = ffi::cc_conn_write(self.handle, buf.as_ptr(), buf.len(), &mut n, &mut err);
            check_error(code, &mut err)?;

            Ok(n)
        }
    }

    /// Close the connection.
    pub fn close(&mut self) -> Result<()> {
        if !self.handle.is_valid() {
            return Ok(());
        }

        unsafe {
            let mut err = ffi::CcError::default();
            let code = ffi::cc_conn_close(self.handle, &mut err);
            self.handle = CcConn::invalid();
            check_error(code, &mut err)
        }
    }
}

impl Drop for Conn {
    fn drop(&mut self) {
        if self.handle.is_valid() {
            unsafe {
                let mut err = ffi::CcError::default();
                ffi::cc_conn_close(self.handle, &mut err);
                // Ignore errors on drop
                if err.code != ffi::CC_OK {
                    ffi::cc_error_free(&mut err);
                }
            }
        }
    }
}

// Conn is Send + Sync
unsafe impl Send for Conn {}
unsafe impl Sync for Conn {}
