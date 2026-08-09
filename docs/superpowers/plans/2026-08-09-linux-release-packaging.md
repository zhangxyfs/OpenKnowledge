# Linux 发布包（tar.gz + .deb）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Windows 上一条命令（`bash scripts/build-linux.sh`）产出 Linux amd64 的 tar.gz 与 .deb，并补平 Linux 桌面运行缺口（xdg-open 浏览器打开 + XDG 登录自启）。

**Architecture:** `OpenBrowser` 按编译标签拆 windows/unix 两文件；autostart 拆「纯内容函数（全平台可测）+ 平台壳（编译标签）」；构建脚本复用 iss 版本提取，tar.gz 直出、.deb 经 nfpm。

**Tech Stack:** Go 1.25（编译标签、交叉编译）、bash（git-bash）、nfpm v2、python3（一次性 .deb 验证）。

## Global Constraints

- Spec：`docs/superpowers/specs/2026-08-09-linux-release-packaging-design.md`（2026-08-09 已批准）
- 架构范围：**仅 linux/amd64**；不碰 macOS/arm64/rpm
- Windows 行为**零变化**：`browser_windows.go` 从 `server.go` 原样搬移，逐字节不动逻辑
- 版本事实源只有 `installer/openknowledge.iss` 的 `#define AppVersion`，构建脚本用 sed 提取
- 打包产物统一出口 `installer/output/`；.deb 安装布局 `/usr/lib/openknowledge/{ok,web/,changelogs/}` + 软链 `/usr/bin/ok`
- 自启只走 XDG（`~/.config/autostart/openknowledge.desktop`），.deb 不带 postinst
- 提交信息风格：`feat: ...` / `build: ...` / `docs: ...`，中文描述
- 直接在 master 分支提交（项目既定工作流）

---

### Task 1: OpenBrowser 平台拆分

**Files:**
- Create: `internal/gui/browser_windows.go`
- Create: `internal/gui/browser_unix.go`
- Modify: `internal/gui/server.go`（移出 OpenBrowser，保留包注释）

**Interfaces:**
- Consumes: 现有 `internal/gui/server.go` 的 `OpenBrowser(url string) uintptr`（第 14~31 行）、`internal/procx.HideWindow`、`maximizeWindowByTitle`
- Produces: 签名不变的 `OpenBrowser(url string) uintptr`——windows 版行为逐字节保留；unix 版供 Task 3 构建出的 Linux 二进制使用

- [ ] **Step 1: 新建 browser_windows.go（原样搬移）**

```go
//go:build windows

package gui

import (
	"fmt"
	"os/exec"
	"time"

	"openknowledge/internal/procx"
)

// OpenBrowser 以最大化窗口打开 Edge/Chrome 应用模式，返回新窗口句柄（未找到返回 0）；
// 失败退回默认浏览器（不保证最大化）。调用方可用返回的 hwnd 做后续聚焦复用。
// 注：maximizeWindowByTitle 必须同步调用：ok gui 开浏览器即退（daemon 托管生命周期），
// 协程会随进程退出而被杀——v2.1 的"不自动最大化"回归正源于此。
func OpenBrowser(url string) uintptr {
	for _, browser := range []string{"msedge", "chrome"} {
		ps := fmt.Sprintf("Start-Process %s -ArgumentList '--app=%s' -WindowStyle Maximized", browser, url)
		cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
		procx.HideWindow(cmd)
		if err := cmd.Run(); err == nil {
			return maximizeWindowByTitle("OpenKnowledge", 10*time.Second)
		}
	}
	fallback := exec.Command("cmd", "/c", "start", url)
	procx.HideWindow(fallback)
	_ = fallback.Run()
	return maximizeWindowByTitle("OpenKnowledge", 10*time.Second)
}
```

- [ ] **Step 2: 新建 browser_unix.go**

