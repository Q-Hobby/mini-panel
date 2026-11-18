package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"mpn/internal/ctl"
)

type fakeHelper struct {
	mu     sync.Mutex
	reqs   []ctl.Request
	answer func(ctl.Request) (ctl.Response, error)
}

func (f *fakeHelper) call(r ctl.Request) (ctl.Response, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, r)
	f.mu.Unlock()
	return f.answer(r)
}

func (f *fakeHelper) ops() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var o []string
	for _, r := range f.reqs {
		o = append(o, r.Op)
	}
	return o
}

// followed returns the page a redirect to /result leads to, as the browser would fetch it.
func followed(h http.Handler, w *httptest.ResponseRecorder, c *http.Cookie) *httptest.ResponseRecorder {
	if w.Code == 303 && w.Header().Get("Location") == "/result" {
		return do(h, "GET", "/result", nil, cookie(c))
	}
	return w
}

func okAnswer(out string) func(ctl.Request) (ctl.Response, error) {
	return func(ctl.Request) (ctl.Response, error) { return ctl.Response{OK: true, Output: out}, nil }
}

// setup returns a panel wired to a fake helper, and a logged-in browser's cookie and form token.
func setup(t *testing.T, answer func(ctl.Request) (ctl.Response, error)) (http.Handler, *fakeHelper, *http.Cookie, string) {
	t.Helper()
	f := &fakeHelper{answer: answer}
	s := newServer()
	s.Call = f.call
	h := s.Handler()
	c := cookieNamed(login(h, goodPW), "mpn")
	m := csrfRe.FindStringSubmatch(do(h, "GET", "/", nil, cookie(c)).Body.String())
	if c == nil || m == nil {
		t.Fatal("could not sign in")
	}
	f.reqs = nil // forget the page load
	return h, f, c, m[1]
}

func TestSitesPageListsSitesAndEscapesNames(t *testing.T) {
	h, _, c, _ := setup(t, okAnswer(`[{"name":"a<script>x","kind":"php","user":"u","hostnames":["h.example.com"],"detail":"php 8.4","tls":false}]`))
	body := do(h, "GET", "/", nil, cookie(c)).Body.String()
	if !strings.Contains(body, "h.example.com") || !strings.Contains(body, "a&lt;script&gt;x") || strings.Contains(body, "<script>") {
		t.Errorf("site list not shown safely:\n%s", body)
	}
}

func TestSitesPageSurvivesAHelperThatFails(t *testing.T) {
	for name, answer := range map[string]func(ctl.Request) (ctl.Response, error){
		"unreachable": func(ctl.Request) (ctl.Response, error) {
			return ctl.Response{}, errors.New("the root helper is not reachable")
		},
		"refused": func(ctl.Request) (ctl.Response, error) { return ctl.Response{Error: "not allowed"}, nil },
		"garbage": okAnswer("this is not json"),
	} {
		h, _, c, _ := setup(t, answer)
		w := do(h, "GET", "/", nil, cookie(c))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `role="alert"`) {
			t.Errorf("%s: %d, no visible error", name, w.Code)
		}
	}
}

func TestChangesNeedASessionATokenAndTheRightOrigin(t *testing.T) {
	h, f, c, tok := setup(t, okAnswer(""))
	form := func(csrf string) url.Values { return url.Values{"csrf": {csrf}, "site": {"blog"}} }
	for name, w := range map[string]int{
		"no token":     do(h, "POST", "/sites/apply", url.Values{"site": {"blog"}}, cookie(c)).Code,
		"wrong token":  do(h, "POST", "/sites/apply", form("wrong"), cookie(c)).Code,
		"other origin": do(h, "POST", "/sites/apply", form(tok), cookie(c), header("Origin", "https://evil.example")).Code,
	} {
		if w != 403 {
			t.Errorf("%s: %d", name, w)
		}
	}
	if w := do(h, "POST", "/sites/apply", form(tok)); w.Code != 303 {
		t.Errorf("no session: %d", w.Code)
	}
	if w := do(h, "GET", "/sites/remove?site=blog", nil); w.Code != 303 {
		t.Errorf("confirm page without a session: %d", w.Code)
	}
	if len(f.reqs) != 0 {
		t.Errorf("the helper was called %d times by refused requests", len(f.reqs))
	}
	if w := do(h, "POST", "/sites/apply", form(tok), cookie(c)); w.Code != 303 || w.Header().Get("Location") != "/result" || len(f.reqs) != 1 || f.reqs[0].Site != "blog" {
		t.Errorf("a valid apply: %d, %+v", w.Code, f.reqs)
	}
}

func TestRemovingASiteNeedsTheNameTyped(t *testing.T) {
	h, f, c, tok := setup(t, okAnswer("removed"))
	if w := do(h, "GET", "/sites/remove?site=-x", nil, cookie(c)); w.Code != 400 {
		t.Errorf("invalid name: %d", w.Code)
	}
	page := do(h, "GET", "/sites/remove?site=blog", nil, cookie(c)).Body.String()
	if !strings.Contains(page, "<b>blog</b>") || !strings.Contains(page, `name="purge"`) {
		t.Errorf("confirmation page:\n%s", page)
	}
	if w := do(h, "POST", "/sites/remove", url.Values{"csrf": {tok}, "site": {"blog"}, "confirm": {"blo"}}, cookie(c)); w.Code != 400 || len(f.reqs) != 0 {
		t.Errorf("a wrong confirmation: %d, %d helper calls", w.Code, len(f.reqs))
	}
	do(h, "POST", "/sites/remove", url.Values{"csrf": {tok}, "site": {"blog"}, "confirm": {"blog"}}, cookie(c))
	do(h, "POST", "/sites/remove", url.Values{"csrf": {tok}, "site": {"blog"}, "confirm": {"blog"}, "purge": {"on"}}, cookie(c))
	if len(f.reqs) != 2 || f.reqs[0].Op != "site.remove" || f.reqs[0].Purge || !f.reqs[1].Purge {
		t.Errorf("requests %+v", f.reqs)
	}
}
