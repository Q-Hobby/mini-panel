package cloudsync

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mpn/internal/backup"
)

func gz(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.Bytes()
}

// realBackup makes a backup the way "mpn backup create" does: real gzip files and a MANIFEST that lists them.
func realBackup(t *testing.T, root, siteName, stamp string, dbs ...string) {
	t.Helper()
	dir := filepath.Join(root, stamp)
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "files.tar.gz"), gz(t, "files of "+stamp), 0o600)
	for _, d := range dbs {
		_ = os.WriteFile(filepath.Join(dir, d+".sql.gz"), gz(t, "dump of "+d), 0o600)
	}
	if err := backup.Seal(dir, backup.Manifest{Site: siteName, Stamp: stamp, Files: true, DBs: dbs}, testKey); err != nil {
		t.Fatal(err)
	}
}

// testKey is the backup key of the server in these tests.
var testKey = []byte("0123456789abcdef0123456789abcdef")

// stored uploads backups with the real sync and returns the storage.
func stored(t *testing.T, siteName string, stamps ...string) (*memRemote, string) {
	t.Helper()
	root := t.TempDir()
	for _, s := range stamps {
		realBackup(t, root, siteName, s, siteName)
	}
	m := newMem()
	var log bytes.Buffer
	if _, err := SyncSite(context.Background(), m, siteName, stamps, func(s string) string { return filepath.Join(root, s) }, 7, &log); err != nil {
		t.Fatal(err)
	}
	return m, root
}

func fetch(m *memRemote, siteName, stamp, dir string, free int64) error {
	var log bytes.Buffer
	return FetchBackup(context.Background(), m, siteName, stamp, dir, free, &log, nil, testKey)
}

// Whoever can write to the storage cannot hand the server a backup: one this server did not sign, a signed one with a file
// swapped, and one signed by another server are all refused.
func TestOnlyABackupThisServerSignedComesBack(t *testing.T) {
	m, _ := stored(t, "blog", "20261101-030000")
	signed := m.files["blog/20261101-030000/MANIFEST"]

	unsigned := backup.Manifest{Site: "blog", Stamp: "20261101-030000", Files: true, DBs: []string{"blog"}}.String()
	m.files["blog/20261101-030000/MANIFEST"] = []byte(unsigned)
	if err := fetch(m, "blog", "20261101-030000", filepath.Join(t.TempDir(), "blog"), -1); err == nil || !strings.Contains(err.Error(), "cloud-unsigned") {
		t.Errorf("an unsigned MANIFEST: %v", err)
	}

	m.files["blog/20261101-030000/MANIFEST"] = signed
	m.files["blog/20261101-030000/blog.sql.gz"] = gz(t, "DROP DATABASE blog;")
	if err := fetch(m, "blog", "20261101-030000", filepath.Join(t.TempDir(), "blog"), -1); err == nil || !strings.Contains(err.Error(), "not the file") {
		t.Errorf("a swapped dump: %v", err)
	}

	m2, _ := stored(t, "blog", "20261101-030000")
	var log bytes.Buffer
	if err := FetchBackup(context.Background(), m2, "blog", "20261101-030000", filepath.Join(t.TempDir(), "blog"), -1, &log, nil, []byte("another server's key, 32 bytes!")); err == nil {
		t.Error("a backup signed by another server was accepted")
	}
}

func TestABackupComesBackIdenticalAndAppearsOnlyWhenWhole(t *testing.T) {
	m, src := stored(t, "blog", "20261101-030000", "20261102-030000")
	dest := filepath.Join(t.TempDir(), "blog")
	if err := fetch(m, "blog", "20261101-030000", dest, -1); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"files.tar.gz", "blog.sql.gz", "MANIFEST"} {
		want, _ := os.ReadFile(filepath.Join(src, "20261101-030000", n))
		got, err := os.ReadFile(filepath.Join(dest, "20261101-030000", n))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: %v", n, err)
		}
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 1 || entries[0].Name() != "20261101-030000" {
		t.Errorf("only the finished backup may be in the folder: %v", entries)
	}
	if err := fetch(m, "blog", "20261101-030000", dest, -1); err == nil || !strings.Contains(err.Error(), "cloud-exists") {
		t.Errorf("a backup that is here already must not be overwritten: %v", err)
	}
}

