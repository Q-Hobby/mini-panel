package web

// English text of the service ports card on the firewall page.
func init() {
	for k, v := range map[string]string{
		"port.title":           "Service ports",
		"port.sub":             "Move SSH, the panel, phpMyAdmin or webmail to another port. The firewall and fail2ban follow the change.",
		"port.col.svc":         "Service",
		"port.col.now":         "Current port",
		"port.col.new":         "New port",
		"port.svc.ssh":         "SSH",
		"port.svc.panel":       "Panel",
		"port.svc.pma":         "phpMyAdmin",
		"port.svc.webmail":     "Webmail",
		"port.none":            "Not installed",
		"port.pma.none":        "Install phpMyAdmin on the Services page first.",
		"port.webmail.none":    "Available once the panel is installed.",
		"port.webmail.off":     "(used when webmail is turned on)",
		"port.busy":            "Finish the change above first.",
		"port.change":          "Change",
		"port.cancel":          "Cancel, keep the old port",
		"port.open":            "Open the panel on port %d",
		"port.wait.ssh":        "SSH now listens on %s as well as %s.",
		"port.wait.ssh.d":      "Open a new SSH session on port %s. When it works, close the old port. Sessions that are open stay open. If nothing is done, the old port alone is restored after 10 minutes.",
		"port.finish.ssh":      "It works, close %s",
		"port.wait.panel":      "The panel now answers on port %d as well as %d.",
		"port.wait.panel.go":   "Open the panel on the new port and finish the change there. The browser warns about the certificate once more if it is self-signed. If nothing is done, the old port alone is restored after 10 minutes.",
		"port.wait.panel.here": "You opened this page on the new port, so it works. Close the old port to finish.",
		"port.finish.panel":    "Close port %d",
		"port.hint.two":        "SSH and the panel change in two steps: the new port opens next to the old one, and the old one closes only after you confirm the new one works.",
		"port.hint.provider":   "If your provider has a firewall of its own (security group), open the new port there first. The panel cannot change it.",
		"port.hint.detect":     "The SSH port is detected from sshd, also when the provider set a port other than 22. The terminal and file manager always use the detected port.",
		"port.fail":            "Port not changed",
		"port.err.bad":         "Invalid port: %s.",
		"port.err.svc":         "Unknown service.",
		"port.err.waiting":     "Another port change is not finished yet. Finish or cancel it first.",
		"act.port.set":         "Change the port of %s to %d",
		"act.port.finish":      "Finish the port change of %s",
		"act.port.cancel":      "Cancel the port change of %s",
	} {
		en[k] = v
	}
}
