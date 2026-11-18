package cloudsync

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// memRemote is storage in memory: files by path, folders implied by the paths as on S3, plus folders made by Mkdir. It records the
// uploads in order.
type memRemote struct {
	files        map[string][]byte // "blog/20261101-030000/MANIFEST" -> content
	dirs         map[string]bool
	uploads      []string // "stamp/name" in the order uploaded
	failOn       string   // an upload whose name is this fails once
	purged       []string
	failDownload string
	downloads    []string
	corrupt      map[string]bool // a download of this path arrives with one byte changed and no error: a storage without hashes
}

func newMem() *memRemote {
	return &memRemote{files: map[string][]byte{}, dirs: map[string]bool{}, corrupt: map[string]bool{}}
}

func (m *memRemote) List(_ context.Context, dir string) ([]Entry, error) {
	prefix := dir + "/"
	if dir == "" {
		prefix = ""
	}
	seen := map[string]Entry{}
	add := func(p string, size int64, isFile bool) {
		if !strings.HasPrefix(p, prefix) {
			return
		}
		rest := strings.TrimPrefix(p, prefix)
		if rest == "" {
			return
		}
		name, _, deeper := strings.Cut(rest, "/")
		if deeper || !isFile {
			seen[name] = Entry{Name: name, Dir: true}
		} else {
			seen[name] = Entry{Name: name, Size: size}
		}
	}
	for p, b := range m.files {
		add(p, int64(len(b)), true)
	}
	for d := range m.dirs {
		add(d, 0, false)
	}
	out := make([]Entry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *memRemote) Upload(_ context.Context, dir, name, local string) error {
	b, err := os.ReadFile(local)
	if err != nil {
		return err
	}
	if m.failOn == name {
		m.failOn = ""
		return errors.New("network down")
	}
	m.uploads = append(m.uploads, path.Base(dir)+"/"+name)
	m.files[dir+"/"+name] = b
	return nil
}

func (m *memRemote) Download(_ context.Context, p, local string) error {
	m.downloads = append(m.downloads, path.Base(p))
	if m.failDownload == path.Base(p) {
		m.failDownload = ""
		return errors.New("network down")
	}
	b, ok := m.files[p]
	if !ok {
		return errors.New("404 not found")
	}
	if m.corrupt[p] {
		b = append([]byte(nil), b...)
		b[len(b)/2] ^= 0xff
	}
	return os.WriteFile(local, b, 0o600)
}

func (m *memRemote) Purge(_ context.Context, dir string) error {
	m.purged = append(m.purged, path.Base(dir))
	for p := range m.files {
		if strings.HasPrefix(p, dir+"/") {
			delete(m.files, p)
		}
	}
	for d := range m.dirs {
		if d == dir || strings.HasPrefix(d, dir+"/") {
			delete(m.dirs, d)
		}
	}
	return nil
}

func (m *memRemote) Mkdir(_ context.Context, dir string) error { m.dirs[dir] = true; return nil }

func makeBackup(t *testing.T, root, stamp string, withManifest bool) {
	t.Helper()
	dir := filepath.Join(root, stamp)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"files.tar.gz", "blog.sql.gz", "notes.txt"} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("x"+n), 0o600)
	}
	if withManifest {
		_ = os.WriteFile(filepath.Join(dir, "MANIFEST"), []byte("mpn backup 1\n"), 0o600)
	}
}

func run(t *testing.T, m *memRemote, root string, local []string, keep int) (Result, error) {
	t.Helper()
	var log bytes.Buffer
	return SyncSite(context.Background(), m, "blog", local, func(s string) string { return filepath.Join(root, s) }, keep, &log)
}

