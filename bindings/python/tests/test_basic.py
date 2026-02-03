"""
Basic integration tests for the cc Python bindings.

These tests mirror the C binding tests in test_basic.c.
"""

import os
import sys

import pytest

# Add the parent directory to the path so we can import cc
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import cc


class TestAPIVersion:
    """Test API version functions."""

    def test_api_version(self):
        """Test that api_version returns expected string."""
        version = cc.api_version()
        assert version == "0.1.0"

    def test_api_version_compatible(self):
        """Test version compatibility checking."""
        assert cc.api_version_compatible(0, 1)
        assert cc.api_version_compatible(0, 0)
        assert not cc.api_version_compatible(1, 0)
        assert not cc.api_version_compatible(0, 99)


class TestLibraryInit:
    """Test library initialization."""

    def test_init_shutdown(self):
        """Test init and shutdown."""
        cc.init()
        cc.shutdown()

    def test_guest_protocol_version(self):
        """Test guest protocol version."""
        cc.init()
        try:
            version = cc.guest_protocol_version()
            assert version == 1
        finally:
            cc.shutdown()


class TestHypervisor:
    """Test hypervisor support detection."""

    def test_supports_hypervisor(self):
        """Test hypervisor detection."""
        cc.init()
        try:
            result = cc.supports_hypervisor()
            # Result is a boolean - either hypervisor is available or not
            assert isinstance(result, bool)
        finally:
            cc.shutdown()

    def test_query_capabilities(self):
        """Test querying system capabilities."""
        cc.init()
        try:
            caps = cc.query_capabilities()
            assert isinstance(caps.hypervisor_available, bool)
            assert isinstance(caps.architecture, str)
            assert caps.architecture in ("x86_64", "arm64", "amd64", "riscv64")
            print(f"Capabilities: hypervisor={caps.hypervisor_available}, arch={caps.architecture}")
        finally:
            cc.shutdown()


class TestCancelToken:
    """Test cancellation tokens."""

    def test_cancel_token_lifecycle(self):
        """Test cancel token creation and cancellation."""
        cc.init()
        try:
            token = cc.CancelToken()
            assert not token.is_cancelled

            token.cancel()
            assert token.is_cancelled

            token.close()
        finally:
            cc.shutdown()

    def test_cancel_token_context_manager(self):
        """Test cancel token as context manager."""
        cc.init()
        try:
            with cc.CancelToken() as token:
                assert not token.is_cancelled
                token.cancel()
                assert token.is_cancelled
        finally:
            cc.shutdown()


class TestOCIClient:
    """Test OCI client operations."""

    def test_client_creation(self):
        """Test OCI client creation."""
        cc.init()
        try:
            with cc.OCIClient() as client:
                cache_dir = client.cache_dir
                assert cache_dir
                print(f"Cache dir: {cache_dir}")
        finally:
            cc.shutdown()

    def test_client_with_custom_cache(self, tmp_path):
        """Test OCI client with custom cache directory."""
        cc.init()
        try:
            cache_dir = str(tmp_path / "cache")
            os.makedirs(cache_dir, exist_ok=True)

            with cc.OCIClient(cache_dir=cache_dir) as client:
                # The Go library may append subdirectories to the cache path
                assert cache_dir in client.cache_dir
        finally:
            cc.shutdown()


@pytest.fixture
def hypervisor_available():
    """Check if hypervisor is available."""
    cc.init()
    try:
        return cc.supports_hypervisor()
    finally:
        cc.shutdown()


@pytest.mark.skipif(
    not os.environ.get("CC_RUN_VM_TESTS"),
    reason="Set CC_RUN_VM_TESTS=1 to run VM tests"
)
class TestWithVM:
    """Tests that require a running VM (hypervisor)."""

    @pytest.fixture(autouse=True)
    def setup(self):
        """Initialize library for each test."""
        cc.init()
        yield
        cc.shutdown()

    def test_pull_and_create_instance(self):
        """Test pulling an image and creating an instance."""
        if not cc.supports_hypervisor():
            pytest.skip("Hypervisor not available")

        with cc.OCIClient() as client:
            # Pull alpine image
            source = client.pull("alpine:latest")
            config = source.get_config()
            print(f"Image architecture: {config.architecture}")

            # Create instance
            options = cc.InstanceOptions(memory_mb=256, cpus=1)
            with cc.Instance(source, options) as inst:
                assert inst.is_running
                print(f"Instance ID: {inst.id}")

    def test_command_execution(self):
        """Test running commands in an instance."""
        if not cc.supports_hypervisor():
            pytest.skip("Hypervisor not available")

        with cc.OCIClient() as client:
            source = client.pull("alpine:latest")

            with cc.Instance(source) as inst:
                # Run echo command
                output = inst.command("echo", "Hello from Python bindings!").output()
                assert b"Hello from Python bindings!" in output

    def test_filesystem_operations(self):
        """Test filesystem read/write operations."""
        if not cc.supports_hypervisor():
            pytest.skip("Hypervisor not available")

        with cc.OCIClient() as client:
            source = client.pull("alpine:latest")

            with cc.Instance(source) as inst:
                test_path = "/tmp/test_file.txt"
                test_data = b"Hello, filesystem!"

                # Write file
                inst.write_file(test_path, test_data)

                # Read file back
                read_data = inst.read_file(test_path)
                assert read_data == test_data

                # Stat file
                info = inst.stat(test_path)
                assert info.size == len(test_data)
                assert not info.is_dir

                # Remove file
                inst.remove(test_path)

    def test_directory_operations(self):
        """Test directory operations."""
        if not cc.supports_hypervisor():
            pytest.skip("Hypervisor not available")

        with cc.OCIClient() as client:
            source = client.pull("alpine:latest")

            with cc.Instance(source) as inst:
                # Create directory
                inst.mkdir("/tmp/testdir")

                # List directory
                entries = inst.read_dir("/tmp")
                names = [e.name for e in entries]
                assert "testdir" in names

                # Remove directory
                inst.remove_all("/tmp/testdir")

    def test_file_handle_operations(self):
        """Test File object operations."""
        if not cc.supports_hypervisor():
            pytest.skip("Hypervisor not available")

        with cc.OCIClient() as client:
            source = client.pull("alpine:latest")

            with cc.Instance(source) as inst:
                # Create and write to file
                with inst.create("/tmp/handle_test.txt") as f:
                    n = f.write(b"Hello, World!")
                    assert n == 13

                # Open and read file
                with inst.open("/tmp/handle_test.txt") as f:
                    data = f.read()
                    assert data == b"Hello, World!"

                    # Seek and read
                    f.seek(0)
                    data2 = f.read(5)
                    assert data2 == b"Hello"


class TestErrors:
    """Test error handling."""

    def test_invalid_handle(self):
        """Test that invalid handles raise appropriate errors."""
        cc.init()
        try:
            # Create and close a client, then try to use it
            client = cc.OCIClient()
            client.close()

            with pytest.raises(cc.CCError):
                _ = client.cache_dir
        finally:
            cc.shutdown()


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
