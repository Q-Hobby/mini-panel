package web

// Vietnamese text of the service ports card on the firewall page.
func init() {
	for k, v := range map[string]string{
		"port.title":           "Cổng dịch vụ",
		"port.sub":             "Chuyển SSH, panel, phpMyAdmin hoặc webmail sang cổng khác. Tường lửa và fail2ban tự cập nhật theo.",
		"port.col.svc":         "Dịch vụ",
		"port.col.now":         "Cổng hiện tại",
		"port.col.new":         "Cổng mới",
		"port.svc.ssh":         "SSH",
		"port.svc.panel":       "Panel",
		"port.svc.pma":         "phpMyAdmin",
		"port.svc.webmail":     "Webmail",
		"port.none":            "Chưa cài",
		"port.pma.none":        "Cài phpMyAdmin ở trang Dịch vụ trước.",
		"port.webmail.none":    "Dùng được khi đã cài panel.",
		"port.webmail.off":     "(dùng khi bật webmail)",
		"port.busy":            "Hãy hoàn tất thay đổi ở trên trước.",
		"port.change":          "Đổi",
		"port.cancel":          "Hủy, giữ cổng cũ",
		"port.open":            "Mở panel ở cổng %d",
		"port.wait.ssh":        "SSH đang nghe ở %s và cả cổng cũ %s.",
		"port.wait.ssh.d":      "Mở một phiên SSH mới ở cổng %s. Nếu vào được thì đóng cổng cũ. Các phiên đang mở vẫn giữ nguyên. Nếu không làm gì, sau 10 phút hệ thống tự trả về cổng cũ.",
		"port.finish.ssh":      "Vào được, đóng %s",
		"port.wait.panel":      "Panel đang trả lời ở cổng %d và cả cổng cũ %d.",
		"port.wait.panel.go":   "Mở panel ở cổng mới rồi hoàn tất ở đó. Nếu chứng chỉ là tự ký, trình duyệt sẽ cảnh báo thêm một lần. Nếu không làm gì, sau 10 phút hệ thống tự trả về cổng cũ.",
		"port.wait.panel.here": "Bạn đang mở trang này ở cổng mới, nghĩa là cổng mới đã dùng được. Đóng cổng cũ để hoàn tất.",
		"port.finish.panel":    "Đóng cổng %d",
		"port.hint.two":        "SSH và panel đổi qua hai bước: cổng mới mở cạnh cổng cũ, cổng cũ chỉ đóng sau khi bạn xác nhận cổng mới dùng được.",
		"port.hint.provider":   "Nếu nhà cung cấp có tường lửa riêng (security group), hãy mở cổng mới ở đó trước. Panel không đổi được tường lửa đó.",
		"port.hint.detect":     "Cổng SSH được dò từ sshd, kể cả khi nhà cung cấp đặt cổng khác 22. Terminal và Tệp luôn dùng cổng dò được.",
		"port.fail":            "Chưa đổi được cổng",
		"port.err.bad":         "Cổng không hợp lệ: %s.",
		"port.err.svc":         "Dịch vụ không hợp lệ.",
		"port.err.waiting":     "Một lần đổi cổng khác chưa hoàn tất. Hãy hoàn tất hoặc hủy nó trước.",
		"act.port.set":         "Đổi cổng %s sang %d",
		"act.port.finish":      "Hoàn tất đổi cổng %s",
		"act.port.cancel":      "Hủy đổi cổng %s",
	} {
		vi[k] = v
	}
}
