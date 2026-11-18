package web

import (
	"net/url"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

const cfToken = "tokentokentokentokentoken123456"

const cfReportJSON = `{"zone":"example.com","host_ip":"203.0.113.7","results":[` +
	`{"type":"A","name":"mail.example.com","status":"added"},` +
	`{"type":"MX","name":"example.com","status":"exists"},` +
	`{"type":"TXT","name":"example.com","status":"conflict","note":"another SPF record is there"},` +
	`{"type":"TXT","name":"mpn._domainkey.example.com","status":"added"}]}`

// cfAnswers is a mail server with one domain, a name server check that says cloudflare, and an add that answers add.
func cfAnswers(cloudflare bool, add func(ctl.Request) (ctl.Response, error)) func(ctl.Request) (ctl.Response, error) {
	det := `{"cloudflare":false}`
	if cloudflare {
		det = `{"cloudflare":true,"zone":"example.com"}`
	}
	return func(r ctl.Request) (ctl.Response, error) {
		switch r.Op {
		case "mail.status":
			return ctl.Response{OK: true, Output: mailOnJSON}, nil
		case "mail.cfdetect":
			return ctl.Response{OK: true, Output: det}, nil
		case "mail.cfadd":
			return add(r)
		}
		return ctl.Response{OK: true}, nil
	}
}

func okReport(ctl.Request) (ctl.Response, error) {
	return ctl.Response{OK: true, Output: cfReportJSON}, nil
}

func TestTheButtonShowsOnlyForADomainAtCloudflareAndTheAnswerIsRemembered(t *testing.T) {
	h, f, c, _ := setupWith(t, siteMailAnswers(true), nil)
	body := do(h, "GET", "/sites/shop/email", nil, cookie(c)).Body.String()
	if !strings.Contains(body, `href="/sites/shop/email/cf?domain=example.com"`) || !strings.Contains(body, "Thêm DNS qua Cloudflare") {
		t.Error("a domain at Cloudflare has the button")
	}
	do(h, "GET", "/sites/shop/email", nil, cookie(c))
	n := 0
	for _, op := range f.ops() {
		if op == "mail.cfdetect" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the name servers are asked once and remembered for a while: %d times", n)
	}

	h2, _, c2, _ := setupWith(t, siteMailAnswers(false), nil)
	if body := do(h2, "GET", "/sites/shop/email", nil, cookie(c2)).Body.String(); strings.Contains(body, "/email/cf") {
		t.Error("a domain elsewhere has no button")
	}
}

func TestMailOffAsksNothingAboutCloudflare(t *testing.T) {
	h, f, c, _ := setupWith(t, mailAnswers(mailOffJSON), nil)
	do(h, "GET", "/email", nil, cookie(c))
	for _, op := range f.ops() {
		if op == "mail.cfdetect" {
			t.Error("with mail off there are no domains to ask about")
		}
	}
}

func TestThePageThatAsksForTheTokenShowsTheRecordsAndTheSteps(t *testing.T) {
	h, _, c, _ := setupWith(t, cfAnswers(true, okReport), nil)
	body := do(h, "GET", "/email/cf?domain=example.com", nil, cookie(c)).Body.String()
	for _, want := range []string{`action="/email/cf"`, `name="token"`, `type="password"`, `name="domain" value="example.com"`, "v=spf1 mx -all",
		"mpn._domainkey.example.com", "https://dash.cloudflare.com/profile/api-tokens", "Edit zone DNS", "example.com", "không được lưu"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, bad := range []string{"/email/cf?domain=", "/email/cf?domain=nosuch.org", "/email/cf?domain=a%20b", "/email/cf?domain=x;rm"} {
		if w := do(h, "GET", bad, nil, cookie(c)); w.Code < 400 || strings.Contains(w.Body.String(), `name="token"`) {
			t.Errorf("%s must be refused, got %d", bad, w.Code)
		}
	}
}

func TestATokenThatIsNotATokenNeverReachesTheHelperAndIsNotShownAgain(t *testing.T) {
	h, f, c, tok := setupWith(t, cfAnswers(true, okReport), nil)
	for _, bad := range []string{"", "short", "has a space in it 1234567890", strings.Repeat("a", 129), "line\nbreak1234567890123456"} {
		w := do(h, "POST", "/email/cf", url.Values{"csrf": {tok}, "domain": {"example.com"}, "token": {bad}}, cookie(c))
		if w.Code != 400 {
			t.Errorf("%q: %d", bad, w.Code)
		}
		if bad != "" && strings.Contains(w.Body.String(), bad) {
			t.Errorf("a token that was typed must not be put back in the page: %q", bad)
		}
	}
	for _, op := range f.ops() {
		if op == "mail.cfadd" {
			t.Fatal("a token of the wrong shape must not be sent on")
		}
	}
}

func TestAddingTheRecordsSendsTheTokenToTheHelperAndSaysWhatHappenedToEach(t *testing.T) {
	h, f, c, tok := setupWith(t, cfAnswers(true, okReport), nil)
	w := do(h, "POST", "/email/cf", url.Values{"csrf": {tok}, "domain": {"example.com"}, "token": {cfToken}}, cookie(c))
	page := followed(h, w, c).Body.String()
	var sent *ctl.Request
	for i := range f.reqs {
		if f.reqs[i].Op == "mail.cfadd" {
			sent = &f.reqs[i]
		}
	}
	if sent == nil || sent.MailDomain != "example.com" || sent.CFToken != cfToken {
		t.Fatalf("the helper gets the domain and the token: %+v", sent)
	}
	for _, want := range []string{"Zone: example.com", "Đã thêm", "Đã có sẵn", "Khác với bản ghi đang có", "mail.example.com", "mpn._domainkey.example.com", "không ghi đè"} {
		if !strings.Contains(page, want) {
			t.Errorf("the result lacks %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, cfToken) {
		t.Error("the token must not appear in the result")
	}
}

func TestEachKindOfFailureIsSaidInVietnameseAndNeverShowsTheHelpersText(t *testing.T) {
	for marker, want := range map[string]string{
		"cf-badtoken: cloudflare: the API token is not valid":             "không chấp nhận API token",
		"cf-noperm: cloudflare: the API token may not do this":            "không đủ quyền",
		"cf-nozone: cloudflare: the domain is not a zone of this account": "không thấy zone",
		"cf-ratelimit: cloudflare: too many requests":                     "giới hạn",
		"cf-network: cloudflare: the API could not be reached":            "Không kết nối được",
	} {
		marker, want := marker, want
		fail := func(ctl.Request) (ctl.Response, error) {
			return ctl.Response{OK: false, Error: "exit status 2", Output: marker}, nil
		}
		h, _, c, tok := setupWith(t, cfAnswers(true, fail), nil)
		w := do(h, "POST", "/email/cf", url.Values{"csrf": {tok}, "domain": {"example.com"}, "token": {cfToken}}, cookie(c))
		page := followed(h, w, c).Body.String()
		if !strings.Contains(page, want) {
			t.Errorf("%s: the page should say %q:\n%s", marker, want, page)
		}
		if strings.Contains(page, "cloudflare: the") || strings.Contains(page, "exit status") {
			t.Errorf("%s: the helper's own text is for the command line", marker)
		}
	}
}
