# Inno Setup 安装程序

- 新增 `installer/openknowledge.iss`（Inno Setup 6/7）与 `scripts/build-installer.sh`：
  一键构建 `installer/output/OpenKnowledgeSetup-2.1.0.exe`（5.7MB）。
- 安装行为：默认安装到 `%LOCALAPPDATA%\Programs\OpenKnowledge`（免管理员）；
  可选桌面快捷方式、加入用户 PATH（[Code] 注册表实现，卸载自动清理）、
  完成页勾选运行 `ok setup` 首次引导；开始菜单含 GUI 与卸载入口。
- 中文界面：`installer/lang/ChineseSimplified.isl`（社区官方翻译，
  kira-96/Inno-Setup-Chinese-Simplified-Translation），按系统语言自动选择。
- 卸载行为：交互卸载询问是否删除 `~/.openknowledge` 数据（默认保留）；
  **静默卸载（/VERYSILENT）一律保留数据**（WizardSilent 守卫，防误删）。
- 已验证：静默安装到沙箱目录（/DIR 需反斜杠路径）、安装后 ok.exe doctor 正常、
  静默卸载清除程序文件且 KB 数据完好。
