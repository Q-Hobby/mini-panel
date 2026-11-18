package gdrive

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fake is a small Google: it knows the endpoints the client uses and keeps what it was sent.
type fake struct {
	mu sync.Mutex
	*httptest.Server
	polls       int // device polls before the person "allows"
	denyRefresh bool
	badTokens   int // answers 401 to this many Drive calls, to see the client ask for a new token
	tokens      int
	files       map[string]File
	parents     map[string]string
	nextID      int
	upload      bytes.Buffer
	failPut     int // the next PUTs of data that answer 500 after storing nothing
	puts        int
	pages       bool
	content     map[string][]byte // what a download returns, by file id
	cutAfter    int               // the next download is cut after this many bytes, once
	ignoreRange bool              // answers a ranged request with the whole file
	ranges      int               // ranged requests seen
}

func newFake(t *testing.T) *fake {
	f := &fake{files: map[string]File{}, parents: map[string]string{}, content: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("scope") != Scope || r.PostForm.Get("client_id") != "cid" {
			http.Error(w, `{"error":"invalid_client"}`, 401)
			return
		}
		fmt.Fprint(w, `{"device_code":"dc","user_code":"ABCD-EFGH","verification_url":"https://www.google.com/device","expires_in":1800,"interval":5}`)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.PostForm.Get("grant_type") {
		case "urn:ietf:params:oauth:grant-type:device_code":
			if f.polls > 0 {
				f.polls--
				w.WriteHeader(428)
				fmt.Fprint(w, `{"error":"authorization_pending"}`)
				return
			}
			fmt.Fprint(w, `{"access_token":"a0","refresh_token":"rt","expires_in":3600}`)
		case "refresh_token":
			if f.denyRefresh || r.PostForm.Get("refresh_token") != "rt" {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":"invalid_grant"}`)
				return
			}
			f.tokens++
			fmt.Fprintf(w, `{"access_token":"tok%d","expires_in":3600}`, f.tokens)
		}
	})
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.badTokens > 0 {
			f.badTokens--
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":{"message":"Invalid Credentials"}}`)
			return false
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok") {
			w.WriteHeader(401)
			return false
		}
		return true
	}
	mux.HandleFunc("/drive/files", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case "GET":
			q := r.URL.Query().Get("q")
			var out []map[string]string
			for id, file := range f.files {
				parent := f.parents[id]
				if !strings.Contains(q, "'"+parent+"' in parents") {
					continue
				}
				if strings.Contains(q, "name='") && !strings.Contains(q, "name='"+file.Name+"'") {
					continue
				}
				out = append(out, map[string]string{"id": id, "name": file.Name, "mimeType": file.MimeType, "size": strconv.FormatInt(file.Size, 10)})
			}
			if f.pages && r.URL.Query().Get("pageToken") == "" && len(out) > 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"files": out[:1], "nextPageToken": "p2"})
				return
			}
			if f.pages && r.URL.Query().Get("pageToken") == "p2" {
				out = out[1:]
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"files": out})
		case "POST":
			var m struct {
				Name, MimeType string
				Parents        []string
			}
			_ = json.NewDecoder(r.Body).Decode(&m)
			f.nextID++
			id := fmt.Sprintf("id%d", f.nextID)
			f.files[id], f.parents[id] = File{ID: id, Name: m.Name, MimeType: m.MimeType}, m.Parents[0]
			fmt.Fprintf(w, `{"id":%q}`, id)
		}
	})
	mux.HandleFunc("/drive/files/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		id := strings.TrimPrefix(r.URL.Path, "/drive/files/")
		if r.Method == "GET" && r.URL.Query().Get("alt") == "media" {
			data, ok := f.content[id]
			if !ok {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"error":{"message":"File not found"}}`)
				return
			}
			start := 0
			if rg := r.Header.Get("Range"); rg != "" {
				f.ranges++
				if !f.ignoreRange {
					_, _ = fmt.Sscanf(rg, "bytes=%d-", &start)
					w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
					w.Header().Set("Content-Length", strconv.Itoa(len(data)-start))
					w.WriteHeader(206)
				}
			}
			if w.Header().Get("Content-Length") == "" {
				w.Header().Set("Content-Length", strconv.Itoa(len(data)-start))
			}
			body := data[start:]
			if f.cutAfter > 0 && len(body) > f.cutAfter {
				n := f.cutAfter
				f.cutAfter = 0
				_, _ = w.Write(body[:n])
				w.(http.Flusher).Flush()
				panic(http.ErrAbortHandler) // the connection breaks in the middle of the file
			}
			_, _ = w.Write(body)
			return
		}
		if _, ok := f.files[id]; !ok {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":{"message":"File not found"}}`)
			return
		}
		delete(f.files, id)
		w.WriteHeader(204)
	})
	mux.HandleFunc("/drive/about", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			fmt.Fprint(w, `{"user":{"emailAddress":"me@example.com"}}`)
		}
	})
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		w.Header().Set("Location", f.URL+"/session/1")
	})
	mux.HandleFunc("/session/1", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.puts++
		cr := r.Header.Get("Content-Range") // bytes a-b/total, or bytes */total
		spec, totalS, _ := strings.Cut(strings.TrimPrefix(cr, "bytes "), "/")
		total, _ := strconv.ParseInt(totalS, 10, 64)
		if spec == "*" {
			if f.upload.Len() == 0 {
				w.WriteHeader(308)
				return
			}
			w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", f.upload.Len()-1))
			w.WriteHeader(308)
			return
		}
		if f.failPut > 0 {
			f.failPut--
			w.WriteHeader(500)
			return
		}
		startS, _, _ := strings.Cut(spec, "-")
		start, _ := strconv.ParseInt(startS, 10, 64)
		if start != int64(f.upload.Len()) {
			w.WriteHeader(400)
			return
		}
		b := make([]byte, r.ContentLength)
		n, _ := r.Body.Read(b)
		for int64(n) < r.ContentLength {
			m, err := r.Body.Read(b[n:])
			n += m
			if err != nil {
				break
			}
		}
		f.upload.Write(b[:n])
		if int64(f.upload.Len()) >= total {
			fmt.Fprint(w, `{"id":"uploaded"}`)
			return
		}
		w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", f.upload.Len()-1))
		w.WriteHeader(308)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fake) client() *Client {
	return &Client{EP: Endpoints{DeviceCode: f.URL + "/device/code", Token: f.URL + "/token", API: f.URL + "/drive", Upload: f.URL + "/upload"},
		ClientID: "cid", ClientSecret: "sec", RefreshToken: "rt", Sleep: func(time.Duration) {}}
}

