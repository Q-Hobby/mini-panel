package web

import (
	"net/url"
	"strings"
	"testing"
)

func TestTheUpdateButtonAsksTheHelperAndOnlyWithAVersion(t *testing.T) {
	h, f, c, csrf := setupWith(t, byOp(map[string]string{"panel.update": "the update has started in the background"}), nil)
	page := do(h, "GET", "/settings", nil, cookie(c)).Body.String()
	if !strings.Contains(page, `action="/settings/update"`) {
		t.Fatal("the settings page has no update form")
	}
	do(h, "POST", "/settings/update", url.Values{"csrf": {csrf}, "version": {"1.2.1"}}, cookie(c))
	var got []string
	for _, q := range f.reqs {
		if q.Op == "panel.update" {
			got = append(got, q.Version)
		}
	}
	if len(got) != 1 || got[0] != "1.2.1" {
		t.Fatalf("the helper got %v", got)
	}
	n := len(f.reqs)
	for _, v := range []string{"1.2.1; reboot", "../x", "latest", "1.2"} {
		do(h, "POST", "/settings/update", url.Values{"csrf": {csrf}, "version": {v}}, cookie(c))
	}
	for _, q := range f.reqs[n:] {
		if q.Op == "panel.update" {
			t.Errorf("%q reached the helper", q.Version)
		}
	}
	// Without the form token nothing happens.
	n = len(f.reqs)
	do(h, "POST", "/settings/update", url.Values{"version": {"1.2.1"}}, cookie(c))
	if len(f.reqs) != n {
		t.Error("an update without the form token reached the helper")
	}
}
