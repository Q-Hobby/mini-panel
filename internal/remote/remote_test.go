package remote

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"

	"mpn/internal/backup"
	"mpn/internal/cloudprov"
	"mpn/internal/cloudsync"
)

// Every option of every backend mpn imports is looked at. An option whose name says it runs a command or reads a file of this
// machine must be off the list of the panel, and the list must name only options that exist, so an rclone upgrade that adds or
// renames one makes this fail.
func TestTheListOfProvidersMatchesRclone(t *testing.T) {
	var dangerous []string
	for _, p := range cloudprov.Providers {
		ri, err := fs.Find(p.Type)
		if err != nil {
			t.Errorf("%s is not imported: %v", p.Type, err)
			continue
		}
		known := map[string]bool{}
		for _, o := range ri.Options {
			known[o.Name] = true
			if cloudprov.Dangerous(o.Name) {
				dangerous = append(dangerous, p.Type+"."+o.Name)
			}
		}
		for k := range p.Allowed() {
			if k != "type" && !known[k] {
				t.Errorf("%s.%s is not an option of rclone", p.Type, k)
			}
		}
	}
	sort.Strings(dangerous)
	// The options that run a command or read a file, as of rclone v1.76.0. They are refused by the list; this only notices new ones.
	for _, must := range []string{"sftp.ssh", "sftp.server_command", "sftp.key_file", "sftp.md5sum_command", "webdav.bearer_token_command", "s3.shared_credentials_file"} {
		found := false
		for _, d := range dangerous {
			found = found || d == must
		}
		if !found {
			t.Errorf("%s is no longer seen as dangerous: the check of names needs looking at", must)
		}
	}
	t.Logf("options refused by name: %s", strings.Join(dangerous, ", "))
}

// Every backend imported is either a form made by hand or a generated one, and no generated field is an option that runs a command
// or reads a file of this machine.
func TestEveryImportedStorageHasAForm(t *testing.T) {
	have := map[string]bool{}
	for _, p := range cloudprov.Providers {
		have[p.Type] = true
		if !p.Generated {
			continue
		}
		ri, err := fs.Find(p.Type)
		if err != nil {
			t.Errorf("%s: %v", p.Type, err)
			continue
		}
		known := map[string]bool{}
		for _, o := range ri.Options {
			known[o.Name] = true
		}
		for _, f := range p.Fields {
			if !known[f.Key] {
				t.Errorf("%s.%s is not an option of rclone", p.Type, f.Key)
			}
			if cloudprov.Dangerous(f.Key) {
				t.Errorf("%s.%s is a dangerous option", p.Type, f.Key)
			}
		}
	}
	for _, ri := range fs.Registry {
		// local and the wrappers come in through other packages of rclone; none of them is a storage of its own.
		switch ri.Name {
		case "local", "tardigrade", "crypt", "alias", "union", "combine", "chunker", "hasher", "compress", "cache", "archive", "memory", "http":
			continue
		}
		if !have[ri.Name] {
			t.Errorf("%s is imported but has no form: run tools/provgen", ri.Name)
		}
	}
}

// A generated storage may hold keys rclone writes itself, but never a dangerous one or one that is not an option of rclone.
func TestGeneratedRemoteKeys(t *testing.T) {
	p, ok := cloudprov.Find("onedrive")
	if !ok || !p.Generated {
		t.Fatal("onedrive must be a generated storage")
	}
	if err := checkKeys(p, []string{"type", "token", "drive_id", "drive_type"}); err != nil {
		t.Errorf("keys rclone writes itself: %v", err)
	}
	for _, k := range []string{"auth_url", "token_url", "nonsense"} {
		if err := checkKeys(p, []string{"type", k}); err == nil && cloudprov.Dangerous(k) {
			t.Errorf("%s must be refused", k)
		}
	}
	if err := checkKeys(p, []string{"type", "nonsense"}); err == nil {
		t.Error("a key that is not an option of rclone must be refused")
	}
	s, _ := cloudprov.Find("smb")
	if err := checkKeys(s, []string{"type", "kerberos_ccache"}); err == nil {
		t.Error("smb.kerberos_ccache reads a file of this machine")
	}
	a, _ := cloudprov.Find("azureblob")
	for _, k := range []string{"service_principal_file", "use_az", "client_certificate_path"} {
		if err := checkKeys(a, []string{"type", k}); err == nil {
			t.Errorf("azureblob.%s must be refused", k)
		}
	}
}

