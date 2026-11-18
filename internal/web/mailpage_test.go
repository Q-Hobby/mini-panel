package web

import (
	"net/url"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

const mailOnJSON = `{"on":true,"host":"mail.example.com","installed":true,"services":{"postfix":true,"dovecot":true,"opendkim":true},"ports":[25,465,587,993],` +
	`"domains":[{"name":"example.com","has_key":true,"mailboxes":1,"aliases":1,"records":[` +
	`{"type":"MX","name":"example.com","value":"10 mail.example.com."},` +
	`{"type":"TXT","name":"example.com","value":"v=spf1 mx -all"},` +
	`{"type":"TXT","name":"mpn._domainkey.example.com","value":"v=DKIM1; k=rsa; p=AAAA"}]}],` +
	`"mailboxes":[{"address":"ten@example.com","quota_mb":500}],"aliases":[{"address":"info@example.com","targets":["ten@example.com","a@b.org"]}]}`

const mailOffJSON = `{"on":false,"host":"","installed":false,"services":{},"ports":[25,465,587,993],"domains":[],"mailboxes":[],"aliases":[]}`

func mailAnswers(status string) func(ctl.Request) (ctl.Response, error) {
	ok := map[string]string{"mail.status": status}
	for _, op := range []string{"enable", "disable", "domainadd", "domaindel", "boxadd", "boxdel", "boxpasswd", "boxquota", "aliasset", "aliasdel"} {
		ok["mail."+op] = "ok"
	}
	return byOp(ok)
}

func TestTheEmailPageOnAMachineWithMailOff(t *testing.T) {
	h, _, c, _ := setupWith(t, mailAnswers(mailOffJSON), nil)
	body := do(h, "GET", "/email", nil, cookie(c)).Body.String()
	for _, want := range []string{`href="/email" class="nav-link" aria-current="page"`, `action="/email/enable"`, `name="host"`, "Bật máy chủ thư", "25, 465, 587, 993"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, not := range []string{`action="/email/box/add"`, `action="/email/domain/add"`, `action="/email/disable"`} {
		if strings.Contains(body, not) {
			t.Errorf("nothing to manage while mail is off: %q", not)
		}
	}
}

func TestTheEmailPageKeepsOnlyWhatBelongsToTheServer(t *testing.T) {
	h, _, c, _ := setupWith(t, mailAnswers(mailOnJSON), nil)
	body := do(h, "GET", "/email", nil, cookie(c)).Body.String()
	for _, want := range []string{"mail.example.com", `action="/email/disable"`, `action="/email/ssl"`, `action="/email/webmail"`, "Tên miền, hộp thư và bí danh"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	// domains, mailboxes and aliases are managed in the tab of each website, as in cPanel
	for _, not := range []string{`action="/email/domain/add"`, `action="/email/box/add"`, `action="/email/alias/set"`, "ten@example.com", "v=spf1 mx -all"} {
		if strings.Contains(body, not) {
			t.Errorf("the Email page must not carry %q", not)
		}
	}
	if strings.Contains(body, "ARGON2ID") || strings.Contains(body, "PRIVATE KEY") {
		t.Error("a secret reached the page")
	}
}

// siteMailAnswers is a machine with mail on, whose one website (shop) has the name example.com, which the mail server knows.
func siteMailAnswers(cloudflare bool) func(ctl.Request) (ctl.Response, error) {
	inner := cfAnswers(cloudflare, okReport)
	return func(r ctl.Request) (ctl.Response, error) {
		if r.Op == "site.list" {
			return ctl.Response{OK: true, Output: siteListJSON("", "", false)}, nil
		}
		return inner(r)
	}
}

func TestTheEmailTabOfASiteShowsOnlyItsOwnDomain(t *testing.T) {
	h, _, c, _ := setupWith(t, siteMailAnswers(false), nil)
	body := do(h, "GET", "/sites/shop/email", nil, cookie(c)).Body.String()
	for _, want := range []string{`href="/sites/shop/email"`, `action="/sites/shop/email/box/add"`, `action="/sites/shop/email/alias/set"`, `action="/sites/shop/email/domain/delete"`,
		`action="/sites/shop/email/box/passwd"`, `action="/sites/shop/email/box/delete"`, `action="/sites/shop/email/alias/delete"`,
		"ten@example.com", "500 MB", "info@example.com", "a@b.org", "10 mail.example.com.", "v=spf1 mx -all", "mpn._domainkey.example.com"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, `action="/email/`) {
		t.Error("the forms of a site return to the site, not to the Email page")
	}
	if strings.Contains(body, "/email/cf") {
		t.Error("a domain elsewhere has no Cloudflare button")
	}
}

func TestTheEmailTabOfASiteOffersToEnableADomainThatIsNotSetUp(t *testing.T) {
	other := strings.Replace(mailOnJSON, `"name":"example.com"`, `"name":"other.org"`, 1)
	h, _, c, _ := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
		switch r.Op {
		case "site.list":
			return ctl.Response{OK: true, Output: siteListJSON("", "", false)}, nil
		case "mail.status":
			return ctl.Response{OK: true, Output: other}, nil
		}
		return ctl.Response{OK: true}, nil
	}, nil)
	body := do(h, "GET", "/sites/shop/email", nil, cookie(c)).Body.String()
	if !strings.Contains(body, `action="/sites/shop/email/domain/add"`) || strings.Contains(body, "other.org") {
		t.Errorf("the domain of the site is offered, and the domain of another is not shown:\n%s", body)
	}
}

func TestTheEmailTabOfASiteWithMailOffSendsToTheEmailPage(t *testing.T) {
	h, _, c, _ := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
		switch r.Op {
		case "site.list":
			return ctl.Response{OK: true, Output: siteListJSON("", "", false)}, nil
		case "mail.status":
			return ctl.Response{OK: true, Output: mailOffJSON}, nil
		}
		return ctl.Response{OK: true}, nil
	}, nil)
	body := do(h, "GET", "/sites/shop/email", nil, cookie(c)).Body.String()
	if !strings.Contains(body, `href="/email"`) || strings.Contains(body, `/email/box/add`) {
		t.Error("with mail off the tab points to the Email page and has no forms")
	}
}

