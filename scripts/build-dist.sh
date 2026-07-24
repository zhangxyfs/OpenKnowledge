#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go build -ldflags "-s -w -H windowsgui" -o dist/ok.exe ./cmd/ok
rm -rf dist/web
cp -r web dist/web
echo "dist/ built: ok.exe + web/"
