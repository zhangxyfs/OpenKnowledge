package gui

import (
	"net/url"
	"strings"
)

// safeAppURL 判定可安全交给浏览器启动命令的应用 URL：仅 http/https、主机为
// 本机回环，且不含可击穿 Windows PowerShell 单引号包裹的字符（单双引号、
// 控制字符）。URL 由 daemon 自生成（http://127.0.0.1:<port>/?token=<hex>），
// 形状之外的输入一律拒绝——这是纵深防御，防未来调用方把外部输入传进来。
func safeAppURL(raw string) bool {
	if strings.ContainsAny(raw, "'\"\r\n\x00") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
