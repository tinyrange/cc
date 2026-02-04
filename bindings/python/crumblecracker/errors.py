"""
Exception hierarchy for the cc Python bindings.

Maps C error codes to Python exception types.
"""


class CCError(Exception):
    """Base exception for all cc errors."""

    def __init__(self, message: str, code: int = 0, op: str | None = None, path: str | None = None):
        self.code = code
        self.op = op
        self.path = path
        super().__init__(message)

    def __str__(self) -> str:
        parts = [super().__str__()]
        if self.op:
            parts.append(f"op={self.op}")
        if self.path:
            parts.append(f"path={self.path}")
        return " ".join(parts)


class InvalidHandleError(CCError):
    """Handle is NULL, zero, or already freed."""

    pass


class InvalidArgumentError(CCError):
    """Function argument is invalid."""

    pass


class NotRunningError(CCError):
    """Instance has terminated."""

    pass


class AlreadyClosedError(CCError):
    """Resource was already closed."""

    pass


class TimeoutError(CCError):
    """Operation exceeded time limit."""

    pass


class HypervisorUnavailableError(CCError):
    """No hypervisor support on this system."""

    pass


class IOError(CCError):
    """Filesystem I/O error (local to guest)."""

    pass


class NetworkError(CCError):
    """Network error (DNS, TCP connect, etc.)."""

    pass


class CancelledError(CCError):
    """Operation was cancelled via cancel token."""

    pass


# Error code to exception class mapping
_ERROR_MAP = {
    1: InvalidHandleError,
    2: InvalidArgumentError,
    3: NotRunningError,
    4: AlreadyClosedError,
    5: TimeoutError,
    6: HypervisorUnavailableError,
    7: IOError,
    8: NetworkError,
    9: CancelledError,
}


def error_from_code(code: int, message: str, op: str | None = None, path: str | None = None) -> CCError:
    """Create an appropriate exception from an error code."""
    exc_class = _ERROR_MAP.get(code, CCError)
    return exc_class(message, code=code, op=op, path=path)
