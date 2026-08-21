#!/usr/bin/env python3
"""一次性 .deb 结构静态验证（无 ar/dpkg 环境）：解析 ar 归档，核对三成员与内容清单。

用法: python scripts/verify-deb.py <deb路径> <期望版本>
零退出 = 合规；否则非零并打印原因。
"""
import io
import sys
import tarfile


def read_ar(path):
    data = open(path, "rb").read()
    if data[:8] != b"!<arch>\n":
        sys.exit("不是 ar 归档")
    members, off = {}, 8
    while off + 60 <= len(data):
        hdr = data[off:off + 60]
        name = hdr[0:16].decode().strip().rstrip("/")
        size = int(hdr[48:58].decode().strip())
        members[name] = data[off + 60:off + 60 + size]
        off += 60 + size + (size % 2)  # 成员按 2 字节对齐
    return members


def main():
    deb, expect_ver = sys.argv[1], sys.argv[2]
    m = read_ar(deb)
    for need in ("debian-binary", "control.tar.gz", "data.tar.gz"):
        if need not in m:
            sys.exit(f"缺少成员 {need}（现有：{list(m)}）")
    if m["debian-binary"].strip() != b"2.0":
        sys.exit("debian-binary 内容异常")

    ctl = tarfile.open(fileobj=io.BytesIO(m["control.tar.gz"]), mode="r:gz")
    ctl_name = "./control" if "./control" in ctl.getnames() else "control"
    control = ctl.extractfile(ctl_name).read().decode()
    if f"Version: {expect_ver}" not in control:
        sys.exit(f"control 版本不符，期望 {expect_ver}:\n{control}")

    data = tarfile.open(fileobj=io.BytesIO(m["data.tar.gz"]), mode="r:gz")
    names = {n.lstrip("./") for n in data.getnames()}
    for need in ("usr/lib/openknowledge/ok",
                 "usr/lib/openknowledge/okd",
                 "usr/lib/openknowledge/web/index.html",
                 "usr/bin/ok"):
        if need not in names:
            sys.exit(f"data.tar.gz 缺少 {need}（共 {len(names)} 项）")
    if not any(n.startswith("usr/lib/openknowledge/changelogs/") for n in names):
        sys.exit("data.tar.gz 缺少 changelogs 内容")
    link = data.getmember("./usr/bin/ok" if "./usr/bin/ok" in data.getnames() else "usr/bin/ok")
    if not link.issym():
        sys.exit("usr/bin/ok 不是符号链接")
    okm = data.getmember("./usr/lib/openknowledge/ok" if "./usr/lib/openknowledge/ok" in data.getnames() else "usr/lib/openknowledge/ok")
    if not (okm.mode & 0o111):
        sys.exit(f"usr/lib/openknowledge/ok 无可执行位: {oct(okm.mode)}")
    print(f"deb OK: 版本 {expect_ver}，{len(names)} 个文件项，usr/bin/ok -> {link.linkname}")


if __name__ == "__main__":
    main()
