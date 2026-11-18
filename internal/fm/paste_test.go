package fm

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestCopyIntoOtherFolder(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "a/one.txt", "1")
	write(t, root, "a/sub/two.txt", "22")
	write(t, root, "b/.keep", "")
	st, err := c.Copy("a", []string{"one.txt", "sub"}, "b", DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if st.Files != 2 || st.Dirs != 1 {
		t.Fatalf("stats %+v", st)
	}
	if read(t, root, "b/one.txt") != "1" || read(t, root, "b/sub/two.txt") != "22" {
		t.Fatal("content not copied")
	}
	if !exists(root, "a/one.txt") || !exists(root, "a/sub/two.txt") {
		t.Fatal("copy removed the original")
	}
	for _, n := range names(root) {
		if strings.Contains(n, ".mpn-") {
			t.Fatal("temporary file left:", n)
		}
	}
}

func TestCopyKeepsMode(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "run.sh", "#!/bin/sh")
	if err := os.Chmod(filepath.Join(root, "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, root, "d/.keep", "")
	if _, err := c.Copy("", []string{"run.sh"}, "d", DefaultLimits); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(root, "d", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o100 == 0 && os.Getenv("OS") != "Windows_NT" {
		t.Fatalf("mode lost: %v", fi.Mode())
	}
}

func TestCopyInSameFolderGetsFreeNames(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "x/page.html", "p")
	write(t, root, "x/page (copy).html", "old")
	write(t, root, "x/dir/f", "f")
	if _, err := c.Copy("x", []string{"page.html", "dir"}, "x", DefaultLimits); err != nil {
		t.Fatal(err)
	}
	if read(t, root, "x/page (copy 2).html") != "p" {
		t.Fatal("expected page (copy 2).html; have", names(root))
	}
	if read(t, root, "x/page (copy).html") != "old" {
		t.Fatal("an existing file was replaced")
	}
	if read(t, root, "x/dir (copy)/f") != "f" {
		t.Fatal("folder copy missing; have", names(root))
	}
}

func TestPasteRefusesExistingName(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "a/f.txt", "new")
	write(t, root, "a/g.txt", "g")
	write(t, root, "b/f.txt", "old")
	for _, op := range []string{"copy", "move"} {
		var err error
		if op == "copy" {
			_, err = c.Copy("a", []string{"g.txt", "f.txt"}, "b", DefaultLimits)
		} else {
			_, err = c.Move("a", []string{"g.txt", "f.txt"}, "b")
		}
		if !errors.Is(err, ErrExists) {
			t.Fatalf("%s: want ErrExists, got %v", op, err)
		}
		if read(t, root, "b/f.txt") != "old" || exists(root, "b/g.txt") || !exists(root, "a/g.txt") {
			t.Fatalf("%s changed something before refusing", op)
		}
	}
}

func TestMove(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "a/f.txt", "f")
	write(t, root, "a/d/inner", "i")
	write(t, root, "b/.keep", "")
	st, err := c.Move("a", []string{"f.txt", "d"}, "b")
	if err != nil {
		t.Fatal(err)
	}
	if st.Files != 1 || st.Dirs != 1 {
		t.Fatalf("stats %+v", st)
	}
	if exists(root, "a/f.txt") || exists(root, "a/d") || read(t, root, "b/f.txt") != "f" || read(t, root, "b/d/inner") != "i" {
		t.Fatal("not moved; have", names(root))
	}
	// Moving within one folder is a no-op, not an error.
	if st, err := c.Move("b", []string{"f.txt"}, "b"); err != nil || st.Files != 0 {
		t.Fatalf("same folder: %+v %v", st, err)
	}
}

func TestPasteRefusesFolderIntoItself(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "d/sub/f", "f")
	for _, dst := range []string{"d", "d/sub"} {
		if _, err := c.Move("", []string{"d"}, dst); !errors.Is(err, ErrInside) {
			t.Fatalf("move into %s: %v", dst, err)
		}
		if _, err := c.Copy("", []string{"d"}, dst, DefaultLimits); !errors.Is(err, ErrInside) {
			t.Fatalf("copy into %s: %v", dst, err)
		}
	}
}

func TestPasteRefusesBadInput(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "a/f", "f")
	write(t, root, "b/.keep", "")
	cases := []struct {
		src   string
		names []string
		dst   string
		want  error
	}{
		{"../x", []string{"f"}, "b", ErrBadPath},
		{"a", []string{"f"}, "../b", ErrBadPath},
		{"a", []string{"../f"}, "b", ErrBadName},
		{"a", []string{"x/f"}, "b", ErrBadName},
		{"a", []string{"missing"}, "b", ErrNotFound},
		{"a", nil, "b", ErrNotFound},
		{"a", []string{"f"}, "a/f", ErrBadPath},
	}
	for _, tc := range cases {
		if _, err := c.Copy(tc.src, tc.names, tc.dst, DefaultLimits); !errors.Is(err, tc.want) {
			t.Errorf("copy %v: want %v, got %v", tc, tc.want, err)
		}
		if _, err := c.Move(tc.src, tc.names, tc.dst); !errors.Is(err, tc.want) {
			t.Errorf("move %v: want %v, got %v", tc, tc.want, err)
		}
	}
	got := names(root)
	sort.Strings(got)
	if strings.Join(got, ",") != "a,a/f,b,b/.keep" {
		t.Fatal("something changed:", got)
	}
}

func TestCopySkipsLinksAndHonoursLimits(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "src/a", "aaaa")
	write(t, root, "src/b", "bbbb")
	write(t, root, "dst/.keep", "")
	symlinkOrSkip(t, filepath.Join(root, "src", "a"), filepath.Join(root, "src", "link"))
	st, err := c.Copy("", []string{"src"}, "dst", DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if st.Skipped != 1 || exists(root, "dst/src/link") {
		t.Fatalf("link not skipped: %+v", st)
	}
	if _, err := c.Copy("", []string{"src"}, "dst", Limits{Entries: 100, Bytes: 5}); !errors.Is(err, ErrExists) {
		t.Fatalf("second paste over the same name: %v", err)
	}
	write(t, root, "dst2/.keep", "")
	if _, err := c.Copy("", []string{"src"}, "dst2", Limits{Entries: 100, Bytes: 5}); !errors.Is(err, ErrTooBig) {
		t.Fatalf("byte limit: %v", err)
	}
}
