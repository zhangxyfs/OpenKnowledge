#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION=$(sed -n 's/^#define AppVersion "\(.*\)"/\1/p' installer/openknowledge.iss)
: "${VERSION:?无法从 installer/openknowledge.iss 提取 AppVersion}"
go build -ldflags "-s -w -H windowsgui -X openknowledge/internal/version.Version=$VERSION" -o dist/ok.exe ./cmd/ok
rm -rf dist/web dist/changelogs
cp -r web dist/web
cp -r docs/changelogs dist/changelogs
echo "dist/ built: ok.exe + web/ + changelogs/"
