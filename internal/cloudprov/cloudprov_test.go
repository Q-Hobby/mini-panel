package cloudprov

import (
	"strings"
	"testing"
)

const hostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"

func TestGoodSettingsOfEveryProviderPass(t *testing.T) {
	for _, s := range []Settings{
		{Type: "s3", Bucket: "my-backups", Values: map[string]string{"provider": "Cloudflare", "access_key_id": "abc123", "secret_access_key": "s3cr3t/+=", "endpoint": "https://x.r2.cloudflarestorage.com"}},
		{Type: "s3", Bucket: "my-backups", Folder: "server1/web", Values: map[string]string{"provider": "AWS", "access_key_id": "AKIA", "secret_access_key": "x", "region": "ap-southeast-1"}},
		{Type: "b2", Bucket: "my-backups", Values: map[string]string{"account": "0012ab", "key": "K001xyz"}},
		{Type: "sftp", Values: map[string]string{"host": "backup.example.com", "user": "bk", "pass": "pa ss\"word", "host_keys": hostKey}},
		{Type: "sftp", Values: map[string]string{"host": "10.0.0.5", "port": "2222", "user": "bk", "key_pem": "-----BEGIN OPENSSH PRIVATE KEY-----\r\nb3Blbn\r\n-----END OPENSSH PRIVATE KEY-----\r\n", "host_keys": hostKey}},
		{Type: "webdav", Values: map[string]string{"url": "https://cloud.example.com/remote.php/dav/files/me/", "vendor": "nextcloud", "user": "me", "pass": "x"}},
		{Type: "ftp", Values: map[string]string{"host": "ftp.example.com", "user": "me", "pass": "x"}},
	} {
		out, err := s.Check()
		if err != nil {
			t.Errorf("%s: %v", s.Type, err)
			continue
		}
		p, _ := Find(s.Type)
		for k, v := range p.Fixed {
			if out[k] != v {
				t.Errorf("%s: the fixed setting %s must be %q, is %q", s.Type, k, v, out[k])
			}
		}
		if s.Type == "sftp" && out["key_pem"] != "" && (strings.Contains(out["key_pem"], "\n") || !strings.Contains(out["key_pem"], `\n`)) {
			t.Errorf("a private key is one line with \\n for rclone: %q", out["key_pem"])
		}
		if s.Type == "sftp" && out["pass"] != "" && out["pass"] != s.Values["pass"] {
			t.Errorf("a password is kept exactly as typed: %q", out["pass"])
		}
	}
}

func TestDefaultsAreFilledIn(t *testing.T) {
	out, err := Settings{Type: "ftp", Values: map[string]string{"host": "ftp.example.com", "user": "me", "pass": "x"}}.Check()
	if err != nil || out["port"] != "21" || out["explicit_tls"] != "true" {
		t.Errorf("%v %v", out, err)
	}
}

