package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestLanguageCatalogsHaveMatchingKeysAndFormats(t *testing.T) {
	// The catalog deliberately uses the same fmt argument order in both languages.
	directives := regexp.MustCompile(`%(?:\[\d+\])?[+#\- 0]*(?:\d+)?(?:\.\d+)?[a-zA-Z%]`)
	for key, source := range vi {
		translated, ok := en[key]
		if !ok || translated == "" {
			t.Errorf("missing English message %q", key)
			continue
		}
		a, b := directives.FindAllString(source, -1), directives.FindAllString(translated, -1)
		if fmt.Sprint(a) != fmt.Sprint(b) {
			t.Errorf("%s: Vietnamese formats %v, English %v", key, a, b)
		}
	}
	for key := range en {
		if _, ok := vi[key]; !ok {
			t.Errorf("English-only key %q", key)
		}
	}
	for key, source := range vi {
		translated, ok := zh[key]
		if !ok || translated == "" {
			t.Errorf("missing Chinese message %q", key)
			continue
		}
		if a, b := directives.FindAllString(source, -1), directives.FindAllString(translated, -1); fmt.Sprint(a) != fmt.Sprint(b) {
			t.Errorf("%s: Vietnamese formats %v, Chinese %v", key, a, b)
		}
	}
	if len(zh) != len(vi) {
		t.Errorf("Chinese has %d messages, Vietnamese %d", len(zh), len(vi))
	}
	if translate("xx", "login.title") != T("login.title") || translate("en", "missing.key") != "missing.key" {
		t.Fatal("fallback changed")
	}
	if got := translate("en", "login.locked", 42); got != "Too many failed attempts. Try again in 42 seconds." {
		t.Fatal(got)
	}
}

