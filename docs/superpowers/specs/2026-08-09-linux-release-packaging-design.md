# Linux 发布包（tar.gz + .deb）设计

日期：2026-08-09 · 状态：已批准 · 目标版本：v2.10.0（拟定）

## 背景与目标

项目目前只有 Windows 安装器一条发布线。已实测 `GOOS=linux/darwin` 交叉编译全部通过（纯 Go 零 CGO），Linux 桌面是最低成本的第二平台。目标：**Windows 上一条命令产出 Linux amd64 的 tar.gz 与 .deb**，并补平 Linux 桌面运行缺口，开箱即用。

已确认的决策：

- 范围：打包 **+ 补运行缺口**（浏览器打开、登录自启）一起做
- .deb 生成：**nfpm**（外部工具，`go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest`）
- 架构：**仅 amd64**
- 验证场：无 WSL——静态验证（结构/清单），真机测试用户后续自行过验收清单

## 范围

### 纳入

1. `OpenBrowser` 平台拆分（unix 走 `xdg-open`）
2. XDG autostart（`ok setup` 写入、`setupx.Uninstall` 移除）
3. `scripts/build-linux.sh`：linux/amd64 构建 + tar.gz 打包 + 调 nfpm 打 .deb
4. `installer/nfpm.yaml`
5. .deb 结构静态验证脚本（一次性 python）
6. README「方式 C：Linux」+ wiki 沉淀

### 不纳入（非目标）

- macOS 任何打包（无机器、需签名公证）
- arm64 架构、.rpm、AppImage、.dmg/.pkg
- Linux 托盘（CGO/dbus 代价与纯 Go 纪律冲突，无头 daemon 即可）
- Windows 安装器行为变更

## 设计

### 1. OpenBrowser 平台拆分

现状：`internal/gui/server.go` 的 `OpenBrowser`（第 18~31 行）硬编码 powershell/cmd——能跨平台编译（命令只是字符串）但在 Linux 上静默失败。

- 新建 `internal/gui/browser_windows.go`（`//go:build windows`）：现有实现**原样搬入**（含 maximizeWindowByTitle 调用与注释）
- 新建 `internal/gui/browser_unix.go`（`//go:build !windows`）：

```go
// OpenBrowser 非 Windows 平台经 xdg-open 打开默认浏览器；
// 失败仅打印 URL（沿用 Windows 版兜底语义）。无窗口句柄概念，恒返回 0。
func OpenBrowser(url string) uintptr {
	if err := exec.Command("xdg-open", url).Start(); err != nil {
		fmt.Println("请在浏览器打开:", url)
	}
	return 0
}
```

- `server.go` 保留包注释与共享部分；`OpenBrowser` 返回 0 与 `window_other.go` 的 `IsWindow=false` 语义配套，调用方（daemon 托盘聚焦）已容忍 0

### 2. XDG autostart

- **写入点**：`ok setup`（hooks/skills/embedding 之外加一步，非 Windows 才执行）——写 `~/.config/autostart/openknowledge.desktop`：

```ini
[Desktop Entry]
Type=Application
Name=OpenKnowledge
Exec=<exe 真实路径（EvalSymlinks 后）> daemon
X-GNOME-Autostart-enabled=true
```

- 幂等覆盖写（父目录 MkdirAll）；exe 路径解析（`os.Executable` + `EvalSymlinks`）在 setupx 内实现——gui 包有同款 `exePath()`，但 setupx 被 gui 依赖、不能反向 import，此处各写一份（约 8 行）比分层污染更干净
- **移除点**：`setupx.Uninstall` 增加删除该文件一步（错误容忍，KB 数据保留语义不变）
- 与 Windows 安装器写 HKCU Run 同语义；tar.gz 用户跑一遍 `ok setup` 即完成自启

### 3. 构建脚本 `scripts/build-linux.sh`（git-bash）

