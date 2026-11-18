package web

import (
	"net/url"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

func fwStatusJSON(on, active bool, conflict string) string {
	return `{"on":` + map[bool]string{true: "true", false: "false"}[on] + `,"active":` + map[bool]string{true: "true", false: "false"}[active] +
		`,"conflict":"` + conflict + `","has_nft":true,"locked":{"ssh":[22],"panel":[2610,2611]},"rules":[` +
		`{"id":1,"action":"allow","proto":"tcp","port":"80","source":"","comment":"HTTP"},` +
		`{"id":2,"action":"deny","proto":"both","port":"","source":"203.0.113.7","comment":"spam <b>x</b>"}]}`
}

func fwAnswers(status string) func(ctl.Request) (ctl.Response, error) {
	return byOp(map[string]string{"firewall.status": status, "firewall.enable": "on", "firewall.disable": "off", "firewall.add": "added", "firewall.del": "gone"})
}

func TestTheFirewallPageShowsStateRulesAndTheLockedPorts(t *testing.T) {
	h, _, c, _ := setupWith(t, fwAnswers(fwStatusJSON(true, true, "")), nil)
	w := do(h, "GET", "/firewall", nil, cookie(c))
	body := w.Body.String()
	for _, want := range []string{"Đang bật", `action="/firewall/disable"`, ">22<", ">2610<", ">2611<", "Luôn mở", "203.0.113.7", `action="/firewall/1/delete"`, `action="/firewall/2/delete"`,
		`action="/firewall/add"`, `action="/firewall/block"`, `href="/firewall" class="nav-link" aria-current="page"`, "Mọi cổng", "TCP và UDP"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "<b>x</b>") || !strings.Contains(body, "spam &lt;b&gt;x&lt;/b&gt;") {
		t.Error("a comment must be escaped")
	}
	if strings.Contains(body, `action="/firewall/enable"`) {
		t.Error("a firewall that is on and loaded needs no enable button")
	}
}

func TestAFirewallThatIsOffOffersToTurnOnAndAnotherFirewallBlocksIt(t *testing.T) {
	h, _, c, _ := setupWith(t, fwAnswers(fwStatusJSON(false, false, "")), nil)
	body := do(h, "GET", "/firewall", nil, cookie(c)).Body.String()
	if !strings.Contains(body, "Đang tắt") || !strings.Contains(body, `action="/firewall/enable"`) || strings.Contains(body, `action="/firewall/disable"`) {
		t.Errorf("off: %s", body)
	}
	h, _, c, _ = setupWith(t, fwAnswers(fwStatusJSON(false, false, "ufw")), nil)
	body = do(h, "GET", "/firewall", nil, cookie(c)).Body.String()
	if strings.Contains(body, `action="/firewall/enable"`) || !strings.Contains(body, "ufw") || !strings.Contains(body, "Hai tường lửa") {
		t.Errorf("with ufw running the page must not offer to enable, and must say why")
	}
	h, _, c, _ = setupWith(t, fwAnswers(fwStatusJSON(true, false, "")), nil)
	body = do(h, "GET", "/firewall", nil, cookie(c)).Body.String()
	if !strings.Contains(body, "chưa nạp") || !strings.Contains(body, `action="/firewall/enable"`) {
		t.Error("on in the database but not loaded in the kernel is shown, with a way to load it")
	}
}

func TestAddingARuleSendsItWithTheAddressOfThePerson(t *testing.T) {
	h, f, c, tok := setupWith(t, fwAnswers(fwStatusJSON(true, true, "")), nil)
	form := url.Values{"csrf": {tok}, "action": {"deny"}, "proto": {"tcp"}, "port": {" 3306 "}, "source": {"198.51.100.0/24"}, "comment": {"db"}}
	w := followed(h, do(h, "POST", "/firewall/add", form, cookie(c), header("X-Real-IP", "192.0.2.50")), c)
	req, ok := lastOp(f, "firewall.add")
	if !ok || req.FwAction != "deny" || req.FwProto != "tcp" || req.FwPort != "3306" || req.FwSource != "198.51.100.0/24" || req.FwComment != "db" || req.Client == "" {
		t.Fatalf("%+v\n%s", req, w.Body.String())
	}
	if backOf(t, w.Body.String()) != "/firewall" {
		t.Errorf("back: %s", w.Body.String())
	}
}

func TestBlockingAnAddressMakesADenyRuleForEveryPort(t *testing.T) {
	h, f, c, tok := setupWith(t, fwAnswers(fwStatusJSON(true, true, "")), nil)
	followed(h, do(h, "POST", "/firewall/block", url.Values{"csrf": {tok}, "source": {"203.0.113.9"}, "comment": {"scanner"}}, cookie(c)), c)
	req, ok := lastOp(f, "firewall.add")
	if !ok || req.FwAction != "deny" || req.FwProto != "both" || req.FwPort != "" || req.FwSource != "203.0.113.9" {
		t.Errorf("%+v", req)
	}
	if w := do(h, "POST", "/firewall/block", url.Values{"csrf": {tok}, "source": {""}}, cookie(c)); w.Code != 400 {
		t.Errorf("an empty address: %d", w.Code)
	}
}

func TestARuleThatCannotWorkNeverReachesTheHelper(t *testing.T) {
	for name, form := range map[string]url.Values{
		"a bad action":       {"action": {"reject"}, "proto": {"tcp"}, "port": {"80"}},
		"a port with a verb": {"action": {"allow"}, "proto": {"tcp"}, "port": {"80; flush ruleset"}},
		"a name as source":   {"action": {"deny"}, "proto": {"both"}, "source": {"evil.example"}},
		"allow everything":   {"action": {"allow"}, "proto": {"both"}},
	} {
		h, f, c, tok := setupWith(t, fwAnswers(fwStatusJSON(true, true, "")), nil)
		form.Set("csrf", tok)
		w := do(h, "POST", "/firewall/add", form, cookie(c))
		if _, sent := lastOp(f, "firewall.add"); sent || w.Code != 400 || !strings.Contains(w.Body.String(), "Luật không hợp lệ") {
			t.Errorf("%s: sent=%v code=%d", name, sent, w.Code)
		}
	}
}

func TestRefusalsOfTheHelperAreExplained(t *testing.T) {
	for output, want := range map[string]string{
		"this rule would block your own address 192.0.2.50 from SSH or the panel": "chặn chính bạn",
		"firewall-conflict: ufw is running":                                       "tường lửa khác",
	} {
		h, _, c, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
			if r.Op == "firewall.add" || r.Op == "firewall.enable" {
				return ctl.Response{Output: output, Error: "exit status 2"}, nil
			}
			return fwAnswers(fwStatusJSON(true, true, ""))(r)
		}, nil)
		path, form := "/firewall/add", url.Values{"csrf": {tok}, "action": {"deny"}, "proto": {"both"}, "source": {"192.0.2.50"}}
		if strings.HasPrefix(output, "firewall-conflict") {
			path, form = "/firewall/enable", url.Values{"csrf": {tok}}
		}
		if body := followed(h, do(h, "POST", path, form, cookie(c)), c).Body.String(); !strings.Contains(body, want) {
			t.Errorf("%q: missing %q in\n%s", output, want, body)
		}
	}
}

