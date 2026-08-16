#!/usr/bin/env python3
"""OpenKnowledge 双仓 release 发布（Gitea + GitHub）。

版本号从 installer/openknowledge.iss（单一事实源）提取；正文默认取
docs/changelogs/<版本>.md（可用 --body 覆盖）。正文规范（"安装器与发布" wiki）：
标题=纯版本号、正文首行不要 H1、不提当前版本号、固定四节（新功能/改进/修复/说明）——
changelog 文件的行文若不符，用 --body 传一份发布口径的正文。
产物取 installer/output/ 下三件套（exe + tar.gz + deb，需先用 build.py /
build-linux.sh 构建）。凭据经 `git credential fill` 取自 Windows 凭据管理器。
幂等：release 已存在则复用续传产物。

用法:
  python scripts/publish-release.py            # 发布 iss 里的版本
  python scripts/publish-release.py --dry-run  # 只打印将要做的事，不调 API
"""
import argparse
import json
import re
import subprocess
import sys
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def app_version():
    text = (ROOT / "installer" / "openknowledge.iss").read_text(encoding="utf-8")
    m = re.search(r'^#define AppVersion "([^"]+)"', text, re.MULTILINE)
    if not m:
        sys.exit("未能从 installer/openknowledge.iss 提取 AppVersion")
    return m.group(1)


def cred(host):
    p = subprocess.run(["git", "credential", "fill"], input=f"protocol=https\nhost={host}\n\n",
                       capture_output=True, text=True, check=True)
    return dict(l.split("=", 1) for l in p.stdout.strip().splitlines())["password"]


def req(url, token, method="GET", data=None, headers=None, raw=False):
    h = {"Authorization": f"token {token}"}
    if headers:
        h.update(headers)
    r = urllib.request.Request(url, data=data, method=method, headers=h)
    with urllib.request.urlopen(r) as resp:
        payload = resp.read()
        return resp.status, (payload if raw else json.loads(payload))


def publish(host, api_base, upload_kind, tag, body, assets, dry_run):
    repo = "zhangxyfs/OpenKnowledge"
    if dry_run:
        print(f"[dry-run] {host}: 建/复用 release {tag}，传 {[a.name for a in assets]}")
        return
    token = cred(host)
    status, rel = req(f"{api_base}/repos/{repo}/releases", token, "POST",
                      data=json.dumps({"tag_name": tag, "name": tag, "body": body}).encode(),
                      headers={"Content-Type": "application/json"})
    if status == 422:  # 已存在 → 查出来复用（续传产物）
        _, rel = req(f"{api_base}/repos/{repo}/releases/tags/{tag}", token)
        print(f"{host}: release 已存在 id={rel['id']}，复用")
    else:
        print(f"{host}: release 创建 id={rel['id']} ({status})")
    if upload_kind == "gitea":
        upload_url = f"{api_base}/repos/{repo}/releases/{rel['id']}/assets"
    else:
        upload_url = rel["upload_url"].split("{")[0]
    for a in assets:
        status, _ = req(f"{upload_url}?name={a.name}", token, "POST", data=a.read_bytes(),
                        headers={"Content-Type": "application/octet-stream"}, raw=True)
        print(f"{host}: {a.name} → {status}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--body", help="release 正文 md 文件（默认 docs/changelogs/<版本>.md）")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    version = app_version()
    tag = f"v{version}"
    body_path = Path(args.body) if args.body else ROOT / "docs" / "changelogs" / f"{version}.md"
    if not body_path.exists():
        sys.exit(f"缺 release 正文: {body_path}（可用 --body 指定）")
    body = body_path.read_text(encoding="utf-8")
    out = ROOT / "installer" / "output"
    assets = [
        out / f"OpenKnowledgeSetup-{version}.exe",
        out / f"openknowledge_{version}_linux_amd64.tar.gz",
        out / f"openknowledge_{version}_amd64.deb",
    ]
    for a in assets:
        if not a.exists():
            sys.exit(f"缺产物: {a}（先跑 build.py / build-linux.sh）")

    publish("z7dream-gitea.iepose.cn", "https://z7dream-gitea.iepose.cn/api/v1", "gitea",
            tag, body, assets, args.dry_run)
    publish("github.com", "https://api.github.com", "github", tag, body, assets, args.dry_run)
    print("done")


if __name__ == "__main__":
    main()
