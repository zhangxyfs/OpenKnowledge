#!/usr/bin/env bash
# Linux 发布包构建：交叉编译 linux/amd64 → tar.gz +（nfpm 可用时）.deb
# 产物：installer/output/openknowledge_<版本>_linux_amd64.tar.gz、openknowledge_<版本>_amd64.deb
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=$(sed -n 's/^#define AppVersion "\(.*\)"/\1/p' installer/openknowledge.iss)
: "${VERSION:?无法从 installer/openknowledge.iss 提取 AppVersion}"

STAGE=dist/linux-amd64
rm -rf "$STAGE"
mkdir -p "$STAGE"
GOOS=linux GOARCH=amd64 go build \
  -ldflags "-s -w -X openknowledge/internal/version.Version=$VERSION" \
  -o "$STAGE/ok" ./cmd/ok
cp -r web "$STAGE/web"
cp -r docs/changelogs "$STAGE/changelogs"

# tar.gz：单层目录，解压后 ./ok setup 即用
PKG=openknowledge_${VERSION}_linux_amd64
mkdir -p installer/output
rm -rf "dist/$PKG"
mkdir -p "dist/$PKG"
cp -r "$STAGE/ok" "$STAGE/web" "$STAGE/changelogs" "dist/$PKG/"
tar -czf "installer/output/$PKG.tar.gz" -C dist "$PKG"
rm -rf "dist/$PKG"
echo "tar.gz built: installer/output/$PKG.tar.gz"

# .deb：nfpm 不在 PATH 时整体失败（tar.gz 产物保留）——本版本目标含 deb，缺工具即未达标
if ! command -v nfpm >/dev/null 2>&1; then
  echo "未找到 nfpm。安装：go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest" >&2
  exit 1
fi
VERSION="$VERSION" nfpm package --packager deb --config installer/nfpm.yaml --target installer/output/
echo "deb built into installer/output/"