func TestBadSettingsAreRefused(t *testing.T) {
	sftp := func(k, v string) Settings {
		s := Settings{Type: "sftp", Values: map[string]string{"host": "backup.example.com", "user": "bk", "pass": "x", "host_keys": hostKey}}
		s.Values[k] = v
		return s
	}
	s3 := func(edit func(*Settings)) Settings {
		s := Settings{Type: "s3", Bucket: "my-backups", Values: map[string]string{"provider": "Cloudflare", "access_key_id": "abc", "secret_access_key": "x", "endpoint": "https://e.example.com"}}
		edit(&s)
		return s
	}
	for name, s := range map[string]Settings{
		"unknown type":           {Type: "local", Values: map[string]string{}},
		"alias type":             {Type: "alias", Values: map[string]string{"remote": "/etc"}},
		"ssh command":            sftp("ssh", "/tmp/evil.sh"),
		"server command":         sftp("server_command", "id"),
		"checksum command":       sftp("md5sum_command", "id"),
		"key file":               sftp("key_file", "/etc/shadow"),
		"known hosts file":       sftp("known_hosts_file", "/etc/shadow"),
		"use agent":              sftp("key_use_agent", "true"),
		"newline in host":        sftp("host", "a.example.com\nssh = id"),
		"bad host":               sftp("host", "a b"),
		"bad port":               sftp("port", "99999"),
		"bad user":               sftp("user", "bk;id"),
		"no host key":            sftp("host_keys", ""),
		"bad host key":           sftp("host_keys", "ssh-ed25519 short"),
		"newline in password":    sftp("pass", "a\nssh = /tmp/x"),
		"key with passphrase":    sftp("key_pem", "-----BEGIN OPENSSH PRIVATE KEY-----\nProc-Type: 4,ENCRYPTED\n-----END OPENSSH PRIVATE KEY-----"),
		"not a key":              sftp("key_pem", "hello"),
		"no auth":                {Type: "sftp", Values: map[string]string{"host": "h.example.com", "user": "bk", "host_keys": hostKey}},
		"bearer command":         {Type: "webdav", Values: map[string]string{"url": "https://dav.example.com", "bearer_token_command": "id"}},
		"webdav file url":        {Type: "webdav", Values: map[string]string{"url": "file:///etc"}},
		"shared credentials":     s3(func(s *Settings) { s.Values["shared_credentials_file"] = "/root/.aws/credentials" }),
		"env auth":               s3(func(s *Settings) { s.Values["env_auth"] = "true" }),
		"unknown provider":       s3(func(s *Settings) { s.Values["provider"] = "Evil" }),
		"no endpoint":            s3(func(s *Settings) { delete(s.Values, "endpoint") }),
		"no bucket":              s3(func(s *Settings) { s.Bucket = "" }),
		"bucket with slash":      s3(func(s *Settings) { s.Bucket = "a/b" }),
		"folder up":              s3(func(s *Settings) { s.Folder = "a/../../b" }),
		"folder dot":             s3(func(s *Settings) { s.Folder = "./a" }),
		"folder absolute":        s3(func(s *Settings) { s.Folder = "/etc" }),
		"folder colon":           s3(func(s *Settings) { s.Folder = "other:remote" }),
		"bucket on sftp":         func() Settings { s := sftp("port", "22"); s.Bucket = "x"; return s }(),
		"endpoint with a quote":  s3(func(s *Settings) { s.Values["endpoint"] = "https://e.example.com\"x" }),
		"far too long secret":    s3(func(s *Settings) { s.Values["secret_access_key"] = strings.Repeat("a", 600) }),
		"control char in secret": s3(func(s *Settings) { s.Values["secret_access_key"] = "a\x00b" }),
	} {
		if _, err := s.Check(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

// No field that reaches rclone may be one of the options that run a command or read a file of this machine.
func TestNoAllowedKeyRunsACommandOrReadsAFile(t *testing.T) {
	// A dangerous key may be on the list only as a fixed value the form cannot change, and only with one of these values: the
	// checksum commands rclone runs on the other machine, and the two switches that turn off reading this machine's environment
	// and ssh-agent.
	safeFixed := map[string]string{"sftp.md5sum_command": "md5sum", "sftp.sha1sum_command": "sha1sum", "s3.env_auth": "false", "sftp.key_use_agent": "false"}
	for _, p := range Providers {
		for _, f := range p.Fields {
			if Dangerous(f.Key) {
				t.Errorf("%s.%s is a field of the form", p.Type, f.Key)
			}
		}
		for k, v := range p.Fixed {
			if Dangerous(k) && safeFixed[p.Type+"."+k] != v {
				t.Errorf("%s.%s = %q is fixed", p.Type, k, v)
			}
		}
	}
	p, _ := Find("sftp")
	if p.Fixed["md5sum_command"] != "md5sum" || p.Fixed["sha1sum_command"] != "sha1sum" {
		t.Error("the checksum commands of SFTP are fixed values the form cannot change")
	}
	for _, f := range p.Fields {
		if f.Key == "md5sum_command" || f.Key == "sha1sum_command" {
			t.Error("the checksum commands must not be fields of the form")
		}
	}
}

func TestShownNeverHoldsASecret(t *testing.T) {
	p, _ := Find("s3")
	got := p.Shown(map[string]string{"provider": "AWS", "access_key_id": "AKIA", "secret_access_key": "TOPSECRET"})
	if got["secret_access_key"] != "" || got["access_key_id"] != "AKIA" {
		t.Errorf("%v", got)
	}
}

func TestRoot(t *testing.T) {
	for _, c := range [][3]string{{"", "", "mpn-backups"}, {"b", "", "b/mpn-backups"}, {"b", "srv/1", "b/srv/1/mpn-backups"}, {"", "x", "x/mpn-backups"}} {
		if got := Root(c[0], c[1]); got != c[2] {
			t.Errorf("%q %q: %q", c[0], c[1], got)
		}
	}
}
