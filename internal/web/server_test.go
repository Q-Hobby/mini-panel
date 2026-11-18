package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"mpn/internal/webauth"
)

const goodPW = "correct-horse-battery"

var (
	once sync.Once
	acct webauth.Account
)

func account() webauth.Account {
	once.Do(func() {
		h, _ := webauth.Hash(goodPW)
		acct = webauth.Account{User: "admin", Hash: h}
	})
	return acct
}

func newServer() *Server {
	return &Server{Account: func() (webauth.Account, error) { return account(), nil },
		Sessions: webauth.NewSessions(10*time.Minute, time.Hour, 10), Throttle: webauth.NewThrottle()}
}

type opt func(*http.Request)

func header(k, v string) opt {
	return func(r *http.Request) {
		if v == "" {
			r.Header.Del(k)
		} else {
			r.Header.Set(k, v)
		}
	}
}
func cookie(c *http.Cookie) opt { return func(r *http.Request) { r.AddCookie(c) } }
func addr(a string) opt         { return func(r *http.Request) { r.RemoteAddr = a } }
func host(h string) opt         { return func(r *http.Request) { r.Host = h } }

func do(h http.Handler, method, target string, form url.Values, opts ...opt) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, "http://localhost"+target, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method == http.MethodPost {
		r.Header.Set("Origin", "http://localhost")
	}
	// Most tests look for the Vietnamese text, which the panel showed by default before 1.2.0; English is the default now, so they
	// ask for Vietnamese the way a browser does. A test that sets a cookie or another header still gets what it asks for.
	r.Header.Set("Accept-Language", "vi")
	for _, o := range opts {
		o(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func login(h http.Handler, pw string, opts ...opt) *httptest.ResponseRecorder {
	return do(h, "POST", "/login", url.Values{"user": {"admin"}, "password": {pw}}, opts...)
}

func cookieNamed(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

var csrfRe = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func TestLoginSetsAStrictHttpOnlyCookieAndOpensTheHome(t *testing.T) {
	h := newServer().Handler()
	w := login(h, goodPW)
	c := cookieNamed(w, "mpn")
	if w.Code != 303 || c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Fatalf("code %d cookie %+v", w.Code, c)
	}
	home := do(h, "GET", "/", nil, cookie(c))
	if home.Code != 200 || !strings.Contains(home.Body.String(), "Website") || !csrfRe.MatchString(home.Body.String()) {
		t.Errorf("home: %d %s", home.Code, home.Body.String())
	}
}

func TestWrongLoginsLookAlikeAndGetNoCookie(t *testing.T) {
	h := newServer().Handler()
	a := login(h, "wrong-password")
	b := do(h, "POST", "/login", url.Values{"user": {"nobody"}, "password": {goodPW}})
	if a.Code != 401 || b.Code != 401 || a.Body.String() != b.Body.String() || len(a.Result().Cookies()) != 0 {
		t.Errorf("a=%d b=%d same body=%v", a.Code, b.Code, a.Body.String() == b.Body.String())
	}
	if big := login(h, strings.Repeat("x", 100<<10)); big.Code != 401 {
		t.Errorf("an oversized body must fail the login, got %d", big.Code)
	}
}

func TestRepeatedFailuresLockOnlyThatAddress(t *testing.T) {
	h := newServer().Handler()
	for i := 0; i < 6; i++ {
		if w := login(h, "bad"); w.Code != 401 {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	w := login(h, goodPW) // even the right password is refused while locked
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || cookieNamed(w, "mpn") != nil {
		t.Errorf("locked address: %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
	if w := login(h, goodPW, addr("198.51.100.7:4000")); w.Code != 303 {
		t.Errorf("another address must still log in, got %d", w.Code)
	}
}

func TestProxyHeaderIsUsedOnlyWhenTrusted(t *testing.T) {
	for _, trust := range []bool{true, false} {
		s := newServer()
		s.TrustProxy = trust
		h := s.Handler()
		for i := 0; i < 6; i++ {
			login(h, "bad", header("X-Real-IP", "203.0.113.9"))
		}
		got := login(h, goodPW, header("X-Real-IP", "203.0.113.10")).Code
		if trust && got != 303 {
			t.Errorf("trusted: a different X-Real-IP must not be locked, got %d", got)
		}
		if !trust && got != 429 {
			t.Errorf("untrusted: the header must be ignored, so the shared address stays locked, got %d", got)
		}
	}
}

func TestPostsMustComeFromThePanelItself(t *testing.T) {
	h := newServer().Handler()
	form := url.Values{"user": {"admin"}, "password": {"x"}}
	for name, o := range map[string]opt{
		"no origin":    header("Origin", ""),
		"other origin": header("Origin", "https://evil.example"),
		"null origin":  header("Origin", "null"),
	} {
		if w := do(h, "POST", "/login", form, o); w.Code != 403 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if w := do(h, "POST", "/login", form, header("Origin", ""), header("Sec-Fetch-Site", "same-origin")); w.Code != 401 {
		t.Errorf("a browser-marked same-origin post must reach the handler, got %d", w.Code)
	}
	// What a real browser sends for the login form: "Origin: null" together with Sec-Fetch-Site: same-origin.
	if w := do(h, "POST", "/login", form, header("Origin", "null"), header("Sec-Fetch-Site", "same-origin")); w.Code != 401 {
		t.Errorf("a browser form post with Origin null must reach the handler, got %d", w.Code)
	}
	if w := do(h, "POST", "/login", form, header("Origin", "null"), header("Sec-Fetch-Site", "cross-site")); w.Code != 403 {
		t.Errorf("Origin null from another site must be refused, got %d", w.Code)
	}
}

func TestOnlyKnownHostsAreServed(t *testing.T) {
	s := newServer()
	s.Hosts = []string{"panel.example.com"}
	h := s.Handler()
	if w := do(h, "GET", "/login", nil, host("evil.example")); w.Code != 421 {
		t.Errorf("unknown host: %d", w.Code)
	}
	for _, ok := range []string{"panel.example.com", "Panel.Example.com:8443", "localhost:8780", "127.0.0.1:8780", "203.0.113.5:2610", "[2001:db8::1]:2610", "[::1]"} {
		if w := do(h, "GET", "/login", nil, host(ok)); w.Code != 200 {
			t.Errorf("%s: %d", ok, w.Code)
		}
	}
}

func TestHomeNeedsALogin(t *testing.T) {
	h := newServer().Handler()
	for name, opts := range map[string][]opt{
		"no cookie":    nil,
		"bogus cookie": {cookie(&http.Cookie{Name: "mpn", Value: "forged"})},
	} {
		if w := do(h, "GET", "/", nil, opts...); w.Code != 303 || w.Header().Get("Location") != "/login" {
			t.Errorf("%s: %d %s", name, w.Code, w.Header().Get("Location"))
		}
	}
}

func TestLogoutNeedsTheTokenAndEndsTheSession(t *testing.T) {
	h := newServer().Handler()
	c := cookieNamed(login(h, goodPW), "mpn")
	token := csrfRe.FindStringSubmatch(do(h, "GET", "/", nil, cookie(c)).Body.String())[1]
	if w := do(h, "POST", "/logout", url.Values{"csrf": {"wrong"}}, cookie(c)); w.Code != 403 {
		t.Errorf("wrong token: %d", w.Code)
	}
	if w := do(h, "GET", "/", nil, cookie(c)); w.Code != 200 {
		t.Error("a refused logout must leave the session alone")
	}
	w := do(h, "POST", "/logout", url.Values{"csrf": {token}}, cookie(c))
	if w.Code != 303 || cookieNamed(w, "mpn").MaxAge >= 0 {
		t.Errorf("logout: %d %+v", w.Code, cookieNamed(w, "mpn"))
	}
	if w := do(h, "GET", "/", nil, cookie(c)); w.Code != 303 {
		t.Error("the old cookie must stop working")
	}
}

func TestLoginNeverReusesASession(t *testing.T) {
	h := newServer().Handler()
	a := cookieNamed(login(h, goodPW), "mpn")
	b := cookieNamed(login(h, goodPW, cookie(a)), "mpn")
	if a.Value == b.Value {
		t.Error("a new login must get a new session value")
	}
	if w := do(h, "GET", "/", nil, cookie(a)); w.Code != 303 {
		t.Error("the session from before the new login must be gone")
	}
}

func TestSecureModeAndHeaders(t *testing.T) {
	s := newServer()
	s.Secure = true
	h := s.Handler()
	c := cookieNamed(login(h, goodPW), "__Host-mpn")
	if c == nil || !c.Secure || !c.HttpOnly {
		t.Fatalf("cookie %+v", c)
	}
	w := do(h, "GET", "/login", nil)
	for k, want := range map[string]string{"Strict-Transport-Security": "max-age", "X-Content-Type-Options": "nosniff",
		"X-Frame-Options": "DENY", "Cache-Control": "no-store", "Referrer-Policy": "same-origin", "Content-Security-Policy": "frame-ancestors 'none'"} {
		if !strings.Contains(w.Header().Get(k), want) {
			t.Errorf("%s = %q", k, w.Header().Get(k))
		}
	}
	if strings.Contains(w.Header().Get("Content-Security-Policy"), "unsafe") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Error("scripts must be allowed from the panel itself only, and nothing inline")
	}
}
