"""
OCI client for pulling and managing container images.
"""

from __future__ import annotations

from ctypes import POINTER, byref, c_char_p

from . import _ffi
from .errors import CCError
from .types import (
    DownloadProgress,
    ImageConfig,
    ProgressCallback,
    PullOptions,
    PullPolicy,
)


class InstanceSource:
    """An opaque reference to a pulled container image.

    This class represents a source that can be used to create VM instances.
    It should not be created directly; instead use OCIClient.pull(),
    OCIClient.load_tar(), or OCIClient.load_dir().
    """

    def __init__(self, handle: _ffi.InstanceSourceHandle):
        self._handle = handle
        self._closed = False

    def __del__(self) -> None:
        self.close()

    def __enter__(self) -> "InstanceSource":
        return self

    def __exit__(self, *args) -> None:
        self.close()

    def close(self) -> None:
        """Release the instance source."""
        if not self._closed and self._handle:
            _ffi.get_lib().cc_instance_source_free(self._handle)
            self._closed = True

    @property
    def handle(self) -> _ffi.InstanceSourceHandle:
        """Get the underlying handle."""
        if self._closed:
            raise CCError("InstanceSource is closed", code=4)
        return self._handle

    def get_config(self) -> ImageConfig:
        """Get the image configuration."""
        lib = _ffi.get_lib()
        config_ptr = POINTER(_ffi.ImageConfigStruct)()
        err = _ffi.CCErrorStruct()

        code = lib.cc_source_get_config(self.handle, byref(config_ptr), byref(err))
        _ffi.check_error(code, err)

        try:
            config = config_ptr.contents

            # Extract environment variables
            env = []
            if config.env and config.env_count > 0:
                for i in range(config.env_count):
                    if config.env[i]:
                        env.append(config.env[i].decode("utf-8"))

            # Extract entrypoint
            entrypoint = []
            if config.entrypoint and config.entrypoint_count > 0:
                for i in range(config.entrypoint_count):
                    if config.entrypoint[i]:
                        entrypoint.append(config.entrypoint[i].decode("utf-8"))

            # Extract cmd
            cmd = []
            if config.cmd and config.cmd_count > 0:
                for i in range(config.cmd_count):
                    if config.cmd[i]:
                        cmd.append(config.cmd[i].decode("utf-8"))

            return ImageConfig(
                architecture=config.architecture.decode("utf-8") if config.architecture else None,
                env=env,
                working_dir=config.working_dir.decode("utf-8") if config.working_dir else None,
                entrypoint=entrypoint,
                cmd=cmd,
                user=config.user.decode("utf-8") if config.user else None,
            )
        finally:
            lib.cc_image_config_free(config_ptr)


