#!/usr/bin/env python3
"""
VM integration tests for the cc Python bindings.

These tests require hypervisor access and will pull container images.

Run with: python3 tests/test_vm.py
"""

import sys
import os

import pytest

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import crumblecracker as cc

pytestmark = pytest.mark.skipif(
    not os.environ.get("CC_RUN_VM_TESTS"),
    reason="Set CC_RUN_VM_TESTS=1 to run VM tests",
)


def check_hypervisor():
    """Check if hypervisor is available and accessible, skip if not."""
    cc.init()
    try:
        if not cc.supports_hypervisor():
            print("SKIPPED: Hypervisor not available")
            return False

        # Try to actually create an instance to verify hypervisor access
        # (entitlements may prevent access even if hypervisor is detected)
        try:
            with cc.OCIClient() as client:
                source = client.pull("alpine:latest")
                try:
                    with cc.Instance(source) as inst:
                        pass  # Success - hypervisor is accessible
                except cc.HypervisorUnavailableError:
                    print("SKIPPED: Hypervisor access denied (entitlements)")
                    return False
        except Exception as e:
            if "denied" in str(e) or "hypervisor unavailable" in str(e).lower():
                print("SKIPPED: Hypervisor access denied (entitlements)")
                return False
            raise

        return True
    finally:
        cc.shutdown()


def test_pull_image():
    """Test pulling an OCI image."""
    print("TEST: Pull alpine:latest...", end=" ", flush=True)
    cc.init()
    try:
        with cc.OCIClient() as client:
            source = client.pull("alpine:latest")
            assert source, "Source should not be None"

            # Get image config
            config = source.get_config()
            assert config.architecture in ("amd64", "arm64", "x86_64"), \
                f"Unexpected architecture: {config.architecture}"

            source.close()
            print(f"PASSED (arch={config.architecture})")
    finally:
        cc.shutdown()


def test_create_instance():
    """Test creating and closing an instance."""
    print("TEST: Create instance...", end=" ", flush=True)
    cc.init()
    try:
        with cc.OCIClient() as client:
            source = client.pull("alpine:latest")

            options = cc.InstanceOptions(memory_mb=256, cpus=1)
            with cc.Instance(source, options) as inst:
                assert inst.is_running, "Instance should be running"
                instance_id = inst.id
                assert instance_id, "Instance ID should not be empty"
                print(f"PASSED (id={instance_id})")
    finally:
        cc.shutdown()


def test_command_execution():
    """Test running a command in an instance."""
    print("TEST: Command execution...", end=" ", flush=True)
    cc.init()
    try:
        with cc.OCIClient() as client:
            source = client.pull("alpine:latest")

            with cc.Instance(source) as inst:
                # Run echo command
                output = inst.command("echo", "Hello from Python!").output()
                assert b"Hello from Python!" in output, \
                    f"Expected 'Hello from Python!' in output, got: {output}"
                print("PASSED")
    finally:
        cc.shutdown()


def test_command_exit_code():
    """Test command exit codes."""
    print("TEST: Command exit code...", end=" ", flush=True)
    cc.init()
    try:
        with cc.OCIClient() as client:
            source = client.pull("alpine:latest")

            with cc.Instance(source) as inst:
                # Successful command
                exit_code = inst.command("true").run()
                assert exit_code == 0, f"Expected exit code 0, got {exit_code}"

                # Failed command
                exit_code = inst.command("false").run()
                assert exit_code != 0, f"Expected non-zero exit code, got {exit_code}"

                print("PASSED")
    finally:
        cc.shutdown()


def test_filesystem_write_read():
    """Test writing and reading files."""
    print("TEST: Filesystem write/read...", end=" ", flush=True)
    cc.init()
    try:
        with cc.OCIClient() as client:
            source = client.pull("alpine:latest")

            with cc.Instance(source) as inst:
                test_path = "/root/test_file.txt"
                test_data = b"Hello, filesystem!"

                # Write file
                inst.write_file(test_path, test_data)

                # Read file back
                read_data = inst.read_file(test_path)
                assert read_data == test_data, \
                    f"Data mismatch: expected {test_data}, got {read_data}"

                print("PASSED")
    finally:
        cc.shutdown()


