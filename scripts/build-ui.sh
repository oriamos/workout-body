#!/bin/sh
# Builds the web UI (web/) and copies it into internal/uisrv/dist so that the
# next `go build ./cmd/training-mcp` embeds it. Run from anywhere.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root/web"
if [ ! -d node_modules ]; then npm ci; fi
npm run build
dest="$root/internal/uisrv/dist"
find "$dest" -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
cp -R "$root/web/dist/." "$dest/"
echo "web UI copied to internal/uisrv/dist; now run: go build ./cmd/training-mcp"
