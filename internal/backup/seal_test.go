package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestASealedManifestCarriesTheSumsAndOnlyItsKeyTrustsIt(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "files.tar.gz"), []byte("files"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "demo.sql.gz"), []byte("dump"), 0o600)
	key, err := LoadKey(filepath.Join(dir, "key"))
	if err != nil {
		t.Fatal(err)
	}
	again, _ := LoadKey(filepath.Join(dir, "key"))
	if string(again) != string(key) {
		t.Fatal("the key changed between two reads")
	}
	if err := Seal(dir, Manifest{Site: "demo", Stamp: "20261104-031500", Files: true, DBs: []string{"demo"}}, key); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(filepath.Join(dir, "MANIFEST"))
	m, err := ParseManifest(string(text), key)
	if err != nil || !m.Signed || len(m.Sums) != 2 {
		t.Fatalf("%+v %v\n%s", m, err, text)
	}
	for _, n := range []string{"files.tar.gz", "demo.sql.gz"} {
		if err := CheckFileSum(m, filepath.Join(dir, n)); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "demo.sql.gz"), []byte("another dump"), 0o600)
	if CheckFileSum(m, filepath.Join(dir, "demo.sql.gz")) == nil {
		t.Error("a changed dump matched its sum")
	}

	other := make([]byte, 32)
	if m2, _ := ParseManifest(string(text), other); m2.Signed {
		t.Error("another server's key trusts the MANIFEST")
	}
	if m2, _ := ParseManifest(string(text)); m2.Signed {
		t.Error("a MANIFEST is trusted without a key")
	}
	// Whoever writes to the storage changes a line, or a sum, and keeps the old mac.
	for _, forged := range []string{
		strings.Replace(string(text), "db=demo\n", "db=demo\ndb=demo_x\n", 1),
		strings.Replace(string(text), "sum=demo.sql.gz:", "sum=demo.sql.gz:0", 1)[:0] + strings.Replace(string(text), "sum=files.tar.gz:", "sum=files.tar.gz:", 1) + "x",
	} {
		if m2, err := ParseManifest(forged, key); err == nil && m2.Signed {
			t.Errorf("a changed MANIFEST is trusted:\n%s", forged)
		}
	}
	// A MANIFEST of 1.1 (no sums, no mac) still reads, unsigned, for a restore of a backup on this server.
	old, err := ParseManifest("mpn backup 1\nsite=demo\nstamp=20261104-031500\nfiles=files.tar.gz\n", key)
	if err != nil || old.Signed {
		t.Errorf("%+v %v", old, err)
	}
	if _, err := ParseManifest("mpn backup 1\nsite=demo\nstamp=20261104-031500\nmac=00\nfiles=files.tar.gz\n", key); err == nil {
		t.Error("a mac line that is not the last one was accepted")
	}
	if _, err := ParseManifest("mpn backup 1\nsite=demo\nstamp=20261104-031500\nsum=../../etc/passwd:" + strings.Repeat("0", 64) + "\n"); err == nil {
		t.Error("a sum of a path was accepted")
	}
}
