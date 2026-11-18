package ctl

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"mpn/internal/cloudprov"
)

const (
	testClientID = "1234567890-abcdefg.apps.googleusercontent.com"
	testSecret   = "GOCSPX-abcdef_123456"
)

func TestCloudArgv(t *testing.T) {
	cases := []struct {
		r    Request
		want []string
	}{
		{Request{Op: "cloud.status"}, []string{"cloud", "status", "-json"}},
		{Request{Op: "cloud.status", Site: "blog"}, []string{"cloud", "status", "-json", "-site=blog"}},
		{Request{Op: "cloud.connect", CloudClientID: testClientID, CloudSecret: testSecret}, []string{"cloud", "connect", "-apply"}},
		{Request{Op: "cloud.finish"}, []string{"cloud", "finish", "-apply"}},
		{Request{Op: "cloud.disconnect"}, []string{"cloud", "disconnect", "-apply"}},
		{Request{Op: "cloud.enable", Site: "blog", CloudOn: true}, []string{"cloud", "enable", "-site=blog", "-on=true", "-apply"}},
		{Request{Op: "cloud.enable", Site: "blog"}, []string{"cloud", "enable", "-site=blog", "-on=false", "-apply"}},
		{Request{Op: "cloud.sync", Site: "blog"}, []string{"cloud", "sync", "-site=blog", "-detach", "-apply"}},
	}
	for _, c := range cases {
		got, err := c.r.Argv()
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, %v want %v", c.r.Op, got, err, c.want)
		}
	}
}

func TestCloudRefusesBadValues(t *testing.T) {
	bad := []Request{
		{Op: "cloud.connect"},
		{Op: "cloud.connect", CloudClientID: testClientID},
		{Op: "cloud.connect", CloudClientID: "short", CloudSecret: testSecret},
		{Op: "cloud.connect", CloudClientID: testClientID, CloudSecret: "x; rm -rf /"},
		{Op: "cloud.connect", CloudClientID: testClientID + "\n--evil", CloudSecret: testSecret},
		{Op: "cloud.enable", Site: "", CloudOn: true},
		{Op: "cloud.enable", Site: "../etc", CloudOn: true},
		{Op: "cloud.sync", Site: "Blog; id"},
		{Op: "cloud.sync"},
		{Op: "cloud.status", Site: "-x"},
	}
	for _, r := range bad {
		if got, err := r.Argv(); err == nil {
			t.Errorf("%+v must be refused, got %v", r, got)
		}
	}
}

// The client secret gives access to the account of the person: it must reach mpn on standard input and nowhere else.
func TestTheClientSecretTravelsOnStandardInputOnly(t *testing.T) {
	r := Request{Op: "cloud.connect", CloudClientID: testClientID, CloudSecret: testSecret}
	argv, _ := r.Argv()
	if j := strings.Join(argv, " "); strings.Contains(j, testSecret) || strings.Contains(j, testClientID) {
		t.Errorf("the arguments must hold neither the secret nor the id: %s", j)
	}
	var in struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.Unmarshal([]byte(r.Stdin()), &in); err != nil || in.ClientID != testClientID || in.ClientSecret != testSecret {
		t.Errorf("stdin: %q %v", r.Stdin(), err)
	}
	if (Request{Op: "cloud.sync", Site: "blog"}).Stdin() != "" {
		t.Error("only connect sends anything on standard input")
	}
}

