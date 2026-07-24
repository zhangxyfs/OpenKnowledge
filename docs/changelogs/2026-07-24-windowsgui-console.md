# GUI 子系统编译：双击 ok.exe 不再弹 cmd 窗口

- dist 构建加 `-H windowsgui`：双击启动不再出现控制台窗口。
- CLI 模式动态挂回控制台：`attachForCLI()`（cmd/ok/console_windows.go，
  kernel32 AttachConsole）——stdout 无效（cmd/PowerShell 交互运行）时挂回父
  控制台恢复可见输出；stdout 已有效（hook 管道、重定向、Git Bash pty）时
  跳过，保证 hook 注入与重定向不受影响。
- 已验证三条 stdio 路径：Git Bash 交互输出、hook 管道注入、重定向到文件。
