package panelssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"mpn/internal/panelkey"
)

// server is a tiny SSH server on 127.0.0.1 that accepts one user with one key and answers any command with the name
// of the user, which is all these tests need to see.
type server struct {
	addr     string
	hostPub  ssh.PublicKey
	hostGlob string
}

func startServer(t *testing.T, allowedUser string, allowedKey ssh.PublicKey) server {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	hostSigner, _ := ssh.NewSignerFromKey(priv)
	hostPub, _ := ssh.NewPublicKey(pub)
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "ssh_host_ed25519_key.pub"), ssh.MarshalAuthorizedKey(hostPub), 0o644)
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if c.User() == allowedUser && string(k.Marshal()) == string(allowedKey.Marshal()) {
			return nil, nil
		}
		return nil, os.ErrPermission
	}}
	cfg.AddHostKey(hostSigner)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			nc, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					c, creqs, _ := ch.Accept()
					go func() {
						for r := range creqs {
							if r.Type == "exec" {
								_ = r.Reply(true, nil)
								_, _ = c.Write([]byte(conn.User() + "\n"))
								_, _ = c.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
								c.Close()
								return
							}
							_ = r.Reply(false, nil)
						}
					}()
				}
			}()
		}
	}()
	return server{addr: l.Addr().String(), hostPub: hostPub, hostGlob: filepath.Join(dir, "ssh_host_*_key.pub")}
}

func panelKey(t *testing.T) (panelkey.Paths, ssh.PublicKey) {
	d := t.TempDir()
	p := panelkey.Paths{Public: filepath.Join(d, "pub"), Private: filepath.Join(d, "key")}
	if _, err := panelkey.Ensure(p, -1, -1); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p.Public)
	pub, _, _, _, _ := ssh.ParseAuthorizedKey(b)
	return p, pub
}

func run(c *ssh.Client) (string, error) {
	s, err := c.NewSession()
	if err != nil {
		return "", err
	}
	defer s.Close()
	out, err := s.Output("whoami")
	return strings.TrimSpace(string(out)), err
}

func TestLoginWithThePanelKeyWorks(t *testing.T) {
	keys, pub := panelKey(t)
	srv := startServer(t, "demo", pub)
	c, err := Client{Addr: srv.addr, KeyPath: keys.Private, HostKeyGlob: srv.hostGlob, Timeout: 5 * time.Second}.Dial("demo")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if who, err := run(c); err != nil || who != "demo" {
		t.Errorf("who=%q err=%v", who, err)
	}
}

func TestLoginIsRefusedWhenSomethingIsWrong(t *testing.T) {
	keys, pub := panelKey(t)
	srv := startServer(t, "demo", pub)
	other, _ := panelKey(t)
	otherSrv := startServer(t, "demo", pub) // a different machine: its host key is not in srv's folder
	cases := map[string]struct {
		c    Client
		user string
	}{
		"another account":       {Client{Addr: srv.addr, KeyPath: keys.Private, HostKeyGlob: srv.hostGlob}, "someone"},
		"a key nobody trusts":   {Client{Addr: srv.addr, KeyPath: other.Private, HostKeyGlob: srv.hostGlob}, "demo"},
		"a host that is not us": {Client{Addr: otherSrv.addr, KeyPath: keys.Private, HostKeyGlob: srv.hostGlob}, "demo"},
		"no host keys to trust": {Client{Addr: srv.addr, KeyPath: keys.Private, HostKeyGlob: filepath.Join(t.TempDir(), "none*")}, "demo"},
		"a missing key file":    {Client{Addr: srv.addr, KeyPath: filepath.Join(t.TempDir(), "none"), HostKeyGlob: srv.hostGlob}, "demo"},
		"a name with a flag":    {Client{Addr: srv.addr, KeyPath: keys.Private, HostKeyGlob: srv.hostGlob}, "-oProxyCommand=x"},
		"nobody listening":      {Client{Addr: "127.0.0.1:1", KeyPath: keys.Private, HostKeyGlob: srv.hostGlob, Timeout: time.Second}, "demo"},
	}
	for name, c := range cases {
		if cl, err := c.c.Dial(c.user); err == nil {
			cl.Close()
			t.Errorf("%s: login succeeded", name)
		}
	}
}
