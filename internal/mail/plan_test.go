package mail

import (
	"strings"
	"testing"

	"mpn/internal/installer"
)

func goodState(t *testing.T) State {
	t.Helper()
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	priv, _, err := NewDKIMKey()
	if err != nil {
		t.Fatal(err)
	}
	return State{
		Host:    "mail.example.com",
		Domains: []string{"example.com"},
		Users:   []User{{"ten@example.com", h, 1024}},
		Aliases: map[string][]string{"info@example.com": {"ten@example.com"}},
		Keys:    map[string]string{"example.com": priv},
	}
}

func allArgs(p installer.Plan) string {
	var b strings.Builder
	for _, s := range p {
		for _, a := range s.Actions {
			b.WriteString(strings.Join(a.Args, " ") + "\n")
		}
	}
	return b.String()
}

func TestStateCheck(t *testing.T) {
	if err := goodState(t).Check(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*State){
		"no host":             func(s *State) { s.Host = "" },
		"duplicate domain":    func(s *State) { s.Domains = []string{"example.com", "example.com"} },
		"mailbox off-domain":  func(s *State) { s.Users[0].Address = "ten@other.org" },
		"duplicate mailbox":   func(s *State) { s.Users = append(s.Users, s.Users[0]) },
		"alias off-domain":    func(s *State) { s.Aliases = map[string][]string{"x@other.org": {"ten@example.com"}} },
		"alias is a mailbox":  func(s *State) { s.Aliases = map[string][]string{"ten@example.com": {"a@b.org"}} },
		"key off-domain":      func(s *State) { s.Keys = map[string]string{"other.org": s.Keys["example.com"]} },
		"key is not a key":    func(s *State) { s.Keys = map[string]string{"example.com": "garbage"} },
		"too many mailboxes":  func(s *State) { s.Users = make([]User, MaxMailboxes+1) },
		"bad domain in list":  func(s *State) { s.Domains = []string{"a b.com"} },
		"bad mailbox address": func(s *State) { s.Users[0].Address = "no-at-sign" },
	}
	for name, mut := range bad {
		s := goodState(t)
		mut(&s)
		if s.Check() == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestSetupPlanWritesEveryFileAndChecksBeforeStarting(t *testing.T) {
	p, err := SetupPlan(goodState(t))
	if err != nil {
		t.Fatal(err)
	}
	written := map[string]uint32{}
	for _, s := range p {
		for _, a := range s.Actions {
			if a.Path != "" {
				written[a.Path] = a.Mode
			}
		}
	}
	for _, f := range []string{PostfixMainFile, PostfixMasterFile, DovecotFile, OpenDKIMFile, VDomains, VMailbox, VAlias, UsersFile, DKIMKeyTable, DKIMSigningTable, DKIMKeyPath("example.com", DefaultSelector)} {
		if _, ok := written[f]; !ok {
			t.Errorf("%s is not written", f)
		}
	}
	// the password list and the private key are secrets: nobody but root and the one service that reads them
	for _, f := range []string{UsersFile, DKIMKeyPath("example.com", DefaultSelector)} {
		if written[f]&0o007 != 0 {
			t.Errorf("%s is readable by everyone (mode %o)", f, written[f])
		}
	}
	args := allArgs(p)
	checks := []string{"postfix check", "doveconf -n", "opendkim -n -x " + OpenDKIMFile}
	last := strings.LastIndex(args, "systemctl restart")
	if last < 0 {
		t.Fatal("the servers are never started")
	}
	for _, c := range checks {
		i := strings.Index(args, c)
		if i < 0 || i > last {
			t.Errorf("%q must run, and before the servers restart", c)
		}
	}
}

// Found on a real server: OpenDKIM refused the key ("is in group 114 which has multiple users") because Postfix had been added to the
// opendkim group so it could reach the socket. The key must be private to opendkim and Postfix must stay out of that group.
func TestPostfixIsNeverPutInTheGroupThatOwnsTheKeys(t *testing.T) {
	p, err := SetupPlan(goodState(t))
	if err != nil {
		t.Fatal(err)
	}
	a := allArgs(p)
	if strings.Contains(a, "usermod") {
		t.Errorf("no account may be added to a group:\n%s", a)
	}
	if !strings.Contains(OpenDKIMConf(), "UserID opendkim:postfix") {
		t.Error("opendkim must run with the group postfix so Postfix can reach its socket")
	}
	key := DKIMKeyPath("example.com", DefaultSelector)
	for _, s := range p {
		for _, act := range s.Actions {
			if act.Path == key && act.Mode != 0o600 {
				t.Errorf("the private key is written with mode %o, want 0600", act.Mode)
			}
		}
	}
	if !strings.Contains(a, "chown opendkim:opendkim "+key) {
		t.Errorf("the key must belong to opendkim:\n%s", a)
	}
}

// Found on a real server: removing a domain left its private DKIM key on the disk.
func TestTheKeyOfARemovedDomainIsDeleted(t *testing.T) {
	s := goodState(t)
	s.Domains = append(s.Domains, "other.org")
	p, err := SetupPlan(s)
	if err != nil {
		t.Fatal(err)
	}
	a := allArgs(p)
	if !strings.Contains(a, "case ' example.com ' in") {
		t.Errorf("only the domains that still have a key may be kept:\n%s", a)
	}
	if !strings.Contains(a, "rm -rf -- \"$d\"") {
		t.Errorf("a key folder with no domain must be removed:\n%s", a)
	}
	// with no key at all the list is empty, so every folder goes
	s.Keys = map[string]string{}
	p, _ = SetupPlan(s)
	if !strings.Contains(allArgs(p), "case '  ' in") {
		t.Error("with no key left, no folder may be kept")
	}
	// the glob only ever looks inside the key folder
	if !strings.HasPrefix(staleKeysScript(nil), "cd "+DKIMDir+" 2>/dev/null || exit 0") {
		t.Error("the script must not run outside the key folder")
	}
}

func TestNoPasswordOrKeyOnACommandLine(t *testing.T) {
	s := goodState(t)
	p, _ := SetupPlan(s)
	args := allArgs(p)
	if strings.Contains(args, "BEGIN RSA PRIVATE KEY") || strings.Contains(args, s.Users[0].Hash) || strings.Contains(args, "correct horse") {
		t.Error("a secret is on a command line, where every account of the machine can read it")
	}
}

func TestSelfSignedCertificateIsOnlyMadeWhenMissing(t *testing.T) {
	sc := selfSignedScript("mail.example.com")
	if !strings.HasPrefix(sc, "[ -s "+CertFile+" ] && [ -s "+KeyFile+" ] && exit 0") {
		t.Errorf("a real certificate must never be replaced:\n%s", sc)
	}
	if !strings.Contains(sc, "-subj '/CN=mail.example.com'") || !strings.Contains(sc, "chmod 0600 "+KeyFile) {
		t.Errorf("the key must be private:\n%s", sc)
	}
}

func TestPackagesPlanAnswersPostfixQuestionsAndStopsTheServers(t *testing.T) {
	p, err := PackagesPlan("mail.example.com")
	if err != nil {
		t.Fatal(err)
	}
	a := allArgs(p)
	for _, want := range []string{"postfix/main_mailer_type select Internet Site", "postfix/mailname string mail.example.com", "postfix", "dovecot-imapd", "opendkim-tools", "systemctl stop postfix dovecot opendkim"} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %q:\n%s", want, a)
		}
	}
	if _, err := PackagesPlan("x'; rm -rf / #"); err == nil {
		t.Error("the host name goes into a shell line; it must be checked")
	}
}

