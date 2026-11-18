package ctl

import (
	"reflect"
	"testing"
)

func TestSecurityArgv(t *testing.T) {
	cases := []struct {
		r    Request
		want []string
	}{
		{Request{Op: "security.status"}, []string{"security", "status", "-json"}},
		{Request{Op: "security.waf", SecMode: "detect"}, []string{"security", "waf", "-mode=detect", "-apply"}},
		{Request{Op: "security.f2b", SecOn: true, SecIgnore: []string{"203.0.113.7", "198.51.100.9/24"}}, []string{"security", "f2b", "-on=true", "-ignore=203.0.113.7,198.51.100.0/24", "-apply"}},
		{Request{Op: "security.f2b"}, []string{"security", "f2b", "-on=false", "-ignore=", "-apply"}},
		{Request{Op: "security.unban", Jail: "mpn-panel", IP: "::ffff:1.2.3.4"}, []string{"security", "unban", "-jail=mpn-panel", "-ip=1.2.3.4", "-apply"}},
	}
	for _, c := range cases {
		got, err := c.r.Argv()
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, %v want %v", c.r.Op, got, err, c.want)
		}
	}
}

func TestSecurityArgvRefusesBadValues(t *testing.T) {
	bad := []Request{
		{Op: "security.waf", SecMode: ""},
		{Op: "security.waf", SecMode: "On; rm -rf /"},
		{Op: "security.f2b", SecOn: true, SecIgnore: []string{"1.2.3.4 -x"}},
		{Op: "security.f2b", SecOn: true, SecIgnore: []string{"$(id)"}},
		{Op: "security.unban", Jail: "sshd; reboot", IP: "1.2.3.4"},
		{Op: "security.unban", Jail: "sshd", IP: "1.2.3.4; id"},
		{Op: "security.unban", Jail: "sshd", IP: "10.0.0.0/8"},
		{Op: "security.unban", Jail: "", IP: "1.2.3.4"},
	}
	for _, r := range bad {
		if got, err := r.Argv(); err == nil {
			t.Errorf("%+v must be refused, got %v", r, got)
		}
	}
}

func TestSecurityOperationsAreClassified(t *testing.T) {
	for _, op := range []string{"security.waf", "security.f2b", "security.unban"} {
		if !IsWrite(op) || !Known(op) {
			t.Errorf("%s changes the machine and must run one at a time", op)
		}
	}
	if IsWrite("security.status") || !Known("security.status") {
		t.Error("security.status only reads")
	}
}