func TestFirstSyncUploadsEveryBackupWithTheManifestLast(t *testing.T) {
	root := t.TempDir()
	makeBackup(t, root, "20261101-030000", true)
	makeBackup(t, root, "20261102-030000", true)
	m := newMem()
	res, err := run(t, m, root, []string{"20261101-030000", "20261102-030000"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Uploaded, ",") != "20261101-030000,20261102-030000" || res.Present != 2 {
		t.Errorf("%+v", res)
	}
	if m.uploads[0] != "20261102-030000/blog.sql.gz" {
		t.Errorf("the newest backup must be uploaded first: %v", m.uploads)
	}
	for _, stamp := range []string{"20261101-030000", "20261102-030000"} {
		var in []string
		for _, u := range m.uploads {
			if strings.HasPrefix(u, stamp+"/") {
				in = append(in, strings.TrimPrefix(u, stamp+"/"))
			}
		}
		if len(in) != 3 || in[2] != "MANIFEST" {
			t.Errorf("%s: %v", stamp, in)
		}
		for _, n := range in {
			if n == "notes.txt" {
				t.Errorf("a file that is not part of a backup must stay: %v", in)
			}
		}
	}
}

func TestASecondSyncUploadsNothingNew(t *testing.T) {
	root := t.TempDir()
	makeBackup(t, root, "20261101-030000", true)
	m := newMem()
	if _, err := run(t, m, root, []string{"20261101-030000"}, 7); err != nil {
		t.Fatal(err)
	}
	n := len(m.uploads)
	res, err := run(t, m, root, []string{"20261101-030000"}, 7)
	if err != nil || len(res.Uploaded) != 0 || len(m.uploads) != n || res.Present != 1 {
		t.Errorf("%+v %v uploads %d→%d", res, err, n, len(m.uploads))
	}
}

func TestACutOffCopyIsMadeAgainNotTrusted(t *testing.T) {
	root := t.TempDir()
	makeBackup(t, root, "20261101-030000", true)
	m := newMem()
	m.failOn = "MANIFEST"
	if _, err := run(t, m, root, []string{"20261101-030000"}, 7); err == nil {
		t.Fatal("the failed upload must be reported")
	}
	res, err := run(t, m, root, []string{"20261101-030000"}, 7)
	if err != nil || len(res.Uploaded) != 1 || res.Present != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if len(m.purged) == 0 {
		t.Error("the cut-off copy must be removed before it is made again")
	}
	count := 0
	for _, u := range m.uploads {
		if strings.HasSuffix(u, "/MANIFEST") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("exactly one finished copy expected, MANIFEST uploaded %d times", count)
	}
}

func TestOnlyTheNewestBackupsAreKeptInTheStorage(t *testing.T) {
	root := t.TempDir()
	stamps := []string{"20261101-030000", "20261102-030000", "20261103-030000", "20261104-030000"}
	for _, s := range stamps {
		makeBackup(t, root, s, true)
	}
	m := newMem()
	if _, err := run(t, m, root, stamps[:3], 3); err != nil {
		t.Fatal(err)
	}
	m.files["blog/my-holiday-photos/a.jpg"] = []byte("x")
	m.files["blog/20261101-030000.zip"] = []byte("x")
	res, err := run(t, m, root, stamps, 3)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Uploaded, ",") != "20261104-030000" || strings.Join(res.Deleted, ",") != "20261101-030000" || res.Present != 3 {
		t.Errorf("%+v", res)
	}
	if _, ok := m.files["blog/my-holiday-photos/a.jpg"]; !ok {
		t.Error("a folder that is not a backup was touched")
	}
	if _, ok := m.files["blog/20261101-030000.zip"]; !ok {
		t.Error("a file that is not a backup was touched")
	}
	if _, ok := m.files["blog/20261101-030000/MANIFEST"]; ok {
		t.Error("the oldest copy must be gone")
	}
}

