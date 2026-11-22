package web

import (
	"net/url"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

func notes(f *fakeHelper) []ctl.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n []ctl.Request
	for _, r := range f.reqs {
		if r.Op == "audit.note" {
			n = append(n, r)
		}
	}
	return n
}

// Signing in, a wrong password and signing out are told to the helper with the address; a wrong name is not kept as typed, because
// it is sometimes the password.
func TestSignInsGoToTheLog(t *testing.T) {
	f := &fakeHelper{answer: okAnswer("")}
	s := newServer()
	s.Call = f.call
	h := s.Handler()
	do(h, "POST", "/login", url.Values{"user": {"MyS3cretPass"}, "password": {"x"}}, addr("203.0.113.9:1"))
	c := cookieNamed(login(h, goodPW), "mpn")
	n := notes(f)
	if len(n) != 2 || n[0].Event != "login.fail" || !n[0].Failed || n[0].Actor != "203.0.113.9" || n[0].Note != "user=other" ||
		n[1].Event != "login.ok" || n[1].Failed {
		t.Fatalf("%+v", n)
	}
	m := csrfRe.FindStringSubmatch(do(h, "GET", "/", nil, cookie(c)).Body.String())
	do(h, "POST", "/logout", url.Values{"csrf": {m[1]}}, cookie(c))
	if n = notes(f); n[len(n)-1].Event != "logout" {
		t.Errorf("%+v", n)
	}
}

// Every change sent to the helper carries the address of the person.
func TestChangesCarryTheAddress(t *testing.T) {
	h, f, c, tok := setup(t, okAnswer(""))
	do(h, "POST", "/databases/add", url.Values{"csrf": {tok}, "site": {"blog"}, "name": {"blog_shop"}}, cookie(c))
	seen := false
	for _, r := range f.reqs {
		if r.Op == "db.add" {
			seen = true
			if r.Actor != "192.0.2.1" {
				t.Errorf("no address: %+v", r)
			}
		}
	}
	if !seen {
		t.Error("db.add was not sent")
	}
}

func lastList(f *fakeHelper, from int) *ctl.Request {
	var asked *ctl.Request
	for i := from; i < len(f.reqs); i++ {
		if f.reqs[i].Op == "audit.list" {
			asked = &f.reqs[i]
		}
	}
	return asked
}

func TestTheActivityPageShowsTheLogAndExportsIt(t *testing.T) {
	log := `{"entries":[{"id":1,"at":1763600000,"ip":"1.1.1.1","site":"blog","op":"db.add","kind":"database","detail":"name=blog_<b>","ok":true},` +
		`{"id":2,"at":1763600100,"ip":"1.1.1.1","site":"blog","op":"db.remove","kind":"database","detail":"name=blog_x","ok":true,"danger":true}],"total":2}`
	h, f, c, _ := setup(t, func(r ctl.Request) (ctl.Response, error) {
		if r.Op == "audit.list" {
			return ctl.Response{OK: true, Output: log}, nil
		}
		if r.Op == "site.list" {
			return ctl.Response{OK: true, Output: `[{"name":"blog","kind":"php","user":"blog","hostnames":["blog.example.com"]}]`}, nil
		}
		return ctl.Response{OK: true, Output: "[]"}, nil
	})
	body := do(h, "GET", "/activity?site=blog&kind=database&ip=1.1.", nil, cookie(c)).Body.String()
	for _, w := range []string{"Tạo cơ sở dữ liệu", "Xóa cơ sở dữ liệu", "name=blog_&lt;b&gt;", "1.1.1.1", "Quan trọng"} {
		if !strings.Contains(body, w) {
			t.Errorf("page lacks %q", w)
		}
	}
	asked := lastList(f, 0)
	if asked == nil || asked.Site != "blog" || asked.AuditKind != "database" || asked.AuditIP != "1.1." || asked.Limit != auditPageSize {
		t.Fatalf("%+v", asked)
	}
	w := do(h, "GET", "/activity.csv?site=blog", nil, cookie(c))
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") || !strings.Contains(w.Body.String(), "db.remove") ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "mpn-activity-blog-") {
		t.Errorf("csv: %v\n%s", w.Header(), w.Body)
	}
	// The tab of a website asks only for that website, whatever the address says.
	n := len(f.reqs)
	do(h, "GET", "/sites/blog/activity?site=shop", nil, cookie(c))
	if r := lastList(f, n); r == nil || r.Site != "blog" {
		t.Errorf("tab asked %+v", r)
	}
	// A filter that is not valid never reaches the helper.
	n = len(f.reqs)
	do(h, "GET", "/activity?ip=1.1.1.1%20-db=/x&kind=nope&from=yesterday", nil, cookie(c))
	if r := lastList(f, n); r == nil || r.AuditIP != "" || r.AuditKind != "" || r.From != "" {
		t.Errorf("bad filter passed on: %+v", r)
	}
}