func TestListingShowsFinishedCopiesNewestFirstWithTheirSize(t *testing.T) {
	m, _ := stored(t, "blog", "20261101-030000", "20261103-030000", "20261102-030000")
	m.files["blog/20261104-030000/files.tar.gz"] = []byte("cut off")
	m.files["blog/holiday-photos/a.jpg"] = []byte("x")
	l, err := ListRemoteBackups(context.Background(), m, "blog")
	if err != nil || len(l) != 3 || l[0].Stamp != "20261103-030000" || l[2].Stamp != "20261101-030000" || l[0].Bytes <= 0 {
		t.Fatalf("%+v %v", l, err)
	}
	if l, err := ListRemoteBackups(context.Background(), m, "shop"); err != nil || len(l) != 0 {
		t.Errorf("a site with no folder has no copies: %v %v", l, err)
	}
	if _, err := ListRemoteBackups(context.Background(), m, "../x"); err == nil {
		t.Error("a bad site name")
	}
}

func TestAFileThatArrivesDamagedWithoutAnErrorIsRefused(t *testing.T) {
	m, _ := stored(t, "blog", "20261101-030000")
	m.corrupt["blog/20261101-030000/files.tar.gz"] = true // a storage without hashes hands back a changed file and says nothing
	dest := filepath.Join(t.TempDir(), "blog")
	err := fetch(m, "blog", "20261101-030000", dest, -1)
	if err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("got %v", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Errorf("a failed download must leave nothing behind: %v", entries)
	}
}

