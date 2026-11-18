package ctl

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSiteAccessRequests(t *testing.T) {
	on, err := Request{Op: "site.access", Site: "blog", Enable: true, Password: "Correct-Horse-9"}.Argv()
	if err != nil || strings.Join(on, " ") != "site access -name blog -enable -stdin -apply" {
		t.Fatalf("enable: %v %v", on, err)
	}
	off, err := Request{Op: "site.access", Site: "blog"}.Argv()
	if err != nil || strings.Join(off, " ") != "site access -name blog -disable -apply" {
		t.Fatalf("disable: %v %v", off, err)
	}
	for _, a := range append(on, off...) {
		if strings.Contains(a, "Correct-Horse-9") {
			t.Errorf("the password is on the command line: %q", a)
		}
	}
	if !IsWrite("site.access") {
		t.Error("site.access changes the machine, so it must run one at a time")
	}
}

func TestSiteAccessRejectsWhatItShould(t *testing.T) {
	for name, r := range map[string]Request{
		"no site":       {Op: "site.access", Enable: true, Password: "Correct-Horse-9"},
		"flag as site":  {Op: "site.access", Site: "-x", Enable: true, Password: "Correct-Horse-9"},
		"no password":   {Op: "site.access", Site: "blog", Enable: true},
		"short":         {Op: "site.access", Site: "blog", Enable: true, Password: "short"},
		"space":         {Op: "site.access", Site: "blog", Enable: true, Password: "has a space in it"},
		"quote":         {Op: "site.access", Site: "blog", Enable: true, Password: "quote'inside-the-pass"},
		"newline":       {Op: "site.access", Site: "blog", Enable: true, Password: "line\nbreak-password"},
		"colon is fine": {Op: "site.access", Site: "blog", Enable: true, Password: "a:b:c:d:e:f:g:h:i"},
	} {
		_, err := r.Argv()
		if name == "colon is fine" {
			if err != nil {
				t.Errorf("a colon in the password is allowed: %v", err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestStdinCarriesThePasswordOnlyWhenEnabling(t *testing.T) {
	if got := (Request{Op: "site.access", Site: "blog", Enable: true, Password: "Correct-Horse-9"}).Stdin(); got != "Correct-Horse-9\n" {
		t.Errorf("enable: %q", got)
	}
	for _, r := range []Request{
		{Op: "site.access", Site: "blog", Password: "Correct-Horse-9"},
		{Op: "db.add", Site: "blog", Password: "Correct-Horse-9"},
		{Op: "site.list"},
	} {
		if got := r.Stdin(); got != "" {
			t.Errorf("%s: stdin %q", r.Op, got)
		}
	}
}

type stdinRunner struct {
	mu     sync.Mutex
	argv   [][]string
	stdins []string
}

func (s *stdinRunner) Run(_ context.Context, argv []string, stdin string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.argv = append(s.argv, argv)
	s.stdins = append(s.stdins, stdin)
	return "ok", nil
}

func TestTheServerHandsThePasswordToTheRunnerOnStdinAndKeepsItOutOfTheLog(t *testing.T) {
	r := &stdinRunner{}
	var logged []string
	s := &Server{Runner: r, Allow: AllowUIDs(997), Timeout: 5 * time.Second,
		Log: func(argv []string, err error) { logged = append(logged, strings.Join(argv, " ")) }}
	resp := ask(t, s, 997, `{"op":"site.access","site":"blog","enable":true,"password":"Correct-Horse-9"}`)
	if !resp.OK || len(r.stdins) != 1 || r.stdins[0] != "Correct-Horse-9\n" {
		t.Fatalf("%+v stdins=%q", resp, r.stdins)
	}
	if strings.Contains(strings.Join(logged, "|"), "Correct-Horse-9") || strings.Contains(resp.Output, "Correct-Horse-9") {
		t.Errorf("the password leaked into the log or the answer: %v %q", logged, resp.Output)
	}
}

func TestPanelKeyRepairRequest(t *testing.T) {
	a, err := Request{Op: "site.panelkey", Site: "blog"}.Argv()
	if err != nil || strings.Join(a, " ") != "site access -name blog -panelkey -apply" || !IsWrite("site.panelkey") {
		t.Errorf("%v %v", a, err)
	}
	for _, bad := range []string{"", "-x", "a b", "../x"} {
		if _, err := (Request{Op: "site.panelkey", Site: bad}).Argv(); err == nil {
			t.Errorf("site %q accepted", bad)
		}
	}
}
