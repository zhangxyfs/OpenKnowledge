# Logo + exe 图标嵌入 + Python 一键构建

- **Logo**：`installer/assets/make_logo.py`（PIL 程序化绘制，可复现）生成
  `logo.png`（256×256）与 `logo.ico`（16~256 多尺寸）——蓝底圆角方块 +
  白色翻开的书（知识）+ 琥珀色星点（AI）。
- **exe 图标与版本信息**：`cmd/ok/winres.json` + go-winres（`make --in`）生成
  `.syso`（已 gitignore，构建时自动生成），go build 自动嵌入；已验证
  dist/ok.exe 关联图标可提取。注意 go-winres 的 RT_VERSION 层级必须是
  `#1 → 0000 → fixed/info`，语言 ID 用 0409；PIL 产出的 .ico 不被其接受，
  图标源用 logo.png。
- **安装程序图标**：SetupIconFile=assets\logo.ico；logo.ico 随包装入 {app}，
  开始菜单与桌面快捷方式均使用 logo。
- **`scripts/build.py`**：一键完成 go-winres → go build（-s -w -H windowsgui）
  → dist/web 拷贝 → ISCC 打包。支持 `--skip-installer` / `--skip-winres`；
  ISCC 路径可用环境变量覆盖。
- 安装目录选择：Inno 向导默认含"选择目标位置"页（静默测试的 /DIR 仅为
  自动化覆盖，用户交互安装时可自由改目录）。
