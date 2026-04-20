from .api import (
    DEFAULT_CONTAINERS_PATH,
    DEFAULT_CVMFS_MIRROR,
    DEFAULT_CVMFS_REPO,
    NeurodeskContainer,
    SharedDirectory,
    SharedPath,
    connect,
    container,
    resolve_base_url,
    resolve_container_reference,
    search,
    share_dir,
)
from .client import PyNeurodeskClient
from .models import (
    CommandResult,
    CVMFSDirectoryEntry,
    CVMFSListResponse,
    CVMFSReadRequest,
    CVMFSReadResponse,
    CVMFSSource,
    ContainerReference,
    DaemonState,
    ImageSource,
    ImageState,
    ImportImageRequest,
    RunCommandRequest,
    ShareMount,
)

__all__ = [
    "CommandResult",
    "CVMFSDirectoryEntry",
    "CVMFSListResponse",
    "CVMFSReadRequest",
    "CVMFSReadResponse",
    "CVMFSSource",
    "ContainerReference",
    "DEFAULT_CONTAINERS_PATH",
    "DEFAULT_CVMFS_MIRROR",
    "DEFAULT_CVMFS_REPO",
    "DaemonState",
    "ImageSource",
    "ImageState",
    "ImportImageRequest",
    "NeurodeskContainer",
    "PyNeurodeskClient",
    "RunCommandRequest",
    "ShareMount",
    "SharedDirectory",
    "SharedPath",
    "connect",
    "container",
    "main",
    "resolve_base_url",
    "resolve_container_reference",
    "search",
    "share_dir",
]


def main() -> None:
    print("pyneurodesk")
