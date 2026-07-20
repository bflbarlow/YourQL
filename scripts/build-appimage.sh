#!/bin/bash
set -e

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT="$PROJECT_DIR/build/bin"

echo "=== Step 1: Generate Wails bindings ==="
cd "$PROJECT_DIR"
export PATH="$HOME/go/bin:$PATH"
wails generate module

echo ""
echo "=== Step 2: Build frontend ==="
cd "$PROJECT_DIR/frontend"
npm run build

echo ""
echo "=== Step 3: Build Docker image ==="
cd "$PROJECT_DIR"
docker build --platform linux/amd64 -t yourql-appimage -f Dockerfile.appimage . 2>&1 | tail -20

echo ""
echo "=== Step 4: Extract AppImage ==="
CID=$(docker create yourql-appimage)
docker cp "$CID:/YourQL-x86_64.AppImage" "$OUTPUT/YourQL-x86_64.AppImage"
docker rm "$CID" >/dev/null 2>&1

echo ""
ls -lh "$OUTPUT/YourQL-x86_64.AppImage"
echo ""
echo "Done! Run on Linux: chmod +x YourQL-x86_64.AppImage && ./YourQL-x86_64.AppImage"