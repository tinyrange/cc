#!/bin/bash
set -e

echo "=== Starting apt install test ==="
echo "Installing curl at runtime..."

# Use -o Debug::pkgDPkgPM=true for verbose dpkg output
apt-get install -y -o Debug::pkgDPkgPM=true curl 2>&1

echo "=== Package installation complete ==="
echo "Testing curl..."
curl --version

echo "=== Test passed ==="
