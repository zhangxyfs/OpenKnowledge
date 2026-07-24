# 引导页重绘 + 卸载功能

- **卸载**：`POST /api/uninstall` + 引导页"卸载"卡片（危险区样式，confirm 确认）。
  `setupx.Uninstall()` 移除：kimi config.toml 中的 hooks 标记块（用户其余配置保留）、
  全部已安装技能目录、全局配置中的 [embedding] 小节（其余小节保留；文件无剩余
  内容时删除文件本身）。**知识库数据（registry、projects、kb.db、knowledge）
  一律不碰**，幂等可重复执行。
- **引导页重绘**：卡片统一为"标题+状态徽标头部行 / 描述 / 底部操作区"结构，
  两列网格（embedding、经验沉淀跨两列，窄屏自适应单列）；embedding 表单改为
  单行三框；新增危险区卸载卡片；修正卡片高度不均与大片留白。
- embedding 卡片说明注明 DeepSeek 无 embedding 接口（404 原因的界面提示）。
