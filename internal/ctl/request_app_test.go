package ctl

import (
	"reflect"
	"strings"
	"testing"
)

func wpInstall() Request {
	return Request{Op: "app.install", Site: "blog", App: "wordpress", AppTitle: "Blog của tôi", AppAdmin: "chusite", AppEmail: "chu@example.com", AppLocale: "vi"}
}

func TestAppInstallArgv(t *testing.T) {
	got, err := wpInstall().Argv()
	want := []string{"app", "install", "-site=blog", "-app=wordpress", "-title=Blog của tôi", "-admin=chusite", "-email=chu@example.com", "-locale=vi", "-apply"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("wordpress: %v %v", got, err)
	}
	got, err = Request{Op: "app.install", Site: "blog", App: "laravel"}.Argv()
	if err != nil || !reflect.DeepEqual(got, []string{"app", "install", "-site=blog", "-app=laravel", "-apply"}) {
		t.Errorf("laravel: %v %v", got, err)
	}
}

func TestAppInstallRefusesBadValues(t *testing.T) {
	mut := func(f func(*Request)) Request { r := wpInstall(); f(&r); return r }
	bad := map[string]Request{
		"no site":        mut(func(r *Request) { r.Site = "" }),
		"path site":      mut(func(r *Request) { r.Site = "../etc" }),
		"unknown app":    mut(func(r *Request) { r.App = "wordpress; id" }),
		"empty app":      mut(func(r *Request) { r.App = "" }),
		"no title":       mut(func(r *Request) { r.AppTitle = "" }),
		"title newline":  mut(func(r *Request) { r.AppTitle = "a\n-apply" }),
		"admin quote":    mut(func(r *Request) { r.AppAdmin = "x';id" }),
		"admin short":    mut(func(r *Request) { r.AppAdmin = "ab" }),
		"email":          mut(func(r *Request) { r.AppEmail = "x" }),
		"email injected": mut(func(r *Request) { r.AppEmail = "a@example.com --admin_user=root" }),
		"locale":         mut(func(r *Request) { r.AppLocale = "xx" }),
	}
	for name, r := range bad {
		if got, err := r.Argv(); err == nil {
			t.Errorf("%s must be refused, got %v", name, got)
		}
	}
	// a value that starts with "-" is attached to its flag with "=", so it can never be read as a flag of its own
	r := wpInstall()
	r.AppTitle = "-apply"
	argv, err := r.Argv()
	if err != nil || !strings.Contains(strings.Join(argv, "\n"), "-title=-apply") {
		t.Errorf("%v %v", argv, err)
	}
}

func TestWordPressManagerArgv(t *testing.T) {
	for _, c := range []struct {
		r    Request
		want []string
	}{
		{Request{Op: "wp.status", Site: "blog"}, []string{"wp", "status", "-json", "-site=blog"}},
		{Request{Op: "wp.search", Site: "blog", WPKind: "plugin", WPTerm: " seo cache "}, []string{"wp", "search", "-site=blog", "-kind=plugin", "-term=seo cache"}},
		{Request{Op: "wp.run", Site: "blog", WPKind: "plugin", WPAction: "install", WPSlug: "contact-form-7"}, []string{"wp", "run", "-site=blog", "-kind=plugin", "-action=install", "-slug=contact-form-7", "-apply"}},
		{Request{Op: "wp.run", Site: "blog", WPKind: "theme", WPAction: "activate", WPSlug: "twentytwentyfour"}, []string{"wp", "run", "-site=blog", "-kind=theme", "-action=activate", "-slug=twentytwentyfour", "-apply"}},
		{Request{Op: "wp.run", Site: "blog", WPKind: "core", WPAction: "update"}, []string{"wp", "run", "-site=blog", "-kind=core", "-action=update", "-slug=", "-apply"}},
	} {
		got, err := c.r.Argv()
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: got %v, %v want %v", c.r, got, err, c.want)
		}
	}
}

func TestWordPressManagerRefusesWhatWouldBeDangerous(t *testing.T) {
	bad := []Request{
		{Op: "wp.run", Site: "blog", WPKind: "plugin", WPAction: "install", WPSlug: "https://evil.example/x.zip"},
		{Op: "wp.run", Site: "blog", WPKind: "plugin", WPAction: "install", WPSlug: "../../x"},
		{Op: "wp.run", Site: "blog", WPKind: "plugin", WPAction: "install", WPSlug: "a b"},
		{Op: "wp.run", Site: "blog", WPKind: "plugin", WPAction: "install", WPSlug: "Seo"},
		{Op: "wp.run", Site: "blog", WPKind: "plugin", WPAction: "delete", WPSlug: "a;rm"},
		{Op: "wp.run", Site: "blog", WPKind: "plugin", WPAction: "eval", WPSlug: "x"},
		{Op: "wp.run", Site: "blog", WPKind: "option", WPAction: "update", WPSlug: "x"},
		{Op: "wp.run", Site: "blog", WPKind: "theme", WPAction: "deactivate", WPSlug: "x"},
		{Op: "wp.run", Site: "blog", WPKind: "core", WPAction: "update", WPSlug: "6.9"},
		{Op: "wp.run", Site: "blog", WPKind: "core", WPAction: "download"},
		{Op: "wp.run", Site: "", WPKind: "plugin", WPAction: "update", WPSlug: "x"},
		{Op: "wp.search", Site: "blog", WPKind: "plugin", WPTerm: ""},
		{Op: "wp.search", Site: "blog", WPKind: "plugin", WPTerm: "a; id"},
		{Op: "wp.search", Site: "blog", WPKind: "plugin", WPTerm: strings.Repeat("a", 61)},
		{Op: "wp.search", Site: "blog", WPKind: "core", WPTerm: "seo"},
	}
	for _, r := range bad {
		if got, err := r.Argv(); err == nil {
			t.Errorf("%+v must be refused, got %v", r, got)
		}
	}
}

func TestAppAndWordPressOperationsAreClassified(t *testing.T) {
	for _, op := range []string{"app.install", "wp.run"} {
		if !IsWrite(op) || !Known(op) {
			t.Errorf("%s changes the machine and must run one at a time", op)
		}
	}
	for _, op := range []string{"wp.status", "wp.search"} {
		if IsWrite(op) || !Known(op) {
			t.Errorf("%s only reads", op)
		}
	}
}
