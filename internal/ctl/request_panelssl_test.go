package ctl

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPanelSSLArgv(t *testing.T) {
	cases := []struct {
		r    Request
		want []string
	}{
		{Request{Op: "panel.sslinfo"}, []string{"panel", "ssl-info"}},
		{Request{Op: "panel.ssl", Domain: "panel.example.com"}, []string{"panel", "ssl", "-domain=panel.example.com", "-apply"}},
		{Request{Op: "panel.sslcustom", Domain: "panel.example.com", Key: "K", Cert: "C"}, []string{"panel", "ssl-custom", "-domain=panel.example.com", "-apply"}},
		{Request{Op: "panel.ssloff"}, []string{"panel", "ssl-off", "-apply"}},
	}
	for _, c := range cases {
		got, err := c.r.Argv()
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, %v want %v", c.r.Op, got, err, c.want)
		}
	}
}

func TestPanelSSLRefusesBadValues(t *testing.T) {
	bad := []Request{
		{Op: "panel.ssl", Domain: ""},
		{Op: "panel.ssl", Domain: "203.0.113.7"},
		{Op: "panel.ssl", Domain: "panel.example.com -apply"},
		{Op: "panel.ssl", Domain: "panel.example.com; id"},
		{Op: "panel.ssl", Domain: "-x.example.com"},
		{Op: "panel.sslcustom", Domain: "panel.example.com", Key: "K"},
		{Op: "panel.sslcustom", Domain: "panel.example.com", Cert: "C"},
		{Op: "panel.sslcustom", Domain: "panel.example.com", Key: "K", Cert: strings.Repeat("a", maxCertText+1)},
		{Op: "panel.sslcustom", Domain: "panel.example.com", Key: "K\x00", Cert: "C"},
	}
	for _, r := range bad {
		if got, err := r.Argv(); err == nil {
			t.Errorf("%+v must be refused, got %v", r, got)
		}
	}
}

// The private key and the certificate travel on standard input, never in the argument list.
func TestPanelCustomCertificateTravelsOnStandardInput(t *testing.T) {
	r := Request{Op: "panel.sslcustom", Domain: "panel.example.com", Key: "SECRETKEY", Cert: "CERT", Chain: "CHAIN", AllowUntrusted: true}
	argv, _ := r.Argv()
	if strings.Contains(strings.Join(argv, " "), "SECRETKEY") {
		t.Error("the key must not be an argument")
	}
	var in struct {
		Key, Cert, Chain string
		AllowUntrusted   bool `json:"allow_untrusted"`
	}
	if err := json.Unmarshal([]byte(r.Stdin()), &in); err != nil || in.Key != "SECRETKEY" || in.Cert != "CERT" || in.Chain != "CHAIN" || !in.AllowUntrusted {
		t.Errorf("stdin: %q %v", r.Stdin(), err)
	}
}

func TestPanelSSLOperationsAreClassified(t *testing.T) {
	for _, op := range []string{"panel.ssl", "panel.sslcustom", "panel.ssloff"} {
		if !IsWrite(op) {
			t.Errorf("%s changes the machine", op)
		}
	}
	if IsWrite("panel.sslinfo") || !Known("panel.sslinfo") {
		t.Error("panel.sslinfo only reads")
	}
}
