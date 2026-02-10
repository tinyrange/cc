//! Error conversion utilities for FFI.

use std::ffi::CStr;

use super::raw::{
    cc_error_free, CcError, CC_ERR_ALREADY_CLOSED, CC_ERR_CANCELLED,
    CC_ERR_HYPERVISOR_UNAVAILABLE, CC_ERR_INVALID_ARGUMENT, CC_ERR_INVALID_HANDLE, CC_ERR_IO,
    CC_ERR_NETWORK, CC_ERR_NOT_RUNNING, CC_ERR_TIMEOUT, CC_OK,
};
use crate::error::Error;

/// Convert a CcError to a Rust Error and free the C error.
///
/// # Safety
///
/// The `err` pointer must be valid and initialized.
pub unsafe fn error_from_cc(err: *mut CcError) -> Error {
    if err.is_null() {
        return Error::Unknown("null error pointer".to_string());
    }

    let err_ref = &*err;
    let code = err_ref.code;

    // Extract strings before freeing
    let message = if err_ref.message.is_null() {
        "Unknown error".to_string()
    } else {
        CStr::from_ptr(err_ref.message)
            .to_string_lossy()
            .into_owned()
    };

    let op = if err_ref.op.is_null() {
        None
    } else {
        Some(CStr::from_ptr(err_ref.op).to_string_lossy().into_owned())
    };

    let path = if err_ref.path.is_null() {
        None
    } else {
        Some(CStr::from_ptr(err_ref.path).to_string_lossy().into_owned())
    };

    // Free the C error
    cc_error_free(err);

    // Convert to Rust error
    match code {
        CC_ERR_INVALID_HANDLE => Error::InvalidHandle,
        CC_ERR_INVALID_ARGUMENT => Error::InvalidArgument(message),
        CC_ERR_NOT_RUNNING => Error::NotRunning,
        CC_ERR_ALREADY_CLOSED => Error::AlreadyClosed,
        CC_ERR_TIMEOUT => Error::Timeout,
        CC_ERR_HYPERVISOR_UNAVAILABLE => Error::HypervisorUnavailable(message),
        CC_ERR_IO => Error::Io { message, op, path },
        CC_ERR_NETWORK => Error::Network(message),
        CC_ERR_CANCELLED => Error::Cancelled,
        _ => Error::Unknown(message),
    }
}

/// Check an error code and convert to Result.
///
/// # Safety
///
/// The `err` pointer must be valid and initialized.
pub unsafe fn check_error(code: i32, err: *mut CcError) -> crate::Result<()> {
    if code == CC_OK {
        // Free the error struct even on success (safe no-op)
        if !err.is_null() {
            cc_error_free(err);
        }
        Ok(())
    } else {
        Err(error_from_cc(err))
    }
}
