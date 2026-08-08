#!/usr/bin/env python3
"""OpenKnowledge 一键构建：编译 dist/（含 exe 图标嵌入）+ 打包安装程序。

用法:
  python scripts/build.py                  # 完整流程（dist + 安装程序）
  python scripts/build.py --skip-installer # 只构建 dist/
  python scripts/build.py --skip-winres    # 跳过 exe 图标/版本信息嵌入

环境变量 ISCC 可覆盖 Inno Setup 编译器路径。
"""
import argparse
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ISCC = os.environ.get("ISCC", r"C:\Users\Administrator\AppData\Local\Programs\Inno Setup 7\ISCC.exe")
LDFLAGS = "-s -w -H windowsgui"


def app_version():
    """从 installer/openknowledge.iss 提取 #define AppVersion，提取不到则报错退出。"""
    text = (ROOT / "installer" / "openknowledge.iss").read_text(encoding="utf-8")
    m = re.search(r'^#define AppVersion "([^"]+)"', text, re.MULTILINE)
    if not m:
        sys.exit("未能从 installer/openknowledge.iss 提取 AppVersion")
    return m.group(1)


def run(cmd, cwd=ROOT):
    print("+", " ".join(str(c) for c in cmd))
    subprocess.run([str(c) for c in cmd], check=True, cwd=cwd)


def main():
    ap = argparse.ArgumentParser(description="OpenKnowledge 一键构建")
    ap.add_argument("--skip-installer", action="store_true", help="只构建 dist/，不打包安装程序")
    ap.add_argument("--skip-winres", action="store_true", help="跳过 exe 图标/版本信息嵌入")
    args = ap.parse_args()

    # 1. exe 图标与版本信息（go-winres，缺失时跳过不报错）
    if not args.skip_winres:
        winres = shutil.which("go-winres") or str(Path(os.environ.get("GOPATH", Path.home() / "go")) / "bin" / "go-winres.exe")
        if Path(winres).exists() if not shutil.which("go-winres") else True:
            run([winres, "make", "--in", "winres.json"], cwd=ROOT / "cmd" / "ok")
        else:
            print("go-winres 未安装，跳过 exe 图标嵌入")
            print("  安装: go install github.com/tc-hib/go-winres@latest")

    # 2. 编译 dist/ok.exe + 拷贝 web/（注入版本号，与 build-dist.sh 一致）
    ldflags = f"{LDFLAGS} -X openknowledge/internal/version.Version={app_version()}"
    (ROOT / "dist").mkdir(exist_ok=True)
    run(["go", "build", "-ldflags", ldflags, "-o", "dist/ok.exe", "./cmd/ok"])
    web_dist = ROOT / "dist" / "web"
    if web_dist.exists():
        shutil.rmtree(web_dist)
    shutil.copytree(ROOT / "web", web_dist)
    # changelogs 随包分发（GUI 更新日志弹窗的数据源；iss 打的是 dist\changelogs）——
    # 与 build-dist.sh 保持一致，漏拷会导致安装包内更新日志陈旧
    cl_dist = ROOT / "dist" / "changelogs"
    if cl_dist.exists():
        shutil.rmtree(cl_dist)
    shutil.copytree(ROOT / "docs" / "changelogs", cl_dist)
    print("dist/ built: ok.exe + web/ + changelogs/")

    # 3. Inno Setup 打包
    if not args.skip_installer:
        if not Path(ISCC).exists():
            sys.exit(f"未找到 ISCC: {ISCC}（可用环境变量 ISCC 覆盖，或 --skip-installer）")
        (ROOT / "installer" / "output").mkdir(parents=True, exist_ok=True)
        run([ISCC, "/Q", "installer/openknowledge.iss"])
        for f in sorted((ROOT / "installer" / "output").glob("*.exe")):
            print(f"安装程序: {f}  ({f.stat().st_size / 1024 / 1024:.1f} MB)")


if __name__ == "__main__":
    main()