func TestEnableDisableDeleteNeedTheTokenAndAValidNumber(t *testing.T) {
	h, f, c, tok := setupWith(t, fwAnswers(fwStatusJSON(true, true, "")), nil)
	followed(h, do(h, "POST", "/firewall/enable", url.Values{"csrf": {tok}}, cookie(c)), c)
	followed(h, do(h, "POST", "/firewall/disable", url.Values{"csrf": {tok}}, cookie(c)), c)
	followed(h, do(h, "POST", "/firewall/2/delete", url.Values{"csrf": {tok}}, cookie(c)), c)
	if _, ok := lastOp(f, "firewall.enable"); !ok {
		t.Error("enable")
	}
	if _, ok := lastOp(f, "firewall.disable"); !ok {
		t.Error("disable")
	}
	if req, ok := lastOp(f, "firewall.del"); !ok || req.RuleID != 2 {
		t.Errorf("delete: %+v", req)
	}
	n := len(f.reqs)
	for _, p := range []string{"/firewall/x/delete", "/firewall/0/delete", "/firewall/-4/delete"} {
		if w := do(h, "POST", p, url.Values{"csrf": {tok}}, cookie(c)); w.Code != 400 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	for _, p := range []string{"/firewall/enable", "/firewall/disable", "/firewall/3/delete", "/firewall/add", "/firewall/block"} {
		if w := do(h, "POST", p, url.Values{"csrf": {"wrong"}}, cookie(c)); w.Code != 403 {
			t.Errorf("%s with a wrong token: %d", p, w.Code)
		}
	}
	if w := do(h, "POST", "/firewall/enable", url.Values{"csrf": {tok}}); w.Code != 303 {
		t.Errorf("no session: %d", w.Code)
	}
	if len(f.reqs) != n {
		t.Errorf("a refused request reached the helper: %v", f.reqs[n:])
	}
}
