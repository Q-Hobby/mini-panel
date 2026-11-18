package ctl

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestTheDomainOfASiteBecomesFixedFlags(t *testing.T) {
	if !Known("site.domain") || !IsWrite("site.domain") {
		t.Fatal("site.domain changes the machine: the helper must run it alone")
	}
	got, err := Request{Op: "site.domain", Site: "blog", Domain: "example.com", WWW: "bare"}.Argv()
	want := []string{"site", "domain", "-name", "blog", "-domain", "example.com", "-www", "bare", "-apply"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("%v\n%v\n%v", got, want, err)
	}
}

func TestADomainThatCouldBeAnOptionOrACommandIsRefused(t *testing.T) {
	for _, d := range []string{"", "-evil.com", "a b.com", "x.com;id", "EXAMPLE.com", "localhost", "a..com", "http://x.com", strings.Repeat("a", 70) + ".com"} {
		if _, err := (Request{Op: "site.domain", Site: "blog", Domain: d}).Argv(); err == nil {
			t.Errorf("domain %q must be refused", d)
		}
	}
	if _, err := (Request{Op: "site.domain", Site: "blog", Domain: "example.com", WWW: "both"}).Argv(); err == nil {
		t.Error("an unknown www choice must be refused")
	}
	if _, err := (Request{Op: "site.domain", Site: "../x", Domain: "example.com"}).Argv(); err == nil {
		t.Error("a bad site name must be refused")
	}
}

func TestSSLOperationsAreFixedCommands(t *testing.T) {
	for op, want := range map[string][]string{
		"site.ssl":     {"site", "ssl", "-name", "blog", "-apply"},
		"site.ssloff":  {"site", "ssl-off", "-name", "blog", "-apply"},
		"site.sslinfo": {"site", "ssl-info", "-name", "blog"},
	} {
		got, err := Request{Op: op, Site: "blog"}.Argv()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v %v", op, got, err)
		}
	}
	if IsWrite("site.sslinfo") || !IsWrite("site.ssl") || !IsWrite("site.sslcustom") || !IsWrite("site.csr") || !IsWrite("site.ssloff") || !IsWrite("site.sslredirect") {
		t.Error("reads and writes are mixed up")
	}
	got, _ := Request{Op: "site.sslredirect", Site: "blog", Redirect: true}.Argv()
	if !reflect.DeepEqual(got, []string{"site", "ssl-redirect", "-name", "blog", "-redirect=true", "-apply"}) {
		t.Errorf("%v", got)
	}
	got, _ = Request{Op: "site.sslredirect", Site: "blog"}.Argv()
	if !reflect.DeepEqual(got, []string{"site", "ssl-redirect", "-name", "blog", "-redirect=false", "-apply"}) {
		t.Errorf("%v", got)
	}
	got, _ = Request{Op: "site.csr", Site: "blog", Algo: "rsa"}.Argv()
	if !reflect.DeepEqual(got, []string{"site", "ssl-csr", "-name", "blog", "-algo", "rsa", "-apply"}) {
		t.Errorf("%v", got)
	}
	if _, err := (Request{Op: "site.csr", Site: "blog", Algo: "dsa; id"}).Argv(); err == nil {
		t.Error("an unknown key type must be refused")
	}
	if _, err := (Request{Op: "site.ssl", Site: "../x"}).Argv(); err == nil {
		t.Error("a bad site name must be refused")
	}
}

func TestACertificateAndItsKeyTravelOnStandardInputOnly(t *testing.T) {
	r := Request{Op: "site.sslcustom", Site: "blog", Key: "-----BEGIN PRIVATE KEY-----\nSECRET\n-----END PRIVATE KEY-----", Cert: "-----BEGIN CERTIFICATE-----\nx\n-----END CERTIFICATE-----", Chain: "chain", AllowUntrusted: true}
	argv, err := r.Argv()
	if err != nil || !reflect.DeepEqual(argv, []string{"site", "ssl-custom", "-name", "blog", "-apply"}) {
		t.Fatalf("%v %v", argv, err)
	}
	for _, a := range argv {
		if strings.Contains(a, "SECRET") {
			t.Error("the key must not be an argument")
		}
	}
	var in struct {
		Key, Cert, Chain string
		AllowUntrusted   bool `json:"allow_untrusted"`
	}
	if err := json.Unmarshal([]byte(r.Stdin()), &in); err != nil || !strings.Contains(in.Key, "SECRET") || in.Chain != "chain" || !in.AllowUntrusted {
		t.Errorf("stdin: %q %v", r.Stdin(), err)
	}
	if (Request{Op: "site.ssl", Site: "blog", Key: "SECRET"}).Stdin() != "" {
		t.Error("only site.sslcustom sends text on standard input")
	}
	for name, bad := range map[string]Request{
		"no certificate": {Op: "site.sslcustom", Site: "blog", Cert: "  "},
		"a huge key":     {Op: "site.sslcustom", Site: "blog", Cert: "x", Key: strings.Repeat("a", maxCertText+1)},
		"a nul":          {Op: "site.sslcustom", Site: "blog", Cert: "x\x00y"},
	} {
		if _, err := bad.Argv(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}
