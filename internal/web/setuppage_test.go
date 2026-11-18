package web

import (
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mpn/internal/webauth"
)

const testSetupKey = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-abcde" // 43 characters, like the real one

// firstRunServer is a panel with no account yet and a setup key on disk. accountErr, when set, is what reading the account gives
// instead of "no such file".
func firstRunServer(t *testing.T, accountErr error) (http.Handler, string) {
	t.Helper()
	h, _, _ := firstRunServerWithFile(t, accountErr)
	return h, testSetupKey
}

func firstRunServerWithFile(t *testing.T, accountErr error) (http.Handler, string, string) {
	t.Helper()
	dir := t.TempDir()
	accountFile, keyFile := filepath.Join(dir, "account.json"), filepath.Join(dir, "setup-key")
	if err := os.WriteFile(keyFile, []byte(testSetupKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newServer()
	s.Account = func() (webauth.Account, error) {
		if accountErr != nil {
			return webauth.Account{}, accountErr
		}
		return webauth.LoadAccount(accountFile)
	}
	s.CreateAccount = func(a webauth.Account) error { return webauth.CreateAccount(accountFile, a) }
	s.SetupKeyFile = keyFile
	return s.Handler(), accountFile, keyFile
}

func setupForm(user string) url.Values {
	return url.Values{"user": {user}, "password": {"a long enough password"}, "password2": {"a long enough password"}}
}

// A stranger who finds the port before the owner gets nowhere without the link.
func TestTheSetupPageIsClosedWithoutTheKeyOfTheLink(t *testing.T) {
	h, accountFile, _ := firstRunServerWithFile(t, nil)
	if w := do(h, "GET", "/setup", nil); w.Code != 403 || !strings.Contains(w.Body.String(), "mpn web setup-link") || strings.Contains(w.Body.String(), `name="password"`) {
		t.Errorf("without the key: %d\n%s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/setup?t=wrong", nil); w.Code != 403 {
		t.Errorf("a wrong key opened the page: %d", w.Code)
	}
	for _, c := range []*http.Cookie{nil, {Name: setupCookie, Value: "wrong"}} {
		var opts []opt
		if c != nil {
			opts = append(opts, cookie(c))
		}
		if w := do(h, "POST", "/setup", setupForm("mallory"), opts...); w.Code != 403 {
			t.Errorf("an account was offered without the key (cookie %v): %d", c, w.Code)
		}
	}
	if _, err := os.Stat(accountFile); !errors.Is(err, fs.ErrNotExist) {
		t.Error("an account was written without the key")
	}
}

// The link puts the key in a cookie and the address loses it at once; the form then makes the account, and the link stops working.
func TestTheSetupLinkOpensThePageOnceAndMakesTheAccount(t *testing.T) {
	h, accountFile, keyFile := firstRunServerWithFile(t, nil)
	w := do(h, "GET", "/setup?t="+testSetupKey, nil)
	if w.Code != 303 || w.Header().Get("Location") != "/setup" {
		t.Fatalf("the key must leave the address: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Error("the address with the key must not travel in a Referer")
	}
	c := cookieNamed(w, setupCookie)
	if c == nil || c.Value != testSetupKey || !c.HttpOnly || c.Path != "/setup" || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("setup cookie: %+v", c)
	}
	if w := do(h, "GET", "/setup", nil, cookie(c)); w.Code != 200 || !strings.Contains(w.Body.String(), `name="password"`) {
		t.Fatalf("with the cookie the form is shown: %d", w.Code)
	}
	if w := do(h, "POST", "/setup", setupForm("admin"), cookie(c)); w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	a, err := webauth.LoadAccount(accountFile)
	if err != nil || a.User != "admin" {
		t.Fatalf("account: %+v %v", a, err)
	}
	if _, err := os.Stat(keyFile); !errors.Is(err, fs.ErrNotExist) {
		t.Error("the key must be removed once the account exists")
	}
	// Now the page is gone for everybody, the link included.
	if w := do(h, "POST", "/setup", setupForm("mallory"), cookie(c)); w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Errorf("a second setup: %d %q", w.Code, w.Header().Get("Location"))
	}
	if a2, _ := webauth.LoadAccount(accountFile); a2.User != "admin" {
		t.Errorf("the account was replaced by %q", a2.User)
	}
}

// The case of the report: an account file that cannot be read (wrong owner, damaged) must not reopen the page.
func TestAnAccountThatCannotBeReadDoesNotReopenSetup(t *testing.T) {
	for name, err := range map[string]error{
		"permission": fs.ErrPermission,
		"damaged":    errors.New("account.json: invalid character"),
		"empty":      errors.New("account.json: no user or hash"),
	} {
		h, key := firstRunServer(t, err)
		c := cookie(&http.Cookie{Name: setupCookie, Value: key})
		if w := do(h, "GET", "/setup", nil, c); w.Code != 303 {
			t.Errorf("%s: GET /setup %d, want a redirect away", name, w.Code)
		}
		if w := do(h, "POST", "/setup", setupForm("mallory"), c); w.Code != 303 || w.Header().Get("Location") != "/" {
			t.Errorf("%s: POST /setup %d %q", name, w.Code, w.Header().Get("Location"))
		}
	}
}

// The case of the review: two people send the form at the same moment. One account is made; the other is told to sign in.
func TestTwoSetupsAtOnceMakeOneAccount(t *testing.T) {
	h, accountFile, _ := firstRunServerWithFile(t, nil)
	c := cookie(&http.Cookie{Name: setupCookie, Value: testSetupKey})
	var wg sync.WaitGroup
	for _, u := range []string{"alice", "bob", "carol", "dave"} {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			do(h, "POST", "/setup", setupForm(u), c)
		}(u)
	}
	wg.Wait()
	a, err := webauth.LoadAccount(accountFile)
	if err != nil {
		t.Fatal(err)
	}
	// Whoever won, nobody replaced the winner afterwards: the file was linked into place once.
	b, _ := os.ReadFile(accountFile)
	if strings.Count(string(b), `"user"`) != 1 || a.User == "" {
		t.Errorf("account file: %s", b)
	}
}

func TestCreateAccountNeverReplacesAnAccount(t *testing.T) {
	p := filepath.Join(t.TempDir(), "account.json")
	if err := webauth.CreateAccount(p, webauth.Account{User: "first", Hash: "h"}); err != nil {
		t.Fatal(err)
	}
	if err := webauth.CreateAccount(p, webauth.Account{User: "second", Hash: "h"}); !errors.Is(err, fs.ErrExist) {
		t.Errorf("err=%v, want ErrExist", err)
	}
	if a, _ := webauth.LoadAccount(p); a.User != "first" {
		t.Errorf("replaced by %q", a.User)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(p), ".account-*")); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
}
