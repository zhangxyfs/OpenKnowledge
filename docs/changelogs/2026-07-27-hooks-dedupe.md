# hooks 配置去重 + init 幂等写入

- **问题**：kimi `config.toml` 中出现多份重复的 `[[hooks]]`（甚至指向不同 exe 路径）。
  根因：`ok init` 曾把一段无标记的 hooks 原文打印出来供手动追加，被重复粘贴后
  无任何机制清理；`ok setup` 的标记块 upsert 只管自己的标记块，不识别存量裸块。
- **修复**：
  - `setupx.StripLegacyOKHooks()`：识别并移除配置中所有指向 ok hook 的无标记
    `[[hooks]]` 表（按 `command` 中的 `ok hook` / `ok.exe hook` 匹配），其它工具
    的 hooks 原样保留。
  - `UpsertHooksBlock`（setup / init / GUI 引导共用）写入前先清除存量 ok hooks，
    已存在则原位覆盖 exe 路径——无论换机、换路径、重复执行，配置中始终只有一份。
  - `Uninstall` 卸载时一并清除无标记的存量 ok hooks。
  - `ok init` 不再打印可粘贴的裸 hooks 块，改为直接调用与 `ok setup` 相同的
    幂等写入逻辑（写失败不阻断项目注册，仅 stderr 提示可 `ok setup` 重试）。
- **兼容**：写入前仍会备份 `config.toml.bak-openknowledge`；已有重复配置的用户
  执行一次 `ok setup`（或任意项目的 `ok init`）即可自动收敛为一份。