```go
//go:build !windows

package gui

import (
	"fmt"
	"os/exec"
)

// OpenBrowser 非 Windows 平台经 xdg-open 打开默认浏览器；失败仅打印 URL
// （沿用 Windows 版"失败退默认浏览器/只打印 URL"的兜底语义）。
// 无窗口句柄概念，恒返回 0（与 window_other.go 的 IsWindow=false 配套）。
func OpenBrowser(url string) uintptr {
	if err := exec.Command("xdg-open", url).Start(); err != nil {
		fmt.Println("请在浏览器打开:", url)
	}
	return 0
}
```

- [ ] **Step 3: server.go 移出 OpenBrowser**

删除 `server.go` 中的 `OpenBrowser` 函数（第 14~31 行）及随之内省不再使用的 import（`fmt`/`os/exec`/`time`/`openknowledge/internal/procx`），保留包级注释（第 1~4 行）不动。改完 `server.go` 应只剩包注释 + `package gui`。

- [ ] **Step 4: 三平台编译矩阵 + 既有测试**

Run:
```bash
go build ./... && go test ./internal/gui/ && \
GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/ok && \
GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/ok && echo MATRIX-OK
```
Expected: 全过（Windows 行为零变化由既有 gui 测试兜底）

- [ ] **Step 5: 提交**

```bash
git add internal/gui/browser_windows.go internal/gui/browser_unix.go internal/gui/server.go
git commit -m "feat(gui): OpenBrowser 按平台拆分——unix 走 xdg-open"
```

---

### Task 2: XDG 登录自启（setup 写入 / uninstall 移除）

**Files:**
- Create: `internal/setupx/autostart.go`（纯函数，无编译标签）
- Create: `internal/setupx/autostart_windows.go`（`//go:build windows`，no-op）
- Create: `internal/setupx/autostart_unix.go`（`//go:build !windows`，实现）
- Modify: `internal/cli/setup.go`（`InstallSkills` 之后挂写入）
- Modify: `internal/setupx/uninstall.go`（返回前加移除步骤）
- Test: `internal/setupx/autostart_test.go`（无标签，测纯函数）
- Test: `internal/setupx/autostart_windows_test.go`（`//go:build windows`，测 no-op）
- Test: `internal/setupx/autostart_unix_test.go`（`//go:build !windows`，测真实写删——Windows 上不编译，仅备 unix 环境用）

**Interfaces:**
- Consumes: `cli.Setup` 里现成的 `exe`（`resolveExe()`，已 EvalSymlinks）；`setupx.Uninstall` 现有步骤结构
- Produces: `AutostartDesktop(exe string) string`、`WriteAutostart(exe string) error`、`RemoveAutostart() error`

- [ ] **Step 1: 写失败测试（纯函数 + windows no-op）**

`internal/setupx/autostart_test.go`（若无包声明则 `package setupx`，沿用现有测试文件惯例）：

```go
func TestAutostartDesktop(t *testing.T) {
	content := AutostartDesktop("/usr/lib/openknowledge/ok")
	for _, want := range []string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=OpenKnowledge",
		"Exec=/usr/lib/openknowledge/ok daemon",
		"X-GNOME-Autostart-enabled=true",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("desktop 文件缺少 %q:\n%s", want, content)
		}
	}
}
```

`internal/setupx/autostart_windows_test.go`：

```go
//go:build windows

package setupx

import "testing"

func TestAutostartWindowsNoop(t *testing.T) {
	if err := WriteAutostart("x"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAutostart(); err != nil {
		t.Fatal(err)
	}
}
```

`internal/setupx/autostart_unix_test.go`：

