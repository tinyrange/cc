"""
Python bindings for the cc virtualization library.

This package provides a Pythonic interface to cc's virtualization primitives,
allowing you to pull OCI images, create VM instances, and interact with them
through filesystem operations, command execution, and networking.

Example:
    import cc

    # Initialize the library
    cc.init()

    # Check for hypervisor support
    if cc.supports_hypervisor():
        print("Hypervisor available!")

    # Pull an image and create an instance
    with cc.OCIClient() as client:
        source = client.pull("alpine:latest")

        with cc.Instance(source) as inst:
            # Run a command
            output = inst.command("echo", "Hello from VM!").output()
            print(output.decode())

            # Read/write files
            inst.write_file("/tmp/test.txt", b"Hello, World!")
            data = inst.read_file("/tmp/test.txt")
            print(data.decode())

    # Shutdown when done
    cc.shutdown()
"""

from .errors import (
    AlreadyClosedError,
    CancelledError,
    CCError,
    HypervisorUnavailableError,
    InvalidArgumentError,
    InvalidHandleError,
    IOError,
    NetworkError,
    NotRunningError,
    TimeoutError,
)
from .types import (
    O_APPEND,
    O_CREATE,
    O_EXCL,
    O_RDONLY,
    O_RDWR,
    O_TRUNC,
    O_WRONLY,
    Capabilities,
    DirEntry,
    DownloadProgress,
    FileInfo,
    ImageConfig,
    InstanceOptions,
    MountConfig,
    ProgressCallback,
    PullOptions,
    PullPolicy,
    SeekWhence,
    SnapshotOptions,
)
from .client import CancelToken, InstanceSource, OCIClient
from .instance import Conn, File, Instance, Listener, Snapshot, query_capabilities
from .cmd import Cmd
from ._ffi import (
    api_version,
    api_version_compatible,
    guest_protocol_version,
    init,
    shutdown,
    supports_hypervisor,
)

__version__ = "0.1.0"

__all__ = [
    # Version info
    "__version__",
    "api_version",
    "api_version_compatible",
    "guest_protocol_version",
    # Library lifecycle
    "init",
    "shutdown",
    "supports_hypervisor",
    "query_capabilities",
    # Client
    "OCIClient",
    "InstanceSource",
    "CancelToken",
    # Instance
    "Instance",
    "InstanceOptions",
    "MountConfig",
    # Files
    "File",
    "FileInfo",
    "DirEntry",
    "SeekWhence",
    "O_RDONLY",
    "O_WRONLY",
    "O_RDWR",
    "O_APPEND",
    "O_CREATE",
    "O_TRUNC",
    "O_EXCL",
    # Commands
    "Cmd",
    # Networking
    "Listener",
    "Conn",
    # Snapshots
    "Snapshot",
    "SnapshotOptions",
    # Images
    "ImageConfig",
    "PullOptions",
    "PullPolicy",
    "DownloadProgress",
    "ProgressCallback",
    # Capabilities
    "Capabilities",
    # Errors
    "CCError",
    "InvalidHandleError",
    "InvalidArgumentError",
    "NotRunningError",
    "AlreadyClosedError",
    "TimeoutError",
    "HypervisorUnavailableError",
    "IOError",
    "NetworkError",
    "CancelledError",
]