// A config file changed by hand to hold an option off the list is refused before rclone builds the remote.
func TestARemoteWithAKeyOffTheListIsNotOpened(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "rclone.conf")
	_ = os.WriteFile(conf, []byte("[mpn]\ntype = sftp\nhost = 127.0.0.1\nuser = bk\nssh = /tmp/evil.sh\n"), 0o600)
	ctx, err := Setup(context.Background(), conf)
	if err != nil {
		t.Fatal(err)
	}
	config.LoadedData().Load()
	if _, err := Open(ctx, "mpn-backups"); err == nil || !strings.Contains(err.Error(), `"ssh"`) {
		t.Errorf("got %v", err)
	}
	_ = os.WriteFile(conf, []byte("[mpn]\ntype = local\n"), 0o600)
	config.LoadedData().Load()
	if _, err := Open(ctx, "/etc"); err == nil {
		t.Error("a local remote is not a storage of the list")
	}
	_ = os.WriteFile(conf, []byte("[other]\ntype = s3\n"), 0o600)
	config.LoadedData().Load()
	if _, err := Open(ctx, "x"); err == nil || !strings.Contains(err.Error(), "cloud-not-connected") {
		t.Errorf("no remote named mpn: %v", err)
	}
}

// sealKey is the backup key of the server in these tests.
var sealKey = []byte("0123456789abcdef0123456789abcdef")

func gz(s string) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.Bytes()
}

// Write makes a config file only root reads, with the secret obscured, the fixed settings in, and remote.json without any secret.
func TestWriteMakesAPrivateConfigWithObscuredSecrets(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "rclone.conf")
	ctx, err := Setup(context.Background(), conf)
	if err != nil {
		t.Fatal(err)
	}
	_ = ctx
	s := cloudprov.Settings{Type: "s3", Bucket: "my-backups", Folder: "srv1", Values: map[string]string{"provider": "Cloudflare", "access_key_id": "AKIA123",
		"secret_access_key": "TOPSECRET", "endpoint": "https://x.r2.cloudflarestorage.com"}}
	settings := filepath.Join(dir, "remote.json")
	if err := Write(s, settings); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(conf)
	text := string(b)
	// rclone reads the secret key of S3 as it is (it is not an IsPassword option): obscuring it made every request fail with 403.
	if !strings.Contains(text, "secret_access_key = TOPSECRET") || !strings.Contains(text, "access_key_id = AKIA123") {
		t.Errorf("the S3 secret must be written the way rclone reads it:\n%s", text)
	}
	for k, v := range map[string]string{"chunk_size": "5Mi", "upload_concurrency": "4", "env_auth": "false"} {
		if !strings.Contains(text, k+" = "+v) {
			t.Errorf("missing %s = %s:\n%s", k, v, text)
		}
	}
	if fi, _ := os.Stat(conf); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	j, _ := os.ReadFile(settings)
	if strings.Contains(string(j), "TOPSECRET") {
		t.Errorf("remote.json must hold no secret: %s", j)
	}
	saved, err := LoadSaved(settings)
	if err != nil || saved.Root() != "my-backups/srv1/mpn-backups" || saved.Shown["provider"] != "Cloudflare" {
		t.Errorf("%+v %v", saved, err)
	}
	// Writing again replaces the remote whole: nothing of the first one stays.
	s2 := cloudprov.Settings{Type: "ftp", Values: map[string]string{"host": "ftp.example.com", "user": "me", "pass": "pw"}}
	if err := Write(s2, settings); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(conf)
	if strings.Contains(string(b), "access_key_id") || strings.Count(string(b), "[") != 1 {
		t.Errorf("the old remote must be gone:\n%s", b)
	}
	// FTP's pass is an IsPassword option: it is obscured, and reveals back to what was typed.
	if strings.Contains(string(b), "pass = pw\n") || !strings.Contains(string(b), "pass = ") {
		t.Errorf("the FTP password must be obscured:\n%s", b)
	}
}

