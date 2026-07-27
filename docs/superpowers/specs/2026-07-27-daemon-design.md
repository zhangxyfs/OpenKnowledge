# OpenKnowledge 常驻 daemon 设计

日期：2026-07-27
状态：已确认（用户两轮设计评审通过）

## 背景与问题

用户观察到系统中有时出现多个 `ok.exe` 进程。排查结论：

1. **GUI 无单实例**：每次双击 exe / `ok gui` / 安装器结束页都新起一个独立 HTTP 服务（随机端口、随机 token）；且看门狗"收到首个心跳才上膛"，浏览器打开失败时进程**永不退出**（真泄漏）。
2. **hook 进程并发**：kimi 每次提问起一个 `ok.exe hook prompt`，其中 kb.db 增量同步在配置 embedding 后按"每变化条目一次串行 HTTP（各 5s 超时）"执行，总时长无上限；多会话并发时多个 ok.exe 并存。
3. **SQLITE_BUSY 实锤**（`~/.openknowledge/ok.log`）：`index.Open` 未设 `busy_timeout`，GUI 与 hook、或多个 hook 并发写同一 kb.db 立即锁冲突。

用户目标：**全系统只有 1 个 ok.exe 常驻进程**，承载 GUI、hook、（CLI 不转发）三类处理；常驻不退出，被杀后由下次 ok.exe 执行自动拉起。

## 已确认的决策

| 决策点 | 结论 |
|--------|------|
| daemon 拉起 | 安装器注册登录自启（HKCU Run）为主；任意 ok.exe 执行发现 daemon 不在则后台拉起兜底 |
| exe 升级后旧 daemon | client 检测 exe 指纹不一致 → 旧 daemon shutdown → 拉起新版，自动切换 |
| hook 在 daemon 不在时的兜底 | 本次请求本地直接处理（现有代码路径），同时后台拉起 daemon；kimi 永不卡 10s 超时 |
| 架构方案 | HTTP 常驻 daemon；CLI 命令（add/search/list 等）保持本地执行，不转发 |

## 架构

```
kimi ──hook──▶ ok.exe hook *（瘦客户端，毫秒级）
                  │  daemon.json 健康？
                  ├─是─▶ HTTP POST 127.0.0.1:<port>/api/hook/* ─▶ daemon 进程内 hook.Handle*
                  └─否─▶ 本地直接处理（现有路径）＋ 后台拉起 daemon

用户 ──双击/ok gui──▶ ensure daemon ─▶ 打开浏览器指向 daemon URL ─▶ 立即退出
安装器 ──HKCU Run──▶ 登录自启 ok.exe daemon
```

### 组件

- **`ok daemon`（新隐藏命令）**：常驻进程。
  - 监听 `127.0.0.1:17888`；端口被占则回退随机端口。
  - 启动成功后把 `{pid, port, token, exe指纹, started_at}` 原子写入（tmp+rename）`~/.openknowledge/daemon.json`。
  - **端口即单实例锁**：第二个 daemon 抢不到端口 → 读 daemon.json 发现活实例 → 自行退出（code 0）。
  - exe 指纹 = exe 路径 + size + mtime（无需改构建脚本，开发构建自动生效）。
- **daemon 承载**：
  1. 现有 GUI 全部 HTTP 路由（`internal/gui` 静态页 + 管理 API，原样复用）；
  2. 新增 `/api/hook/prompt|post-tool|stop`：body 为 kimi 原始事件 JSON，进程内调用 `hook.Handle*`，响应 `{stdout, stderr, code}`；
  3. 新增 `/api/health`：返回 `{version, exe指纹}`，供客户端健康检查（200ms 超时）。
  
  hook 转发与 GUI 共用 daemon 启动时生成的同一 token（客户端从 daemon.json 读取）。
- **瘦客户端 `ok hook *`**：
  1. `hooks-disabled` 全局开关客户端短路（读标志文件，零成本）；
  2. 读 daemon.json → 健康检查（GET /api/health，200ms 超时）；
  3. 健康且指纹一致 → 转发 stdin（转发总超时 9s，给 kimi 的 10s hook 上限留余量；超时按放行处理、exit 0），把响应写回 stdout/stderr 并 `os.Exit(code)`（stop 的 exit 2 阻断语义由客户端执行，kimi 无感）；
  4. 不在/不健康 → 后台拉起 daemon（`DETACHED_PROCESS`，stdio 重定向到 `~/.openknowledge/daemon.log`），本次本地处理；
  5. 指纹不一致 → POST 旧 daemon `/api/shutdown`（短超时）→ 后台拉起新 daemon，本次本地处理。

### GUI 行为变化

- 心跳端点保留（页面 5s 心跳驱动 kb.db 变更自动刷新，`api.go` 现有逻辑）。
- **取消看门狗杀进程**：daemon 常驻，关闭页面/浏览器不退出进程。
- `ok gui` / 双击 exe 不再阻塞：ensure daemon → 开浏览器 → 立即退出。
- `/api/shutdown` 保留，供 `ok daemon stop`、版本切换、卸载使用。

## 错误处理与边界

- hook 路径全面 fail-open 不变：daemon 通信异常 → 本地处理；本地处理异常 → 放行。
- 僵尸 daemon.json（pid 死/端口不通/文件损坏）→ 视为"daemon 不在"，覆盖重写。
- daemon 与本地兜底路径在拉起瞬间仍可能并发写 kb.db → `index.Open` DSN 加 `_busy_timeout=3000`（或等价 pragma），双保险。
- 多会话同时拉起 daemon：端口锁保证唯一存活，其余自退；daemon.json 原子写防半截。
- `hooks-disabled` / `ok off`：客户端短路 + daemon 端保留原检查，双保险；语义不变，无需停 daemon。

## 生命周期

- **安装器**（`installer/openknowledge.iss`）：
  - 新增 HKCU `Software\Microsoft\Windows\CurrentVersion\Run` 项 `OpenKnowledge = "{app}\ok.exe daemon"`；
  - 卸载时先执行 `ok.exe daemon stop`（失败则 taskkill），再删文件与 Run 项。
- **`ok daemon stop`**：读 daemon.json → POST `/api/shutdown` → 删 daemon.json。
- `setupx.Uninstall()` 增加停 daemon 步骤（调上述逻辑）。

## 测试

- 单测：
  - daemon.json 读写与三态判定（健康 / 僵尸 / 指纹不符）；
  - `/api/hook/*`：prompt 注入内容透传、stop 的 code=2 与 stderr 映射、post-tool 落状态；
  - 瘦客户端转发（httptest 假 daemon）：正常转发、daemon 不存在时本地兜底、指纹不符时 shutdown+拉起；
  - 端口竞争：两个 daemon 进程只存活一个。
- 集成测试（`cmd/ok/integration_test.go` 风格）：真起 daemon → `ok hook prompt` 验证转发 → 杀 daemon 再跑验证"本地兜底 + 后台拉起"。
- 既有测试全部保持绿色（看门狗移除会改 `internal/gui/server.go` 及其测试）。

## 明确不做（YAGNI）

- CLI 命令（add/search/list/setup 等）不转发 daemon，保持本地执行；
- 不引入 named pipe / 新 IPC 依赖；
- 不做 daemon 端口可配置（固定 17888 + 占用时随机回退足够）；
- 不做开机自启之外的守护（无 watchdog 进程、无崩溃自动重启——被杀后由下次 ok.exe 执行拉起即可）。
