# ok init 缺省取当前目录名 + openknowledge-init 技能免输入

- `ok init` 的项目名参数改为可选：缺省取当前目录基名（`ok init [项目名]`）。
- `openknowledge-init` 技能改为直接执行无参数的 `ok init`，agent 不再向用户
  询问项目名；技能 description 与 `ok setup` 引导文本同步更新。