func TestAFileThatIsNotGzipIsRefused(t *testing.T) {
	m, _ := stored(t, "blog", "20261101-030000")
	m.files["blog/20261101-030000/blog.sql.gz"] = []byte("this is not gzip data at all")
	dest := filepath.Join(t.TempDir(), "blog")
	if err := fetch(m, "blog", "20261101-030000", dest, -1); err == nil || !strings.Contains(err.Error(), "gzip") {
		t.Errorf("got %v", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Errorf("nothing may be left: %v", entries)
	}
}

func TestAManifestThatDoesNotMatchIsRefused(t *testing.T) {
	for name, text := range map[string]string{
		"another site":        "mpn backup 1\nsite=shop\nstamp=20261101-030000\nfiles=files.tar.gz\n",
		"another stamp":       "mpn backup 1\nsite=blog\nstamp=20261105-030000\nfiles=files.tar.gz\n",
		"a database of other": "mpn backup 1\nsite=blog\nstamp=20261101-030000\nfiles=files.tar.gz\ndb=shop_orders\n",
		"not a manifest":      "hello\n",
		"an unknown key":      "mpn backup 1\nsite=blog\nstamp=20261101-030000\nrun=rm -rf /\n",
		"a path in the name":  "mpn backup 1\nsite=blog\nstamp=20261101-030000\ndb=../../etc/passwd\n",
		"far too big":         strings.Repeat("x", 70<<10),
	} {
		m, _ := stored(t, "blog", "20261101-030000")
		m.files["blog/20261101-030000/MANIFEST"] = []byte(text)
		dest := filepath.Join(t.TempDir(), "blog")
		if err := fetch(m, "blog", "20261101-030000", dest, -1); err == nil {
			t.Errorf("%s: must be refused", name)
		}
		if entries, _ := os.ReadDir(dest); len(entries) != 0 {
			t.Errorf("%s: left %v", name, entries)
		}
	}
}

// GNU tar pads a .tar.gz to a whole record with zero bytes, and gzip -t accepts that; so must the check. Garbage after the stream,
// a second member that is broken, or a cut-off stream must still fail.
func TestTheGzipCheckMatchesGzipT(t *testing.T) {
	dir := t.TempDir()
	good := gz(t, "hello")
	two := append(gz(t, "a"), gz(t, "b")...)
	for name, c := range map[string]struct {
		data []byte
		ok   bool
	}{
		"plain":            {good, true},
		"padded by tar":    {append(append([]byte(nil), good...), make([]byte, 10240-len(good))...), true},
		"two members":      {two, true},
		"two then padding": {append(append([]byte(nil), two...), make([]byte, 500)...), true},
		"cut off":          {good[:len(good)-3], false},
		"garbage after":    {append(append([]byte(nil), good...), []byte("junk")...), false},
		"zeros then junk":  {append(append(append([]byte(nil), good...), make([]byte, 100)...), 'x'), false},
		"not gzip":         {[]byte("hello"), false},
		"empty":            {nil, false},
	} {
		p := filepath.Join(dir, strings.ReplaceAll(name, " ", "_"))
		_ = os.WriteFile(p, c.data, 0o600)
		if err := gzipOK(p); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAFileTheManifestListsButTheStorageLacksIsRefused(t *testing.T) {
	m, _ := stored(t, "blog", "20261101-030000")
	delete(m.files, "blog/20261101-030000/blog.sql.gz")
	err := fetch(m, "blog", "20261101-030000", filepath.Join(t.TempDir(), "blog"), -1)
	if err == nil || !strings.Contains(err.Error(), "cloud-nocopy") {
		t.Errorf("got %v", err)
	}
}

func TestACutOffCopyCannotBeFetched(t *testing.T) {
	m := newMem()
	m.files["blog/20261101-030000/files.tar.gz"] = gz(t, "x")
	if err := fetch(m, "blog", "20261101-030000", filepath.Join(t.TempDir(), "blog"), -1); err == nil || !strings.Contains(err.Error(), "cloud-nocopy") {
		t.Errorf("got %v", err)
	}
	if err := fetch(m, "blog", "20261109-030000", filepath.Join(t.TempDir(), "blog"), -1); err == nil || !strings.Contains(err.Error(), "cloud-nocopy") {
		t.Errorf("a backup the storage does not have: %v", err)
	}
}

func TestNotEnoughDiskSpaceStopsBeforeAnythingIsDownloaded(t *testing.T) {
	m, _ := stored(t, "blog", "20261101-030000")
	before := len(m.downloads)
	dest := filepath.Join(t.TempDir(), "blog")
	err := fetch(m, "blog", "20261101-030000", dest, 1<<20)
	if err == nil || !strings.Contains(err.Error(), "cloud-nospace") {
		t.Fatalf("got %v", err)
	}
	if got := m.downloads[before:]; len(got) != 1 || got[0] != "MANIFEST" {
		t.Errorf("only the MANIFEST may be downloaded: %v", got)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Errorf("nothing may be left: %v", entries)
	}
	if err := fetch(m, "blog", "20261101-030000", dest, 10<<30); err != nil {
		t.Errorf("with room it works: %v", err)
	}
}

func TestAFailureLeavesNothingAndARetrySucceeds(t *testing.T) {
	m, _ := stored(t, "blog", "20261101-030000")
	m.failDownload = "files.tar.gz"
	dest := filepath.Join(t.TempDir(), "blog")
	if err := fetch(m, "blog", "20261101-030000", dest, -1); err == nil {
		t.Fatal("the network failure must be reported")
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("a half-downloaded backup must not stay: %v", entries)
	}
	if err := fetch(m, "blog", "20261101-030000", dest, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "20261101-030000", "MANIFEST")); err != nil {
		t.Error("the backup is complete after the retry")
	}
}

func TestOnlyTheFilesOfABackupAreDownloaded(t *testing.T) {
	m, _ := stored(t, "blog", "20261101-030000")
	for _, n := range []string{"authorized_keys", "notes.txt", "evil.sh"} {
		m.files["blog/20261101-030000/"+n] = []byte("x")
	}
	dest := filepath.Join(t.TempDir(), "blog")
	if err := fetch(m, "blog", "20261101-030000", dest, -1); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dest, "20261101-030000"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "MANIFEST,blog.sql.gz,files.tar.gz" {
		t.Errorf("a stranger's file ended up in the backup: %v", names)
	}
}

func TestBadNamesAreRefused(t *testing.T) {
	m := newMem()
	for _, c := range [][2]string{{"../x", "20261101-030000"}, {"blog", "../../etc"}, {"blog", "bad"}, {"", "20261101-030000"}} {
		if err := fetch(m, c[0], c[1], t.TempDir(), -1); err == nil {
			t.Errorf("%q %q must be refused", c[0], c[1])
		}
	}
}

// A copy in the storage may have been written by somebody else. It cannot bring in a dump of a database that this server records as
// another site's, even when the name fits.
func TestAFetchRefusesADatabaseThatTheRecordsGiveToAnotherSite(t *testing.T) {
	root := t.TempDir()
	realBackup(t, root, "blog", "20261103-030000", "blog_shop")
	m := newMem()
	var log bytes.Buffer
	if _, err := SyncSite(context.Background(), m, "blog", []string{"20261103-030000"}, func(s string) string { return filepath.Join(root, s) }, 7, &log); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "blog")
	owner := func(db string) bool { return db != "blog_shop" } // the records give blog_shop to the site blog-shop
	err := FetchBackup(context.Background(), m, "blog", "20261103-030000", dest, -1, &log, owner, testKey)
	if err == nil || !strings.Contains(err.Error(), "blog_shop") {
		t.Fatalf("err=%v", err)
	}
	if _, serr := os.Stat(filepath.Join(dest, "20261103-030000")); serr == nil {
		t.Error("the backup was kept")
	}
}