func TestAnOldCutOffCopyIsRemovedOnceANewerOneIsFinished(t *testing.T) {
	root := t.TempDir()
	makeBackup(t, root, "20261102-030000", true)
	m := newMem()
	m.files["blog/20261101-030000/files.tar.gz"] = []byte("half")
	res, err := run(t, m, root, []string{"20261102-030000"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Deleted, ",") != "20261101-030000" {
		t.Errorf("%+v", res)
	}
}

func TestAnUnfinishedLocalBackupIsNotUploaded(t *testing.T) {
	root := t.TempDir()
	makeBackup(t, root, "20261101-030000", false)
	m := newMem()
	if _, err := run(t, m, root, []string{"20261101-030000"}, 7); err == nil || !strings.Contains(err.Error(), "MANIFEST") {
		t.Errorf("got %v", err)
	}
	if len(m.uploads) != 0 {
		t.Errorf("nothing may be uploaded: %v", m.uploads)
	}
}

func TestBadInputIsRefused(t *testing.T) {
	m := newMem()
	var log bytes.Buffer
	if _, err := SyncSite(context.Background(), m, "../x", nil, nil, 7, &log); err == nil {
		t.Error("a bad site name")
	}
	if _, err := SyncSite(context.Background(), m, "blog", nil, nil, 0, &log); err == nil {
		t.Error("keep must be at least one")
	}
	root := t.TempDir()
	makeBackup(t, root, "20261101-030000", true)
	res, err := run(t, m, root, []string{"../../etc", "bad", "20261101-030000"}, 7)
	if err != nil || len(res.Uploaded) != 1 {
		t.Errorf("only valid stamps are used: %+v %v", res, err)
	}
}

func TestClientDetailsAreChecked(t *testing.T) {
	if err := ValidClient("1234567890-abc.apps.googleusercontent.com", "GOCSPX-abcdef_123456"); err != nil {
		t.Error(err)
	}
	for _, bad := range [][2]string{{"", "GOCSPX-abcdef"}, {"short", "GOCSPX-abcdef"}, {"a b c d e f g h i j", "GOCSPX-abcdef"},
		{"1234567890-abc.apps.googleusercontent.com", "x"}, {"1234567890-abc.apps.googleusercontent.com", "sec ret;rm -rf"}, {"1234567890-abc\n.apps", "GOCSPX-abcdef"}} {
		if ValidClient(bad[0], bad[1]) == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestTheConnectionFilesArePrivateAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "etc", "gdrive.json")
	in := Config{ClientID: "id-1234567890", ClientSecret: "sec-12345678", RefreshToken: "rt", RootID: "root1", Email: "me@example.com"}
	if err := SaveConfig(cfg, in); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(cfg); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("the secret must be readable by root only, mode %v", fi.Mode().Perm())
	}
	out, err := LoadConfig(cfg)
	if err != nil || out != in {
		t.Errorf("%+v %v", out, err)
	}
	if _, err := LoadConfig(filepath.Join(dir, "none.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing file is os.ErrNotExist: %v", err)
	}
	p := Pending{ClientID: "a", DeviceCode: "dc", UserCode: "AB-CD", URL: "u", Expires: time.Now().Add(time.Hour).Unix()}
	pf := filepath.Join(dir, "pending.json")
	if err := SavePending(pf, p); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadPending(pf); err != nil || got != p || got.Expired(time.Now()) || !got.Expired(time.Now().Add(2*time.Hour)) {
		t.Errorf("%+v %v", got, err)
	}
}

func TestOnlyOneSyncRunsAtATime(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "run", "sync.lock")
	l, err := TakeLock(lock, func(int) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TakeLock(lock, func(int) bool { return true }); !errors.Is(err, ErrBusy) {
		t.Errorf("a second sync must wait: %v", err)
	}
	l.Release()
	if _, err := TakeLock(lock, func(int) bool { return true }); err != nil {
		t.Fatal(err)
	}
	l3, err := TakeLock(lock, func(int) bool { return false })
	if err != nil {
		t.Errorf("a stale lock must be taken over: %v", err)
	}
	if l3 != nil {
		l3.Release()
	}
}
