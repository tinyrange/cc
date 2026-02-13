"""
IPC client for communicating with the cc-helper process.

Handles helper discovery, spawning, and the synchronous RPC protocol
over Unix domain sockets.
"""

from __future__ import annotations

import os
import platform
import shutil
import socket
import subprocess
import sys
import tempfile
import time
from collections.abc import Callable
from threading import Lock
from typing import Any

from .errors import CCError, error_from_code
from ._ipc_protocol import (
    HEADER_SIZE,
    MSG_ERROR,
    Decoder,
    Encoder,
    decode_header,
    encode_header,
)


class HelperNotFoundError(CCError):
    """Raised when the cc-helper binary cannot be found."""

    def __init__(self, searched_paths: list[str]):
        self.searched_paths = searched_paths
        super().__init__(
            f"cc-helper not found (searched: {searched_paths})",
            code=7,
        )


def _find_helper() -> tuple[str | None, list[str]]:
    """Find the cc-helper binary.

    Search order:
    1. CC_HELPER_PATH environment variable
    2. Adjacent to this package directory
    3. Platform-specific user directory
    4. System PATH

    Returns:
        (path, searched_paths) — path is None if not found.
    """
    searched: list[str] = []
    system = platform.system()
    exe_suffix = ".exe" if system == "Windows" else ""
    helper_name = f"cc-helper{exe_suffix}"

    # 1. CC_HELPER_PATH environment variable
    env_path = os.environ.get("CC_HELPER_PATH")
    if env_path:
        searched.append(env_path)
        if os.path.isfile(env_path):
            return env_path, []

    # 2. Adjacent to this package directory
    pkg_dir = os.path.dirname(os.path.abspath(__file__))
    for search_dir in [pkg_dir, os.path.dirname(pkg_dir)]:
        path = os.path.join(search_dir, helper_name)
        searched.append(path)
        if os.path.isfile(path):
            return path, []

    # 3. Platform-specific user directory
    if system == "Darwin":
        home = os.path.expanduser("~")
        path = os.path.join(home, "Library", "Application Support", "cc", "bin", helper_name)
        searched.append(path)
        if os.path.isfile(path):
            return path, []
    elif system == "Linux":
        home = os.path.expanduser("~")
        path = os.path.join(home, ".local", "share", "cc", "bin", helper_name)
        searched.append(path)
        if os.path.isfile(path):
            return path, []
    elif system == "Windows":
        app_data = os.environ.get("LOCALAPPDATA", "")
        if app_data:
            path = os.path.join(app_data, "cc", "bin", helper_name)
            searched.append(path)
            if os.path.isfile(path):
                return path, []

    # 4. System PATH
    found = shutil.which("cc-helper")
    if found:
        return found, []
    searched.append("$PATH")

    return None, searched


# Socket path counter for uniqueness
_socket_counter = 0
_socket_counter_lock = Lock()


def _next_socket_path() -> str:
    """Generate a unique temporary socket path.

    On Windows, uses a shorter path to stay within the 108-char sun_path limit.
    """
    global _socket_counter
    with _socket_counter_lock:
        _socket_counter += 1
        count = _socket_counter

    system = platform.system()
    if system == "Windows":
        # Use a short subdirectory under temp to minimize path length
        tmp_dir = os.path.join(tempfile.gettempdir(), "cc")
        os.makedirs(tmp_dir, exist_ok=True)
        return os.path.join(tmp_dir, f"h-{os.getpid()}-{count}.sock")
    else:
        return os.path.join(
            tempfile.gettempdir(),
            f"cc-helper-{os.getpid()}-{time.time_ns()}-{count}.sock",
        )


