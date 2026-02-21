"""
OCI client for pulling and managing container images.
"""

from __future__ import annotations

import ctypes
import platform
from ctypes import POINTER, byref, c_char_p
from typing import TYPE_CHECKING, Any

from . import _ffi
from .errors import CCError
from .types import (
    DownloadProgress,
    ImageConfig,
    ProgressCallback,
    PullOptions,
    PullPolicy,
)

if TYPE_CHECKING:
    from .instance import Snapshot


class InstanceSource:
    """An opaque reference to a pulled container image.

    This class represents a source that can be used to create VM instances.
    It should not be created directly; instead use OCIClient.pull(),
    OCIClient.load_tar(), or OCIClient.load_dir().

    When using the IPC backend, this stores metadata needed to recreate the
    source on the helper side (source_type, source_path, image_ref, cache_dir).
    """

    def __init__(self, handle: Any = None, *, ipc_source_type: int = 0, ipc_source_path: str = "",
                 ipc_image_ref: str = "", ipc_cache_dir: str = "") -> None:
        self._handle = handle
        self._closed = False
        # IPC backend metadata
        self._ipc_source_type = ipc_source_type
        self._ipc_source_path = ipc_source_path
        self._ipc_image_ref = ipc_image_ref
        self._ipc_cache_dir = ipc_cache_dir

    def __del__(self) -> None:
        self.close()

    def __enter__(self) -> "InstanceSource":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    def close(self) -> None:
        """Release the instance source."""
        if not self._closed and self._handle is not None:
            if not _ffi.using_ipc():
                _ffi.get_lib().cc_instance_source_free(self._handle)
            self._closed = True

    @property
    def handle(self) -> Any:
        """Get the underlying handle."""
        if self._closed:
            raise CCError("InstanceSource is closed", code=4)
        return self._handle

    def get_config(self) -> ImageConfig:
        """Get the image configuration."""
        if _ffi.using_ipc():
            # IPC currently doesn't expose source config over the wire.
            # Return a best-effort config with host architecture filled in.
            machine = platform.machine().lower()
            arch_map = {
                "x86_64": "amd64",
                "amd64": "amd64",
                "aarch64": "arm64",
                "arm64": "arm64",
            }
            return ImageConfig(
                architecture=arch_map.get(machine, machine),
                env=[],
                working_dir=None,
                entrypoint=[], cmd=[], user=None,
            )

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
        self._cache_dir_str = cache_dir or ""
        self._closed = False

        if _ffi.using_ipc():
            # IPC backend: OCI client lives in the helper process.
            # We just store the cache_dir to pass when creating instances.
            self._handle = None
            return

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

    def __del__(self) -> None:
        self.close()

    def __enter__(self) -> "OCIClient":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    def close(self) -> None:
        """Close the client and release resources."""
        if not self._closed:
            if self._handle is not None and not _ffi.using_ipc():
                _ffi.get_lib().cc_oci_client_free(self._handle)
            self._closed = True

    @property
    def handle(self) -> Any:
        """Get the underlying handle."""
        if self._closed:
            raise CCError("OCIClient is closed", code=4)
        return self._handle

    @property
    def cache_dir(self) -> str:
        """Get the cache directory path."""
        if _ffi.using_ipc():
            return self._cache_dir_str
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
        if _ffi.using_ipc():
            # For IPC, we store metadata about how to recreate this source
            # in the helper process. The actual pull happens at instance creation.
            return InstanceSource(
                ipc_source_type=2,  # ref
                ipc_image_ref=image_ref,
                ipc_cache_dir=self._cache_dir_str,
            )

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

            def _progress_wrapper(progress_ptr: Any, user_data: Any) -> None:
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
        if _ffi.using_ipc():
            return InstanceSource(
                ipc_source_type=0,  # tar
                ipc_source_path=tar_path,
                ipc_cache_dir=self._cache_dir_str,
            )

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
        if _ffi.using_ipc():
            return InstanceSource(
                ipc_source_type=1,  # directory
                ipc_source_path=dir_path,
                ipc_cache_dir=self._cache_dir_str,
            )

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
        if _ffi.using_ipc():
            raise CCError("export_dir is not supported via IPC backend", code=2)

        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()

        code = lib.cc_oci_client_export_dir(
            self.handle,
            source.handle,
            dir_path.encode("utf-8"),
            byref(err),
        )
        _ffi.check_error(code, err)

    def build_dockerfile(
        self,
        dockerfile: bytes,
        *,
        cache_dir: str,
        context_dir: str | None = None,
        build_args: dict[str, str] | None = None,
        cancel_token: "CancelToken | None" = None,
    ) -> "Snapshot":
        """Build an image from Dockerfile content.

        Args:
            dockerfile: Dockerfile content as bytes
            cache_dir: Cache directory for intermediate layers (required)
            context_dir: Directory for COPY/ADD instructions (optional)
            build_args: Build arguments for ARG instructions (optional)
            cancel_token: Optional cancellation token

        Returns:
            A Snapshot that can be used to create instances.

        Example:
            dockerfile = b'''
            FROM alpine:3.19
            RUN apk add --no-cache curl
            '''
            snapshot = client.build_dockerfile(dockerfile, cache_dir="/tmp/cache")
        """
        # Import here to avoid circular imports
        from .instance import Snapshot

        if _ffi.using_ipc():
            backend = _ffi.get_ipc_backend()
            handle = backend.build_dockerfile(
                dockerfile, cache_dir,
                context_dir=context_dir or "",
                build_args=build_args,
            )
            return Snapshot(handle, _ipc=True)

        lib = _ffi.get_lib()
        snapshot_handle = _ffi.SnapshotHandle()
        err = _ffi.CCErrorStruct()

        # Build options struct
        opts = _ffi.DockerfileOptionsStruct()
        opts.cache_dir = cache_dir.encode("utf-8")

        if context_dir is not None:
            opts.context_dir = context_dir.encode("utf-8")

        # Build args array
        build_args_array = None
        if build_args:
            build_args_array = (_ffi.BuildArgStruct * len(build_args))()
            for i, (key, value) in enumerate(build_args.items()):
                build_args_array[i].key = key.encode("utf-8")
                build_args_array[i].value = value.encode("utf-8")
            opts.build_args = build_args_array
            opts.build_arg_count = len(build_args)

        # Get cancel token handle
        cancel_handle = _ffi.CancelTokenHandle.invalid()
        if cancel_token:
            cancel_handle = cancel_token.handle

        # Convert dockerfile to C buffer
        dockerfile_buf = (ctypes.c_uint8 * len(dockerfile)).from_buffer_copy(dockerfile)

        lib.cc_build_dockerfile_source(
            self.handle,
            dockerfile_buf,
            len(dockerfile),
            byref(opts),
            cancel_handle,
            byref(snapshot_handle),
            byref(err),
        )

        # Check for errors (function returns void, check error struct)
        if err.code != 0:
            _ffi.check_error(err.code, err)

        return Snapshot(snapshot_handle)


class CancelToken:
    """A cancellation token for long-running operations.

    Example:
        token = CancelToken()
        # In another thread:
        token.cancel()
    """

    def __init__(self) -> None:
        self._freed = False
        self._cancelled = False
        if _ffi.using_ipc():
            self._handle: Any = None
            return
        self._handle = _ffi.get_lib().cc_cancel_token_new()

    def __del__(self) -> None:
        self.close()

    def __enter__(self) -> "CancelToken":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    def close(self) -> None:
        """Free the cancel token."""
        if not self._freed:
            if self._handle is not None and not _ffi.using_ipc():
                _ffi.get_lib().cc_cancel_token_free(self._handle)
            self._freed = True

    @property
    def handle(self) -> Any:
        """Get the underlying handle."""
        return self._handle

    def cancel(self) -> None:
        """Cancel the token. All operations using this token will be cancelled."""
        self._cancelled = True
        if self._handle is not None and not _ffi.using_ipc():
            _ffi.get_lib().cc_cancel_token_cancel(self._handle)

    @property
    def is_cancelled(self) -> bool:
        """Check if the token has been cancelled."""
        if _ffi.using_ipc():
            return self._cancelled
        return bool(_ffi.get_lib().cc_cancel_token_is_cancelled(self._handle))
