"""
Command execution for VM instances.
"""

from __future__ import annotations

from ctypes import POINTER, byref, c_char_p, c_int, c_size_t, c_uint8, c_void_p, cast
from typing import TYPE_CHECKING

from . import _ffi
from .errors import CCError

if TYPE_CHECKING:
    from .instance import Instance


class Cmd:
    """A command to run in a VM instance.

    Commands can be configured with environment variables and working directory
    before being executed.

    Example:
        # Simple command
        cmd = instance.command("echo", "hello", "world")
        output = cmd.output()

        # With environment
        cmd = instance.command("env")
        cmd.set_env("MY_VAR", "my_value")
        cmd.set_dir("/tmp")
        exit_code = cmd.run()

        # Async execution
        cmd = instance.command("sleep", "10")
        cmd.start()
        # ... do other work ...
        cmd.wait()
    """

    def __init__(self, instance: "Instance", name: str, args: list[str]):
        """Create a new command.

        Users should use Instance.command() instead of this constructor.
        """
        self._ipc = _ffi.using_ipc()
        self._instance = instance
        self._started = False
        self._freed = False

        if self._ipc:
            backend = _ffi.get_ipc_backend()
            self._handle = backend.cmd_new(name, args)
            return

        lib = _ffi.get_lib()
        handle = _ffi.CmdHandle()
        err = _ffi.CCErrorStruct()

        # Build NULL-terminated args array
        args_array = (c_char_p * (len(args) + 1))()
        for i, arg in enumerate(args):
            args_array[i] = arg.encode("utf-8")
        args_array[len(args)] = None

        code = lib.cc_cmd_new(
            instance.handle, name.encode("utf-8"), args_array, byref(handle), byref(err)
        )
        _ffi.check_error(code, err)

        self._handle = handle

    @classmethod
    def entrypoint(cls, instance: "Instance", args: list[str] | None = None) -> "Cmd":
        """Create a command using the container's entrypoint."""
        cmd = cls.__new__(cls)
        cmd._ipc = _ffi.using_ipc()
        cmd._instance = instance
        cmd._started = False
        cmd._freed = False

        if cmd._ipc:
            backend = _ffi.get_ipc_backend()
            cmd._handle = backend.cmd_entrypoint(args or [])
            return cmd

        lib = _ffi.get_lib()
        handle = _ffi.CmdHandle()
        err = _ffi.CCErrorStruct()

        # Build args array
        args_ptr = None
        if args:
            args_array = (c_char_p * (len(args) + 1))()
            for i, arg in enumerate(args):
                args_array[i] = arg.encode("utf-8")
            args_array[len(args)] = None
            args_ptr = args_array

        code = lib.cc_cmd_entrypoint(instance.handle, args_ptr, byref(handle), byref(err))
        _ffi.check_error(code, err)

        cmd._handle = handle
        return cmd

    def __del__(self) -> None:
        self._free()

    def _free(self) -> None:
        """Free the command if not yet started."""
        if not self._freed and not self._started and self._handle:
            if self._ipc:
                _ffi.get_ipc_backend().cmd_free(self._handle)
            else:
                _ffi.get_lib().cc_cmd_free(self._handle)
            self._freed = True

    @property
    def handle(self) -> _ffi.CmdHandle:
        """Get the underlying handle."""
        if self._freed:
            raise CCError("Cmd has been freed", code=4)
        return self._handle

    def set_dir(self, dir: str) -> "Cmd":
        """Set the working directory for the command.

        Args:
            dir: Working directory path

        Returns:
            self for method chaining
        """
        if self._ipc:
            _ffi.get_ipc_backend().cmd_set_dir(self._handle, dir)
            return self

        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()

        code = lib.cc_cmd_set_dir(self.handle, dir.encode("utf-8"), byref(err))
        _ffi.check_error(code, err)

        return self

    def set_env(self, key: str, value: str) -> "Cmd":
        """Set an environment variable.

        Args:
            key: Variable name
            value: Variable value

        Returns:
            self for method chaining
        """
        if self._ipc:
            _ffi.get_ipc_backend().cmd_set_env(self._handle, key, value)
            return self

        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()

        code = lib.cc_cmd_set_env(
            self.handle, key.encode("utf-8"), value.encode("utf-8"), byref(err)
        )
        _ffi.check_error(code, err)

        return self

    def get_env(self, key: str) -> str | None:
        """Get an environment variable.

        Args:
            key: Variable name

        Returns:
            Variable value, or None if not set
        """
        if self._ipc:
            val = _ffi.get_ipc_backend().cmd_get_env(self._handle, key)
            return val if val else None

        lib = _ffi.get_lib()
        ptr = lib.cc_cmd_get_env(self.handle, key.encode("utf-8"))
        if not ptr:
            return None
        return _ffi._get_string_and_free(lib, ptr)

    def environ(self) -> list[str]:
        """Get all environment variables.

        Returns:
            List of "KEY=VALUE" strings
        """
        if self._ipc:
            return _ffi.get_ipc_backend().cmd_environ(self._handle)

        lib = _ffi.get_lib()
        env_ptr = POINTER(c_void_p)()
        count = c_size_t()
        err = _ffi.CCErrorStruct()

        code = lib.cc_cmd_environ(self.handle, byref(env_ptr), byref(count), byref(err))
        _ffi.check_error(code, err)

        result = []
        if env_ptr and count.value > 0:
            for i in range(count.value):
                ptr = env_ptr[i]
                if ptr:
                    value = cast(ptr, c_char_p).value
                    if value:
                        result.append(value.decode("utf-8"))
                    lib.cc_free_string(ptr)
            # Free the array itself
            lib.cc_free_bytes(cast(env_ptr, POINTER(c_uint8)))

        return result

    def start(self) -> None:
        """Start the command (non-blocking).

        The command runs asynchronously. Use wait() to wait for completion.
        """
        if self._started:
            raise CCError("Cmd has already been started", code=2)

        if self._ipc:
            _ffi.get_ipc_backend().cmd_start(self._handle)
            self._started = True
            return

        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()

        code = lib.cc_cmd_start(self.handle, byref(err))
        _ffi.check_error(code, err)

        self._started = True

    def wait(self) -> int:
        """Wait for the command to complete.

        Returns:
            Exit code
        """
        if not self._started:
            raise CCError("Cmd has not been started", code=2)

        if self._ipc:
            return _ffi.get_ipc_backend().cmd_wait(self._handle)

        lib = _ffi.get_lib()
        exit_code = c_int()
        err = _ffi.CCErrorStruct()

        code = lib.cc_cmd_wait(self.handle, byref(exit_code), byref(err))
        _ffi.check_error(code, err)

        return exit_code.value

    def run(self) -> int:
        """Run the command and wait for completion.

        Returns:
            Exit code
        """
        if self._ipc:
            self._started = True
            return _ffi.get_ipc_backend().cmd_run(self._handle)

        lib = _ffi.get_lib()
        exit_code = c_int()
        err = _ffi.CCErrorStruct()

        code = lib.cc_cmd_run(self.handle, byref(exit_code), byref(err))
        self._started = True  # Mark as started so we don't try to free
        _ffi.check_error(code, err)

        return exit_code.value

    def output(self) -> bytes:
        """Run the command and capture stdout.

        Returns:
            Standard output as bytes
        """
        if self._ipc:
            self._started = True
            data, _ = _ffi.get_ipc_backend().cmd_output(self._handle)
            return data

        lib = _ffi.get_lib()
        out_ptr = POINTER(c_uint8)()
        length = c_size_t()
        exit_code = c_int()
        err = _ffi.CCErrorStruct()

        code = lib.cc_cmd_output(
            self.handle, byref(out_ptr), byref(length), byref(exit_code), byref(err)
        )
        self._started = True
        _ffi.check_error(code, err)

        if out_ptr and length.value > 0:
            result = bytes(out_ptr[: length.value])
            lib.cc_free_bytes(out_ptr)
            return result
        return b""

    def combined_output(self) -> bytes:
        """Run the command and capture stdout + stderr.

        Returns:
            Combined output as bytes
        """
        if self._ipc:
            self._started = True
            data, _ = _ffi.get_ipc_backend().cmd_combined_output(self._handle)
            return data

        lib = _ffi.get_lib()
        out_ptr = POINTER(c_uint8)()
        length = c_size_t()
        exit_code = c_int()
        err = _ffi.CCErrorStruct()

        code = lib.cc_cmd_combined_output(
            self.handle, byref(out_ptr), byref(length), byref(exit_code), byref(err)
        )
        self._started = True
        _ffi.check_error(code, err)

        if out_ptr and length.value > 0:
            result = bytes(out_ptr[: length.value])
            lib.cc_free_bytes(out_ptr)
            return result
        return b""

    @property
    def exit_code(self) -> int:
        """Get the exit code (after wait/run)."""
        if self._ipc:
            return _ffi.get_ipc_backend().cmd_exit_code(self._handle)
        return _ffi.get_lib().cc_cmd_exit_code(self.handle)

    def kill(self) -> None:
        """Kill a started command and release resources.

        Safe to call on commands that have already completed.
        After calling, the handle is invalid.
        """
        if self._freed:
            return

        if self._ipc:
            _ffi.get_ipc_backend().cmd_kill(self._handle)
            self._freed = True
            return

        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()

        code = lib.cc_cmd_kill(self.handle, byref(err))
        _ffi.check_error(code, err)

        self._freed = True
