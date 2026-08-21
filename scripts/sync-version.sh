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

# exe 版本资源（winres.json）：bump 时易漏（v2.9.0 起曾停在 2.8.0.0 直至 2.16.0 发现），
# 四段式 = 三段版本号 + ".0"
VERSION4="${VERSION}.0"
for f in cmd/ok/winres.json cmd/okd/winres.json cmd/okmanager/winres.json; do
  if grep -q "\"file_version\": \"${VERSION4}\"" "$f" && grep -q "\"product_version\": \"${VERSION4}\"" "$f"; then
    echo "$f: 已是 ${VERSION4}，无需变更"
  else
    # 分隔符必须避开分组 alternation 的 "|"（曾因此报 unknown option to `s' 且更新被跳过）
    sed -i -E "s~\"(file_version|product_version)\": \"[0-9.]+\"~\"\1\": \"${VERSION4}\"~g" "$f"
    echo "$f: 版本资源 → ${VERSION4}"
  fi
done

# 官网：VER 变量、Release 直链、安装/下载文案里的版本号
# 覆盖 site/index.html（VER + 直链 + version 徽标）、site/changelog.html（下载按钮文案）、
# site/assets/site.js（英文字典里的直链与文案）
for f in site/index.html site/changelog.html site/assets/site.js; do
  [ -f "$f" ] || continue
  before_md=$(md5sum "$f" | cut -d' ' -f1)
  sed -i \
    -e "s|var VER = 'v[0-9.]*'|var VER = 'v${VERSION}'|" \
    -e "s|releases/download/v[0-9.]*/|releases/download/v${VERSION}/|g" \
    -e "s|OpenKnowledgeSetup-[0-9.]*\.exe|OpenKnowledgeSetup-${VERSION}.exe|g" \
    -e "s|openknowledge_[0-9.]*_amd64\.deb|openknowledge_${VERSION}_amd64.deb|g" \
    -e "s|openknowledge_[0-9.]*_linux_amd64\.tar\.gz|openknowledge_${VERSION}_linux_amd64.tar.gz|g" \
    -e "s|下载最新版 v[0-9.]*|下载最新版 v${VERSION}|g" \
    -e "s|Download latest v[0-9.]*|Download latest v${VERSION}|g" \
    -e "s|\(badge/version-\)[0-9.]*\(-[0-9a-fA-F]\{6\}\)|\1${VERSION}\2|g" \
    "$f"
  after_md=$(md5sum "$f" | cut -d' ' -f1)
  if [ "$before_md" != "$after_md" ]; then
    echo "$f: 版本引用已更新 → $VERSION"
  else
    echo "$f: 已是 $VERSION，无需变更"
  fi
done