def test_filesystem_stat():
    """Test file stat operations."""
    print("TEST: Filesystem stat...", end=" ", flush=True)
    cc.init()
    try:
        with cc.OCIClient() as client:
            source = client.pull("alpine:latest")

            with cc.Instance(source) as inst:
                test_path = "/root/stat_test.txt"
                test_data = b"Hello, World!"

                inst.write_file(test_path, test_data)

                info = inst.stat(test_path)
                assert info.size == len(test_data), \
                    f"Size mismatch: expected {len(test_data)}, got {info.size}"
                assert not info.is_dir, "Should not be a directory"

                print("PASSED")
    finally:
        cc.shutdown()


def test_filesystem_mkdir():
    """Test directory creation."""
    print("TEST: Filesystem mkdir...", end=" ", flush=True)
    cc.init()
    try:
        with cc.OCIClient() as client:
            source = client.pull("alpine:latest")

            with cc.Instance(source) as inst:
                # Create directory
                inst.mkdir("/root/testdir")

                # Verify it exists
                info = inst.stat("/root/testdir")
                assert info.is_dir, "Should be a directory"

                # List parent directory
                entries = inst.read_dir("/root")
                names = [e.name for e in entries]
                assert "testdir" in names, f"testdir not found in {names}"

                print("PASSED")
    finally:
        cc.shutdown()


def test_filesystem_remove():
    """Test file removal."""
    print("TEST: Filesystem remove...", end=" ", flush=True)
    cc.init()
    try:
        with cc.OCIClient() as client:
            source = client.pull("alpine:latest")

            with cc.Instance(source) as inst:
                test_path = "/root/remove_test.txt"

                # Create file
                inst.write_file(test_path, b"delete me")

                # Remove it
                inst.remove(test_path)

                # Verify it's gone
                try:
                    inst.stat(test_path)
                    assert False, "File should have been removed"
                except cc.IOError:
                    pass  # Expected

                print("PASSED")
    finally:
        cc.shutdown()


def test_file_handle():
    """Test File object operations."""
    print("TEST: File handle...", end=" ", flush=True)
    cc.init()
    try:
        with cc.OCIClient() as client:
            source = client.pull("alpine:latest")

            with cc.Instance(source) as inst:
                test_path = "/root/handle_test.txt"

                # Create and write
                with inst.create(test_path) as f:
                    n = f.write(b"Hello, World!")
                    assert n == 13, f"Expected 13 bytes written, got {n}"

                # Open and read
                with inst.open(test_path) as f:
                    data = f.read()
                    assert data == b"Hello, World!", f"Data mismatch: {data}"

                    # Seek and read partial
                    f.seek(0)
                    data = f.read(5)
                    assert data == b"Hello", f"Partial read mismatch: {data}"

                print("PASSED")
    finally:
        cc.shutdown()


def main():
    print("=== VM Integration Tests ===\n")

    # Check hypervisor first
    if not check_hypervisor():
        print("\nSkipping VM tests - no hypervisor available")
        return 0

    tests = [
        test_pull_image,
        test_create_instance,
        test_command_execution,
        test_command_exit_code,
        test_filesystem_write_read,
        test_filesystem_stat,
        test_filesystem_mkdir,
        test_filesystem_remove,
        test_file_handle,
    ]

    passed = 0
    failed = 0

    for test in tests:
        try:
            test()
            passed += 1
        except Exception as e:
            print(f"FAILED: {e}")
            failed += 1

    print(f"\n=== Results: {passed} passed, {failed} failed ===")

    if failed > 0:
        return 1
    print("\nAll VM tests passed!")
    return 0


if __name__ == "__main__":
    sys.exit(main())
