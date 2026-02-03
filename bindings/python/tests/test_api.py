#!/usr/bin/env python3
"""
Simple test script for API version and library initialization.

Run with: python3 tests/test_api.py
"""

import sys
import os

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import cc


def test_api_version():
    """Test API version functions."""
    print("TEST: cc.api_version()...", end=" ")
    version = cc.api_version()
    assert version == "0.1.0", f"Expected 0.1.0, got {version}"
    print("PASSED")


def test_api_version_compatible():
    """Test version compatibility checking."""
    print("TEST: cc.api_version_compatible()...", end=" ")
    assert cc.api_version_compatible(0, 1), "0.1 should be compatible"
    assert cc.api_version_compatible(0, 0), "0.0 should be compatible"
    assert not cc.api_version_compatible(1, 0), "1.0 should NOT be compatible"
    assert not cc.api_version_compatible(0, 99), "0.99 should NOT be compatible"
    print("PASSED")


def test_init_shutdown():
    """Test library initialization and shutdown."""
    print("TEST: cc.init() / cc.shutdown()...", end=" ")
    cc.init()
    cc.shutdown()
    print("PASSED")


def test_guest_protocol_version():
    """Test guest protocol version."""
    print("TEST: cc.guest_protocol_version()...", end=" ")
    cc.init()
    try:
        version = cc.guest_protocol_version()
        assert version == 1, f"Expected 1, got {version}"
    finally:
        cc.shutdown()
    print("PASSED")


def main():
    print("=== API Tests ===\n")

    test_api_version()
    test_api_version_compatible()
    test_init_shutdown()
    test_guest_protocol_version()

    print("\n=== All API tests passed! ===")
    return 0


if __name__ == "__main__":
    sys.exit(main())