class OCIClient:
    """Client for pulling and managing OCI container images.

    Example:
        with OCIClient() as client:
            source = client.pull("alpine:latest")
            # Use source to create instances...
    """

    def __init__(self, cache_dir: str | None = None):
        """Create a new OCI client.

        Args:
            cache_dir: Optional custom cache directory. If None, uses the default.
        """
        lib = _ffi.get_lib()
        handle = _ffi.OCIClientHandle()
        err = _ffi.CCErrorStruct()

        if cache_dir is not None:
            code = lib.cc_oci_client_new_with_cache(
                cache_dir.encode("utf-8"), byref(handle), byref(err)
            )
        else:
            code = lib.cc_oci_client_new(byref(handle), byref(err))

        _ffi.check_error(code, err)
        self._handle = handle
        self._closed = False

    def __del__(self) -> None:
        self.close()

    def __enter__(self) -> "OCIClient":
        return self

    def __exit__(self, *args) -> None:
        self.close()

    def close(self) -> None:
        """Close the client and release resources."""
        if not self._closed and self._handle:
            _ffi.get_lib().cc_oci_client_free(self._handle)
            self._closed = True

    @property
    def handle(self) -> _ffi.OCIClientHandle:
        """Get the underlying handle."""
        if self._closed:
            raise CCError("OCIClient is closed", code=4)
        return self._handle

    @property
    def cache_dir(self) -> str:
        """Get the cache directory path."""
        lib = _ffi.get_lib()
        ptr = lib.cc_oci_client_cache_dir(self.handle)
        return _ffi._get_string_and_free(lib, ptr)

    def pull(
        self,
        image_ref: str,
        options: PullOptions | None = None,
        progress_callback: ProgressCallback | None = None,
        cancel_token: "CancelToken | None" = None,
    ) -> InstanceSource:
        """Pull an OCI image from a registry.

        Args:
            image_ref: Image reference (e.g., "alpine:latest", "docker.io/library/nginx:1.21")
            options: Pull options (platform, auth, policy)
            progress_callback: Optional callback for download progress
            cancel_token: Optional cancellation token

        Returns:
            An InstanceSource that can be used to create instances.
        """
        lib = _ffi.get_lib()
        source = _ffi.InstanceSourceHandle()
        err = _ffi.CCErrorStruct()

        # Build options struct
        opts_ptr = None
        if options:
            opts = _ffi.PullOptionsStruct()
            if options.platform_os:
                opts.platform_os = options.platform_os.encode("utf-8")
            if options.platform_arch:
                opts.platform_arch = options.platform_arch.encode("utf-8")
            if options.username:
                opts.username = options.username.encode("utf-8")
            if options.password:
                opts.password = options.password.encode("utf-8")
            opts.policy = int(options.policy)
            opts_ptr = byref(opts)

        # Set up progress callback - must be a valid callback type or null pointer
        c_callback = _ffi.ProgressCallbackType()  # Creates a null function pointer
        if progress_callback:

            def _progress_wrapper(progress_ptr, user_data):
                p = progress_ptr.contents
                progress = DownloadProgress(
                    current=p.current,
                    total=p.total,
                    filename=p.filename.decode("utf-8") if p.filename else None,
                    blob_index=p.blob_index,
                    blob_count=p.blob_count,
                    bytes_per_second=p.bytes_per_second,
                    eta_seconds=p.eta_seconds,
                )
                progress_callback(progress)

            c_callback = _ffi.ProgressCallbackType(_progress_wrapper)

        # Get cancel token handle
        cancel_handle = _ffi.CancelTokenHandle.invalid()
        if cancel_token:
            cancel_handle = cancel_token.handle

        code = lib.cc_oci_client_pull(
            self.handle,
            image_ref.encode("utf-8"),
            opts_ptr,
            c_callback,
            None,  # user_data
            cancel_handle,
            byref(source),
            byref(err),
        )
        _ffi.check_error(code, err)

        return InstanceSource(source)

    def load_tar(self, tar_path: str, options: PullOptions | None = None) -> InstanceSource:
        """Load an image from a local tar file (docker save format).

        Args:
            tar_path: Path to the tar file
            options: Optional pull options

        Returns:
            An InstanceSource that can be used to create instances.
        """
        lib = _ffi.get_lib()
        source = _ffi.InstanceSourceHandle()
        err = _ffi.CCErrorStruct()

        opts_ptr = None
        if options:
            opts = _ffi.PullOptionsStruct()
            if options.platform_os:
                opts.platform_os = options.platform_os.encode("utf-8")
            if options.platform_arch:
                opts.platform_arch = options.platform_arch.encode("utf-8")
            opts.policy = int(options.policy)
            opts_ptr = byref(opts)

        code = lib.cc_oci_client_load_tar(
            self.handle,
            tar_path.encode("utf-8"),
            opts_ptr,
            byref(source),
            byref(err),
        )
        _ffi.check_error(code, err)

        return InstanceSource(source)

    def load_dir(self, dir_path: str, options: PullOptions | None = None) -> InstanceSource:
        """Load an image from a prebaked directory.

        Args:
            dir_path: Path to the directory
            options: Optional pull options

        Returns:
            An InstanceSource that can be used to create instances.
        """
        lib = _ffi.get_lib()
        source = _ffi.InstanceSourceHandle()
        err = _ffi.CCErrorStruct()

        opts_ptr = None
        if options:
            opts = _ffi.PullOptionsStruct()
            if options.platform_os:
                opts.platform_os = options.platform_os.encode("utf-8")
            if options.platform_arch:
                opts.platform_arch = options.platform_arch.encode("utf-8")
            opts.policy = int(options.policy)
            opts_ptr = byref(opts)

        code = lib.cc_oci_client_load_dir(
            self.handle,
            dir_path.encode("utf-8"),
            opts_ptr,
            byref(source),
            byref(err),
        )
        _ffi.check_error(code, err)

        return InstanceSource(source)

    def export_dir(self, source: InstanceSource, dir_path: str) -> None:
        """Export an instance source to a directory.

        Args:
            source: The instance source to export
            dir_path: Path to the output directory
        """
        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()

        code = lib.cc_oci_client_export_dir(
            self.handle,
            source.handle,
            dir_path.encode("utf-8"),
            byref(err),
        )
        _ffi.check_error(code, err)


class CancelToken:
    """A cancellation token for long-running operations.

    Example:
        token = CancelToken()
        # In another thread:
        token.cancel()
    """

    def __init__(self):
        self._handle = _ffi.get_lib().cc_cancel_token_new()
        self._freed = False

    def __del__(self) -> None:
        self.close()

    def __enter__(self) -> "CancelToken":
        return self

    def __exit__(self, *args) -> None:
        self.close()

    def close(self) -> None:
        """Free the cancel token."""
        if not self._freed and self._handle:
            _ffi.get_lib().cc_cancel_token_free(self._handle)
            self._freed = True

    @property
    def handle(self) -> _ffi.CancelTokenHandle:
        """Get the underlying handle."""
        return self._handle

    def cancel(self) -> None:
        """Cancel the token. All operations using this token will be cancelled."""
        _ffi.get_lib().cc_cancel_token_cancel(self._handle)

    @property
    def is_cancelled(self) -> bool:
        """Check if the token has been cancelled."""
        return bool(_ffi.get_lib().cc_cancel_token_is_cancelled(self._handle))
