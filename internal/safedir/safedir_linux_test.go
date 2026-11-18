//go:build linux

package safedir

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func ids() (int, int) { return os.Getuid(), os.Getgid() }

func TestMakesEveryMissingFolderWithTheMode(t *testing.T) {
	base := t.TempDir()
	uid, gid := ids()
	if err := MkdirAll(base, []string{"htdocs", "app", "public"}, uid, gid, 0o2750); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"htdocs", "htdocs/app", "htdocs/app/public"} {
		fi, err := os.Lstat(filepath.Join(base, p))
		if err != nil {
			t.Fatal(err)
		}
		if !fi.IsDir() || fi.Mode().Perm() != 0o750 || fi.Mode()&os.ModeSetgid == 0 {
			t.Errorf("%s: mode %v", p, fi.Mode())
		}
	}
}

func TestLeavesAnExistingFolderAlone(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "htdocs"), 0o755); err != nil {
		t.Fatal(err)
	}
	uid, gid := ids()
	if err := MkdirAll(base, []string{"htdocs", "public"}, uid, gid, 0o2750); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(filepath.Join(base, "htdocs"))
	if fi.Mode().Perm() != 0o755 || fi.Mode()&os.ModeSetgid != 0 {
		t.Errorf("the existing folder was changed: %v", fi.Mode())
	}
	fi, _ = os.Stat(filepath.Join(base, "htdocs", "public"))
	if fi.Mode()&os.ModeSetgid == 0 {
		t.Errorf("the new folder below it was not made with the mode: %v", fi.Mode())
	}
}

// The case of the report: the user put a link where the web root goes. Nothing at the target may change.
func TestNeverFollowsALink(t *testing.T) {
	for _, at := range []string{"htdocs", "htdocs/app"} {
		base, victim := t.TempDir(), t.TempDir()
		if err := os.Chmod(victim, 0o700); err != nil {
			t.Fatal(err)
		}
		if at == "htdocs/app" {
			if err := os.Mkdir(filepath.Join(base, "htdocs"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(victim, filepath.Join(base, at)); err != nil {
			t.Fatal(err)
		}
		uid, gid := ids()
		err := MkdirAll(base, []string{"htdocs", "app", "public"}, uid, gid, 0o2750)
		if !errors.Is(err, ErrLink) {
			t.Errorf("%s: err=%v, want ErrLink", at, err)
		}
		fi, _ := os.Stat(victim)
		if fi.Mode().Perm() != 0o700 || fi.Mode()&os.ModeSetgid != 0 {
			t.Errorf("%s: the target of the link was changed: %v", at, fi.Mode())
		}
		if entries, _ := os.ReadDir(victim); len(entries) != 0 {
			t.Errorf("%s: something was made inside the target: %v", at, entries)
		}
	}
}

func TestADanglingLinkCountsAsPresent(t *testing.T) {
	base := t.TempDir()
	if err := os.Symlink(filepath.Join(base, "nowhere"), filepath.Join(base, "htdocs")); err != nil {
		t.Fatal(err)
	}
	uid, gid := ids()
	if err := MkdirAll(base, []string{"htdocs"}, uid, gid, 0o2750); !errors.Is(err, ErrLink) {
		t.Errorf("err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(base, "nowhere")); err == nil {
		t.Error("the target of a dangling link was made")
	}
}

func TestBaseMustNotBeALink(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	uid, gid := ids()
	if err := MkdirAll(link, []string{"htdocs"}, uid, gid, 0o2750); err == nil {
		t.Error("a base that is a link was followed")
	}
}

func TestRefusesNamesThatLeaveTheTree(t *testing.T) {
	base := t.TempDir()
	uid, gid := ids()
	for _, rel := range [][]string{{".."}, {"htdocs", ".."}, {"a/b"}, {""}, {"."}} {
		if err := MkdirAll(base, rel, uid, gid, 0o2750); err == nil {
			t.Errorf("%q accepted", rel)
		}
	}
}
