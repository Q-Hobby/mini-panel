package panelkey

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func temp(t *testing.T) Paths {
	d := t.TempDir()
	return Paths{Public: filepath.Join(d, "etc", "panel.pub"), Private: filepath.Join(d, "var", "key")}
}

func TestEnsureMakesOneConsistentPairAndKeepsIt(t *testing.T) {
	p := temp(t)
	created, err := Ensure(p, -1, -1)
	if err != nil || !created || !consistent(p) {
		t.Fatalf("created=%v err=%v", created, err)
	}
	before, _ := os.ReadFile(p.Private)
	created, err = Ensure(p, -1, -1)
	after, _ := os.ReadFile(p.Private)
	if err != nil || created || string(before) != string(after) {
		t.Errorf("a good pair must be left alone (created=%v err=%v)", created, err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(p.Private); fi.Mode().Perm() != 0o600 {
			t.Errorf("private key mode %v, want 0600", fi.Mode().Perm())
		}
		if fi, _ := os.Stat(p.Public); fi.Mode().Perm() != 0o644 {
			t.Errorf("public key mode %v, want 0644", fi.Mode().Perm())
		}
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(p.Private), ".key-*")); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

func TestEnsureRepairsADamagedOrMismatchedPair(t *testing.T) {
	for name, damage := range map[string]func(Paths){
		"private missing": func(p Paths) { _ = os.Remove(p.Private) },
		"private garbage": func(p Paths) { _ = os.WriteFile(p.Private, []byte("not a key"), 0o600) },
		"public missing":  func(p Paths) { _ = os.Remove(p.Public) },
		"public swapped": func(p Paths) {
			other := temp(t)
			_, _ = Ensure(other, -1, -1)
			b, _ := os.ReadFile(other.Public)
			_ = os.WriteFile(p.Public, b, 0o644)
		},
	} {
		p := temp(t)
		_, _ = Ensure(p, -1, -1)
		damage(p)
		created, err := Ensure(p, -1, -1)
		if err != nil || !created || !consistent(p) {
			t.Errorf("%s: created=%v err=%v", name, created, err)
		}
	}
}

func TestAuthorizedLineIsRestrictedAndMarked(t *testing.T) {
	p := temp(t)
	_, _ = Ensure(p, -1, -1)
	line, err := AuthorizedLine(p.Public)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, `restrict,pty,from="127.0.0.1,::1" ssh-ed25519 `) || !strings.HasSuffix(line, " "+Marker) || strings.Contains(line, "\n") {
		t.Errorf("line: %q", line)
	}
}

func TestAuthorizedLineRefusesAnythingButOnePlainKey(t *testing.T) {
	p := temp(t)
	_, _ = Ensure(p, -1, -1)
	good, _ := os.ReadFile(p.Public)
	for name, content := range map[string]string{
		"a password file":    "root:x:0:0:root:/root:/bin/bash\n",
		"empty":              "",
		"options in front":   `command="rm -rf /" ` + string(good),
		"two keys":           string(good) + string(good),
		"a huge file":        strings.Repeat("a", 5000),
		"an rsa-looking one": "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAgQC0 x\n",
	} {
		if err := os.WriteFile(p.Public, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if line, err := AuthorizedLine(p.Public); err == nil {
			t.Errorf("%s: accepted as %q", name, line)
		}
	}
	if _, err := AuthorizedLine(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a missing file must be an error")
	}
}