class IPCClient:
    """Synchronous IPC client for communicating with cc-helper."""

    def __init__(self, sock: socket.socket, socket_path: str, process: subprocess.Popen[bytes] | None = None):
        self._sock = sock
        self._socket_path = socket_path
        self._process = process
        self._lock = Lock()
        self._closed = False

    def call(self, msg_type: int, payload: bytes = b"") -> bytes:
        """Send a request and wait for a response (synchronous RPC)."""
        with self._lock:
            if self._closed:
                raise CCError("Client closed", code=4)

            # Write request header + payload
            header = encode_header(msg_type, len(payload))
            self._sock.sendall(header)
            if payload:
                self._sock.sendall(payload)

            # Read response header
            resp_header = self._recv_exact(HEADER_SIZE)
            resp_type, resp_len = decode_header(resp_header)

            # Read response payload
            resp_payload = self._recv_exact(resp_len) if resp_len > 0 else b""

            # Check for error response
            if resp_type == MSG_ERROR:
                dec = Decoder(resp_payload)
                err = dec.error()
                if err:
                    raise err
                raise CCError("Unknown error from helper", code=99)

            return resp_payload

    def call_with_encoder(self, msg_type: int, encode_fn: Callable[["Encoder"], None]) -> bytes:
        """Convenience: call using an Encoder callback."""
        from ._ipc_protocol import Encoder

        enc = Encoder()
        encode_fn(enc)
        return self.call(msg_type, enc.get_bytes())

    def _recv_exact(self, n: int) -> bytes:
        """Read exactly n bytes from the socket."""
        data = bytearray()
        while len(data) < n:
            chunk = self._sock.recv(n - len(data))
            if not chunk:
                raise CCError("Connection closed by helper", code=7)
            data.extend(chunk)
        return bytes(data)

    def close(self) -> None:
        """Close the client connection and helper process."""
        if self._closed:
            return
        self._closed = True

        # Close socket
        try:
            self._sock.close()
        except OSError:
            pass

        # Shut down helper process
        if self._process is not None:
            try:
                self._process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                self._process.kill()
                self._process.wait(timeout=5)

        # Clean up socket file
        if self._socket_path:
            _remove_socket(self._socket_path)

    @property
    def is_closed(self) -> bool:
        return self._closed

    def __del__(self) -> None:
        self.close()

    def __enter__(self) -> "IPCClient":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


def _remove_socket(path: str) -> None:
    """Remove a socket file, retrying briefly on Windows."""
    if platform.system() == "Windows":
        for _ in range(5):
            try:
                os.remove(path)
                return
            except OSError:
                time.sleep(0.05)
    else:
        try:
            os.remove(path)
        except OSError:
            pass


def spawn_helper() -> IPCClient:
    """Spawn a new cc-helper process and connect to it.

    Raises:
        HelperNotFoundError: If cc-helper binary cannot be found.
        CCError: If connection to the helper fails.
    """
    helper_path, searched = _find_helper()
    if helper_path is None:
        raise HelperNotFoundError(searched)

    socket_path = _next_socket_path()

    # Build subprocess creation kwargs
    kwargs: dict[str, Any] = {
        "stdin": subprocess.DEVNULL,
        "stderr": sys.stderr,
        "stdout": subprocess.DEVNULL,
    }

    # On Windows, hide the console window
    if platform.system() == "Windows":
        startupinfo = subprocess.STARTUPINFO()  # type: ignore[attr-defined]
        startupinfo.dwFlags |= subprocess.STARTF_USESHOWWINDOW  # type: ignore[attr-defined]
        startupinfo.wShowWindow = 0  # SW_HIDE
        kwargs["startupinfo"] = startupinfo

    process = subprocess.Popen(
        [helper_path, "-socket", socket_path],
        **kwargs,
    )

    # Wait for the socket to appear and connect
    deadline = time.monotonic() + 10.0
    last_err: Exception | None = None

    while time.monotonic() < deadline:
        if os.path.exists(socket_path):
            try:
                sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                sock.connect(socket_path)
                return IPCClient(sock, socket_path, process)
            except OSError as e:
                last_err = e
                sock.close()
        time.sleep(0.01)

    # Cleanup on failure
    process.kill()
    process.wait()
    _remove_socket(socket_path)

    raise CCError(
        f"Failed to connect to cc-helper: {last_err}",
        code=7,
    )


def connect_to(socket_path: str) -> IPCClient:
    """Connect to an existing cc-helper at the given socket path."""
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    try:
        sock.connect(socket_path)
    except OSError as e:
        sock.close()
        raise CCError(f"Failed to connect to cc-helper: {e}", code=7) from e
    return IPCClient(sock, socket_path)
