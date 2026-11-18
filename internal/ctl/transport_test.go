package ctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRunner struct {
	mu      sync.Mutex
	calls   [][]string
	running int32
	maxRun  int32
	delay   time.Duration
	err     error
}

func (f *fakeRunner) Run(_ context.Context, argv []string, _ string) (string, error) {
	n := atomic.AddInt32(&f.running, 1)
	for {
		m := atomic.LoadInt32(&f.maxRun)
		if n <= m || atomic.CompareAndSwapInt32(&f.maxRun, m, n) {
			break
		}
	}
	time.Sleep(f.delay)
	atomic.AddInt32(&f.running, -1)
	f.mu.Lock()
	f.calls = append(f.calls, argv)
	f.mu.Unlock()
	return "out:" + strings.Join(argv, " "), f.err
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func (f *fakeRunner) called() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }

// ask runs one request through a server whose caller has the given uid.
func ask(t *testing.T, s *Server, uid uint32, raw string) Response {
	t.Helper()
	s.Peer = func(net.Conn) (uint32, error) { return uid, nil }
	cl, sv := net.Pipe()
	go s.handle(sv)
	_ = cl.SetDeadline(time.Now().Add(5 * time.Second))
	go func() { _, _ = cl.Write([]byte(raw)) }()
	var buf bytes.Buffer
	b := make([]byte, 4096)
	for {
		n, err := cl.Read(b)
		buf.Write(b[:n])
		if err != nil || bytes.Contains(buf.Bytes(), []byte("\n")) {
			break
		}
	}
	_ = cl.Close()
	var resp Response
	if err := jsonUnmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("answer %q: %v", buf.String(), err)
	}
	return resp
}

func newServer(r Runner) *Server {
	return &Server{Runner: r, Allow: AllowUIDs(997), Timeout: 5 * time.Second}
}

func TestServerRunsAnAllowedRequest(t *testing.T) {
	f := &fakeRunner{}
	resp := ask(t, newServer(f), 997, `{"op":"site.apply","site":"blog"}`)
	if !resp.OK || resp.Output != "out:site apply -name blog -apply" {
		t.Errorf("%+v", resp)
	}
}

func TestServerRefusesOtherCallersAndBadRequests(t *testing.T) {
	cases := map[string]struct {
		uid uint32
		raw string
	}{
		"other user":              {1000, `{"op":"site.list"}`},
		"root is not on the list": {0, `{"op":"site.list"}`},
		"unknown op":              {997, `{"op":"shell"}`},
		"extra field":             {997, `{"op":"site.list","argv":["x"]}`},
		"not json":                {997, `rm -rf /`},
		"invalid value":           {997, `{"op":"site.apply","site":"-purge"}`},
	}
	for name, c := range cases {
		f := &fakeRunner{}
		resp := ask(t, newServer(f), c.uid, c.raw)
		if resp.OK || resp.Error == "" || f.called() != 0 {
			t.Errorf("%s: %+v, runner called %d times", name, resp, f.called())
		}
	}
}

func TestServerReportsFailureWithItsOutput(t *testing.T) {
	f := &fakeRunner{err: errors.New("exit status 2")}
	resp := ask(t, newServer(f), 997, `{"op":"site.list"}`)
	if resp.OK || resp.Error != "exit status 2" || !strings.HasPrefix(resp.Output, "out:") {
		t.Errorf("%+v", resp)
	}
}

func TestWritesRunOneAtATime(t *testing.T) {
	f := &fakeRunner{delay: 80 * time.Millisecond}
	s := newServer(f)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); ask(t, s, 997, `{"op":"backup.create","site":"blog"}`) }()
	}
	wg.Wait()
	if f.called() != 4 || atomic.LoadInt32(&f.maxRun) != 1 {
		t.Errorf("calls=%d, most at once=%d", f.called(), f.maxRun)
	}
}

func TestOversizedRequestIsRefused(t *testing.T) {
	big := `{"op":"site.list","site":"` + strings.Repeat("a", maxRequest) + `"}`
	if _, err := readRequest(strings.NewReader(big)); err == nil {
		t.Error("a request over the size limit must be refused")
	}
}

func TestCapWriterKeepsTheStartAndNeverBlocks(t *testing.T) {
	w := &capWriter{max: 10}
	if n, err := w.Write([]byte(strings.Repeat("x", 100))); n != 100 || err != nil {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got := w.String(); !strings.HasPrefix(got, "xxxxxxxxxx\n[output cut]") || strings.Count(got, "x") != 10 {
		t.Errorf("%q", got)
	}
}
