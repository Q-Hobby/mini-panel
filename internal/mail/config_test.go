package mail

import (
	"strings"
	"testing"
)

func TestPostfixMainNamesTheHostAndTheLists(t *testing.T) {
	m, err := PostfixMain("mail.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"myhostname = mail.example.com",
		"virtual_mailbox_domains = texthash:" + VDomains,
		"virtual_mailbox_maps = texthash:" + VMailbox,
		"virtual_alias_maps = texthash:" + VAlias,
		"virtual_transport = lmtp:unix:private/dovecot-lmtp",
		"smtpd_tls_cert_file = " + CertFile,
		"smtpd_milters = unix:opendkim/opendkim.sock",
		"smtpd_relay_restrictions = permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination",
	} {
		if !strings.Contains(m, want) {
			t.Errorf("main.cf lacks %q", want)
		}
	}
	// plain port 25 must never offer a login, or a stolen password could relay from anywhere without TLS
	if !strings.Contains(m, "smtpd_sasl_auth_enable = no") {
		t.Error("the login belongs to the submission ports only")
	}
	if _, err := PostfixMain("localhost"); err == nil {
		t.Error("a host name without a dot is not a mail host")
	}
	if _, err := PostfixMain("a.com\nmynetworks = 0.0.0.0/0"); err == nil {
		t.Error("a newline in the host name would inject a setting")
	}
}

func TestSubmissionPortsDemandLoginAndTLS(t *testing.T) {
	for _, svc := range []string{"submission inet", "smtps     inet"} {
		i := strings.Index(PostfixMaster, svc)
		if i < 0 {
			t.Fatalf("master.cf lacks %q", svc)
		}
		block := PostfixMaster[i:]
		if j := strings.Index(block[1:], "\npickup"); j > 0 {
			block = block[:j]
		}
		if !strings.Contains(block, "smtpd_sasl_auth_enable=yes") || !strings.Contains(block, "reject") {
			t.Errorf("%s must require a login and reject the rest:\n%s", svc, block)
		}
	}
	if !strings.Contains(PostfixMaster, "smtpd_tls_security_level=encrypt") || !strings.Contains(PostfixMaster, "smtpd_tls_wrappermode=yes") {
		t.Error("587 needs STARTTLS and 465 needs TLS from the first byte")
	}
}

func TestDovecotListensOnTheLoopbackForPlainImap(t *testing.T) {
	d, err := Dovecot("mail.example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(d, "inet_listener imap {")
	if i < 0 || !strings.Contains(d[i:i+80], "address = 127.0.0.1") {
		t.Errorf("port 143 must listen on the loopback only:\n%s", d)
	}
	for _, want := range []string{"ssl = required", "disable_plaintext_auth = yes", "driver = passwd-file", UsersFile, "mail_location = maildir:" + MailRoot + "/%d/%n"} {
		if !strings.Contains(d, want) {
			t.Errorf("dovecot.conf lacks %q", want)
		}
	}
}

func TestUsersTextRefusesAnythingButOurHash(t *testing.T) {
	h, _ := HashPassword("correct horse battery")
	txt, err := UsersText([]User{{"b@example.com", h, 0}, {"a@example.com", h, 500}}, false)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(txt), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[1], "a@example.com:") || !strings.HasPrefix(lines[2], "b@example.com:") {
		t.Fatalf("sorted, one line each:\n%s", txt)
	}
	if !strings.Contains(lines[1], "userdb_quota_rule=*:storage=500M") || strings.Contains(lines[2], "quota") {
		t.Errorf("only a mailbox with a size gets a quota rule:\n%s", txt)
	}
	// Dovecot reads user:password:uid:gid:gecos:home:shell:extra. The quota rule itself holds a colon, so count the fields before it.
	for _, l := range lines[1:] {
		head, _, _ := strings.Cut(l, "userdb_")
		if n := strings.Count(head, ":"); n != 7 {
			t.Errorf("the extra field must be the eighth: %d colons before it in %q", n, l)
		}
	}
	for name, u := range map[string]User{
		"plain password": {"a@example.com", "secret", 0},
		"extra field":    {"a@example.com", h + ":0:0", 0},
		"newline":        {"a@example.com", h + "\nb@example.com:x", 0},
		"bad address":    {"a@@example.com", h, 0},
		"bad quota":      {"a@example.com", h, -1},
	} {
		if _, err := UsersText([]User{u}, false); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestListsRefuseWhatCouldAddALine(t *testing.T) {
	if txt, err := DomainsText([]string{"b.com", "a.com"}); err != nil || !strings.Contains(txt, "a.com OK\nb.com OK\n") {
		t.Errorf("%q %v", txt, err)
	}
	if _, err := DomainsText([]string{"a.com\nevil.com"}); err == nil {
		t.Error("newline in a domain")
	}
	if txt, err := MailboxesText([]string{"ten@example.com"}); err != nil || !strings.Contains(txt, "ten@example.com example.com/ten/\n") {
		t.Errorf("%q %v", txt, err)
	}
	if _, err := MailboxesText([]string{"../x@example.com"}); err == nil {
		t.Error("a path must not get into the maildir column")
	}
	if txt, err := AliasesText(map[string][]string{"info@example.com": {"a@example.com", "b@other.org"}}); err != nil || !strings.Contains(txt, "info@example.com a@example.com, b@other.org\n") {
		t.Errorf("%q %v", txt, err)
	}
	if _, err := AliasesText(map[string][]string{"info@example.com": {}}); err == nil {
		t.Error("an alias with no target")
	}
	if _, err := AliasesText(map[string][]string{"info@example.com": {"a@example.com\nx@y.com"}}); err == nil {
		t.Error("newline in a target")
	}
}

func TestDKIMTables(t *testing.T) {
	k, s, err := DKIMTables([]string{"example.com"}, "mpn")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(k, "mpn._domainkey.example.com example.com:mpn:"+DKIMDir+"/example.com/mpn.private\n") {
		t.Errorf("key table:\n%s", k)
	}
	if !strings.Contains(s, "*@example.com mpn._domainkey.example.com\n") {
		t.Errorf("signing table:\n%s", s)
	}
	if _, _, err := DKIMTables([]string{"example.com"}, "../x"); err == nil {
		t.Error("a selector is part of a path")
	}
	if _, _, err := DKIMTables([]string{"a b.com"}, "mpn"); err == nil {
		t.Error("a bad domain")
	}
}
