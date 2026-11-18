package web

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"mpn/internal/ctl"
)

const (
	pmaTestUser = "pma_rdwxyz_ab12cd34"
	pmaTestPass = "Zk3hQ8vNp2LwXr7TsB4mYcJd"
)

var pmaKey = []byte(strings.Repeat("k", 40))

func pmaAnswers() func(ctl.Request) (ctl.Response, error) {
	return byOp(map[string]string{
		"db.list":    `[{"name":"blog","site":"blog"}]`,
		"site.list":  blogList,
		"db.pma":     `{"user":"` + pmaTestUser + `","password":"` + pmaTestPass + `","database":"blog","expires":1893456000}`,
		"db.pmadrop": "dropped",
	})
}

func withPMA(s *Server) { s.PMAPort, s.PMAKey = 2611, pmaKey }

var ticketRe = regexp.MustCompile(`signon\.php\?t=([A-Za-z0-9_-]+)`)

func TestOpeningTheBrowserLeadsToItsOwnPortWithATicketAndNoPassword(t *testing.T) {
	h, f, c, tok := setupWith(t, pmaAnswers(), withPMA)
	w := do(h, "POST", "/databases/pma", url.Values{"csrf": {tok}, "name": {"blog"}}, cookie(c))
	if w.Code != 200 {
		t.Fatalf("%d\n%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	m := ticketRe.FindStringSubmatch(body)
	if m == nil || len(m[1]) < 40 {
		t.Fatalf("no ticket in the link:\n%s", body)
	}
	if !strings.Contains(body, `href="https://localhost:2611/signon.php?t=`+m[1]+`"`) {
		t.Errorf("the link must go to the browser's own port:\n%s", body)
	}
	for _, secret := range []string{pmaTestPass, pmaTestUser} {
		if strings.Contains(body, secret) {
			t.Errorf("%q must never reach the administrator's browser", secret)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("the page must not be cached")
	}
	if len(f.reqs) == 0 || f.reqs[len(f.reqs)-1].Op != "db.pma" || f.reqs[len(f.reqs)-1].Name != "blog" {
		t.Errorf("requests %+v", f.reqs)
	}
}

func TestATicketWorksOnceForTheRightKeyOnly(t *testing.T) {
	h, _, c, tok := setupWith(t, pmaAnswers(), withPMA)
	open := func() string {
		w := do(h, "POST", "/databases/pma", url.Values{"csrf": {tok}, "name": {"blog"}}, cookie(c))
		return ticketRe.FindStringSubmatch(w.Body.String())[1]
	}
	key := header("X-Mpn-Key", string(pmaKey))
	id := open()
	if w := do(h, "GET", "/_pma/ticket?t="+id, nil); w.Code != 404 {
		t.Errorf("no key: %d", w.Code)
	}
	if w := do(h, "GET", "/_pma/ticket?t="+id, nil, header("X-Mpn-Key", strings.Repeat("x", 40))); w.Code != 404 {
		t.Errorf("wrong key: %d", w.Code)
	}
	if w := do(h, "GET", "/_pma/ticket?t="+id, nil, header("X-Mpn-Key", string(pmaKey[:39]))); w.Code != 404 {
		t.Errorf("a prefix of the key: %d", w.Code)
	}
	// the wrong tries must not have used the ticket up
	w := do(h, "GET", "/_pma/ticket?t="+id, nil, key)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"user":"`+pmaTestUser+`"`) || !strings.Contains(w.Body.String(), `"password":"`+pmaTestPass+`"`) || !strings.Contains(w.Body.String(), `"database":"blog"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("what is traded must not be cached")
	}
	if w := do(h, "GET", "/_pma/ticket?t="+id, nil, key); w.Code != 404 {
		t.Errorf("a ticket must work once: %d", w.Code)
	}
	for _, bad := range []string{"", "x", "../../etc", strings.Repeat("a", 43)} {
		if w := do(h, "GET", "/_pma/ticket?t="+url.QueryEscape(bad), nil, key); w.Code != 404 {
			t.Errorf("ticket %q: %d", bad, w.Code)
		}
	}
}

func TestATicketExpiresAfterAMinute(t *testing.T) {
	var tk pmaTickets
	now := time.Now()
	tk.put("a", pmaTicket{User: "u", Expires: now.Add(pmaTicketTTL)}, now)
	if _, ok := tk.take("a", now.Add(pmaTicketTTL+time.Second)); ok {
		t.Error("an old ticket must not be accepted")
	}
	tk.put("b", pmaTicket{User: "u", Expires: now.Add(pmaTicketTTL)}, now)
	if _, ok := tk.take("b", now.Add(pmaTicketTTL-time.Second)); !ok {
		t.Error("a fresh ticket must be accepted")
	}
}

func TestTheTicketStoreIsBoundedAndForgetsOldOnes(t *testing.T) {
	var tk pmaTickets
	now := time.Now()
	for i := 0; i < pmaMaxTicket; i++ {
		if !tk.put(string(rune('a'+i%26))+strings.Repeat("x", i), pmaTicket{Expires: now.Add(time.Minute)}, now) {
			t.Fatalf("ticket %d refused too early", i)
		}
	}
	if tk.put("one-too-many", pmaTicket{Expires: now.Add(time.Minute)}, now) {
		t.Error("the store must be bounded, or somebody signed in could fill the memory")
	}
	if !tk.put("later", pmaTicket{Expires: now.Add(3 * time.Minute)}, now.Add(2*time.Minute)) {
		t.Error("expired tickets must make room")
	}
}

func TestNginxAsksWhetherTheVisitorIsSignedIn(t *testing.T) {
	h, _, c, _ := setupWith(t, pmaAnswers(), withPMA)
	if w := do(h, "GET", "/_pma/auth", nil, cookie(c)); w.Code != 204 {
		t.Errorf("signed in: %d", w.Code)
	}
	if w := do(h, "GET", "/_pma/auth", nil); w.Code != 401 {
		t.Errorf("no cookie: %d", w.Code)
	}
	bad := *c
	bad.Value = "not-a-session"
	if w := do(h, "GET", "/_pma/auth", nil, cookie(&bad)); w.Code != 401 {
		t.Errorf("a made-up session: %d", w.Code)
	}
}

func TestSigningOutOfTheBrowserDropsOnlyATemporaryUser(t *testing.T) {
	h, f, _, _ := setupWith(t, pmaAnswers(), withPMA)
	key := header("X-Mpn-Key", string(pmaKey))
	if w := do(h, "GET", "/_pma/end?u="+pmaTestUser, nil, key); w.Code != 204 {
		t.Fatalf("%d", w.Code)
	}
	last := f.reqs[len(f.reqs)-1]
	if last.Op != "db.pmadrop" || last.User != pmaTestUser {
		t.Errorf("%+v", last)
	}
	n := len(f.reqs)
	for _, u := range []string{"root", "blog", "pma_", "", "pma_rdwxyz_ab12cd34'; DROP USER root"} {
		if w := do(h, "GET", "/_pma/end?u="+url.QueryEscape(u), nil, key); w.Code != 404 {
			t.Errorf("user %q: %d", u, w.Code)
		}
	}
	if w := do(h, "GET", "/_pma/end?u="+pmaTestUser, nil); w.Code != 404 {
		t.Errorf("without the key: %d", w.Code)
	}
	if len(f.reqs) != n {
		t.Errorf("a refused request must not reach the helper: %+v", f.reqs[n:])
	}
}

func TestTheBrowserIsOffUnlessItIsInstalledAndKeyed(t *testing.T) {
	for name, tweak := range map[string]func(*Server){
		"not installed": nil,
		"short key":     func(s *Server) { s.PMAPort, s.PMAKey = 2611, []byte("short") },
		"no port":       func(s *Server) { s.PMAKey = pmaKey },
	} {
		h, _, c, tok := setupWith(t, pmaAnswers(), tweak)
		if w := do(h, "POST", "/databases/pma", url.Values{"csrf": {tok}, "name": {"blog"}}, cookie(c)); w.Code != 404 {
			t.Errorf("%s: open %d", name, w.Code)
		}
		if w := do(h, "GET", "/_pma/ticket?t=x", nil, header("X-Mpn-Key", string(pmaKey))); w.Code != 404 {
			t.Errorf("%s: ticket %d", name, w.Code)
		}
		if body := do(h, "GET", "/sites/blog/databases", nil, cookie(c)).Body.String(); strings.Contains(body, "/databases/pma") {
			t.Errorf("%s: the button must not be offered", name)
		}
	}
}

func TestOpeningNeedsTheFormTokenAndAValidDatabase(t *testing.T) {
	h, f, c, tok := setupWith(t, pmaAnswers(), withPMA)
	if w := do(h, "POST", "/databases/pma", url.Values{"name": {"blog"}}, cookie(c)); w.Code != 403 {
		t.Errorf("without the token: %d", w.Code)
	}
	if w := do(h, "POST", "/databases/pma", url.Values{"csrf": {"wrong"}, "name": {"blog"}}, cookie(c)); w.Code != 403 {
		t.Errorf("with a wrong token: %d", w.Code)
	}
	if w := do(h, "GET", "/databases/pma?name=blog", nil, cookie(c)); w.Code == 200 {
		t.Error("opening must not work as a plain link (it would then work from any page)")
	}
	n := len(f.reqs)
	for _, bad := range []string{"", "x'; DROP", "A B", "-apply"} {
		if w := do(h, "POST", "/databases/pma", url.Values{"csrf": {tok}, "name": {bad}}, cookie(c)); w.Code != 400 {
			t.Errorf("name %q: %d", bad, w.Code)
		}
	}
	for _, r := range f.reqs[n:] {
		if r.Op == "db.pma" {
			t.Errorf("a bad name reached the helper: %+v", r)
		}
	}
}

func TestFailuresOfOpeningAreToldInVietnameseNotAsExitStatus(t *testing.T) {
	for _, c := range []struct{ output, errText, want string }{
		{`database "nope": not found`, "exit status 2", "không quản lý cơ sở dữ liệu này"},
		{`no database named "blog" on the server`, "exit status 1", "không còn trên máy chủ"},
		{"something else broke", "exit status 1", "Không mở được phpMyAdmin: something else broke"},
		{"", "exit status 1", "Không mở được phpMyAdmin: exit status 1"},
	} {
		h, _, cookieJar, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
			if r.Op == "db.pma" {
				return ctl.Response{Output: c.output, Error: c.errText}, nil
			}
			return pmaAnswers()(r)
		}, withPMA)
		w := do(h, "POST", "/databases/pma", url.Values{"csrf": {tok}, "name": {"blog"}}, cookie(cookieJar))
		body := w.Body.String()
		if w.Code != 502 || !strings.Contains(body, c.want) || (c.errText == "exit status 2" && strings.Contains(body, "exit status")) {
			t.Errorf("%q: %d\n%s", c.output, w.Code, body)
		}
	}
}

