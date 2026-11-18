package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"

	"mpn/internal/panelkey"
	"mpn/internal/panelssh"
	"mpn/internal/sshtest"
)

type termEnv struct {
	url    string // http://host:port
	ssh    *sshtest.Server
	cookie string
	token  string
	srv    *Server
}

// startTerminalEnv serves the panel over a real HTTP server, with a fake SSH server behind the panel's own client.
func startTerminalEnv(t *testing.T, tweak func(*Server)) termEnv {
	t.Helper()
	d := t.TempDir()
	keys := panelkey.Paths{Public: filepath.Join(d, "pub"), Private: filepath.Join(d, "key")}
	if _, err := panelkey.Ensure(keys, -1, -1); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(keys.Public)
	pub, _, _, _, _ := ssh.ParseAuthorizedKey(b)
	fake := sshtest.Start(t, "blog", pub)
	var env termEnv
	h, _, c, tok := setupWith(t, sitePageAnswers(), func(s *Server) {
		s.PasswdFile = passwdWith(t, "/bin/bash")
		s.Dialer = panelssh.Client{Addr: fake.Addr, KeyPath: keys.Private, HostKeyGlob: fake.HostGlob, Timeout: 3 * time.Second}
		if tweak != nil {
			tweak(s)
		}
		env.srv = s
	})
	hs := httptest.NewServer(h)
	t.Cleanup(hs.Close)
	env.url, env.ssh, env.cookie, env.token = hs.URL, fake, c.Name+"="+c.Value, tok
	return env
}

func (e termEnv) dial(t *testing.T, path string, header http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	if header == nil {
		header = http.Header{}
	}
	if header.Get("Cookie") == "" {
		header.Set("Cookie", e.cookie)
	}
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.url, "http")+path, &websocket.DialOptions{HTTPHeader: header})
}

func (e termEnv) wsPath() string {
	return "/sites/blog/terminal/ws?csrf=" + e.token + "&cols=100&rows=30"
}

func readUntil(t *testing.T, c *websocket.Conn, want string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var got strings.Builder
	for !strings.Contains(got.String(), want) {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q, got %q: %v", want, got.String(), err)
		}
		got.Write(data)
	}
	return got.String()
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if ok() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Errorf("timed out waiting for %s", what)
}

