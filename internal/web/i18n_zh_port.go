package web

// Simplified Chinese text of the service ports card on the firewall page.
func init() {
	for k, v := range map[string]string{
		"port.title":           "服务端口",
		"port.sub":             "将 SSH、面板、phpMyAdmin 或 Webmail 改到其他端口。防火墙和 fail2ban 会随之更新。",
		"port.col.svc":         "服务",
		"port.col.now":         "当前端口",
		"port.col.new":         "新端口",
		"port.svc.ssh":         "SSH",
		"port.svc.panel":       "面板",
		"port.svc.pma":         "phpMyAdmin",
		"port.svc.webmail":     "Webmail",
		"port.none":            "未安装",
		"port.pma.none":        "请先在“服务”页面安装 phpMyAdmin。",
		"port.webmail.none":    "安装面板后可用。",
		"port.webmail.off":     "（开启 Webmail 时使用）",
		"port.busy":            "请先完成上面的更改。",
		"port.change":          "更改",
		"port.cancel":          "取消，保留旧端口",
		"port.open":            "在端口 %d 打开面板",
		"port.wait.ssh":        "SSH 现在同时监听 %s 和旧端口 %s。",
		"port.wait.ssh.d":      "在端口 %s 打开一个新的 SSH 会话。能登录后再关闭旧端口。已打开的会话不受影响。如果不做任何操作，10 分钟后会自动恢复为仅使用旧端口。",
		"port.finish.ssh":      "可以登录，关闭 %s",
		"port.wait.panel":      "面板现在同时在端口 %d 和旧端口 %d 上响应。",
		"port.wait.panel.go":   "在新端口打开面板并在那里完成更改。如果证书是自签名的，浏览器会再提示一次。如果不做任何操作，10 分钟后会自动恢复为仅使用旧端口。",
		"port.wait.panel.here": "你正在新端口上打开此页面，说明新端口可用。关闭旧端口即可完成。",
		"port.finish.panel":    "关闭端口 %d",
		"port.hint.two":        "SSH 和面板分两步更改：新端口先与旧端口同时开放，确认新端口可用后才关闭旧端口。",
		"port.hint.provider":   "如果服务商有自己的防火墙（安全组），请先在那里开放新端口。面板无法修改它。",
		"port.hint.detect":     "SSH 端口从 sshd 检测，服务商设置的端口不是 22 时也一样。终端和文件管理器始终使用检测到的端口。",
		"port.fail":            "端口未更改",
		"port.err.bad":         "端口无效：%s。",
		"port.err.svc":         "未知服务。",
		"port.err.waiting":     "另一个端口更改尚未完成。请先完成或取消它。",
		"act.port.set":         "将 %s 的端口改为 %d",
		"act.port.finish":      "完成 %s 的端口更改",
		"act.port.cancel":      "取消 %s 的端口更改",
	} {
		zh[k] = v
	}
}
