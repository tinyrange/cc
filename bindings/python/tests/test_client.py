#!/usr/bin/env python3
"""
Simple test script for CancelToken and OCIClient.

Run with: python3 tests/test_client.py
"""

import sys
import os
import tempfile

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import crumblecracker as cc


def test_cancel_token():
    """Test cancel token creation and cancellation."""
    print("TEST: cc.CancelToken()...", end=" ")
    cc.init()
    try:
        token = cc.CancelToken()
        assert not token.is_cancelled, "New token should not be cancelled"

        token.cancel()
        assert token.is_cancelled, "Token should be cancelled after cancel()"

        token.close()
        print("PASSED")
    finally:
        cc.shutdown()


def test_cancel_token_context_manager():
    """Test cancel token as context manager."""
    print("TEST: CancelToken context manager...", end=" ")
    cc.init()
    try:
        with cc.CancelToken() as token:
            assert not token.is_cancelled
            token.cancel()
            assert token.is_cancelled
        print("PASSED")
    finally:
        cc.shutdown()


def test_oci_client_creation():
    """Test OCI client creation."""
    print("TEST: cc.OCIClient()...", end=" ")
    cc.init()
    try:
        with cc.OCIClient() as client:
            cache_dir = client.cache_dir
            assert cache_dir, "Cache dir should not be empty"
            print(f"PASSED (cache={cache_dir})")
    finally:
        cc.shutdown()


def test_oci_client_custom_cache():
    """Test OCI client with custom cache directory."""
    print("TEST: OCIClient with custom cache...", end=" ")
    cc.init()
    try:
        with tempfile.TemporaryDirectory() as tmpdir:
            cache_path = os.path.join(tmpdir, "cache")
            os.makedirs(cache_path)

            with cc.OCIClient(cache_dir=cache_path) as client:
                # The Go library may append subdirectories
                assert cache_path in client.cache_dir, \
                    f"Expected {cache_path} in {client.cache_dir}"
                print(f"PASSED (path={client.cache_dir})")
    finally:
        cc.shutdown()


def main():
    print("=== Client Tests ===\n")

    test_cancel_token()
    test_cancel_token_context_manager()
    test_oci_client_creation()
    test_oci_client_custom_cache()

    print("\n=== All client tests passed! ===")
    return 0


if __name__ == "__main__":
    sys.exit(main())
