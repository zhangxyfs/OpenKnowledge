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
# Windows 宿主落盘为 0644，须钉权限位；Git Bash 挂载 noacl 时本行是静默 no-op，
# 此时由下方 tar --mode 与 nfpm file_info 兜底（Linux/macOS 宿主本行生效）
chmod 0755 "$STAGE/ok"
cp -r web "$STAGE/web"
cp -r docs/changelogs "$STAGE/changelogs"

# llama.cpp 运行时（llama-server，ubuntu x64 CPU 版）——内置 embedding sidecar
LLAMA_TAG=b10405
LLAMA_BASE=${LLAMA_CPP_BASE_URL:-https://github.com/ggml-org/llama.cpp/releases/download}
mkdir -p "$STAGE/runtime"
if [ ! -f "$STAGE/runtime/llama-server" ]; then
  curl -fSL -o dist/llama-linux.tar.gz "$LLAMA_BASE/$LLAMA_TAG/llama-$LLAMA_TAG-bin-ubuntu-x64.tar.gz"
  # 资产内含 llama-b10405/ 顶层目录（--strip-components=1 剥掉）；且含 .so 符号链接，
  # Windows Git Bash 无建链权限时 tar 回退为实体拷贝、目标未解出时报错——解两遍兜底
  #（Linux 宿主第一遍即成功，第二遍幂等覆盖无害；资产损坏时第二遍仍会失败被 set -e 拦住）
  tar -xzf dist/llama-linux.tar.gz -C "$STAGE/runtime" --strip-components=1 || true
  tar -xzf dist/llama-linux.tar.gz -C "$STAGE/runtime" --strip-components=1
  rm -f dist/llama-linux.tar.gz
  chmod 0755 "$STAGE/runtime/llama-server" 2>/dev/null || true
fi

# tar.gz：单层目录，解压后 ./ok setup 即用
PKG=openknowledge_${VERSION}_linux_amd64
mkdir -p installer/output
rm -rf "dist/$PKG"
mkdir -p "dist/$PKG"
cp -r "$STAGE/ok" "$STAGE/web" "$STAGE/changelogs" "$STAGE/runtime" "dist/$PKG/"
# Git Bash 挂载 noacl：chmod 不改 ACL，stat 只认 .exe/MZ/#! 不识 ELF，
# 文件权限位进不了 tar——打包时强制 ok 条目 mode=0755（双保险之一，另一处是 nfpm file_info）
TAR="installer/output/$PKG.tar"
tar -cf "$TAR" -C dist --exclude="$PKG/ok" --exclude="$PKG/runtime/llama-server" "$PKG"
tar -rf "$TAR" -C dist --mode=0755 "$PKG/ok" "$PKG/runtime/llama-server"
gzip -f "$TAR"
rm -rf "dist/$PKG"
echo "tar.gz built: installer/output/$PKG.tar.gz"

# .deb：nfpm 不在 PATH 时整体失败（tar.gz 产物保留）——本版本目标含 deb，缺工具即未达标
if ! command -v nfpm >/dev/null 2>&1; then
  echo "未找到 nfpm。安装：go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest" >&2
  exit 1
fi
VERSION="$VERSION" nfpm package --packager deb --config installer/nfpm.yaml --target installer/output/
echo "deb built into installer/output/"
