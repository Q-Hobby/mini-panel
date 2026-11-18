package web

import (
	"net/url"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

const secStatusJSON = `{"waf_mode":"detect","waf_installed":true,"crs_version":"3.3.5-2","f2b_on":true,"f2b_installed":true,"f2b_active":true,` +
	`"ignore":["203.0.113.7"],"jails":[` +
	`{"name":"sshd","failed":2,"total_failed":9,"banned":1,"total_banned":3,"ips":["198.51.100.4"]},` +
	`{"name":"mpn-panel","failed":0,"total_failed":0,"banned":0,"total_banned":0,"ips":[]},` +
	`{"name":"mpn-modsec","failed":0,"total_failed":0,"banned":0,"total_banned":0,"ips":[]}]}`

func secAnswers(status string) func(ctl.Request) (ctl.Response, error) {
	return byOp(map[string]string{"security.status": status, "security.waf": "ok", "security.f2b": "ok", "security.unban": "ok"})
}

func TestTheSecurityPageShowsBothFeaturesAndTheBannedAddresses(t *testing.T) {
	h, _, c, _ := setupWith(t, secAnswers(secStatusJSON), nil)
	body := do(h, "GET", "/security", nil, cookie(c)).Body.String()
	for _, want := range []string{`href="/security" class="nav-link" aria-current="page"`, `action="/security/waf"`, `action="/security/f2b"`,
		`value="detect" class="mt-1 accent-primary" checked`, "3.3.5-2", "Chỉ ghi nhận", "198.51.100.4", `action="/security/unban"`,
		`name="jail" value="sshd"`, `name="ip" value="198.51.100.4"`, "203.0.113.7", "mpn-modsec", "Lưu danh sách"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "libnginx-mod-http-modsecurity") {
		t.Error("the package note is for a machine that does not have them yet")
	}
}

func TestSecurityPageOnAMachineWithNeitherInstalled(t *testing.T) {
	status := `{"waf_mode":"off","waf_installed":false,"crs_version":"","f2b_on":false,"f2b_installed":false,"f2b_active":false,"ignore":null,"jails":[]}`
	h, _, c, _ := setupWith(t, secAnswers(status), nil)
	body := do(h, "GET", "/security", nil, cookie(c)).Body.String()
	for _, want := range []string{"libnginx-mod-http-modsecurity", "Bật fail2ban"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "Tắt fail2ban") || strings.Contains(body, `action="/security/unban"`) {
		t.Error("nothing to turn off or unban yet")
	}
}

func TestChangingTheModeSendsItToTheHelper(t *testing.T) {
	h, f, c, tok := setupWith(t, secAnswers(secStatusJSON), nil)
	w := followed(h, do(h, "POST", "/security/waf", url.Values{"csrf": {tok}, "mode": {"block"}}, cookie(c)), c)
	req, ok := lastOp(f, "security.waf")
	if !ok || req.SecMode != "block" {
		t.Fatalf("%+v\n%s", req, w.Body.String())
	}
	if backOf(t, w.Body.String()) != "/security" {
		t.Errorf("back: %s", w.Body.String())
	}
}

func TestAnInvalidModeNeverReachesTheHelper(t *testing.T) {
	h, f, c, tok := setupWith(t, secAnswers(secStatusJSON), nil)
	for _, mode := range []string{"", "On", "block; reboot", "DetectionOnly"} {
		w := do(h, "POST", "/security/waf", url.Values{"csrf": {tok}, "mode": {mode}}, cookie(c))
		if _, sent := lastOp(f, "security.waf"); sent || w.Code != 400 {
			t.Errorf("mode %q: sent=%v code=%d", mode, sent, w.Code)
		}
	}
}

func TestTurningFail2banOnAlwaysTrustsTheAddressOfThePerson(t *testing.T) {
	h, f, c, tok := setupWith(t, secAnswers(secStatusJSON), nil)
	form := url.Values{"csrf": {tok}, "on": {"1"}, "ignore": {"203.0.113.7\n198.51.100.0/24, 203.0.113.7"}}
	followed(h, do(h, "POST", "/security/f2b", form, cookie(c), header("X-Real-IP", "192.0.2.50")), c)
	req, ok := lastOp(f, "security.f2b")
	if !ok || !req.SecOn {
		t.Fatalf("%+v", req)
	}
	// Without a trusted proxy the address of the person is the one the connection came from, which httptest sets to 192.0.2.1.
	if strings.Join(req.SecIgnore, " ") != "203.0.113.7 198.51.100.0/24 192.0.2.1" {
		t.Errorf("trusted addresses: %v", req.SecIgnore)
	}
	followed(h, do(h, "POST", "/security/f2b", url.Values{"csrf": {tok}, "on": {"0"}}, cookie(c)), c)
	if req, _ := lastOp(f, "security.f2b"); req.SecOn {
		t.Error("the off button must turn it off")
	}
}

func TestABadTrustedAddressNeverReachesTheHelper(t *testing.T) {
	h, f, c, tok := setupWith(t, secAnswers(secStatusJSON), nil)
	for _, bad := range []string{"not-an-ip", "1.2.3.4; id", "300.1.1.1", "$(id)"} {
		w := do(h, "POST", "/security/f2b", url.Values{"csrf": {tok}, "on": {"1"}, "ignore": {bad}}, cookie(c))
		if _, sent := lastOp(f, "security.f2b"); sent || w.Code != 400 {
			t.Errorf("%q: sent=%v code=%d", bad, sent, w.Code)
		}
	}
}

func TestUnbanChecksJailAndAddress(t *testing.T) {
	h, f, c, tok := setupWith(t, secAnswers(secStatusJSON), nil)
	followed(h, do(h, "POST", "/security/unban", url.Values{"csrf": {tok}, "jail": {"sshd"}, "ip": {"198.51.100.4"}}, cookie(c)), c)
	if req, ok := lastOp(f, "security.unban"); !ok || req.Jail != "sshd" || req.IP != "198.51.100.4" {
		t.Errorf("%+v", req)
	}
	n := len(f.reqs)
	for _, form := range []url.Values{{"jail": {"recidive"}, "ip": {"1.2.3.4"}}, {"jail": {"sshd"}, "ip": {"1.2.3.0/24"}}, {"jail": {"sshd"}, "ip": {""}}} {
		form.Set("csrf", tok)
		if w := do(h, "POST", "/security/unban", form, cookie(c)); w.Code != 400 {
			t.Errorf("%v: %d", form, w.Code)
		}
	}
	for _, p := range []string{"/security/waf", "/security/f2b", "/security/unban"} {
		if w := do(h, "POST", p, url.Values{"csrf": {"wrong"}}, cookie(c)); w.Code != 403 {
			t.Errorf("%s with a wrong token: %d", p, w.Code)
		}
	}
	if len(f.reqs) != n {
		t.Errorf("a refused request reached the helper: %v", f.reqs[n:])
	}
}

func TestKnownFailuresAreExplained(t *testing.T) {
	h, _, c, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
		if r.Op == "security.waf" {
			return ctl.Response{Output: "the OWASP Core Rule Set is not installed (/usr/share/modsecurity-crs/rules or x is missing)", Error: "exit status 2"}, nil
		}
		return secAnswers(secStatusJSON)(r)
	}, nil)
	body := followed(h, do(h, "POST", "/security/waf", url.Values{"csrf": {tok}, "mode": {"block"}}, cookie(c)), c).Body.String()
	if !strings.Contains(body, "OWASP CRS chưa có trên máy") {
		t.Errorf("missing explanation:\n%s", body)
	}
}
