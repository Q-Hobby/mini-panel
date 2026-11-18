package fm

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pkg/sftp"
)

// newClient serves a temporary directory over SFTP, inside the test, and returns a client whose home is that directory.
func newClient(t *testing.T) (Client, string) {
	t.Helper()
	root := filepath.ToSlash(t.TempDir())
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	srv, err := sftp.NewServer(struct {
		io.Reader
		io.WriteCloser
	}{sr, sw})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	cli, err := sftp.NewClientPipe(cr, cw)
	if err != nil {
		t.Fatal(err)
	}
	// The server must see its input end before the client is closed: the client waits for the server to hang up.
	t.Cleanup(func() { cw.Close(); srv.Close(); cli.Close() })
	return Client{C: cli, Root: root}, root
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(root, rel string) bool {
	_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil
}

func names(root string) []string {
	var out []string
	_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err == nil && p != root {
			r, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(r))
		}
		return nil
	})
	return out
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skip("cannot make a symbolic link here:", err)
	}
}

func TestPathsCannotLeaveTheHome(t *testing.T) {
	c := Client{Root: "/home/blog"}
	for _, bad := range []string{"..", "../etc", "a/../../etc", "a/..", `a\b`, "a\x00b", "a\nb", strings.Repeat("a", 5000)} {
		if p, err := c.Abs(bad); err == nil {
			t.Errorf("%q accepted as %q", bad, p)
		}
	}
	for in, want := range map[string]string{"": "/home/blog", "htdocs": "/home/blog/htdocs", "/htdocs//x/": "/home/blog/htdocs/x", "/etc/passwd": "/home/blog/etc/passwd", "./a/./b": "/home/blog/a/b"} {
		if got, err := c.Abs(in); err != nil || got != want {
			t.Errorf("%q: got %q %v, want %q", in, got, err, want)
		}
	}
	if Clean("/a//b/../c/") != "a/c" || Clean("") != "" || Clean("/") != "" {
		t.Error("Clean")
	}
}