1. sed 从 `installer/openknowledge.iss` 提取 `AppVersion`（与 `build-dist.sh` 同款手法，版本事实源不变）
2. `GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X openknowledge/internal/version.Version=<v>"` → `dist/linux-amd64/ok`
3. 拷贝 `web/`、`changelogs/` 进暂存（与 dist 布局一致）
4. **tar.gz**：`installer/output/openknowledge_<v>_linux_amd64.tar.gz`，内含单层目录 `openknowledge_<v>_linux_amd64/`（ok + web/ + changelogs/）
5. **.deb**：检测 `nfpm` 在 PATH（缺失则打印安装命令并非零退出），`VERSION=<v> nfpm package --config installer/nfpm.yaml --target installer/output/`

### 4. `installer/nfpm.yaml`

```yaml
name: openknowledge
arch: amd64
platform: linux
version: ${VERSION}
maintainer: OpenKnowledge
description: 为 AI 编程助手提供的项目知识库（hooks 自动注入 + 强制工作流规则）
homepage: https://z7dream-gitea.iepose.cn/zhangxyfs/OpenKnowledge
recommends: [xdg-utils]
contents:
  - src: dist/linux-amd64/ok          dst: /usr/lib/openknowledge/ok
  - src: dist/linux-amd64/web/        dst: /usr/lib/openknowledge/web/
  - src: dist/linux-amd64/changelogs/ dst: /usr/lib/openknowledge/changelogs/
  - src: /usr/lib/openknowledge/ok    dst: /usr/bin/ok
    type: symlink
```

- 无 postinst/prerm：自启走 `ok setup`，系统包不动用户目录
- `Recommends: xdg-utils` 而非 Depends：无它仅 GUI 打不开浏览器，核心功能不受影响
- 实际字段以 nfpm v2 文档为准微调（如 `type: symlink` 的写法）

### 5. 静态验证（无 WSL）

- `go test ./...` 全绿；三平台编译矩阵：`GOOS=windows/linux/darwin go build ./cmd/ok` 全过
- tar.gz：`tar -tzf` 输出对照预期清单
- .deb：git-bash 无 `ar`——一次性 python 脚本（`scripts/verify-deb.py`）解析 ar 归档：断言三成员 `debian-binary`/`control.tar.gz`/`data.tar.gz` 齐全、control 内 `Version:` 与 iss 一致、data.tar.gz 内文件清单完整（ok/web/index.html/changelogs/、usr/bin/ok 软链）
- 真机验收清单随交付给出（`dpkg -i`、gtk 桌面双击、`ok gui` 开浏览器、重启验证自启）

### 6. 文档

- README 安装节加「方式 C：Linux」：tar.gz 解压 `./ok setup` / `sudo dpkg -i` 两种用法
- wiki「安装器与发布」条目增量沉淀

## 错误处理

- 版本提取失败 → 脚本首行 `set -euo pipefail` 兜底非零退出
- nfpm 缺失 → 明确提示安装命令，tar.gz 产物已完成不受影响
- xdg-open 缺失（极简系统）→ 打印 URL，不影响 daemon/注入主路径
- autostart 写入失败 → setup 警告不中断（fail-open 铁律）

## 测试

- `browser_unix.go`：编译矩阵覆盖（无单测价值——exec 外部命令；Windows 侧行为零变化由既有测试兜底）
- autostart：拆纯函数 + 平台壳——`autostartDesktop(exe string) string` 生成 desktop 文件内容（平台无关，Windows 上单测可覆盖内容生成）；`autostart_windows.go`（no-op）/ `autostart_unix.go`（`//go:build !windows`，写/删 `~/.config/autostart/openknowledge.desktop`）按项目既有 `_windows/_other` 约定拆编译标签；setupx 单测覆盖：内容生成函数（全平台可跑）+ Windows no-op 断言；unix 壳仅数行 I/O，编译矩阵兜底
- build 脚本：手动跑一次验收产物

## 风险

- nfpm 版本差异导致 yaml 字段微调——实现时以 `nfpm --version` 输出与官方文档为准
- .deb 未真机实测——交付附真机验收清单，风险显性化
