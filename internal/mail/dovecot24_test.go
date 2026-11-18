package mail

import (
	"strings"
	"testing"
)

func TestDovecotIs24(t *testing.T) {
	for v, want := range map[string]bool{
		"2.4.1-4 (7d8c0e5759)": true,
		"2.4.0 (abc)\n":        true,
		"2.3.21 (47349e2482)":  false,
		"2.3.16":               false,
		"3.0.0":                true,
		"":                     false,
		"dovecot: not found":   false,
	} {
		if got := DovecotIs24(v); got != want {
			t.Errorf("DovecotIs24(%q) = %v, want %v", v, got, want)
		}
	}
}

// The 2.4 file starts with the version setting (doveconf refuses anything else first), keeps plain IMAP on the loopback, and uses
// none of the 2.3 names that 2.4 rejects.
func TestDovecot24Syntax(t *testing.T) {
	d, err := Dovecot("mail.example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(d, "dovecot_config_version = 2.4.0\n") {
		t.Errorf("the first setting must be dovecot_config_version:\n%s", d)
	}
	i := strings.Index(d, "inet_listener imap {")
	if i < 0 || !strings.Contains(d[i:i+80], "listen = 127.0.0.1") {
		t.Errorf("port 143 must listen on the loopback only:\n%s", d)
	}
	for _, want := range []string{"ssl = required", "auth_allow_cleartext = no", "passdb passwd-file {", "passwd_file_path = " + UsersFile, "mail_driver = maildir", "ssl_server_cert_file = " + CertFile} {
		if !strings.Contains(d, want) {
			t.Errorf("dovecot.conf (2.4) lacks %q", want)
		}
	}
	for _, old := range []string{"mail_location", "disable_plaintext_auth", "ssl_cert =", "ssl_key =", "driver = passwd-file", "%d", "%n", "quota_rule", "$mail_plugins"} {
		if strings.Contains(d, old) {
			t.Errorf("dovecot.conf (2.4) still has the 2.3 form %q", old)
		}
	}
}

func TestUsersTextQuotaFor24(t *testing.T) {
	h, _ := HashPassword("correct horse battery")
	txt, err := UsersText([]User{{"a@example.com", h, 500}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(txt, "userdb_quota_storage_size=500M") || strings.Contains(txt, "quota_rule") {
		t.Errorf("2.4 takes the size of the named quota:\n%s", txt)
	}
}

func TestPackagesPlanRemovesConflictingServersFirst(t *testing.T) {
	p, err := PackagesPlan("mail.example.com")
	if err != nil {
		t.Fatal(err)
	}
	acts := p[0].Actions
	if len(acts) == 0 || !strings.Contains(strings.Join(acts[0].Args, " "), "exim4-daemon-light") {
		t.Errorf("the first action must remove Exim and the others, before anything is installed:\n%s", p)
	}
	script := removeConflictsScript()
	for _, pkg := range ConflictingPackages {
		if !strings.Contains(script, pkg) {
			t.Errorf("the script does not look for %s", pkg)
		}
	}
	if !strings.Contains(script, "install ok installed") || !strings.Contains(script, `[ -n "$found" ] || exit 0`) {
		t.Errorf("only installed packages are removed, and a machine without them changes nothing:\n%s", script)
	}
}

func TestWebmailPinsBothCPUs(t *testing.T) {
	for arch, sum := range map[string]string{"amd64": webmailSHA256AMD64, "arm64": webmailSHA256ARM64} {
		if len(sum) != 64 || strings.Trim(sum, "0123456789abcdef") != "" {
			t.Errorf("%s: %q is not a SHA-256", arch, sum)
		}
	}
	if webmailSHA256AMD64 == webmailSHA256ARM64 {
		t.Error("the two files cannot have the same checksum")
	}
	if !strings.HasSuffix(WebmailURL, "/v"+WebmailVersion+"/mpn-mail_linux_"+WebmailArch) {
		t.Errorf("WebmailURL %q does not name the file for %s", WebmailURL, WebmailArch)
	}
}