func TestDeviceFlow(t *testing.T) {
	f := newFake(t)
	c := f.client()
	d, err := StartDevice(context.Background(), nil2(), c.EP, "cid")
	if err != nil || d.UserCode != "ABCD-EFGH" || d.DeviceCode != "dc" || d.Interval != 5 {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := StartDevice(context.Background(), nil2(), c.EP, "wrong"); err == nil {
		t.Error("a client Google does not know must fail")
	}
	f.polls = 2
	for i := 0; i < 2; i++ {
		if _, err := PollDevice(context.Background(), nil2(), c.EP, "cid", "sec", "dc"); !errors.Is(err, ErrPending) {
			t.Fatalf("poll %d: %v", i, err)
		}
	}
	rt, err := PollDevice(context.Background(), nil2(), c.EP, "cid", "sec", "dc")
	if err != nil || rt != "rt" {
		t.Errorf("%q %v", rt, err)
	}
}

func nil2() *http.Client { return &http.Client{Timeout: 5 * time.Second} }

func TestTheAccessTokenIsRefreshedOnceAndAgainWhenGoogleRefusesIt(t *testing.T) {
	f := newFake(t)
	c := f.client()
	if c.Email(context.Background()) != "me@example.com" || c.Email(context.Background()) != "me@example.com" {
		t.Fatal("email")
	}
	if f.tokens != 1 {
		t.Errorf("one token serves both calls, got %d", f.tokens)
	}
	f.badTokens = 1
	if c.Email(context.Background()) != "me@example.com" || f.tokens != 2 {
		t.Errorf("a refused token must be replaced: tokens=%d", f.tokens)
	}
}

func TestARevokedPermissionIsReportedAtOnce(t *testing.T) {
	f := newFake(t)
	f.denyRefresh = true
	c := f.client()
	_, err := c.EnsureFolder(context.Background(), "root", "x")
	if !errors.Is(err, ErrRevoked) {
		t.Errorf("got %v", err)
	}
}

func TestFoldersAreMadeOnceAndListedWithAllPages(t *testing.T) {
	f := newFake(t)
	c := f.client()
	ctx := context.Background()
	a, err := c.EnsureFolder(ctx, "root", "mpn-backups")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := c.EnsureFolder(ctx, "root", "mpn-backups"); b != a {
		t.Errorf("the same folder must be found again: %q %q", a, b)
	}
	for _, n := range []string{"one", "two", "three"} {
		if _, err := c.EnsureFolder(ctx, a, n); err != nil {
			t.Fatal(err)
		}
	}
	f.pages = true
	l, err := c.List(ctx, a)
	if err != nil || len(l) != 3 {
		t.Fatalf("%v %v", l, err)
	}
	for _, x := range l {
		if !x.IsFolder() {
			t.Errorf("%+v", x)
		}
	}
	if err := c.Delete(ctx, l[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, l[0].ID); err != nil {
		t.Errorf("deleting what is gone is not a failure: %v", err)
	}
}

func TestNamesWithQuotesCannotChangeTheQuery(t *testing.T) {
	if got := qs(`a'b\c`); got != `a\'b\\c` {
		t.Errorf("%q", got)
	}
}

func bigFile(t *testing.T, n int) (string, []byte) {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	p := filepath.Join(t.TempDir(), "files.tar.gz")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p, b
}

func TestUploadSendsTheWholeFileInPieces(t *testing.T) {
	old := chunk
	chunk = 256 << 10
	defer func() { chunk = old }()
	f := newFake(t)
	p, want := bigFile(t, 3*256<<10+1234)
	id, err := f.client().Upload(context.Background(), "root", "files.tar.gz", p)
	if err != nil || id != "uploaded" {
		t.Fatalf("%q %v", id, err)
	}
	if !bytes.Equal(f.upload.Bytes(), want) {
		t.Error("what Google has is not what was sent")
	}
	if f.puts != 4 {
		t.Errorf("four pieces expected, got %d requests", f.puts)
	}
}

func TestUploadGoesOnAfterAFailedPiece(t *testing.T) {
	old := chunk
	chunk = 256 << 10
	defer func() { chunk = old }()
	f := newFake(t)
	f.failPut = 2
	p, want := bigFile(t, 2*256<<10+77)
	id, err := f.client().Upload(context.Background(), "root", "x", p)
	if err != nil || id != "uploaded" {
		t.Fatalf("%q %v", id, err)
	}
	if !bytes.Equal(f.upload.Bytes(), want) {
		t.Error("a piece failed twice and the file must still arrive whole")
	}
}

func TestUploadGivesUpWhenGoogleKeepsFailing(t *testing.T) {
	f := newFake(t)
	f.failPut = 1000
	p, _ := bigFile(t, 1000)
	if _, err := f.client().Upload(context.Background(), "root", "x", p); err == nil {
		t.Error("an upload that never succeeds must fail")
	}
}

func TestUploadOfAnEmptyOrMissingFile(t *testing.T) {
	f := newFake(t)
	if _, err := f.client().Upload(context.Background(), "root", "x", filepath.Join(t.TempDir(), "none")); err == nil {
		t.Error("a missing file must fail")
	}
}