```go
//go:build !windows

package setupx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAndRemoveAutostart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := WriteAutostart("/usr/lib/openknowledge/ok"); err != nil {
		t.Fatal(err)
	}
	p := autostartPath()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Exec=/usr/lib/openknowledge/ok daemon") {
		t.Fatalf("unexpected content:\n%s", data)
	}
	// 幂等覆盖
	if err := WriteAutostart("/opt/ok"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(p)
	if !strings.Contains(string(data), "Exec=/opt/ok daemon") {
		t.Fatalf("overwrite failed:\n%s", data)
	}
	if err := RemoveAutostart(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("file should be gone, err=%v", err)
	}
	// 重复移除不报错
	if err := RemoveAutostart(); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != filepath.Join(os.Getenv("HOME"), ".config", "autostart") {
		t.Fatalf("unexpected path: %s", p)
	}
}
```

注意：`autostart_test.go` 若需 `strings` import 自行补齐；先读 `setupx_test.go` 确认包名惯例。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/setupx/ -run Autostart -v`
Expected: FAIL —— `AutostartDesktop undefined`（编译错误）

- [ ] **Step 3: 实现三个文件**

`internal/setupx/autostart.go`：

```go
package setupx

import "fmt"

// AutostartDesktop 生成 XDG autostart desktop 文件内容（平台无关纯函数，便于测试）。
func AutostartDesktop(exe string) string {
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=OpenKnowledge
Exec=%s daemon
X-GNOME-Autostart-enabled=true
`, exe)
}
```

`internal/setupx/autostart_windows.go`：

```go
//go:build windows

package setupx

// WriteAutostart Windows 平台自启由安装器写注册表（HKCU Run），此处 no-op。
func WriteAutostart(_ string) error { return nil }

// RemoveAutostart Windows 平台 no-op（注册表项随安装器卸载清除）。
func RemoveAutostart() error { return nil }
```

`internal/setupx/autostart_unix.go`：

```go
//go:build !windows

package setupx

import (
	"os"
	"path/filepath"
)

// autostartPath 返回 XDG 自启文件路径（~/.config/autostart/openknowledge.desktop）。
func autostartPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "autostart", "openknowledge.desktop")
}

// WriteAutostart 写入 XDG 登录自启项（幂等覆盖）。
func WriteAutostart(exe string) error {
	p := autostartPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(AutostartDesktop(exe)), 0o644)
}

// RemoveAutostart 移除登录自启项；不存在不视为错误。
func RemoveAutostart() error {
	if err := os.Remove(autostartPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
```

- [ ] **Step 4: 接线 setup 与 uninstall**

`internal/cli/setup.go`：`setupx.InstallSkills(exe)` 成功之后、`fs.Visit` 之前插入：

```go
	if err := setupx.WriteAutostart(exe); err != nil {
		fmt.Fprintf(stderr, "警告：登录自启写入失败: %v\n", err)
	}
```

（Windows 上是 nil no-op，不产生任何输出。）

`internal/setupx/uninstall.go`：在 `Uninstall` 的 return 之前追加（读现有代码找 embedding 移除之后的收尾位置）：

```go
	// 4. 移除登录自启项（XDG；Windows 注册表项由安装器卸载清除，此处 no-op）。
	// 错误容忍：与 daemon 停止同级，不进 UninstallResult。
	_ = RemoveAutostart()
```

- [ ] **Step 5: 跑测试确认通过 + 全量**

Run: `go test ./internal/setupx/ -v -run Autostart && go test ./...`
Expected: Autostart 用例 PASS（windows 上跑纯函数 + no-op 两个）；全仓不回归

- [ ] **Step 6: 提交**

```bash
git add internal/setupx/autostart*.go internal/cli/setup.go internal/setupx/uninstall.go
git commit -m "feat(setup): Linux 登录自启——ok setup 写 XDG autostart，uninstall 移除"
```

---

### Task 3: build-linux.sh + tar.gz

**Files:**
- Create: `scripts/build-linux.sh`

**Interfaces:**
- Consumes: iss 版本行；`./cmd/ok`；`web/`；`docs/changelogs/`；Task 1/2 的产物已合入源码
- Produces: `installer/output/openknowledge_<版本>_linux_amd64.tar.gz`；暂存布局 `dist/linux-amd64/{ok,web/,changelogs/}`（Task 4 的 nfpm.yaml 以此为 src）

- [ ] **Step 1: 写脚本**

```bash
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
VERSION="$VERSION" nfpm package --config installer/nfpm.yaml --target installer/output/
echo "deb built into installer/output/"
```

- [ ] **Step 2: 装 nfpm（Task 4 前置，此处只为脚本可跑通前半段）**

Run: `go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest && nfpm --version`
Expected: 打印版本号；装不上则记录原因继续（Task 3 只验 tar.gz，nfpm 缺失时脚本 exit 1 属预期）

- [ ] **Step 3: 跑脚本验 tar.gz**

Run: `bash scripts/build-linux.sh || true`（nfpm.yaml 尚未存在时允许后半段失败）
再验：
```bash
tar -tzf installer/output/openknowledge_*_linux_amd64.tar.gz | head -8
```
Expected: 清单含 `openknowledge_<版本>_linux_amd64/ok`、`.../web/index.html`、`.../changelogs/2.9.0.md` 等

- [ ] **Step 4: 提交**

```bash
git add scripts/build-linux.sh
git commit -m "build: scripts/build-linux.sh——linux/amd64 交叉编译 + tar.gz 打包"
```

---

### Task 4: nfpm.yaml + .deb + 静态验证

**Files:**
- Create: `installer/nfpm.yaml`
- Create: `scripts/verify-deb.py`
- Modify: 无（build-linux.sh 的 deb 段已在 Task 3 写好）

**Interfaces:**
- Consumes: Task 3 的暂存布局 `dist/linux-amd64/`、`installer/output/` 出口、`VERSION` 环境变量（nfpm 内置 `${VERSION}` 展开）
- Produces: `installer/output/openknowledge_<版本>_amd64.deb`；`python scripts/verify-deb.py <deb路径> <版本>` 零退出即结构合规

- [ ] **Step 1: 写 installer/nfpm.yaml**

```yaml
name: openknowledge
arch: amd64
platform: linux
version: ${VERSION}
maintainer: OpenKnowledge
description: |
  为 AI 编程助手提供的项目知识库：hooks 自动注入 + 混合检索 + 强制工作流规则。
  安装后运行 ok setup 完成首次引导（hooks / 技能 / embedding / 登录自启）。
homepage: https://z7dream-gitea.iepose.cn/zhangxyfs/OpenKnowledge
recommends:
  - xdg-utils
contents:
  - src: dist/linux-amd64/ok
    dst: /usr/lib/openknowledge/ok
  - src: dist/linux-amd64/web/
    dst: /usr/lib/openknowledge/web/
  - src: dist/linux-amd64/changelogs/
    dst: /usr/lib/openknowledge/changelogs/
  - src: /usr/lib/openknowledge/ok
    dst: /usr/bin/ok
    type: symlink
```

注意：nfpm v2 的软链写法以 `nfpm` 文档为准——若上面 `type: symlink` 报错，查阅 `nfpm` 帮助/文档改为当时支持的形式（如 `type: symlink` 需 `src` 为链接目标），并把最终写法写进报告。

- [ ] **Step 2: 写 scripts/verify-deb.py（ar 归档静态解析）**

```python
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
    control = ctl.read("./control").decode() if "./control" in ctl.getnames() else ctl.read("control").decode()
    if f"Version: {expect_ver}" not in control:
        sys.exit(f"control 版本不符，期望 {expect_ver}:\n{control}")

    data = tarfile.open(fileobj=io.BytesIO(m["data.tar.gz"]), mode="r:gz")
    names = {n.lstrip("./") for n in data.getnames()}
    for need in ("usr/lib/openknowledge/ok",
                 "usr/lib/openknowledge/web/index.html",
                 "usr/bin/ok"):
        if need not in names:
            sys.exit(f"data.tar.gz 缺少 {need}（共 {len(names)} 项）")
    if not any(n.startswith("usr/lib/openknowledge/changelogs/") for n in names):
        sys.exit("data.tar.gz 缺少 changelogs 内容")
    link = data.getmember("./usr/bin/ok" if "./usr/bin/ok" in data.getnames() else "usr/bin/ok")
    if not link.issym():
        sys.exit("usr/bin/ok 不是符号链接")
    print(f"deb OK: 版本 {expect_ver}，{len(names)} 个文件项，usr/bin/ok -> {link.linkname}")


if __name__ == "__main__":
    main()
```

- [ ] **Step 3: 出包并验证**

Run:
```bash
bash scripts/build-linux.sh
DEB=$(ls installer/output/openknowledge_*_amd64.deb | head -1)
VER=$(sed -n 's/^#define AppVersion "\(.*\)"/\1/p' installer/openknowledge.iss)
python scripts/verify-deb.py "$DEB" "$VER"
```
Expected: 脚本全绿，verify 输出 `deb OK: ...`；若 nfpm.yaml 字段报错，按 Step 1 注意项修正后重跑

- [ ] **Step 4: 提交**

```bash
git add installer/nfpm.yaml scripts/verify-deb.py
git commit -m "build: nfpm 打 .deb（/usr/lib 布局 + /usr/bin/ok 软链）+ ar 结构静态验证"
```

---

### Task 5: README 方式 C + 全量回归

**Files:**
- Modify: `README.md`（安装节加方式 C）
- Modify: `README_EN.md`（同步英文）

**Interfaces:**
- Consumes: Task 1-4 全部产物
- Produces: 面向用户的 Linux 安装说明

- [ ] **Step 1: README.md 安装节追加**

在「方式 B：手动构建」之前插入：

```markdown
**方式 C：Linux（amd64）**

发布产物提供两种格式（均无依赖、静态编译）：

- `openknowledge_<版本>_linux_amd64.tar.gz`：解压后 `cd openknowledge_* && ./ok setup`（写 hooks/技能 + 配置登录自启）
- `openknowledge_<版本>_amd64.deb`：`sudo dpkg -i` 安装到 `/usr/lib/openknowledge/`（`ok` 进 PATH），然后运行 `ok setup`

> 构建命令（维护者）：`bash scripts/build-linux.sh`（.deb 需先 `go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest`）。
```

- [ ] **Step 2: README_EN.md 同步**

在 "Option B: Manual build" 之前插入：

```markdown
**Option C: Linux (amd64)**

Two formats (both dependency-free, statically compiled):

- `openknowledge_<version>_linux_amd64.tar.gz`: extract, then `cd openknowledge_* && ./ok setup` (hooks/skills + login autostart)
- `openknowledge_<version>_amd64.deb`: `sudo dpkg -i` installs to `/usr/lib/openknowledge/` (`ok` lands in PATH), then run `ok setup`

> Build (maintainers): `bash scripts/build-linux.sh` (.deb requires `go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest` first).
```

- [ ] **Step 3: 全量回归 + 编译矩阵**

Run: `go build ./... && go vet ./... && go test ./... && GOOS=linux go build -o /dev/null ./cmd/ok && GOOS=darwin go build -o /dev/null ./cmd/ok && echo ALL-OK`
Expected: 全过

- [ ] **Step 4: 提交**

```bash
git add README.md README_EN.md
git commit -m "docs(readme): 安装方式 C——Linux tar.gz/.deb 使用说明（中英）"
```

- [ ] **Step 5: 交付说明（写进任务报告）**

给用户的真机验收清单（无 WSL，本计划只做到静态验证）：
1. `sudo dpkg -i openknowledge_*.deb` → `which ok` 有输出、`ok doctor` 正常
2. `ok setup` → `~/.config/autostart/openknowledge.desktop` 存在；重登后 daemon 自起
3. `ok gui` → 默认浏览器打开管理页
4. tar.gz 路径同样跑一遍 2、3
