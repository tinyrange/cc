#!/usr/bin/env python3
"""
Run all Python binding tests.

Usage:
    python3 tests/run_all.py           # Run non-VM tests
    python3 tests/run_all.py --vm      # Run VM tests (requires hypervisor)

Requires LIBCC_PATH environment variable to be set.
"""

import sys
import os
import subprocess
import argparse

# Get the directory containing this script
TESTS_DIR = os.path.dirname(os.path.abspath(__file__))
PYTHON_DIR = os.path.dirname(TESTS_DIR)


def run_test(script_name: str) -> bool:
    """Run a test script and return True if it passed."""
    script_path = os.path.join(TESTS_DIR, script_name)
    print(f"\n{'=' * 60}")
    print(f"Running: {script_name}")
    print('=' * 60)

    result = subprocess.run(
        [sys.executable, script_path],
        cwd=PYTHON_DIR,
        env=os.environ.copy(),
    )

    return result.returncode == 0


def main():
    parser = argparse.ArgumentParser(description="Run Python binding tests")
    parser.add_argument("--vm", action="store_true",
                        help="Run VM tests (requires hypervisor)")
    args = parser.parse_args()

    # Check for LIBCC_PATH
    if "LIBCC_PATH" not in os.environ:
        print("ERROR: LIBCC_PATH environment variable not set")
        print("Set it to the path of libcc.dylib/libcc.so")
        print("Example: export LIBCC_PATH=/path/to/build/libcc.dylib")
        return 1

    # List of test scripts to run
    tests = [
        "test_api.py",
        "test_hypervisor.py",
        "test_client.py",
    ]

    if args.vm:
        tests.append("test_vm.py")

    # Run all tests
    passed = 0
    failed = 0

    for test in tests:
        if run_test(test):
            passed += 1
        else:
            failed += 1

    # Summary
    print(f"\n{'=' * 60}")
    print("SUMMARY")
    print('=' * 60)
    print(f"Passed: {passed}")
    print(f"Failed: {failed}")
    print(f"Total:  {passed + failed}")

    if failed > 0:
        print("\nSome tests FAILED!")
        return 1

    print("\nAll tests PASSED!")
    return 0


if __name__ == "__main__":
    sys.exit(main())
