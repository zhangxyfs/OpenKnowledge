#!/usr/bin/env bash
# 构建安装程序：先构建 dist/，再调用 Inno Setup 编译安装包。
# 输出：installer/output/OpenKnowledgeSetup-<version>.exe
set -euo pipefail
cd "$(dirname "$0")/.."

ISCC="${ISCC:-C:/Users/Administrator/AppData/Local/Programs/Inno Setup 7/ISCC.exe}"

bash scripts/build-dist.sh

if [ ! -x "$ISCC" ] && [ ! -f "$ISCC" ]; then
  echo "未找到 ISCC: $ISCC（可用 ISCC=/path/to/ISCC.exe 覆盖）" >&2
  exit 1
fi

mkdir -p installer/output
"$ISCC" //Q installer/openknowledge.iss
ls -lh installer/output/
