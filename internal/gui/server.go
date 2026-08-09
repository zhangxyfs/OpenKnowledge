// Package gui 提供 ok gui / 无参数启动的 Web 管理界面：
// 127.0.0.1 HTTP 服务、令牌鉴权、心跳版本号与浏览器自动打开。
// 进程生命周期由 internal/daemon 托管（常驻，不再随页面关闭退出）。
package gui
