package fm

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func modeOf(t *testing.T, root, rel string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode()
}

// Windows keeps no owner, group and others bits, so what a mode really is can only be checked on Linux.
func needUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are only checked on Linux")
	}
}

func TestChmodRefusesWhatItShould(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "a.txt", "x")
	for name, call := range map[string]func() error{
		"home":          func() error { _, err := c.Chmod([]string{""}, 0o644, ChmodOpts{}, DefaultLimits); return err },
		"home as dot":   func() error { _, err := c.Chmod([]string{"."}, 0o644, ChmodOpts{}, DefaultLimits); return err },
		"dotdot":        func() error { _, err := c.Chmod([]string{"../x"}, 0o644, ChmodOpts{}, DefaultLimits); return err },
		"backslash":     func() error { _, err := c.Chmod([]string{`a\b`}, 0o644, ChmodOpts{}, DefaultLimits); return err },
		"setuid":        func() error { _, err := c.Chmod([]string{"a.txt"}, 0o4755, ChmodOpts{}, DefaultLimits); return err },
		"sticky":        func() error { _, err := c.Chmod([]string{"a.txt"}, 0o1777, ChmodOpts{}, DefaultLimits); return err },
		"nothing named": func() error { _, err := c.Chmod(nil, 0o644, ChmodOpts{}, DefaultLimits); return err },
		"missing":       func() error { _, err := c.Chmod([]string{"nope"}, 0o644, ChmodOpts{}, DefaultLimits); return err },
	} {
		if err := call(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := c.Chmod([]string{""}, 0o644, ChmodOpts{}, DefaultLimits); !errors.Is(err, ErrRoot) {
		t.Errorf("home: %v", err)
	}
	if _, err := c.Chmod([]string{"a.txt"}, 0o4755, ChmodOpts{}, DefaultLimits); !errors.Is(err, ErrBadMode) {
		t.Errorf("setuid: %v", err)
	}
	if _, err := c.Chmod([]string{"nope"}, 0o644, ChmodOpts{}, DefaultLimits); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestChmodChangesTheNamedItemsOnly(t *testing.T) {
	needUnix(t)
	c, root := newClient(t)
	write(t, root, "a.txt", "x")
	write(t, root, "b.txt", "x")
	write(t, root, "d/in.txt", "x")
	st, err := c.Chmod([]string{"a.txt", "d"}, 0o600, ChmodOpts{}, DefaultLimits)
	if err != nil || st.Files != 1 || st.Dirs != 1 {
		t.Fatalf("stats %+v, err %v", st, err)
	}
	if modeOf(t, root, "a.txt").Perm() != 0o600 || modeOf(t, root, "d").Perm() != 0o600 {
		t.Error("the named items were not changed")
	}
	if modeOf(t, root, "b.txt").Perm() != 0o644 || modeOf(t, root, "d/in.txt").Perm() != 0o644 {
		t.Error("something that was not named, or something inside a folder without Recursive, was changed")
	}
	_ = os.Chmod(filepath.Join(root, "d"), 0o755) // so the temporary directory can be removed
}

func TestChmodKeepsTheSetgidBitOfAFolder(t *testing.T) {
	needUnix(t)
	c, root := newClient(t)
	write(t, root, "htdocs/index.php", "x")
	if err := os.Chmod(filepath.Join(root, "htdocs"), 0o755|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Chmod([]string{"htdocs"}, 0o750, ChmodOpts{}, DefaultLimits); err != nil {
		t.Fatal(err)
	}
	m := modeOf(t, root, "htdocs")
	if m.Perm() != 0o750 || m&os.ModeSetgid == 0 {
		t.Errorf("mode %v: permissions must change and setgid must stay", m)
	}
}

func TestChmodRecursiveHonoursTheKindsChosen(t *testing.T) {
	needUnix(t)
	c, root := newClient(t)
	write(t, root, "site/index.php", "x")
	write(t, root, "site/sub/deep.txt", "x")
	st, err := c.Chmod([]string{"site"}, 0o640, ChmodOpts{Recursive: true, Files: true}, DefaultLimits)
	if err != nil || st.Files != 2 || st.Dirs != 0 {
		t.Fatalf("files only: %+v %v", st, err)
	}
	if modeOf(t, root, "site/index.php").Perm() != 0o640 || modeOf(t, root, "site/sub/deep.txt").Perm() != 0o640 {
		t.Error("files were not changed")
	}
	if modeOf(t, root, "site").Perm() != 0o755 || modeOf(t, root, "site/sub").Perm() != 0o755 {
		t.Error("folders must stay as they were when only files are chosen")
	}
	st, err = c.Chmod([]string{"site"}, 0o750, ChmodOpts{Recursive: true, Dirs: true}, DefaultLimits)
	if err != nil || st.Dirs != 2 || st.Files != 0 {
		t.Fatalf("folders only: %+v %v", st, err)
	}
	if modeOf(t, root, "site/index.php").Perm() != 0o640 {
		t.Error("files changed although only folders were chosen")
	}
	st, err = c.Chmod([]string{"site"}, 0o755, ChmodOpts{Recursive: true}, DefaultLimits)
	if err != nil || st.Dirs != 2 || st.Files != 2 {
		t.Fatalf("both: %+v %v", st, err)
	}
}

func TestChmodWalkTakesAwayAFoldersAccessLast(t *testing.T) {
	needUnix(t)
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits, so this cannot fail as root")
	}
	c, root := newClient(t)
	write(t, root, "d/f.txt", "x")
	if _, err := c.Chmod([]string{"d"}, 0o000, ChmodOpts{Recursive: true}, DefaultLimits); err != nil {
		t.Fatalf("a folder must be changed after what is inside it: %v", err)
	}
	_ = os.Chmod(filepath.Join(root, "d"), 0o755)
}

func TestChmodLeavesLinksAloneAndStopsAtTheLimit(t *testing.T) {
	needUnix(t)
	c, root := newClient(t)
	write(t, root, "target.txt", "x")
	write(t, root, "d/f1", "x")
	write(t, root, "d/f2", "x")
	write(t, root, "d/f3", "x")
	if err := os.Symlink(filepath.Join(root, "target.txt"), filepath.Join(root, "d", "link")); err != nil {
		t.Skip("no symbolic links here")
	}
	st, err := c.Chmod([]string{"d"}, 0o600, ChmodOpts{Recursive: true}, DefaultLimits)
	if err != nil || st.Skipped != 1 || st.Files != 3 {
		t.Fatalf("%+v %v", st, err)
	}
	if modeOf(t, root, "target.txt").Perm() != 0o644 {
		t.Error("the target of a link was changed")
	}
	_, err = c.Chmod([]string{"d"}, 0o644, ChmodOpts{Recursive: true}, Limits{Entries: 2})
	if !errors.Is(err, ErrTooMany) {
		t.Errorf("limit: %v", err)
	}
}
