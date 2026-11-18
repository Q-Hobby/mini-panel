package mail

import (
	"strings"
	"testing"
)

func TestValidDomain(t *testing.T) {
	for _, ok := range []string{"example.com", "mail.example.co.uk", "a-b.vn", "xn--abc.com"} {
		if err := ValidDomain(ok); err != nil {
			t.Errorf("%q must be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "localhost", "Example.com", "exa mple.com", "example..com", "-a.com", "a-.com", "example.com.", "a.b", "1.2.3.4", "ex@mple.com", "a.com\n", strings.Repeat("a", 64) + ".com"} {
		if ValidDomain(bad) == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestSplitAddress(t *testing.T) {
	l, d, err := SplitAddress("ten.ho+tag@example.com")
	if err != nil || l != "ten.ho+tag" || d != "example.com" {
		t.Fatalf("%q %q %v", l, d, err)
	}
	for _, bad := range []string{"", "a", "@example.com", "a@", "a@@example.com", "a@b@example.com", "A@example.com", "a b@example.com", "a..b@example.com", "a.@example.com", ".a@example.com", "a@localhost", "a\n@example.com", "a,b@example.com", "a:b@example.com", "a/b@example.com"} {
		if _, _, err := SplitAddress(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestPasswordRules(t *testing.T) {
	for _, bad := range []string{"", "short", strings.Repeat("a", 129), "has\nnewline123", "tab\there12345", "nul\x00byte12345"} {
		if ValidPassword(bad) == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	for _, ok := range []string{"correct horse battery", "mật khẩu rất dài của tôi"} {
		if err := ValidPassword(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
}

// The hash goes into a colon-separated file read by Dovecot. It must be one line, hold no colon and no secret, and differ every time.
func TestHashPassword(t *testing.T) {
	pw := "correct horse battery"
	a, err := HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := HashPassword(pw)
	if a == b {
		t.Error("two hashes of one password must differ (random salt)")
	}
	if !ValidHash(a) || !strings.HasPrefix(a, "{ARGON2ID}$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("unexpected hash %q", a)
	}
	if strings.ContainsAny(a, ":\n\r \t") || strings.Contains(a, pw) {
		t.Errorf("hash %q must be one token without the password", a)
	}
	for _, bad := range []string{"", "plain", a + ":x", a + "\n", "{PLAIN}secret", "{ARGON2ID}$argon2id$v=19$m=1,t=1,p=1$AAAA$BBBB"} {
		if ValidHash(bad) {
			t.Errorf("%q must not pass as a hash", bad)
		}
	}
	if _, err := HashPassword("short"); err == nil {
		t.Error("a short password must not be hashed")
	}
}

func TestDKIMKey(t *testing.T) {
	priv, txt, err := NewDKIMKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(priv, "-----BEGIN RSA PRIVATE KEY-----") || !strings.HasPrefix(txt, "v=DKIM1; k=rsa; p=") {
		t.Fatalf("unexpected key material: %.40q %.30q", priv, txt)
	}
	again, err := DKIMTextFromPrivate(priv)
	if err != nil || again != txt {
		t.Errorf("the record must come back from the stored key: %v", err)
	}
	if _, err := DKIMTextFromPrivate("not a key"); err == nil {
		t.Error("garbage is not a key")
	}
}

func TestRecords(t *testing.T) {
	r := Records("example.com", "mail.example.com", "mpn", "v=DKIM1; k=rsa; p=AAAA")
	want := map[string]string{
		"MX example.com":                 "10 mail.example.com.",
		"TXT example.com":                "v=spf1 mx -all",
		"TXT mpn._domainkey.example.com": "v=DKIM1; k=rsa; p=AAAA",
	}
	got := map[string]string{}
	for _, x := range r {
		got[x.Type+" "+x.Name] = x.Value
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if !strings.HasPrefix(got["TXT _dmarc.example.com"], "v=DMARC1;") {
		t.Errorf("dmarc: %v", got)
	}
}
