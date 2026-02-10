#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

echo "=== Publishing npm platform packages ==="
echo ""
echo "This script will build and publish all 4 platform packages."
echo "You'll need to login to npm before each publish (2-hour session tokens)."
echo ""

# Build and publish darwin-arm64
echo "=== 1/4: @crumblecracker/cc-darwin-arm64 ==="
mkdir -p "$SCRIPT_DIR/darwin-arm64/bin"
GOOS=darwin GOARCH=arm64 go build -o "$SCRIPT_DIR/darwin-arm64/bin/cc-helper" "$ROOT/cmd/cc-helper"
codesign --sign - --entitlements "$ROOT/tools/entitlements.xml" --force "$SCRIPT_DIR/darwin-arm64/bin/cc-helper"
echo "Built darwin-arm64. Publishing..."
echo ">>> Run: npm login (if session expired)"
read -p "Press enter when ready to publish darwin-arm64..."
(cd "$SCRIPT_DIR/darwin-arm64" && npm publish --access public --provenance=false)
echo ""

# Build and publish darwin-x64
echo "=== 2/4: @crumblecracker/cc-darwin-x64 ==="
mkdir -p "$SCRIPT_DIR/darwin-x64/bin"
GOOS=darwin GOARCH=amd64 go build -o "$SCRIPT_DIR/darwin-x64/bin/cc-helper" "$ROOT/cmd/cc-helper"
codesign --sign - --entitlements "$ROOT/tools/entitlements.xml" --force "$SCRIPT_DIR/darwin-x64/bin/cc-helper"
echo "Built darwin-x64. Publishing..."
read -p "Press enter when ready to publish darwin-x64..."
(cd "$SCRIPT_DIR/darwin-x64" && npm publish --access public --provenance=false)
echo ""

# Build and publish linux-x64
echo "=== 3/4: @crumblecracker/cc-linux-x64 ==="
mkdir -p "$SCRIPT_DIR/linux-x64/bin"
GOOS=linux GOARCH=amd64 go build -o "$SCRIPT_DIR/linux-x64/bin/cc-helper" "$ROOT/cmd/cc-helper"
echo "Built linux-x64. Publishing..."
read -p "Press enter when ready to publish linux-x64..."
(cd "$SCRIPT_DIR/linux-x64" && npm publish --access public --provenance=false)
echo ""

# Build and publish linux-arm64
echo "=== 4/4: @crumblecracker/cc-linux-arm64 ==="
mkdir -p "$SCRIPT_DIR/linux-arm64/bin"
GOOS=linux GOARCH=arm64 go build -o "$SCRIPT_DIR/linux-arm64/bin/cc-helper" "$ROOT/cmd/cc-helper"
echo "Built linux-arm64. Publishing..."
read -p "Press enter when ready to publish linux-arm64..."
(cd "$SCRIPT_DIR/linux-arm64" && npm publish --access public --provenance=false)
echo ""

echo "=== All packages published! ==="
echo ""
echo "Next steps:"
echo "1. Set up OIDC trusted publishing for each package on npmjs.com"
echo "2. Clean up built binaries: rm -rf $SCRIPT_DIR/*/bin"
