package web

import (
	"net/url"
	"strings"
	"testing"
)

func TestAResultIsReadOnceAndReloadingRepeatsNothing(t *testing.T) {
	h, f, c, tok := setup(t, okAnswer("password: S3cretPw"))
	w := do(h, "POST", "/databases/add", url.Values{"csrf": {tok}, "site": {"blog"}}, cookie(c))
	if w.Code != 303 || w.Header().Get("Location") != "/result" || strings.Contains(w.Body.String(), "S3cretPw") {
		t.Fatalf("the form post must only redirect: %d %q", w.Code, w.Header().Get("Location"))
	}
	first := do(h, "GET", "/result", nil, cookie(c))
	if first.Code != 200 || !strings.Contains(first.Body.String(), "S3cretPw") {
		t.Fatalf("the owner must see the result once: %d", first.Code)
	}
	again := do(h, "GET", "/result", nil, cookie(c))
	if again.Code != 303 || again.Header().Get("Location") != "/" || strings.Contains(again.Body.String(), "S3cretPw") {
		t.Errorf("a second look must not show it: %d %q", again.Code, again.Header().Get("Location"))
	}
	if len(f.reqs) != 1 {
		t.Errorf("reloading must not reach the helper again, calls: %d", len(f.reqs))
	}
}

func TestAResultBelongsToTheSessionThatDidIt(t *testing.T) {
	h, _, c, tok := setup(t, okAnswer("secret output"))
	other := cookieNamed(login(h, goodPW, addr("198.51.100.8:1")), "mpn")
	do(h, "POST", "/databases/add", url.Values{"csrf": {tok}, "site": {"blog"}}, cookie(c))
	if w := do(h, "GET", "/result", nil, cookie(other)); w.Code != 303 || strings.Contains(w.Body.String(), "secret output") {
		t.Errorf("another session must not see it: %d", w.Code)
	}
	if w := do(h, "GET", "/result", nil, cookie(c)); w.Code != 200 {
		t.Errorf("the owner must still be able to read it after the other session looked: %d", w.Code)
	}
}

func TestResultPageNeedsASession(t *testing.T) {
	h, _, _, _ := setup(t, okAnswer(""))
	if w := do(h, "GET", "/result", nil); w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Errorf("%d %q", w.Code, w.Header().Get("Location"))
	}
}