// Every secret field of the form is either obscured because rclone marks it IsPassword, or written as it is because rclone reads it so.
// The two lists below are what rclone v1.76.0 says; a change in a later rclone shows up here.
func TestSecretsAreObscuredExactlyWhenRcloneExpectsIt(t *testing.T) {
	want := map[string]bool{"s3.secret_access_key": false, "b2.key": false, "sftp.pass": true, "sftp.key_pem": false, "webdav.pass": true, "ftp.pass": true}
	for _, p := range cloudprov.Providers {
		if p.Generated { // obscuredKeys reads IsPassword from rclone itself, so a generated form cannot disagree with it
			continue
		}
		obs, err := obscuredKeys(p.Type)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range p.Fields {
			if !f.Secret {
				continue
			}
			w, ok := want[p.Type+"."+f.Key]
			if !ok {
				t.Errorf("%s.%s: say whether rclone obscures it", p.Type, f.Key)
			} else if obs[f.Key] != w {
				t.Errorf("%s.%s: rclone obscures it %v, expected %v", p.Type, f.Key, obs[f.Key], w)
			}
		}
	}
}

// The real Remote, over a local folder, through the real sync and fetch. local is off the list of the panel, so the remote is
// built directly here; what is tested is the code of this package that turns rclone into cloudsync.Remote.
func TestSyncAndFetchThroughRclone(t *testing.T) {
	ctx, err := Setup(context.Background(), filepath.Join(t.TempDir(), "rclone.conf"))
	if err != nil {
		t.Fatal(err)
	}
	store := t.TempDir()
	f, err := fs.NewFs(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	r := &Remote{f: f}
	if err := r.Test(ctx, t.TempDir()); err != nil {
		t.Fatalf("test: %v", err)
	}
	if left, _ := os.ReadDir(store); len(left) != 0 {
		t.Errorf("the test file must be gone: %v", left)
	}
	src := t.TempDir()
	stamps := []string{"20261101-030000", "20261102-030000", "20261103-030000"}
	for _, s := range stamps {
		d := filepath.Join(src, s)
		_ = os.MkdirAll(d, 0o700)
		_ = os.WriteFile(filepath.Join(d, "files.tar.gz"), gz("files "+s), 0o600)
		_ = os.WriteFile(filepath.Join(d, "blog.sql.gz"), gz("dump "+s), 0o600)
		if err := backup.Seal(d, backup.Manifest{Site: "blog", Stamp: s, Files: true, DBs: []string{"blog"}}, sealKey); err != nil {
			t.Fatal(err)
		}
	}
	var log bytes.Buffer
	res, err := cloudsync.SyncSite(ctx, r, "blog", stamps, func(s string) string { return filepath.Join(src, s) }, 2, &log)
	if err != nil || len(res.Uploaded) != 2 || res.Present != 2 {
		t.Fatalf("%+v %v\n%s", res, err, log.String())
	}
	list, err := cloudsync.ListRemoteBackups(ctx, r, "blog")
	if err != nil || len(list) != 2 || list[0].Stamp != "20261103-030000" {
		t.Fatalf("%+v %v", list, err)
	}
	dest := filepath.Join(t.TempDir(), "blog")
	if err := cloudsync.FetchBackup(ctx, r, "blog", "20261102-030000", dest, -1, &log, nil, sealKey); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dest, "20261102-030000", "files.tar.gz"))
	want, _ := os.ReadFile(filepath.Join(src, "20261102-030000", "files.tar.gz"))
	if !bytes.Equal(got, want) {
		t.Error("the fetched backup differs")
	}
	// one more backup: the oldest copy goes
	more := "20261104-030000"
	d := filepath.Join(src, more)
	_ = os.MkdirAll(d, 0o700)
	_ = os.WriteFile(filepath.Join(d, "files.tar.gz"), gz("files"), 0o600)
	_ = backup.Seal(d, backup.Manifest{Site: "blog", Stamp: more, Files: true}, sealKey)
	res, err = cloudsync.SyncSite(ctx, r, "blog", append(stamps, more), func(s string) string { return filepath.Join(src, s) }, 2, &log)
	if err != nil || strings.Join(res.Deleted, ",") != "20261102-030000" {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(store, "blog", "20261102-030000")); !os.IsNotExist(err) {
		t.Error("the old copy must be gone from the storage")
	}
}
