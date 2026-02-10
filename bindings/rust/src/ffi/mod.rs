//! FFI bindings to libcc.
//!
//! This module contains low-level C bindings. Users should prefer the
//! safe Rust wrappers in the parent modules.

pub mod error;
pub mod handles;
pub mod raw;

pub use error::{check_error, error_from_cc};
pub use handles::*;
pub use raw::*;
