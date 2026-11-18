package acme

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccountKeyIsCreatedOnceAndReused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "account.key")
	k1, err := loadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := loadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if !k1.Equal(k2) {
		t.Error("the second call must load the stored key, not create a new one")
	}
	if err := os.WriteFile(path, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateKey(path); err == nil {
		t.Error("a corrupt key file must be an error, not silently replaced")
	}
}

func TestChallengeFileIsWrittenAndRemoved(t *testing.T) {
	root := t.TempDir()
	cleanup, err := putChallenge(root, "abcdEFGH_-12", "token.thumbprint")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, ".well-known", "acme-challenge", "abcdEFGH_-12")
	if b, _ := os.ReadFile(p); string(b) != "token.thumbprint" {
		t.Errorf("file=%q", b)
	}
	cleanup()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("challenge file must be removed")
	}
}

func TestChallengeTokenCannotEscapeTheDirectory(t *testing.T) {
	for _, tok := range []string{"../../etc/passwd", "a/b", "short", "", "bad token here", strings.Repeat("a", 200)} {
		if _, err := putChallenge(t.TempDir(), tok, "x"); err == nil {
			t.Errorf("token %q must be rejected", tok)
		}
	}
}

func TestEncodeChain(t *testing.T) {
	out := encodeChain([][]byte{{1, 2, 3}, {4, 5}})
	if n := bytes.Count(out, []byte("-----BEGIN CERTIFICATE-----")); n != 2 {
		t.Errorf("want 2 blocks, got %d:\n%s", n, out)
	}
}

func TestIssueNeedsNames(t *testing.T) {
	if _, _, err := Issue(context.Background(), Config{}, nil); err == nil {
		t.Error("no names must be an error")
	}
}
