package web

import (
	"net/url"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

func portAnswers(ports string) func(ctl.Request) (ctl.Response, error) {
	return byOp(map[string]string{"firewall.status": fwStatusJSON(true, true, ""), "port.status": ports,
		"port.set": "moved", "port.finish": "finished", "port.cancel": "back"})
}

const portsNow = `{"ssh":[2222],"ssh_old":null,"panel":2610,"panel_new":0,"pma":2611,"webmail":2612,"webmail_on":false}`

func TestTheFirewallPageShowsTheServicePorts(t *testing.T) {
	h, _, c, _ := setupWith(t, portAnswers(portsNow), nil)
	body := do(h, "GET", "/firewall", nil, cookie(c)).Body.String()
	for _, want := range []string{"Cổng dịch vụ", ">2222<", ">2610<", ">2611<", ">2612<", "dùng khi bật webmail", `name="service" value="ssh"`,
		`name="service" value="panel"`, `name="service" value="pma"`, `name="service" value="webmail"`, "security group"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, `name="service" value="mysql"`) {
		t.Error("MySQL is not offered")
	}
}

func TestAWaitingChangeOfSSHOffersToFinishOrCancel(t *testing.T) {
	h, _, c, _ := setupWith(t, portAnswers(`{"ssh":[22,2200],"ssh_old":[22],"panel":2610,"pma":0,"webmail":2612}`), nil)
	body := do(h, "GET", "/firewall", nil, cookie(c)).Body.String()
	for _, want := range []string{"SSH đang nghe ở 2200 và cả cổng cũ 22", "Vào được, đóng 22", `action="/firewall/port/finish"`, `action="/firewall/port/cancel"`, "Hãy hoàn tất thay đổi ở trên trước", "Chưa cài"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// The old port of the panel closes only from a page opened on the new port: that is the proof the new one works.
func TestTheChangeOfThePanelIsFinishedOnlyFromTheNewPort(t *testing.T) {
	ports := `{"ssh":[22],"panel":2610,"panel_new":8443,"pma":2611,"webmail":2612}`
	h, _, c, _ := setupWith(t, portAnswers(ports), nil)
	body := do(h, "GET", "/firewall", nil, cookie(c), host("203.0.113.5:2610")).Body.String()
	if strings.Contains(body, `value="panel"><button class="btn-primary"`) || !strings.Contains(body, `href="https://203.0.113.5:8443/firewall"`) {
		t.Errorf("on the old port the page must send the person to the new one:\n%s", body)
	}
	body = do(h, "GET", "/firewall", nil, cookie(c), host("203.0.113.5:8443")).Body.String()
	if !strings.Contains(body, "Đóng cổng 2610") || !strings.Contains(body, `action="/firewall/port/finish"`) {
		t.Errorf("on the new port the page offers to close the old one:\n%s", body)
	}
}

func TestChangingAPortSendsItAndABadOneNeverLeaves(t *testing.T) {
	h, f, c, tok := setupWith(t, portAnswers(portsNow), nil)
	w := followed(h, do(h, "POST", "/firewall/port", url.Values{"csrf": {tok}, "service": {"ssh"}, "port": {" 2200 "}}, cookie(c)), c)
	req, ok := lastOp(f, "port.set")
	if !ok || req.SvcKey != "ssh" || req.Port != 2200 || backOf(t, w.Body.String()) != "/firewall" {
		t.Fatalf("%+v\n%s", req, w.Body.String())
	}
	w = followed(h, do(h, "POST", "/firewall/port", url.Values{"csrf": {tok}, "service": {"panel"}, "port": {"8443"}}, cookie(c)), c)
	if got := backOf(t, w.Body.String()); got != "https://localhost:8443/firewall" {
		t.Errorf("after moving the panel the page leads to the new port: %s", got)
	}
	n := len(f.reqs)
	for name, form := range map[string]url.Values{
		"mysql":          {"service": {"mysql"}, "port": {"3307"}},
		"not a number":   {"service": {"ssh"}, "port": {"22; reboot"}},
		"a web port":     {"service": {"ssh"}, "port": {"443"}},
		"below 1024":     {"service": {"panel"}, "port": {"999"}},
		"behind nginx":   {"service": {"webmail"}, "port": {"8781"}},
		"finish webmail": {"service": {"webmail"}},
	} {
		form.Set("csrf", tok)
		path := "/firewall/port"
		if name == "finish webmail" {
			path = "/firewall/port/finish"
		}
		if w := do(h, "POST", path, form, cookie(c)); w.Code != 400 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if len(f.reqs) != n {
		t.Errorf("a refused port reached the helper: %v", f.reqs[n:])
	}
	followed(h, do(h, "POST", "/firewall/port/cancel", url.Values{"csrf": {tok}, "service": {"ssh"}}, cookie(c)), c)
	if req, ok := lastOp(f, "port.cancel"); !ok || req.SvcKey != "ssh" {
		t.Errorf("cancel: %+v", req)
	}
	if w := do(h, "POST", "/firewall/port", url.Values{"csrf": {"wrong"}, "service": {"ssh"}, "port": {"2200"}}, cookie(c)); w.Code != 403 {
		t.Errorf("a wrong token: %d", w.Code)
	}
}