func TestTheTerminalPageHasItsOwnPolicyAndANonceThatChangesEachTime(t *testing.T) {
	h, _, c, _ := setupWith(t, sitePageAnswers(), func(s *Server) { s.PasswdFile = passwdWith(t, "/bin/bash") })
	w1 := do(h, "GET", "/sites/blog/terminal", nil, cookie(c))
	w2 := do(h, "GET", "/sites/blog/terminal", nil, cookie(c))
	csp := w1.Header().Get("Content-Security-Policy")
	nonce := ""
	if i := strings.Index(csp, "'nonce-"); i >= 0 {
		nonce = csp[i+len("'nonce-") : i+strings.Index(csp[i+1:], "'")+1]
	}
	if nonce == "" || !strings.Contains(csp, "connect-src 'self'") || !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe") {
		t.Fatalf("policy: %q", csp)
	}
	body := w1.Body.String()
	for _, want := range []string{`data-nonce="` + nonce + `"`, "terminal.js?v=", "xterm.js?v=", "addon-fit.js?v=", "xterm.css?v=", `data-ws="/sites/blog/terminal/ws"`, `aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(w2.Header().Get("Content-Security-Policy"), nonce) {
		t.Error("the nonce must be new for every page load")
	}
	if other := do(h, "GET", "/sites/blog", nil, cookie(c)).Header().Get("Content-Security-Policy"); other != basePolicy {
		t.Errorf("every other page keeps the strict policy, got %q", other)
	}
}

func TestTheTerminalTabAsksToEnableTheLoginWhenItIsOff(t *testing.T) {
	h, _, c, _ := setupWith(t, sitePageAnswers(), func(s *Server) { s.PasswdFile = passwdWith(t, "/usr/sbin/nologin") })
	body := do(h, "GET", "/sites/blog/terminal", nil, cookie(c)).Body.String()
	if strings.Contains(body, "xterm.js") || !strings.Contains(body, `href="/sites/blog"`) {
		t.Errorf("no terminal can start without a login:\n%s", body)
	}
}

func TestAKeyboardSessionReachesTheShellAndBack(t *testing.T) {
	e := startTerminalEnv(t, nil)
	c, _, err := e.dial(t, e.wsPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	readUntil(t, c, "ready")
	if got := e.ssh.PtySize(); got != [2]uint32{100, 30} {
		t.Errorf("terminal size asked for: %v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageBinary, []byte("hello\r"))
	readUntil(t, c, "hello")
	eventually(t, "the typed text to reach the shell", func() bool { return strings.Contains(e.ssh.Input(), "hello") })
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":120,"rows":40}`))
	eventually(t, "the new size to reach the shell", func() bool {
		r := e.ssh.Resizes()
		return len(r) == 1 && r[0] == [2]uint32{120, 40}
	})
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"nonsense"}`))
	_ = c.Write(ctx, websocket.MessageText, []byte(`not json`))
	_ = c.Write(ctx, websocket.MessageBinary, []byte("exit\r"))
	readUntil(t, c, "logout")
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Errorf("the socket must be closed with a normal close frame when the shell ends, got %v", err)
	}
}

func TestTheTerminalRefusesWhatItShould(t *testing.T) {
	e := startTerminalEnv(t, nil)
	for name, c := range map[string]struct {
		path   string
		header http.Header
		want   int
	}{
		"no session (sent to the sign-in page)": {e.wsPath(), http.Header{"Cookie": {"mpn=forged"}}, http.StatusOK},
		"no form token":                         {"/sites/blog/terminal/ws", nil, http.StatusForbidden},
		"wrong form token":                      {"/sites/blog/terminal/ws?csrf=wrong", nil, http.StatusForbidden},
		"another origin":                        {e.wsPath(), http.Header{"Origin": {"https://evil.example"}}, http.StatusForbidden},
		"unknown site":                          {"/sites/nope/terminal/ws?csrf=" + e.token, nil, http.StatusNotFound},
		"bad site name":                         {"/sites/-x/terminal/ws?csrf=" + e.token, nil, http.StatusNotFound},
	} {
		conn, resp, err := e.dial(t, c.path, c.header)
		if err == nil {
			conn.CloseNow()
			t.Errorf("%s: connected", name)
			continue
		}
		if resp == nil || resp.StatusCode != c.want {
			t.Errorf("%s: %v, want status %d", name, resp, c.want)
		}
	}
	if e.ssh.Logins() != 0 {
		t.Errorf("a refused request must not log in: %d logins", e.ssh.Logins())
	}
}

func TestALoginFailureIsExplainedInTheTerminal(t *testing.T) {
	e := startTerminalEnv(t, func(s *Server) { s.Dialer = failingDialer{} })
	c, _, err := e.dial(t, e.wsPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if got := readUntil(t, c, "error"); !strings.Contains(got, "Không đăng nhập được") {
		t.Errorf("message: %q", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, err := c.Read(ctx); err == nil {
		t.Error("the socket must close after the explanation")
	}
	e2 := startTerminalEnv(t, func(s *Server) { s.Dialer = nil })
	if _, resp, err := e2.dial(t, e2.wsPath(), nil); err == nil || resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("without a dialer: %v %v", resp, err)
	}
}

type failingDialer struct{}

func (failingDialer) Dial(string) (*ssh.Client, error) { return nil, errors.New("key refused") }

func TestOnlyAFewTerminalsAtOnce(t *testing.T) {
	e := startTerminalEnv(t, func(s *Server) { s.MaxTerminals = 1 })
	first, _, err := e.dial(t, e.wsPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, first, "ready")
	if c, resp, err := e.dial(t, e.wsPath(), nil); err == nil || resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		if c != nil {
			c.CloseNow()
		}
		t.Errorf("the second terminal: %v %v", resp, err)
	}
	first.CloseNow()
	eventually(t, "the slot to be freed", func() bool { return e.srv.terms.Load() == 0 })
	third, _, err := e.dial(t, e.wsPath(), nil)
	if err != nil {
		t.Fatalf("after closing the first: %v", err)
	}
	third.CloseNow()
}

func TestAnIdleTerminalClosesAndFreesItsSlot(t *testing.T) {
	e := startTerminalEnv(t, func(s *Server) { s.TermIdle = 250 * time.Millisecond })
	c, _, err := e.dial(t, e.wsPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	readUntil(t, c, "ready")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	for {
		if _, _, err := c.Read(ctx); err != nil {
			if ctx.Err() != nil {
				t.Fatal("the idle terminal was not closed")
			}
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				t.Errorf("an idle terminal must be closed with a normal close frame, got %v", err)
			}
			break
		}
	}
	eventually(t, "the slot to be freed", func() bool { return e.srv.terms.Load() == 0 })
}

func TestTheTerminalLogsOpenAndCloseWithoutContent(t *testing.T) {
	var events []string
	e := startTerminalEnv(t, func(s *Server) {
		s.TermLog = func(ev, site string, err error) { events = append(events, ev+":"+site) }
	})
	c, _, err := e.dial(t, e.wsPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, c, "ready")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageBinary, []byte("secret-typing"))
	c.CloseNow()
	eventually(t, "the close to be logged", func() bool { return len(events) >= 2 })
	if !reflect.DeepEqual(events[:2], []string{"open:blog", "close:blog"}) {
		t.Errorf("events %v", events)
	}
	if strings.Contains(strings.Join(events, " "), "secret") {
		t.Error("what is typed must never reach the log")
	}
}
