package update

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// testdata holds a release signed by minisign 0.12 with a test key (the secret key was thrown away): mpn_linux_amd64, SHA256SUMS
// and SHA256SUMS.minisig for "mpn 1.2.0", and the same for "mpn 1.0.9".
func release(t *testing.T, dir string) (key string, sums, sig, bin []byte) {
	t.Helper()
	read := func(n string) []byte {
		b, err := os.ReadFile("testdata/" + dir + "/" + n)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	return string(read("../test.pub")), read("SHA256SUMS"), read("SHA256SUMS.minisig"), read("mpn_linux_amd64")
}

func TestASignedReleaseIsAccepted(t *testing.T) {
	key, sums, sig, bin := release(t, "v120")
	v, err := Check(key, sums, sig, bin, "mpn_linux_amd64", Version{1, 1, 1}, false)
	if err != nil || v != (Version{1, 2, 0}) {
		t.Fatalf("v=%v err=%v", v, err)
	}
}

func TestWhatMustBeRefused(t *testing.T) {
	key, sums, sig, bin := release(t, "v120")
	bad := append([]byte(nil), bin...)
	bad[0] ^= 1
	if _, err := Check(key, sums, sig, bad, "mpn_linux_amd64", Version{1, 1, 1}, false); err == nil {
		t.Error("a changed program was accepted")
	}
	// Somebody rewrites SHA256SUMS to match their own program.
	h := sha256.Sum256(bad)
	forged := []byte(strings.Replace(string(sums), strings.Fields(string(sums))[0], hex.EncodeToString(h[:]), 1))
	if _, err := Check(key, forged, sig, bad, "mpn_linux_amd64", Version{1, 1, 1}, false); err == nil || !strings.Contains(err.Error(), "not signed") {
		t.Errorf("a rewritten SHA256SUMS: %v", err)
	}
	if _, err := Check(ReleaseKey, sums, sig, bin, "mpn_linux_amd64", Version{1, 1, 1}, false); err == nil {
		t.Error("a release signed with another key was accepted")
	}
	if _, err := Check(key, sums, sig, bin, "mpn_linux_arm64", Version{1, 1, 1}, false); err == nil {
		t.Error("a CPU that SHA256SUMS does not list was accepted")
	}
}

func TestAnOlderReleaseIsRefusedUnlessAskedFor(t *testing.T) {
	key, sums, sig, bin := release(t, "v109")
	if _, err := Check(key, sums, sig, bin, "mpn_linux_amd64", Version{1, 2, 0}, false); err == nil || !strings.Contains(err.Error(), "older") {
		t.Errorf("going back: %v", err)
	}
	if v, err := Check(key, sums, sig, bin, "mpn_linux_amd64", Version{1, 2, 0}, true); err != nil || v != (Version{1, 0, 9}) {
		t.Errorf("going back on purpose: %v %v", v, err)
	}
	if v, err := Check(key, sums, sig, bin, "mpn_linux_amd64", Version{1, 0, 9}, false); err != nil || v != (Version{1, 0, 9}) {
		t.Errorf("the same version again: %v %v", v, err)
	}
}

func TestVersions(t *testing.T) {
	for _, s := range []string{"1.2.0", "v1.2.0", " 10.0.1 "} {
		if _, err := ParseVersion(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	for _, s := range []string{"1.2", "1.2.0-test", "latest", "1.2.0.1"} {
		if _, err := ParseVersion(s); err == nil {
			t.Errorf("%q parsed", s)
		}
	}
	if !(Version{1, 9, 9}).Less(Version{1, 10, 0}) || (Version{2, 0, 0}).Less(Version{1, 99, 99}) {
		t.Error("order")
	}
}