func TestASiteCannotChangeTheMailOfADomainThatIsNotItsOwn(t *testing.T) {
	h, f, c, tok := setupWith(t, siteMailAnswers(false), nil)
	n := len(f.reqs)
	for path, form := range map[string]url.Values{
		"/sites/shop/email/box/add":       {"csrf": {tok}, "local": {"x"}, "domain": {"other.org"}, "password": {"0123456789abc"}},
		"/sites/shop/email/box/delete":    {"csrf": {tok}, "address": {"x@other.org"}},
		"/sites/shop/email/box/passwd":    {"csrf": {tok}, "address": {"x@other.org"}, "password": {"0123456789abc"}},
		"/sites/shop/email/alias/set":     {"csrf": {tok}, "local": {"info"}, "domain": {"other.org"}, "targets": {"a@b.org"}},
		"/sites/shop/email/alias/delete":  {"csrf": {tok}, "address": {"info@other.org"}},
		"/sites/shop/email/domain/add":    {"csrf": {tok}, "domain": {"other.org"}},
		"/sites/shop/email/domain/delete": {"csrf": {tok}, "domain": {"other.org"}},
		"/sites/shop/email/box/quota":     {"csrf": {tok}, "address": {"x@other.org"}, "quota": {"5"}},
	} {
		followed(h, do(h, "POST", path, form, cookie(c)), c)
	}
	for _, r := range f.reqs[n:] {
		if strings.HasPrefix(r.Op, "mail.") && r.Op != "mail.status" && r.Op != "mail.cfdetect" {
			t.Errorf("a domain of another site reached the helper: %+v", r)
		}
	}
}

func TestAnActionOnTheTabOfASiteComesBackToTheTab(t *testing.T) {
	h, f, c, tok := setupWith(t, siteMailAnswers(false), nil)
	w := do(h, "POST", "/sites/shop/email/box/add", url.Values{"csrf": {tok}, "local": {"ten"}, "domain": {"example.com"}, "password": {"0123456789abc"}}, cookie(c))
	page := followed(h, w, c)
	req, ok := lastOp(f, "mail.boxadd")
	if !ok || req.MailAddress != "ten@example.com" {
		t.Fatalf("%+v", req)
	}
	if body := page.Body.String(); !strings.Contains(body, `href="/sites/shop/email"`) {
		t.Errorf("the result must lead back to the tab of the site:\n%s", body)
	}
}

func TestAServerThatIsStoppedIsSaid(t *testing.T) {
	stopped := strings.Replace(mailOnJSON, `"dovecot":true`, `"dovecot":false`, 1)
	h, _, c, _ := setupWith(t, mailAnswers(stopped), nil)
	body := do(h, "GET", "/email", nil, cookie(c)).Body.String()
	if !strings.Contains(body, "có dịch vụ không chạy") {
		t.Errorf("a stopped service must be said:\n%s", body)
	}
}

