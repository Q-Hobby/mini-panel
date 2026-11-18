// Package sshtest is a small SSH server that runs inside a test. It accepts one account with one public key, answers a
// command with the account's name, and gives a terminal that echoes what it is sent, so the code that logs in as a
// website's account can be tested without a real sshd.
package sshtest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type Server struct {
	Addr     string // host:port to dial
	Home     string // a real temporary directory that the "sftp" subsystem serves
	HostGlob string // pattern that finds the server's public host key, as sshd's /etc/ssh/ssh_host_*_key.pub

	mu      sync.Mutex
	resizes [][2]uint32
	input   bytes.Buffer
	pty     [2]uint32
	logins  int
}

// Start runs the server until the test ends.
func Start(t *testing.T, allowedUser string, allowedKey ssh.PublicKey) *Server {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	hostSigner, _ := ssh.NewSignerFromKey(priv)
	hostPub, _ := ssh.NewPublicKey(pub)
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "ssh_host_ed25519_key.pub"), ssh.MarshalAuthorizedKey(hostPub), 0o644)
	srv := &Server{HostGlob: filepath.Join(dir, "ssh_host_*_key.pub"), Home: filepath.ToSlash(t.TempDir())}
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if c.User() == allowedUser && bytes.Equal(k.Marshal(), allowedKey.Marshal()) {
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
	srv.Addr = l.Addr().String()
	go func() {
		for {
			nc, err := l.Accept()
			if err != nil {
				return
			}
			go srv.serve(nc, cfg)
		}
	}()
	return srv
}

// Resizes lists the window sizes (columns, rows) the client reported after the terminal was opened.
func (s *Server) Resizes() [][2]uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][2]uint32(nil), s.resizes...)
}

// Input is everything the client typed into the terminal.
func (s *Server) Input() string { s.mu.Lock(); defer s.mu.Unlock(); return s.input.String() }

// PtySize is the size (columns, rows) the client asked for when it opened the terminal.
func (s *Server) PtySize() [2]uint32 { s.mu.Lock(); defer s.mu.Unlock(); return s.pty }

// Logins counts the successful logins so far.
func (s *Server) Logins() int { s.mu.Lock(); defer s.mu.Unlock(); return s.logins }

func (s *Server) serve(nc net.Conn, cfg *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.logins++
	s.mu.Unlock()
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		c, creqs, err := ch.Accept()
		if err != nil {
			continue
		}
		go s.session(conn.User(), c, creqs)
	}
}

func (s *Server) session(user string, c ssh.Channel, reqs <-chan *ssh.Request) {
	shell := make(chan struct{})
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		for r := range reqs {
			switch r.Type {
			case "pty-req":
				if len(r.Payload) >= 4 {
					n := binary.BigEndian.Uint32(r.Payload)
					rest := r.Payload[4+n:]
					s.mu.Lock()
					s.pty = [2]uint32{binary.BigEndian.Uint32(rest), binary.BigEndian.Uint32(rest[4:])}
					s.mu.Unlock()
				}
				_ = r.Reply(true, nil)
			case "window-change":
				s.mu.Lock()
				s.resizes = append(s.resizes, [2]uint32{binary.BigEndian.Uint32(r.Payload), binary.BigEndian.Uint32(r.Payload[4:])})
				s.mu.Unlock()
			case "subsystem":
				if len(r.Payload) >= 4 && string(r.Payload[4:]) == "sftp" {
					_ = r.Reply(true, nil)
					go func() {
						if sv, err := sftp.NewServer(c, sftp.WithServerWorkingDirectory(s.Home)); err == nil {
							_ = sv.Serve()
							_ = sv.Close()
						}
					}()
				} else {
					_ = r.Reply(false, nil)
				}
			case "shell":
				_ = r.Reply(true, nil)
				close(shell)
			case "exec":
				_ = r.Reply(true, nil)
				_, _ = c.Write([]byte(user + "\n"))
				_, _ = c.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
				c.Close()
				return
			default:
				_ = r.Reply(false, nil)
			}
		}
	}()
	select {
	case <-shell:
	case <-ended:
		return
	}
	_, _ = c.Write([]byte("ready\r\n$ "))
	buf := make([]byte, 4096)
	for {
		n, err := c.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.input.Write(buf[:n])
			s.mu.Unlock()
			_, _ = c.Write(buf[:n])
			if strings.Contains(string(buf[:n]), "exit") {
				_, _ = c.Write([]byte("\r\nlogout\r\n"))
				_, _ = c.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
				c.Close()
				return
			}
		}
		if err != nil {
			return
		}
	}
}