func TestCloudRestoreArgv(t *testing.T) {
	got, err := Request{Op: "cloud.list", Site: "blog"}.Argv()
	if err != nil || !reflect.DeepEqual(got, []string{"cloud", "list", "-site=blog"}) {
		t.Errorf("list: %v %v", got, err)
	}
	got, err = Request{Op: "cloud.fetch", Site: "blog", Backup: "20261104-031500"}.Argv()
	if err != nil || !reflect.DeepEqual(got, []string{"cloud", "fetch", "-site=blog", "-backup=20261104-031500", "-detach", "-apply"}) {
		t.Errorf("fetch: %v %v", got, err)
	}
	for _, r := range []Request{{Op: "cloud.list"}, {Op: "cloud.list", Site: "../x"}, {Op: "cloud.fetch", Site: "blog"},
		{Op: "cloud.fetch", Site: "blog", Backup: "latest"}, {Op: "cloud.fetch", Site: "blog", Backup: "../../etc"}, {Op: "cloud.fetch", Site: "blog", Backup: "20261104-031500; id"},
		{Op: "cloud.fetch", Site: "Bad Name", Backup: "20261104-031500"}} {
		if got, err := r.Argv(); err == nil {
			t.Errorf("%+v must be refused, got %v", r, got)
		}
	}
	if !IsWrite("cloud.fetch") || IsWrite("cloud.list") || !Known("cloud.list") {
		t.Error("fetch changes the machine; list only reads")
	}
}

func s3Settings() *cloudprov.Settings {
	return &cloudprov.Settings{Type: "s3", Bucket: "my-backups", Values: map[string]string{"provider": "Cloudflare", "access_key_id": "AKIA0123456789",
		"secret_access_key": "SECRET/abc+def=", "endpoint": "https://acct.r2.cloudflarestorage.com"}}
}

// The settings of a storage hold its secret: they reach mpn on standard input and never on the command line.
func TestStorageSettingsTravelOnStandardInputOnly(t *testing.T) {
	r := Request{Op: "cloud.configure", CloudStorage: s3Settings()}
	argv, err := r.Argv()
	if err != nil || !reflect.DeepEqual(argv, []string{"cloud", "configure", "-apply"}) {
		t.Fatalf("%v %v", argv, err)
	}
	var in cloudprov.Settings
	if err := json.Unmarshal([]byte(r.Stdin()), &in); err != nil || in.Values["secret_access_key"] != "SECRET/abc+def=" || in.Bucket != "my-backups" {
		t.Errorf("stdin: %q %v", r.Stdin(), err)
	}
	if !IsWrite("cloud.configure") {
		t.Error("configure changes the machine")
	}
}

// Options of rclone that run a command or read a file of this machine must be refused before the helper runs anything.
func TestStorageSettingsThatCouldRunACommandAreRefused(t *testing.T) {
	for _, extra := range []struct{ typ, key, val string }{
		{"sftp", "ssh", "/tmp/evil.sh"}, {"sftp", "server_command", "sh -c id"}, {"sftp", "md5sum_command", "id"}, {"sftp", "key_file", "/etc/shadow"},
		{"webdav", "bearer_token_command", "id"}, {"s3", "shared_credentials_file", "/root/.aws/credentials"}, {"s3", "env_auth", "true"},
	} {
		s := &cloudprov.Settings{Type: extra.typ, Values: map[string]string{extra.key: extra.val}}
		switch extra.typ {
		case "s3":
			s = s3Settings()
			s.Values[extra.key] = extra.val
		case "sftp":
			s.Values["host"], s.Values["user"], s.Values["pass"] = "backup.example.com", "bk", "pw"
			s.Values["host_keys"] = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"
		case "webdav":
			s.Values["url"] = "https://dav.example.com/"
		}
		if got, err := (Request{Op: "cloud.configure", CloudStorage: s}).Argv(); err == nil {
			t.Errorf("%s.%s must be refused, got %v", extra.typ, extra.key, got)
		}
	}
	if _, err := (Request{Op: "cloud.configure"}).Argv(); err == nil {
		t.Error("configure without settings")
	}
}

func TestCloudOperationsAreClassified(t *testing.T) {
	for _, op := range []string{"cloud.connect", "cloud.finish", "cloud.disconnect", "cloud.enable", "cloud.sync"} {
		if !IsWrite(op) || !Known(op) {
			t.Errorf("%s changes the machine and must run one at a time", op)
		}
	}
	if IsWrite("cloud.status") || !Known("cloud.status") {
		t.Error("cloud.status only reads")
	}
}