func TestEnablingMailSendsTheCheckedHost(t *testing.T) {
	h, f, c, tok := setupWith(t, mailAnswers(mailOffJSON), nil)
	followed(h, do(h, "POST", "/email/enable", url.Values{"csrf": {tok}, "host": {"  Mail.Example.com "}}, cookie(c)), c)
	req, ok := lastOp(f, "mail.enable")
	if !ok || req.MailHost != "mail.example.com" {
		t.Fatalf("%+v", req)
	}
}

func TestBadValuesNeverReachTheHelper(t *testing.T) {
	h, f, c, tok := setupWith(t, mailAnswers(mailOnJSON), nil)
	n := len(f.reqs)
	forms := map[string]url.Values{
		"/email/enable":        {"host": {"localhost"}},
		"/email/domain/add":    {"domain": {"a b.com"}},
		"/email/domain/delete": {"domain": {"-apply"}},
		"/email/box/add":       {"local": {"../x"}, "domain": {"example.com"}, "password": {"correct horse battery"}},
		"/email/box/add ":      {"local": {"ten"}, "domain": {"example.com"}, "password": {"short"}},
		"/email/box/add  ":     {"local": {"ten"}, "domain": {"example.com"}, "password": {"correct horse battery"}, "quota": {"-5"}},
		"/email/box/passwd":    {"address": {"ten@example.com"}, "password": {"line\nbreak12345"}},
		"/email/box/delete":    {"address": {"nobody"}},
		"/email/alias/set":     {"local": {"info"}, "domain": {"example.com"}, "targets": {"not-an-address"}},
		"/email/alias/set ":    {"local": {"info"}, "domain": {"example.com"}, "targets": {""}},
	}
	for path, form := range forms {
		form.Set("csrf", tok)
		if w := do(h, "POST", strings.TrimSpace(path), form, cookie(c)); w.Code != 400 {
			t.Errorf("%q %v: code %d", path, form, w.Code)
		}
	}
	if len(f.reqs) != n {
		t.Errorf("a refused request reached the helper: %v", f.reqs[n:])
	}
}

func TestAMailboxIsMadeFromTheTwoBoxesAndTheQuota(t *testing.T) {
	h, f, c, tok := setupWith(t, mailAnswers(mailOnJSON), nil)
	followed(h, do(h, "POST", "/email/box/add", url.Values{"csrf": {tok}, "local": {"Ten"}, "domain": {"example.com"}, "password": {"correct horse battery"}, "quota": {"250"}}, cookie(c)), c)
	req, ok := lastOp(f, "mail.boxadd")
	if !ok || req.MailAddress != "ten@example.com" || req.MailQuota != 250 || req.MailPassword != "correct horse battery" {
		t.Fatalf("%+v", req)
	}
	followed(h, do(h, "POST", "/email/box/add", url.Values{"csrf": {tok}, "local": {"b"}, "domain": {"example.com"}, "password": {"correct horse battery"}}, cookie(c)), c)
	if req, _ := lastOp(f, "mail.boxadd"); req.MailQuota != 0 {
		t.Errorf("an empty size box means no limit: %+v", req)
	}
}

func TestAnAliasTakesTargetsFromLinesAndCommas(t *testing.T) {
	h, f, c, tok := setupWith(t, mailAnswers(mailOnJSON), nil)
	followed(h, do(h, "POST", "/email/alias/set", url.Values{"csrf": {tok}, "local": {"info"}, "domain": {"example.com"}, "targets": {"a@b.org\r\nc@d.org, e@f.org"}}, cookie(c)), c)
	req, ok := lastOp(f, "mail.aliasset")
	if !ok || req.MailAddress != "info@example.com" || strings.Join(req.MailTargets, " ") != "a@b.org c@d.org e@f.org" {
		t.Fatalf("%+v", req)
	}
}

func TestPurgeIsSentOnlyWhenTicked(t *testing.T) {
	h, f, c, tok := setupWith(t, mailAnswers(mailOnJSON), nil)
	followed(h, do(h, "POST", "/email/box/delete", url.Values{"csrf": {tok}, "address": {"ten@example.com"}}, cookie(c)), c)
	if req, _ := lastOp(f, "mail.boxdel"); req.Purge {
		t.Error("stored mail must not be deleted unless the person ticked the box")
	}
	followed(h, do(h, "POST", "/email/box/delete", url.Values{"csrf": {tok}, "address": {"ten@example.com"}, "purge": {"1"}}, cookie(c)), c)
	if req, _ := lastOp(f, "mail.boxdel"); !req.Purge {
		t.Error("ticked, so the stored mail goes too")
	}
}

