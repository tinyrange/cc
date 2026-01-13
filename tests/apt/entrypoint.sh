#!/bin/bash
set -ex

echo "=== Starting apt install test ==="
echo "Installing curl at runtime..."

# Download only first
apt-get install -y --download-only curl 2>&1

# Check downloaded packages
ls -la /var/cache/apt/archives/

# Now try to install manually with dpkg to see output
echo "=== Manual dpkg install ==="
dpkg -i /var/cache/apt/archives/*.deb 2>&1 || {
    echo "=== dpkg failed, exit code $? ==="
    dpkg -l | head -20 || true
}

echo "=== Package installation complete ==="
echo "Testing curl..."
which curl || echo "curl not in PATH"
ls -la /usr/bin/curl || echo "curl binary not found"
curl --version

echo "=== Test passed ==="
