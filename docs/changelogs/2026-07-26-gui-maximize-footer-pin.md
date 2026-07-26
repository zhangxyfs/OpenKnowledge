# GUI 启动最大化修复 + 分页栏置底

- **启动未最大化修复**：`cmd /c start msedge --start-maximized` 不生效（start
  吞参数且 Edge 常忽略该 flag），改为 PowerShell
  `Start-Process <browser> -ArgumentList '--app=<url>' -WindowStyle Maximized`
  启动 Edge/Chrome 应用模式；默认浏览器回退不保证最大化。
- **分页栏置底**：body/main/#page-manage 改为 flex 列布局（min-height 100vh），
  `.entries-footer` `margin-top: auto` 固定到窗口底部；内容超一屏时正常随文
  档流下移。

补充：Start-Process -WindowStyle Maximized 在 Edge 单实例场景同样被吞（实测
zoomed=False）。最终方案：启动后 goroutine 轮询顶层窗口（EnumWindows 按标题
匹配），找到后 ShowWindow(SW_MAXIMIZE) 兜底，实测 zoomed=True。