func TestLanguageSelection(t *testing.T) {
	for _, tc := range []struct{ browser, cookie, want string }{
		{"", "", "en"}, {"en-US,en;q=0.9,vi;q=0.5", "", "en"}, {"vi;q=0.9,en;q=0.2", "", "vi"},
		{"en;q=0", "", "en"}, {"vi;q=0", "", "en"}, {"fr,de", "", "en"}, {"fr,vi;q=0.8", "", "vi"},
		{"en", "vi", "vi"}, {"vi", "en", "en"}, {"vi", "invalid", "vi"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Language", tc.browser)
		if tc.cookie != "" {
			r.AddCookie(&http.Cookie{Name: "mpn_language", Value: tc.cookie})
		}
		if got := requestLanguage(r); got != tc.want {
			t.Errorf("%+v: %s", tc, got)
		}
	}
}

func TestLanguageSwitchPersistsAndPreservesLocation(t *testing.T) {
	s := newServer()
	s.Secure = true
	h := s.Handler()
	w := do(h, "GET", "/language/en", nil, header("Referer", "http://localhost/sites/blog/files?dir=htdocs&filter=css"))
	c := cookieNamed(w, "mpn_language")
	if w.Code != 303 || c == nil || c.Value != "en" || !c.HttpOnly || !c.Secure || c.Path != "/" || c.MaxAge <= 0 {
		t.Fatalf("%d: %+v", w.Code, c)
	}
	if got := w.Header().Get("Location"); got != "/sites/blog/files?dir=htdocs&filter=css" {
		t.Fatal(got)
	}
	body := do(h, "GET", "/login", nil, cookie(c)).Body.String()
	for _, want := range []string{`lang="en"`, "Username", "Sign in to your panel.", `href="/language/vi"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if w := do(h, "GET", "/language/vi", nil, header("Referer", "https://evil.example/")); w.Header().Get("Location") != "/" {
		t.Fatal("external redirect")
	}
	if w := do(h, "GET", "/language/fr", nil); w.Code != 404 || cookieNamed(w, "mpn_language") != nil {
		t.Fatal("unsupported language accepted")
	}
}

func TestEnglishServerMessagesAndPages(t *testing.T) {
	h, _, session, _ := setup(t, okAnswer(""))
	english := cookie(&http.Cookie{Name: "mpn_language", Value: "en"})
	for path, want := range map[string]string{"/": "Websites", "/settings": "Account settings", "/monitor": "Server monitoring", "/firewall": "Firewall", "/security": "Security", "/email": "Email", "/services": "Services", "/sites/new": "Add website", "/not-a-page": "This page could not be found"} {
		w := do(h, "GET", path, nil, cookie(session), english)
		if !strings.Contains(w.Body.String(), want) || !strings.Contains(w.Body.String(), `lang="en"`) {
			t.Errorf("%s: expected %q in English", path, want)
		}
	}
	w := login(newServer().Handler(), "wrong", english)
	if !strings.Contains(w.Body.String(), "Incorrect username or password.") {
		t.Fatal(w.Body.String())
	}
	// The error is produced by a helper without an HTTP request parameter.
	w = do(h, "GET", "/sites/not-valid!", nil, cookie(session), english)
	if !strings.Contains(w.Body.String(), "Website not found.") {
		t.Fatalf("helper error: %s", w.Body.String())
	}
	_, problem := dbNameForText(translatorFor("en"), "blog", "bad!")
	if problem != "Use only letters a-z, numbers and underscores (_)." {
		t.Fatal(problem)
	}
	if got := describeScheduleText(translatorFor("en"), "*/5 * * * *"); got != "Every 5 minutes" {
		t.Fatal(got)
	}
	// Localized POST validation and first-run setup also use the selected language.
	w = do(h, "POST", "/login", url.Values{"user": {"admin"}, "password": {"wrong"}}, english)
	if !strings.Contains(w.Body.String(), "Incorrect username or password.") {
		t.Fatal("POST language lost")
	}
}

func TestChinesePanel(t *testing.T) {
	h := newServer().Handler()
	w := do(h, "GET", "/language/zh", nil)
	c := cookieNamed(w, "mpn_language")
	if w.Code != 303 || c == nil || c.Value != "zh" {
		t.Fatalf("%d %+v", w.Code, c)
	}
	body := do(h, "GET", "/login", nil, cookie(c)).Body.String()
	for _, want := range []string{`lang="zh"`, zh["auth.username"], zh["login.heading"], ">简体中文<", `href="/language/vi"`, `href="/language/en"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.5")
	if got := requestLanguage(r); got != "zh" {
		t.Errorf("browser language: %s", got)
	}
	if got := translate("zh", "login.locked", 42); !strings.Contains(got, "42") || got == translate("vi", "login.locked", 42) {
		t.Errorf("Chinese message: %s", got)
	}
}

func TestConcurrentLanguagesDoNotLeak(t *testing.T) {
	h := newServer().Handler()
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			language, want := "vi", "Tên đăng nhập"
			if i%2 == 0 {
				language, want = "en", "Username"
			}
			w := do(h, "GET", "/login", nil, cookie(&http.Cookie{Name: "mpn_language", Value: language}))
			if !strings.Contains(w.Body.String(), want) || w.Header().Get("Content-Language") != language {
				t.Errorf("language leaked for %s", language)
			}
		}(i)
	}
	wg.Wait()
}

func TestEnglishFirstRunAndOperationResult(t *testing.T) {
	h, key := firstRunServer(t, nil)
	english := cookie(&http.Cookie{Name: "mpn_language", Value: "en"})
	opened := cookie(&http.Cookie{Name: setupCookie, Value: key})
	w := do(h, "GET", "/setup", nil, english, opened)
	for _, want := range []string{`lang="en"`, "Create your account.", "Confirm password", "Create account and sign in"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("setup missing %q", want)
		}
	}
	w = do(h, "POST", "/setup", url.Values{"user": {"admin"}, "password": {"short"}}, english, opened)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "Password must contain at least 12 characters.") {
		t.Fatal("setup validation was not localized")
	}
	h, _, session, csrf := setup(t, okAnswer("raw command output"))
	w = do(h, "POST", "/databases/add", url.Values{"csrf": {csrf}, "site": {"blog"}}, cookie(session), english)
	if w.Code != 303 {
		t.Fatalf("operation: %d", w.Code)
	}
	w = do(h, "GET", "/result", nil, cookie(session), english)
	for _, want := range []string{"Add database for blog", "Completed.", "raw command output"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("result missing %q", want)
		}
	}
}
