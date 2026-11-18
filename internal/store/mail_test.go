package store

import (
	"errors"
	"path/filepath"
	"testing"

	"mpn/internal/mail"
)

func mailStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "mpn.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMailSettings(t *testing.T) {
	s := mailStore(t)
	if on, _ := s.MailOn(); on {
		t.Error("mail is off until the person turns it on")
	}
	if err := s.SetMailOn(true); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.MailOn(); !on {
		t.Error("mail should be on")
	}
	if err := s.SetMailHost("mail.example.com"); err != nil {
		t.Fatal(err)
	}
	if h, _ := s.MailHost(); h != "mail.example.com" {
		t.Errorf("host %q", h)
	}
	if s.SetMailHost("not a host") == nil {
		t.Error("a bad host must be refused")
	}
}

func TestMailboxesAliasesAndTheStateTheyMake(t *testing.T) {
	s := mailStore(t)
	key, _, err := mail.NewDKIMKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddMailDomain("example.com", key); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMailDomain("example.com", ""); !errors.Is(err, ErrExists) {
		t.Errorf("a domain twice: %v", err)
	}
	h, _ := mail.HashPassword("correct horse battery")
	if err := s.AddMailbox("ten@example.com", h, 500); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMailbox("ten@example.com", h, 0); !errors.Is(err, ErrExists) {
		t.Errorf("a mailbox twice: %v", err)
	}
	if err := s.AddMailbox("x@nowhere.org", h, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("a mailbox on an unknown domain: %v", err)
	}
	if s.AddMailbox("a@example.com", "plain", 0) == nil {
		t.Error("only a hash made by mpn may be stored")
	}
	// an address is either a mailbox or an alias
	if err := s.SetMailAlias("ten@example.com", []string{"a@b.org"}); !errors.Is(err, ErrExists) {
		t.Errorf("an alias over a mailbox: %v", err)
	}
	if err := s.SetMailAlias("info@example.com", []string{"ten@example.com", "ten@example.com", "z@b.org"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMailbox("info@example.com", h, 0); !errors.Is(err, ErrExists) {
		t.Errorf("a mailbox over an alias: %v", err)
	}
	if s.SetMailAlias("info@example.com", nil) == nil {
		t.Error("an alias without a target")
	}

	st, err := s.MailState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Domains) != 1 || len(st.Users) != 1 || len(st.Aliases["info@example.com"]) != 2 || st.Keys["example.com"] == "" {
		t.Fatalf("state: %+v", st)
	}
	st.Host = "mail.example.com"
	if err := st.Check(); err != nil {
		t.Errorf("the state of the store must be one the files can express: %v", err)
	}

	// the list a page shows has the quota but never the hash
	boxes, _ := s.Mailboxes("example.com")
	if len(boxes) != 1 || boxes[0].QuotaMB != 500 {
		t.Errorf("mailboxes %+v", boxes)
	}
	if err := s.SetMailboxQuota("ten@example.com", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMailboxPassword("ten@example.com", h); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMailboxPassword("nobody@example.com", h); !errors.Is(err, ErrNotFound) {
		t.Errorf("a password for a mailbox that does not exist: %v", err)
	}

	// deleting the domain takes everything of it along
	if err := s.DeleteMailDomain("example.com"); err != nil {
		t.Fatal(err)
	}
	if b, _ := s.Mailboxes(""); len(b) != 0 {
		t.Errorf("mailboxes remain: %+v", b)
	}
	if a, _ := s.MailAliases(""); len(a) != 0 {
		t.Errorf("aliases remain: %+v", a)
	}
}

func TestDomainListCountsWhatBelongsToIt(t *testing.T) {
	s := mailStore(t)
	_ = s.AddMailDomain("a.com", "")
	_ = s.AddMailDomain("b.com", "")
	h, _ := mail.HashPassword("correct horse battery")
	_ = s.AddMailbox("x@a.com", h, 0)
	_ = s.AddMailbox("y@a.com", h, 0)
	_ = s.SetMailAlias("z@b.com", []string{"x@a.com"})
	ds, err := s.MailDomains()
	if err != nil || len(ds) != 2 {
		t.Fatalf("%v %v", ds, err)
	}
	if ds[0].Name != "a.com" || ds[0].Mailboxes != 2 || ds[0].HasKey || ds[1].Aliases != 1 {
		t.Errorf("counts: %+v", ds)
	}
}
