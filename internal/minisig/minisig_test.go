package minisig

import (
	"os"
	"strings"
	"testing"
)

// testdata was made with minisign 0.12 itself (minisign -G -W, then -S -t ...); the secret key was thrown away.
func load(t *testing.T) (PublicKey, []byte, string) {
	t.Helper()
	pub, err := os.ReadFile("testdata/test.pub")
	if err != nil {
		t.Fatal(err)
	}
	k, err := ParsePublicKey(string(pub))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile("testdata/data.bin")
	sig, _ := os.ReadFile("testdata/data.bin.minisig")
	return k, data, string(sig)
}

func TestASignatureOfMinisignIsAccepted(t *testing.T) {
	k, data, sigText := load(t)
	sig, err := ParseSignature(sigText)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(k, data, sig); err != nil {
		t.Fatal(err)
	}
	if sig.TrustedComment != "file:data.bin version:1.2.0" {
		t.Errorf("trusted comment %q", sig.TrustedComment)
	}
}

func TestAChangedFileOrCommentIsRefused(t *testing.T) {
	k, data, sigText := load(t)
	sig, _ := ParseSignature(sigText)
	bad := append([]byte(nil), data...)
	bad[0] ^= 1
	if Verify(k, bad, sig) == nil {
		t.Error("a changed file was accepted")
	}
	// The trusted comment says which version this is; changing it must break the signature, or an old binary could be passed
	// off as new.
	forged := strings.Replace(sigText, "version:1.2.0", "version:9.9.9", 1)
	fs, err := ParseSignature(forged)
	if err != nil {
		t.Fatal(err)
	}
	if Verify(k, data, fs) == nil {
		t.Error("a changed trusted comment was accepted")
	}
}

func TestAnotherKeyAndDamagedInputAreRefused(t *testing.T) {
	k, data, sigText := load(t)
	sig, _ := ParseSignature(sigText)
	other := k
	other.ID[0] ^= 1
	if Verify(other, data, sig) == nil {
		t.Error("a signature of another key id was accepted")
	}
	other = k
	other.Key = append([]byte(nil), k.Key...)
	other.Key[0] ^= 1
	if Verify(other, data, sig) == nil {
		t.Error("another key with the same id was accepted")
	}
	for _, s := range []string{"", "untrusted comment: x\nAAAA\n", strings.Replace(sigText, "RUQ", "RWQ", 1)} {
		if _, err := ParseSignature(s); err == nil {
			t.Errorf("parsed %q", s)
		}
	}
	if _, err := ParsePublicKey("RWQ"); err == nil {
		t.Error("a short key was parsed")
	}
}
