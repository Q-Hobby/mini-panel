package ctl

import (
	"strings"
	"testing"
)

func TestMailRequestsBecomeCheckedArguments(t *testing.T) {
	good := map[string]Request{
		"mail status -json":                                                        {Op: "mail.status"},
		"mail records -domain=example.com -json":                                   {Op: "mail.records", MailDomain: "example.com"},
		"mail enable -host=mail.example.com -apply":                                {Op: "mail.enable", MailHost: "mail.example.com"},
		"mail disable -apply":                                                      {Op: "mail.disable"},
		"mail ssl -apply":                                                          {Op: "mail.ssl"},
		"mail webmail -on=true -apply":                                             {Op: "mail.webmail", Enable: true},
		"mail webmail -on=false -apply":                                            {Op: "mail.webmail"},
		"mail domain-add -domain=example.com -apply":                               {Op: "mail.domainadd", MailDomain: "example.com"},
		"mail domain-del -domain=example.com -purge=true -apply":                   {Op: "mail.domaindel", MailDomain: "example.com", Purge: true},
		"mail box-add -address=ten@example.com -quota=500 -apply":                  {Op: "mail.boxadd", MailAddress: "ten@example.com", MailQuota: 500, MailPassword: "correct horse battery"},
		"mail box-passwd -address=ten@example.com -apply":                          {Op: "mail.boxpasswd", MailAddress: "ten@example.com", MailPassword: "correct horse battery"},
		"mail box-quota -address=ten@example.com -quota=0 -apply":                  {Op: "mail.boxquota", MailAddress: "ten@example.com"},
		"mail box-del -address=ten@example.com -purge=false -apply":                {Op: "mail.boxdel", MailAddress: "ten@example.com"},
		"mail alias-set -address=info@example.com -targets=a@b.org,c@d.org -apply": {Op: "mail.aliasset", MailAddress: "info@example.com", MailTargets: []string{"a@b.org", "c@d.org"}},
		"mail alias-del -address=info@example.com -apply":                          {Op: "mail.aliasdel", MailAddress: "info@example.com"},
	}
	for want, r := range good {
		argv, err := r.Argv()
		if err != nil {
			t.Errorf("%s: %v", r.Op, err)
			continue
		}
		if got := strings.Join(argv, " "); got != want {
			t.Errorf("%s\n got: %s\nwant: %s", r.Op, got, want)
		}
	}
}

func TestBadMailValuesNeverReachTheCommandLine(t *testing.T) {
	bad := []Request{
		{Op: "mail.records", MailDomain: "-apply"},
		{Op: "mail.records", MailDomain: "a b.com"},
		{Op: "mail.enable", MailHost: ""},
		{Op: "mail.enable", MailHost: "mail.example.com -apply"},
		{Op: "mail.domainadd", MailDomain: "example.com\n-apply"},
		{Op: "mail.boxadd", MailAddress: "../x@example.com", MailPassword: "correct horse battery"},
		{Op: "mail.boxadd", MailAddress: "ten@example.com", MailPassword: "short"},
		{Op: "mail.boxadd", MailAddress: "ten@example.com", MailPassword: "correct horse battery", MailQuota: -1},
		{Op: "mail.boxpasswd", MailAddress: "ten@example.com", MailPassword: "has\nnewline123"},
		{Op: "mail.boxdel", MailAddress: "-purge=true@example.com"},
		{Op: "mail.aliasset", MailAddress: "info@example.com"},
		{Op: "mail.aliasset", MailAddress: "info@example.com", MailTargets: []string{"a@b.org,evil@c.org"}},
		{Op: "mail.aliasset", MailAddress: "info@example.com", MailTargets: make([]string, 21)},
		{Op: "mail.aliasdel", MailAddress: "no-at-sign"},
	}
	for _, r := range bad {
		if argv, err := r.Argv(); err == nil {
			t.Errorf("%+v must be refused, got %v", r, argv)
		}
	}
}

// The password of a mailbox goes to mpn on standard input, never in the arguments or the answer.
func TestMailPasswordTravelsOnStdinOnly(t *testing.T) {
	for _, op := range []string{"mail.boxadd", "mail.boxpasswd"} {
		r := Request{Op: op, MailAddress: "ten@example.com", MailPassword: "correct horse battery"}
		if r.Stdin() != "correct horse battery\n" {
			t.Errorf("%s: stdin is %q", op, r.Stdin())
		}
		argv, err := r.Argv()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(argv, " "), "correct horse") {
			t.Errorf("%s: the password is in the arguments", op)
		}
	}
	if (Request{Op: "mail.status", MailPassword: "x"}).Stdin() != "" {
		t.Error("only the two password operations send anything on stdin")
	}
}

func TestMailOperationsAreKnownAndWritesAreMarked(t *testing.T) {
	for _, op := range []string{"mail.enable", "mail.disable", "mail.ssl", "mail.domainadd", "mail.domaindel", "mail.boxadd", "mail.boxdel", "mail.boxpasswd", "mail.boxquota", "mail.aliasset", "mail.aliasdel"} {
		if !Known(op) || !IsWrite(op) {
			t.Errorf("%s must be a known write", op)
		}
	}
	for _, op := range []string{"mail.status", "mail.records"} {
		if !Known(op) || IsWrite(op) {
			t.Errorf("%s must be a known read", op)
		}
	}
}
