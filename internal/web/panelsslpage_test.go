package web

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

const pSSLSelfSigned = `{"mode":"selfsigned","domain":"","issuer":"mpn-panel","names":[],"not_before":0,"not_after":0,"covers":false,"trusted":false,"panel_port":2610,"pma_port":2611}`
const pSSLLE = `{"mode":"letsencrypt","domain":"panel.example.com","issuer":"Let's Encrypt","names":["panel.example.com"],"not_before":1700000000,"not_after":4102444800,"covers":true,"trusted":true,"panel_port":2610,"pma_port":2611}`

func pSSLAnswers(info string) func(ctl.Request) (ctl.Response, error) {
	return byOp(map[string]string{"security.status": secStatusJSON, "panel.sslinfo": info, "panel.ssl": "ok", "panel.sslcustom": "ok", "panel.ssloff": "ok"})
}

func TestTheCertificateCardShowsBothPortsOfALetsEncryptPanel(t *testing.T) {
	h, _, c, _ := setupWith(t, pSSLAnswers(pSSLLE), nil)
	body := do(h, "GET", "/security", nil, cookie(c)).Body.String()
	for _, want := range []string{"Chứng chỉ SSL của panel", "https://panel.example.com:2610", "https://panel.example.com:2611", "Let&#39;s Encrypt",
		`action="/security/panel-ssl/letsencrypt"`, `action="/security/panel-ssl/custom"`, `action="/security/panel-ssl/off"`, "Dùng chứng chỉ tự ký"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestASelfSignedPanelOffersToGetACertificateAndHasNothingToRemove(t *testing.T) {
	h, _, c, _ := setupWith(t, pSSLAnswers(pSSLSelfSigned), nil)
	body := do(h, "GET", "/security", nil, cookie(c)).Body.String()
	if !strings.Contains(body, "chứng chỉ tự ký") || !strings.Contains(body, `action="/security/panel-ssl/letsencrypt"`) || strings.Contains(body, `action="/security/panel-ssl/off"`) {
		t.Errorf("self-signed card:\n%s", body)
	}
	if strings.Contains(body, "https://:") {
		t.Error("no address is shown before a name is set")
	}
}

func TestGettingACertificateSendsTheNameToTheHelper(t *testing.T) {
	h, f, c, tok := setupWith(t, pSSLAnswers(pSSLSelfSigned), nil)
	w := followed(h, do(h, "POST", "/security/panel-ssl/letsencrypt", url.Values{"csrf": {tok}, "domain": {"  Panel.Example.com "}}, cookie(c)), c)
	req, ok := lastOp(f, "panel.ssl")
	if !ok || req.Domain != "panel.example.com" {
		t.Fatalf("%+v\n%s", req, w.Body.String())
	}
	if backOf(t, w.Body.String()) != "/security" {
		t.Errorf("back: %s", w.Body.String())
	}
}

func TestABadNameNeverReachesTheHelper(t *testing.T) {
	h, f, c, tok := setupWith(t, pSSLAnswers(pSSLSelfSigned), nil)
	for _, d := range []string{"", "203.0.113.7", "localhost", "panel.example.com; reboot", "a b.com"} {
		for _, path := range []string{"/security/panel-ssl/letsencrypt", "/security/panel-ssl/custom"} {
			form := url.Values{"csrf": {tok}, "domain": {d}, "cert": {"-----BEGIN CERTIFICATE-----"}, "key": {"-----BEGIN PRIVATE KEY-----"}}
			if w := do(h, "POST", path, form, cookie(c)); w.Code != 400 || !strings.Contains(w.Body.String(), "tên miền") {
				t.Errorf("%q to %s: %d", d, path, w.Code)
			}
		}
	}
	if len(f.reqs) != 0 {
		t.Errorf("a refused request reached the helper: %+v", f.reqs)
	}
}

func TestACustomCertificateNeedsItsKeyAndIsSentWithIt(t *testing.T) {
	h, f, c, tok := setupWith(t, pSSLAnswers(pSSLSelfSigned), nil)
	form := url.Values{"csrf": {tok}, "domain": {"panel.example.com"}, "cert": {"-----BEGIN CERTIFICATE-----\nx\n-----END CERTIFICATE-----"}}
	if w := do(h, "POST", "/security/panel-ssl/custom", form, cookie(c)); w.Code != 400 || !strings.Contains(w.Body.String(), "khóa riêng") {
		t.Errorf("no key: %d", w.Code)
	}
	for name, mutate := range map[string]func(url.Values){
		"a key that is not PEM": func(v url.Values) { v.Set("key", "hello") },
		"a passphrase key":      func(v url.Values) { v.Set("key", "-----BEGIN ENCRYPTED PRIVATE KEY-----") },
		"a cert that is not":    func(v url.Values) { v.Set("key", "-----BEGIN PRIVATE KEY-----"); v.Set("cert", "hello") },
	} {
		v := url.Values{"csrf": {tok}, "domain": {"panel.example.com"}, "cert": {form.Get("cert")}}
		mutate(v)
		if w := do(h, "POST", "/security/panel-ssl/custom", v, cookie(c)); w.Code != 400 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if len(f.reqs) != 0 {
		t.Fatalf("refused forms reached the helper: %+v", f.reqs)
	}
	form.Set("key", "-----BEGIN PRIVATE KEY-----\nk\n-----END PRIVATE KEY-----")
	form.Set("allow_untrusted", "1")
	followed(h, do(h, "POST", "/security/panel-ssl/custom", form, cookie(c)), c)
	req, ok := lastOp(f, "panel.sslcustom")
	if !ok || req.Domain != "panel.example.com" || !strings.Contains(req.Key, "PRIVATE KEY") || !req.AllowUntrusted {
		t.Errorf("%+v", req)
	}
}

func TestGoingBackToSelfSignedAndTheTokenRule(t *testing.T) {
	h, f, c, tok := setupWith(t, pSSLAnswers(pSSLLE), nil)
	followed(h, do(h, "POST", "/security/panel-ssl/off", url.Values{"csrf": {tok}}, cookie(c)), c)
	if _, ok := lastOp(f, "panel.ssloff"); !ok {
		t.Error("the helper was not asked")
	}
	n := len(f.reqs)
	for _, p := range []string{"/security/panel-ssl/letsencrypt", "/security/panel-ssl/custom", "/security/panel-ssl/off"} {
		if w := do(h, "POST", p, url.Values{"csrf": {"wrong"}, "domain": {"panel.example.com"}}, cookie(c)); w.Code != 403 {
			t.Errorf("%s with a wrong token: %d", p, w.Code)
		}
		if w := do(h, "POST", p, url.Values{"domain": {"panel.example.com"}}); w.Code != 303 {
			t.Errorf("%s without a session: %d", p, w.Code)
		}
	}
	if len(f.reqs) != n {
		t.Errorf("a refused request reached the helper: %v", f.reqs[n:])
	}
}

func TestKnownCertificateFailuresAreExplained(t *testing.T) {
	for output, want := range map[string]string{
		"site-name: panel.example.com is the address of the website \"blog\"":   "tên miền này đang là địa chỉ của một website",
		"dns-check-failed: panel.example.com: the name has no A or AAAA record": "chưa trỏ về máy chủ này",
	} {
		h, _, c, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
			if r.Op == "panel.ssl" {
				return ctl.Response{Output: output, Error: "exit status 2"}, nil
			}
			return pSSLAnswers(pSSLSelfSigned)(r)
		}, nil)
		body := followed(h, do(h, "POST", "/security/panel-ssl/letsencrypt", url.Values{"csrf": {tok}, "domain": {"panel.example.com"}}, cookie(c)), c).Body.String()
		if !strings.Contains(strings.ToLower(body), want) {
			t.Errorf("%q: missing %q in\n%s", output, want, body)
		}
	}
}

// A name added by the SSL page is accepted without restarting the panel; an IP address and the names it started with still are.
func TestTheHostsFileAddsNamesWhileThePanelRuns(t *testing.T) {
	file := filepath.Join(t.TempDir(), "panel-hosts")
	s := &Server{Hosts: []string{"first.example.com"}, HostsFile: file}
	if s.hostOK("panel.example.com:2610") {
		t.Error("a name nobody listed must be refused")
	}
	if err := os.WriteFile(file, []byte("panel.example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"panel.example.com", "PANEL.example.com:2610", "panel.example.com:2611", "first.example.com:2610", "203.0.113.7:2610", "localhost"} {
		if !s.hostOK(h) {
			t.Errorf("%q must be accepted", h)
		}
	}
	if s.hostOK("evil.example.com:2610") || s.hostOK("panel.example.com.evil.com") {
		t.Error("another name must be refused")
	}
	if err := os.WriteFile(file, []byte("other.example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !s.hostOK("other.example.com") {
		t.Error("a changed file is read again")
	}
	_ = os.Remove(file)
	if s.hostOK("other.example.com") || !s.hostOK("first.example.com") {
		t.Error("removing the file takes back the added names only")
	}
}
