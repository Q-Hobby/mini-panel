package web

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

// What another machine sends to /_notify/ reaches the alerts service without a sign-in and without the check of the form's origin,
// with the address of the sender; any other path under it does not exist.
func TestTheHubPassesReportsToTheService(t *testing.T) {
	dir, err := os.MkdirTemp("", "ntf")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip("no unix sockets here")
	}
	var gotPath, gotIP, gotBody string
	go http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotPath, gotIP, gotBody = r.URL.Path, r.Header.Get("X-Real-IP"), string(b)
		w.Write([]byte(`{"name":"web-2"}`))
	}))
	s := newServer()
	s.NotifySocket = sock
	h := s.Handler()
	w := do(h, "POST", "/_notify/beat", nil, addr("203.0.113.7:9"), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://evil.example")
		r.Body = io.NopCloser(strings.NewReader(`{"id":1,"key":"k"}`))
	})
	if w.Code != 200 || gotPath != "/beat" || gotIP != "203.0.113.7" || gotBody != `{"id":1,"key":"k"}` || !strings.Contains(w.Body.String(), "web-2") {
		t.Fatalf("%d %q %q %q %s", w.Code, gotPath, gotIP, gotBody, w.Body)
	}
	if w := do(h, "POST", "/_notify/other", nil, func(r *http.Request) { r.Header.Set("Content-Type", "application/json") }); w.Code == 200 {
		t.Error("an unknown path was passed on")
	}
	if w := do(h, "POST", "/_notify/beat", nil, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }); w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("%d", w.Code)
	}
	// Pairing is counted like a sign-in.
	var last int
	for i := 0; i < 20; i++ {
		last = do(h, "POST", "/_notify/pair", nil, addr("198.51.100.99:1"), func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			r.Body = io.NopCloser(strings.NewReader(`{"secret":"x"}`))
		}).Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("pairing is not limited: %d", last)
	}
}

// Without the alerts service configured, /_notify/ is not there.
func TestNoRelayWithoutTheService(t *testing.T) {
	s := newServer()
	if w := do(s.Handler(), "POST", "/_notify/beat", nil, func(r *http.Request) { r.Header.Set("Content-Type", "application/json") }); w.Code == 200 {
		t.Errorf("%d", w.Code)
	}
}

func TestTheNotifyPageSendsTheTokenToTheHelperOnly(t *testing.T) {
	role := ""
	h, f, c, tok := setup(t, func(r ctl.Request) (ctl.Response, error) {
		if r.Op == "notify.hub" {
			role = "hub"
		}
		if r.Op == "notify.status" {
			return ctl.Response{OK: true, Output: `{"role":"` + role + `","name":"web-1","bot":"b","rules":{},"nodes":[],"chats":[]}`}, nil
		}
		if r.Op == "notify.pair" {
			return ctl.Response{OK: true, Output: "mpn1.CODE\n"}, nil
		}
		return ctl.Response{OK: true}, nil
	})
	body := do(h, "GET", "/notify", nil, cookie(c)).Body.String()
	if !strings.Contains(body, "/notify/hub") || !strings.Contains(body, "/notify/agent") || !strings.Contains(body, `value="web-1"`) {
		t.Fatal("the page does not offer both roles")
	}
	const token = "123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw"
	do(h, "POST", "/notify/hub", url.Values{"csrf": {tok}, "token": {" " + token + " "}, "name": {"web-1"}, "lang": {"vi"}}, cookie(c))
	var hub *ctl.Request
	for i := range f.reqs {
		if f.reqs[i].Op == "notify.hub" {
			hub = &f.reqs[i]
		}
	}
	if hub == nil || hub.Token != token || hub.Name != "web-1" || hub.Lang != "vi" {
		t.Fatalf("%+v", hub)
	}
	if strings.Contains(hub.AuditDetail(), token) || !strings.Contains(hub.Stdin(), token) {
		t.Error("the token must go on standard input and never into the log")
	}
	if argv, _ := hub.Argv(); strings.Contains(strings.Join(argv, " "), token) {
		t.Error("the token is on the command line")
	}
	// The pairing code is shown once, on the page, not in the address.
	w := do(h, "POST", "/notify/pair", url.Values{"csrf": {tok}, "name": {"web-2"}, "url": {"https://1.2.3.4:2610"}}, cookie(c))
	if loc := w.Header().Get("Location"); loc != "/notify" {
		t.Fatalf("redirect %q", loc)
	}
	if b := do(h, "GET", "/notify", nil, cookie(c)).Body.String(); !strings.Contains(b, "mpn1.CODE") {
		t.Error("the code is not shown")
	}
	if b := do(h, "GET", "/notify", nil, cookie(c)).Body.String(); strings.Contains(b, "mpn1.CODE") {
		t.Error("the code is shown twice")
	}
	n := len(f.reqs)
	do(h, "POST", "/notify/off", url.Values{"csrf": {tok}, "confirm": {"no"}}, cookie(c))
	for _, r := range f.reqs[n:] {
		if r.Op == "notify.off" {
			t.Error("turned off without the confirmation")
		}
	}
}
