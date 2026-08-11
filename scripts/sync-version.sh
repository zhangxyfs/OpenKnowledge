#!/usr/bin/env bash
# 同步版本号：从 installer/openknowledge.iss（版本号单一事实源）提取 AppVersion，
# 重写 README.md / README_EN.md 的 version 静态徽标，以及 site/index.html（官网单页）
# 里的版本号变量与 Release 直链。幂等，可重复执行。
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

# 官网单页：VER 变量、Release 直链、安装卡片里的示例文件名
f=site/index.html
if [ -f "$f" ]; then
  before=$(sed -n "s/.*var VER = 'v\([0-9.]*\)'.*/\1/p" "$f" | head -1)
  sed -i \
    -e "s|var VER = 'v[0-9.]*'|var VER = 'v${VERSION}'|" \
    -e "s|releases/download/v[0-9.]*/|releases/download/v${VERSION}/|g" \
    -e "s|OpenKnowledgeSetup-[0-9.]*\.exe|OpenKnowledgeSetup-${VERSION}.exe|g" \
    -e "s|openknowledge_[0-9.]*_amd64\.deb|openknowledge_${VERSION}_amd64.deb|g" \
    -e "s|openknowledge_[0-9.]*_linux_amd64\.tar\.gz|openknowledge_${VERSION}_linux_amd64.tar.gz|g" \
    "$f"
  if [ "$before" != "$VERSION" ]; then
    echo "$f: 官网版本 $before → $VERSION"
  else
    echo "$f: 已是 $VERSION，无需变更"
  fi
fi