func TestNames(t *testing.T) {
	for _, ok := range []string{"index.php", ".htaccess", "tệp tiếng việt.txt", "a b"} {
		if !ValidName(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`, "a\x00", "a\n", strings.Repeat("x", 256), "\xff\xfe"} {
		if ValidName(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestListSortsFoldersFirstAndCutsLongLists(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "b.txt", "x")
	write(t, root, "A.txt", "x")
	write(t, root, ".hidden", "x")
	_ = os.Mkdir(filepath.Join(root, "zdir"), 0o755)
	_ = os.Mkdir(filepath.Join(root, "adir"), 0o755)
	es, cut, err := c.List("")
	if err != nil || cut {
		t.Fatal(err, cut)
	}
	var got []string
	for _, e := range es {
		got = append(got, e.Name)
	}
	if strings.Join(got, ",") != "adir,zdir,.hidden,A.txt,b.txt" || !es[0].IsDir || es[2].IsDir {
		t.Errorf("order: %v", got)
	}
	old := MaxEntries
	MaxEntries = 2
	defer func() { MaxEntries = old }()
	if es, cut, _ := c.List(""); !cut || len(es) != 2 {
		t.Errorf("cut=%v len=%d", cut, len(es))
	}
	if _, _, err := c.List("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing folder: %v", err)
	}
	if _, _, err := c.List("../x"); !errors.Is(err, ErrBadPath) {
		t.Errorf("escape: %v", err)
	}
}

func TestMakeRenameAndRemove(t *testing.T) {
	c, root := newClient(t)
	if err := c.Mkdir("", "site"); err != nil || !exists(root, "site") {
		t.Fatal(err)
	}
	if !errors.Is(c.Mkdir("", "site"), ErrExists) || !errors.Is(c.Mkdir("", "a/b"), ErrBadName) || !errors.Is(c.Mkdir("missing", "x"), ErrNotFound) {
		t.Error("mkdir refusals")
	}
	if err := c.Create("site", "a.txt"); err != nil || !exists(root, "site/a.txt") {
		t.Fatal(err)
	}
	if !errors.Is(c.Create("site", "a.txt"), ErrExists) {
		t.Error("create must not touch an existing file")
	}
	write(t, root, "site/b.txt", "bee")
	if !errors.Is(c.Rename("site/a.txt", "b.txt"), ErrExists) || read(t, root, "site/b.txt") != "bee" {
		t.Error("rename must not replace a file")
	}
	if err := c.Rename("site/a.txt", "c.txt"); err != nil || exists(root, "site/a.txt") || !exists(root, "site/c.txt") {
		t.Fatal(err)
	}
	if !errors.Is(c.Rename("", "x"), ErrRoot) || !errors.Is(c.Rename("site/c.txt", "../c"), ErrBadName) || !errors.Is(c.Rename("site/none", "x"), ErrNotFound) {
		t.Error("rename refusals")
	}
	write(t, root, "site/sub/deep/f.txt", "x")
	if !errors.Is(c.Remove("site/b.txt", ""), ErrRoot) || !exists(root, "site/b.txt") {
		t.Error("a list that includes the home directory must delete nothing")
	}
	if !errors.Is(c.Remove("site/b.txt", "../etc"), ErrBadPath) || !exists(root, "site/b.txt") {
		t.Error("a list with a bad path must delete nothing")
	}
	if err := c.Remove("site/b.txt", "site/sub"); err != nil || exists(root, "site/b.txt") || exists(root, "site/sub") {
		t.Fatal(err, names(root))
	}
}

func TestRemovingALinkLeavesItsTarget(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "real/keep.txt", "keep")
	symlinkOrSkip(t, filepath.Join(root, "real"), filepath.Join(root, "link"))
	if err := c.Remove("link"); err != nil {
		t.Fatal(err)
	}
	if exists(root, "link") || read(t, root, "real/keep.txt") != "keep" {
		t.Error("deleting a link must remove only the link")
	}
}

func TestTextRoundTripConflictsAndRefusals(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "a.php", "<?php echo 1;")
	text, mt, err := c.ReadText("a.php", 1<<20)
	if err != nil || text != "<?php echo 1;" {
		t.Fatal(text, err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(filepath.Join(root, "a.php"), 0o640)
	}
	newMt, err := c.WriteText("a.php", "<?php echo 2;", mt)
	if err != nil || read(t, root, "a.php") != "<?php echo 2;" || newMt == 0 {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(filepath.Join(root, "a.php")); fi.Mode().Perm() != 0o640 {
			t.Errorf("the mode must be kept, got %v", fi.Mode().Perm())
		}
	}
	if _, err := c.WriteText("a.php", "stale", mt-100); !errors.Is(err, ErrChanged) || read(t, root, "a.php") != "<?php echo 2;" {
		t.Errorf("a stale save must change nothing: %v", err)
	}
	for _, n := range names(root) {
		if strings.Contains(n, ".mpn-") {
			t.Errorf("temporary file left: %s", n)
		}
	}
	write(t, root, "bin.dat", "ab\x00cd")
	write(t, root, "latin.txt", "caf\xe9")
	write(t, root, "big.txt", strings.Repeat("x", 100))
	_ = os.Mkdir(filepath.Join(root, "dir"), 0o755)
	for rel, want := range map[string]error{"bin.dat": ErrBinary, "latin.txt": ErrBinary, "dir": ErrIsDir, "none": ErrNotFound} {
		if _, _, err := c.ReadText(rel, 1<<20); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", rel, err, want)
		}
	}
	if _, _, err := c.ReadText("big.txt", 50); !errors.Is(err, ErrTooBig) {
		t.Errorf("too big: %v", err)
	}
	if _, err := c.WriteText("dir", "x", 0); !errors.Is(err, ErrIsDir) {
		t.Errorf("writing a folder: %v", err)
	}
}

func TestUploadRefusesToReplaceStopsAtTheLimitAndLeavesNothingHalfDone(t *testing.T) {
	c, root := newClient(t)
	if n, err := c.Upload("", "new.txt", strings.NewReader("hello"), false, 100); err != nil || n != 5 || read(t, root, "new.txt") != "hello" {
		t.Fatal(n, err)
	}
	if _, err := c.Upload("", "new.txt", strings.NewReader("other"), false, 100); !errors.Is(err, ErrExists) || read(t, root, "new.txt") != "hello" {
		t.Errorf("must not replace: %v", err)
	}
	if _, err := c.Upload("", "new.txt", strings.NewReader("other"), true, 100); err != nil || read(t, root, "new.txt") != "other" {
		t.Errorf("overwrite: %v", err)
	}
	if _, err := c.Upload("", "big.bin", strings.NewReader(strings.Repeat("x", 200)), false, 100); !errors.Is(err, ErrTooBig) || exists(root, "big.bin") {
		t.Errorf("over the limit: %v", err)
	}
	for _, bad := range []string{"../x", "a/b", ""} {
		if _, err := c.Upload("", bad, strings.NewReader("x"), false, 100); !errors.Is(err, ErrBadName) {
			t.Errorf("name %q: %v", bad, err)
		}
	}
	for _, n := range names(root) {
		if strings.Contains(n, ".mpn-") || n == "big.bin" {
			t.Errorf("leftover: %s", n)
		}
	}
}

func zipEntries(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	out := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

func TestZipPacksFoldersAndFilesAndLeavesOutLinks(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "site/index.php", "<?php")
	write(t, root, "site/css/app.css", "body{}")
	write(t, root, "site/readme.txt", "hi")
	write(t, root, "other.txt", "other")
	_ = os.Mkdir(filepath.Join(root, "site/empty"), 0o755)
	if runtime.GOOS != "windows" {
		_ = os.Symlink("/etc/passwd", filepath.Join(root, "site/passwd-link"))
	}
	st, err := c.Zip("site", []string{"css", "index.php", "empty", "passwd-link"}, "bundle", DefaultLimits)
	if runtime.GOOS == "windows" {
		st, err = c.Zip("site", []string{"css", "index.php", "empty"}, "bundle", DefaultLimits)
	}
	if err != nil {
		t.Fatal(err)
	}
	got := zipEntries(t, filepath.Join(root, "site", "bundle.zip"))
	if got["index.php"] != "<?php" || got["css/app.css"] != "body{}" || len(got) != 4 { // index.php, css/, css/app.css, empty/
		t.Errorf("entries: %v", got)
	}
	if _, ok := got["passwd-link"]; ok || (runtime.GOOS != "windows" && st.Skipped != 1) {
		t.Errorf("links must be left out, skipped=%d", st.Skipped)
	}
	if st.Files != 2 || st.Dirs != 2 {
		t.Errorf("stats: %+v", st)
	}
	if _, err := c.Zip("site", []string{"index.php"}, "bundle.zip", DefaultLimits); !errors.Is(err, ErrExists) {
		t.Errorf("an existing archive must not be replaced: %v", err)
	}
	if _, err := c.Zip("site", []string{"../other.txt"}, "x.zip", DefaultLimits); !errors.Is(err, ErrBadName) {
		t.Errorf("a name that climbs out: %v", err)
	}
	if _, err := c.Zip("site", []string{"nope"}, "y.zip", DefaultLimits); !errors.Is(err, ErrNotFound) {
		t.Errorf("a missing item: %v", err)
	}
	if _, err := c.Zip("site", nil, "z.zip", DefaultLimits); err == nil {
		t.Error("nothing to pack must be an error")
	}
	if _, err := c.Zip("site", []string{"css", "index.php"}, "small.zip", Limits{Entries: 2, Bytes: 1 << 20}); !errors.Is(err, ErrTooMany) || exists(root, "site/small.zip") {
		t.Errorf("entry limit: %v", err)
	}
	if _, err := c.Zip("site", []string{"readme.txt"}, "tiny.zip", Limits{Entries: 10, Bytes: 1}); !errors.Is(err, ErrTooBig) || exists(root, "site/tiny.zip") {
		t.Errorf("size limit: %v", err)
	}
	for _, n := range names(root) {
		if strings.Contains(n, ".mpn-") {
			t.Errorf("temporary file left: %s", n)
		}
	}
}

func makeZip(t *testing.T, root, rel string, files map[string]string, tweak func(*zip.Writer)) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	if tweak != nil {
		tweak(zw)
	}
	_ = zw.Close()
	write(t, root, rel, buf.String())
}

func TestUnzipRefusesEveryWayOutOfTheFolder(t *testing.T) {
	c, root := newClient(t)
	_ = os.Mkdir(filepath.Join(root, "dest"), 0o755)
	makeZip(t, root, "evil.zip", map[string]string{
		"ok.txt": "fine", "sub/ok2.txt": "fine too",
		"../escape.txt": "x", "a/../../escape2.txt": "x", "/abs.txt": "x", `..\win.txt`: "x", `C:\drive.txt`: "x", "x/../../../deep.txt": "x",
	}, func(zw *zip.Writer) {
		h := &zip.FileHeader{Name: "link"}
		h.SetMode(os.ModeSymlink | 0o777)
		w, _ := zw.CreateHeader(h)
		_, _ = w.Write([]byte("/etc/passwd"))
	})
	st, err := c.Unzip("evil.zip", "dest", false, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, root, "dest/ok.txt") != "fine" || read(t, root, "dest/sub/ok2.txt") != "fine too" {
		t.Error("the safe entries must be extracted")
	}
	if st.Files != 2 || st.Skipped != 7 {
		t.Errorf("stats: %+v", st)
	}
	for _, n := range names(root) {
		switch n {
		case "dest", "dest/ok.txt", "dest/sub", "dest/sub/ok2.txt", "evil.zip":
		default:
			t.Errorf("unexpected file after extraction: %s", n)
		}
	}
}

func TestUnzipDoesNotWriteThroughALinkInTheFolder(t *testing.T) {
	c, root := newClient(t)
	_ = os.Mkdir(filepath.Join(root, "dest"), 0o755)
	_ = os.Mkdir(filepath.Join(root, "outside"), 0o755)
	symlinkOrSkip(t, filepath.Join(root, "outside"), filepath.Join(root, "dest", "jump"))
	makeZip(t, root, "a.zip", map[string]string{"jump/planted.txt": "x", "fine.txt": "ok"}, nil)
	st, err := c.Unzip("a.zip", "dest", false, DefaultLimits)
	if err != nil || exists(root, "outside/planted.txt") || read(t, root, "dest/fine.txt") != "ok" || st.Skipped != 1 {
		t.Errorf("err=%v skipped=%d outside=%v", err, st.Skipped, names(filepath.Join(root, "outside")))
	}
}

func TestUnzipKeepsExistingFilesUnlessAsked(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "dest/a.txt", "mine")
	makeZip(t, root, "a.zip", map[string]string{"a.txt": "theirs"}, nil)
	if st, err := c.Unzip("a.zip", "dest", false, DefaultLimits); err != nil || read(t, root, "dest/a.txt") != "mine" || st.Skipped != 1 {
		t.Errorf("must keep: %v %+v", err, st)
	}
	if st, err := c.Unzip("a.zip", "dest", true, DefaultLimits); err != nil || read(t, root, "dest/a.txt") != "theirs" || st.Files != 1 {
		t.Errorf("overwrite: %v %+v", err, st)
	}
}

func TestUnzipStopsAtTheRealSizeNotTheClaimedOne(t *testing.T) {
	c, root := newClient(t)
	_ = os.Mkdir(filepath.Join(root, "dest"), 0o755)
	makeZip(t, root, "bomb.zip", map[string]string{"zeros.bin": strings.Repeat("\x00", 1<<20)}, nil)
	if fi, _ := os.Stat(filepath.Join(root, "bomb.zip")); fi.Size() > 20000 {
		t.Fatalf("test archive should be tiny, is %d bytes", fi.Size())
	}
	if _, err := c.Unzip("bomb.zip", "dest", false, Limits{Entries: 10, Bytes: 4096}); !errors.Is(err, ErrTooBig) {
		t.Fatalf("a bomb: %v", err)
	}
	for _, n := range names(root) {
		if strings.HasPrefix(n, "dest/") {
			t.Errorf("a refused extraction left %s", n)
		}
	}
	makeZip(t, root, "many.zip", map[string]string{"a": "1", "b": "2", "c": "3"}, nil)
	if _, err := c.Unzip("many.zip", "dest", false, Limits{Entries: 2, Bytes: 1 << 20}); !errors.Is(err, ErrTooMany) {
		t.Errorf("entry limit: %v", err)
	}
}

func TestUnzipRefusesWhatIsNotAZipOrWhereItCannotGo(t *testing.T) {
	c, root := newClient(t)
	write(t, root, "fake.zip", "this is not a zip")
	write(t, root, "afile", "x")
	makeZip(t, root, "ok.zip", map[string]string{"a": "1"}, nil)
	_ = os.Mkdir(filepath.Join(root, "dest"), 0o755)
	if _, err := c.Unzip("fake.zip", "dest", false, DefaultLimits); err == nil || !strings.Contains(err.Error(), "not a zip") {
		t.Errorf("not a zip: %v", err)
	}
	if _, err := c.Unzip("ok.zip", "afile", false, DefaultLimits); !errors.Is(err, ErrBadPath) {
		t.Errorf("into a file: %v", err)
	}
	if _, err := c.Unzip("ok.zip", "../dest", false, DefaultLimits); !errors.Is(err, ErrBadPath) {
		t.Errorf("out of the home: %v", err)
	}
	if _, err := c.Unzip("nope.zip", "dest", false, DefaultLimits); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing archive: %v", err)
	}
}

func TestUnzipNeverSetsSpecialModeBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("modes mean nothing here")
	}
	c, root := newClient(t)
	_ = os.Mkdir(filepath.Join(root, "dest"), 0o755)
	makeZip(t, root, "m.zip", nil, func(zw *zip.Writer) {
		h := &zip.FileHeader{Name: "suid"}
		h.SetMode(0o4777)
		w, _ := zw.CreateHeader(h)
		_, _ = w.Write([]byte("x"))
		h2 := &zip.FileHeader{Name: "plain"}
		h2.SetMode(0o600)
		w2, _ := zw.CreateHeader(h2)
		_, _ = w2.Write([]byte("x"))
	})
	if _, err := c.Unzip("m.zip", "dest", false, DefaultLimits); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(root, "dest/suid")); fi.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || fi.Mode().Perm() != 0o755 {
		t.Errorf("setuid entry: %v", fi.Mode())
	}
	if fi, _ := os.Stat(filepath.Join(root, "dest/plain")); fi.Mode().Perm() != 0o644 {
		t.Errorf("plain entry: %v", fi.Mode())
	}
}