func TestTheCertificateOfTheMailHostIsShownAndRequested(t *testing.T) {
	self := strings.Replace(mailOnJSON, `"ports":[25,465,587,993],`, `"ports":[25,465,587,993],"cert":{"mode":"selfsigned","days":80,"issuer":"mail.example.com","covers":true},`, 1)
	h, f, c, tok := setupWith(t, mailAnswers(self), nil)
	body := do(h, "GET", "/email", nil, cookie(c)).Body.String()
	for _, want := range []string{`action="/email/ssl"`, "Tự ký", "Lấy chứng chỉ Let&#39;s Encrypt"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	followed(h, do(h, "POST", "/email/ssl", url.Values{"csrf": {tok}}, cookie(c)), c)
	if _, ok := lastOp(f, "mail.ssl"); !ok {
		t.Error("the button must ask the helper for the certificate")
	}
	le := strings.Replace(mailOnJSON, `"ports":[25,465,587,993],`, `"ports":[25,465,587,993],"cert":{"mode":"letsencrypt","days":71,"issuer":"R11","covers":true},`, 1)
	h, _, c, _ = setupWith(t, mailAnswers(le), nil)
	body = do(h, "GET", "/email", nil, cookie(c)).Body.String()
	if !strings.Contains(body, "Còn 71 ngày") || !strings.Contains(body, "Cấp lại ngay") {
		t.Errorf("a Let's Encrypt certificate shows its days and a renew button:\n%s", body)
	}
}

func TestTheWebMailCardAndItsSwitch(t *testing.T) {
	off := strings.Replace(mailOnJSON, `"ports":[25,465,587,993],`, `"ports":[25,465,587,993],"webmail":{"on":false,"installed":false,"active":false,"port":2612},`, 1)
	h, f, c, tok := setupWith(t, mailAnswers(off), nil)
	body := do(h, "GET", "/email", nil, cookie(c)).Body.String()
	if !strings.Contains(body, `action="/email/webmail"`) || !strings.Contains(body, "Bật web mail") {
		t.Errorf("a switch to turn it on:\n%s", body)
	}
	followed(h, do(h, "POST", "/email/webmail", url.Values{"csrf": {tok}, "on": {"1"}}, cookie(c)), c)
	if req, ok := lastOp(f, "mail.webmail"); !ok || !req.Enable {
		t.Errorf("%+v", req)
	}
	followed(h, do(h, "POST", "/email/webmail", url.Values{"csrf": {tok}, "on": {"0"}}, cookie(c)), c)
	if req, _ := lastOp(f, "mail.webmail"); req.Enable {
		t.Error("the off button must turn it off")
	}
	on := strings.Replace(mailOnJSON, `"ports":[25,465,587,993],`, `"ports":[25,465,587,993],"webmail":{"on":true,"installed":true,"active":true,"port":2612},`, 1)
	h, _, c, _ = setupWith(t, mailAnswers(on), nil)
	body = do(h, "GET", "/email", nil, cookie(c), host("localhost:2610")).Body.String()
	if !strings.Contains(body, `href="https://localhost:2612"`) || !strings.Contains(body, "Tắt web mail") {
		t.Errorf("the address follows the name the page was opened with:\n%s", body)
	}
}

func TestEveryEmailActionNeedsTheCSRFToken(t *testing.T) {
	h, f, c, _ := setupWith(t, mailAnswers(mailOnJSON), nil)
	n := len(f.reqs)
	for _, p := range []string{"enable", "disable", "ssl", "webmail", "domain/add", "domain/delete", "box/add", "box/passwd", "box/quota", "box/delete", "alias/set", "alias/delete"} {
		if w := do(h, "POST", "/email/"+p, url.Values{"csrf": {"wrong"}}, cookie(c)); w.Code != 403 {
			t.Errorf("/email/%s with a wrong token: %d", p, w.Code)
		}
	}
	if len(f.reqs) != n {
		t.Errorf("a refused request reached the helper: %v", f.reqs[n:])
	}
}

func TestKnownEmailFailuresAreExplained(t *testing.T) {
	h, _, c, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
		if r.Op == "mail.enable" {
			return ctl.Response{Output: "mail-conflict: exim4 is already running on port 25; remove it first", Error: "exit status 2"}, nil
		}
		return mailAnswers(mailOffJSON)(r)
	}, nil)
	body := followed(h, do(h, "POST", "/email/enable", url.Values{"csrf": {tok}, "host": {"mail.example.com"}}, cookie(c)), c).Body.String()
	if !strings.Contains(body, "đang chiếm cổng 25") {
		t.Errorf("missing explanation:\n%s", body)
	}
}
