#!/usr/bin/env python3
"""
Simple test script for hypervisor detection and capabilities.

Run with: python3 tests/test_hypervisor.py
"""

import sys
import os

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import crumblecracker as cc


def test_supports_hypervisor():
    """Test hypervisor support detection."""
    print("TEST: cc.supports_hypervisor()...", end=" ")
    cc.init()
    try:
        result = cc.supports_hypervisor()
        assert isinstance(result, bool), f"Expected bool, got {type(result)}"
        status = "available" if result else "not available"
        print(f"PASSED (hypervisor {status})")
        return result
    finally:
        cc.shutdown()


def test_query_capabilities():
    """Test querying system capabilities."""
    print("TEST: cc.query_capabilities()...", end=" ")
    cc.init()
    try:
        caps = cc.query_capabilities()
        assert isinstance(caps.hypervisor_available, bool)
        assert isinstance(caps.architecture, str)
        assert caps.architecture in ("x86_64", "arm64", "amd64", "riscv64"), \
            f"Unexpected architecture: {caps.architecture}"
        print(f"PASSED (arch={caps.architecture}, hypervisor={caps.hypervisor_available})")
    finally:
        cc.shutdown()


def main():
    print("=== Hypervisor Tests ===\n")

    test_supports_hypervisor()
    test_query_capabilities()

    print("\n=== All hypervisor tests passed! ===")
    return 0


if __name__ == "__main__":
    sys.exit(main())