func TestMailboxDirScriptBuildsThePathFromCheckedParts(t *testing.T) {
	s, err := MailboxDirScript("ten@example.com")
	if err != nil || s != "rm -rf -- /var/vmail/example.com/ten" {
		t.Errorf("%q %v", s, err)
	}
	for _, bad := range []string{"../../etc@example.com", "a/b@example.com", "@example.com", "ten@..", ""} {
		if _, err := MailboxDirScript(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestTheChallengeBlockServesOnlyTheChallenge(t *testing.T) {
	c, err := ACMEConfig("mail.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"server_name mail.example.com;", "listen 80;", "/.well-known/acme-challenge/", "location / { return 404; }"} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q:\n%s", want, c)
		}
	}
	if strings.Contains(c, "proxy_pass") {
		t.Error("nothing but the challenge may be served on this name")
	}
	for _, bad := range []string{"", "localhost", "a.com; return 200", "a.com\nserver_name evil.com"} {
		if _, err := ACMEConfig(bad); err == nil {
			t.Errorf("%q must be refused: it goes into an nginx file", bad)
		}
	}
}

func TestOffKeepsTheData(t *testing.T) {
	a := allArgs(OffPlan())
	if strings.Contains(a, "rm ") || strings.Contains(a, MailRoot) {
		t.Errorf("turning mail off must not delete mailboxes:\n%s", a)
	}
}
