//! Handle types for opaque references to Go objects.
//!
//! Each handle type is a newtype wrapper around u64 to provide type safety.

/// Macro to define a handle type.
macro_rules! define_handle {
    ($name:ident) => {
        /// Opaque handle to a Go object.
        #[repr(C)]
        #[derive(Debug, Clone, Copy, PartialEq, Eq)]
        pub struct $name {
            _h: u64,
        }

        impl $name {
            /// Create an invalid (null) handle.
            #[inline]
            pub const fn invalid() -> Self {
                Self { _h: 0 }
            }

            /// Check if this handle is valid (non-zero).
            #[inline]
            pub const fn is_valid(&self) -> bool {
                self._h != 0
            }
        }

        impl Default for $name {
            fn default() -> Self {
                Self::invalid()
            }
        }
    };
}

define_handle!(CcOciClient);
define_handle!(CcInstanceSource);
define_handle!(CcInstance);
define_handle!(CcFile);
define_handle!(CcCmd);
define_handle!(CcListener);
define_handle!(CcConn);
define_handle!(CcSnapshot);
define_handle!(CcSnapshotFactory);
define_handle!(CcCancelToken);
