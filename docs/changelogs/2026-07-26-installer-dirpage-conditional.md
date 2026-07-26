# 安装目录页按版本条件显示

- 诉求：升级（版本变化）时静默装进旧目录不打扰；同版本重装/首次安装才让用户选目录。
- 实现（installer/openknowledge.iss [Code]）：读卸载注册表的 DisplayVersion/
  InstallLocation——IsUpgrade() 判定版本不同 → ShouldSkipPage(wpSelectDir)
  跳过目录页；InitializeWizard 始终预填旧目录（同版本重装时页面出现但默认
  旧路径）。UsePreviousAppDir 保持 no，改由代码接管。