func TestOpeningFailsCleanlyWhenTheHelperDoes(t *testing.T) {
	h, f, c, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
		if r.Op == "db.pma" {
			return ctl.Response{}, errors.New("the root helper is not reachable")
		}
		return pmaAnswers()(r)
	}, withPMA)
	w := do(h, "POST", "/databases/pma", url.Values{"csrf": {tok}, "name": {"blog"}}, cookie(c))
	if w.Code != 502 || !strings.Contains(w.Body.String(), "not reachable") || ticketRe.MatchString(w.Body.String()) {
		t.Errorf("%d\n%s", w.Code, w.Body.String())
	}
	_ = f
	// an answer that is not what was asked for must not become a ticket
	h, _, c, tok = setupWith(t, byOp(map[string]string{"db.pma": `{"user":"root","password":"x","database":"blog"}`, "db.pmadrop": "ok"}), withPMA)
	w = do(h, "POST", "/databases/pma", url.Values{"csrf": {tok}, "name": {"blog"}}, cookie(c))
	if w.Code == 200 || ticketRe.MatchString(w.Body.String()) {
		t.Errorf("a user that is not a temporary one must be refused: %d", w.Code)
	}
}

func TestTheDatabasesTabOffersTheButtonOnlyWhenTheBrowserIsOn(t *testing.T) {
	h, _, c, _ := setupWith(t, pmaAnswers(), withPMA)
	body := do(h, "GET", "/sites/blog/databases", nil, cookie(c)).Body.String()
	for _, want := range []string{`action="/databases/pma"`, `name="name" value="blog"`, "phpMyAdmin"} {
		if !strings.Contains(body, want) {
			t.Errorf("databases page: missing %q", want)
		}
	}
}
