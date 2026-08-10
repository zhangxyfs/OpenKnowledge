#!/usr/bin/env bash
# 同步 README 版本徽标：从 installer/openknowledge.iss（版本号单一事实源）提取
# AppVersion，重写 README.md / README_EN.md 的 version 静态徽标。幂等，可重复执行。
# 时机：版本 bump（改 iss + 写 changelog）之后、提交之前跑一次。
# 注：仓库私有，shields.io 动态徽标无法匿名读取 Gitea，故用本脚本半自动同步。
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=$(sed -n 's/^#define AppVersion "\(.*\)"/\1/p' installer/openknowledge.iss)
: "${VERSION:?无法从 installer/openknowledge.iss 提取 AppVersion}"

changed=0
for f in README.md README_EN.md; do
  before=$(sed -n 's/.*badge\/version-\([0-9.]*\)-.*/\1/p' "$f" | head -1)
  sed -i "s|\(badge/version-\)[0-9.]*\(-[0-9a-fA-F]\{6\}\)|\1${VERSION}\2|" "$f"
  if [ "$before" != "$VERSION" ]; then
    echo "$f: version 徽标 $before → $VERSION"
    changed=1
  fi
done
[ "$changed" = 0 ] && echo "README 徽标已是 $VERSION，无需变更"
