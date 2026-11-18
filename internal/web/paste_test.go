package web

import (
	"net/url"
	"strings"
	"testing"
)

func TestPasteCopiesAndMoves(t *testing.T) {
	e := startFM(t)
	paste := func(form url.Values) (int, string) {
		resp, _ := e.post(t, "/sites/blog/files/paste", form)
		if resp.StatusCode != 303 {
			return resp.StatusCode, ""
		}
		_, page := e.get(t, resp.Header.Get("Location"))
		return 303, page
	}
	e.post(t, "/sites/blog/files/mkdir", url.Values{"dir": {""}, "name": {"dest"}})
	if !e.exists("dest") {
		t.Fatal("mkdir dest")
	}

	code, page := paste(url.Values{"dir": {"dest"}, "from": {""}, "mode": {"copy"}, "item": {"notes.txt", "css"}})
	if code != 303 || !strings.Contains(page, "Đã sao chép 2 tệp và 1 thư mục") {
		t.Fatalf("copy: %d %.300s", code, page)
	}
	if e.read(t, "dest/notes.txt") != "hello" || e.read(t, "dest/css/site.css") != "a{}\n" || !e.exists("notes.txt") {
		t.Fatal("copy result")
	}

	code, page = paste(url.Values{"dir": {""}, "from": {""}, "mode": {"copy"}, "item": {"notes.txt"}})
	if code != 303 || e.read(t, "notes (copy).txt") != "hello" {
		t.Fatalf("copy in place: %d %.300s", code, page)
	}

	code, page = paste(url.Values{"dir": {"dest"}, "from": {""}, "mode": {"cut"}, "item": {"index.php"}})
	if code != 303 || !strings.Contains(page, "Đã di chuyển 1 tệp và 0 thư mục") || e.exists("index.php") || !e.exists("dest/index.php") {
		t.Fatalf("move: %d %.300s", code, page)
	}

	for name, c := range map[string]struct {
		form url.Values
		want string
	}{
		"existing name": {url.Values{"dir": {"dest"}, "from": {""}, "mode": {"cut"}, "item": {"notes.txt"}}, "đã có"},
		"into itself":   {url.Values{"dir": {"dest/css"}, "from": {""}, "mode": {"cut"}, "item": {"dest"}}, "chính nó"},
		"climbing from": {url.Values{"dir": {"dest"}, "from": {".."}, "mode": {"copy"}, "item": {"x"}}, "Đường dẫn"},
		"bad name":      {url.Values{"dir": {"dest"}, "from": {""}, "mode": {"copy"}, "item": {"../notes.txt"}}, "Tên"},
	} {
		code, page := paste(c.form)
		if code != 303 || !strings.Contains(page, "Không dán được") || !strings.Contains(strings.ToLower(page), strings.ToLower(c.want)) {
			t.Errorf("%s: %d, want %q in page", name, code, c.want)
		}
	}
	if resp, _ := e.post(t, "/sites/blog/files/paste", url.Values{"dir": {"../.."}, "from": {""}, "item": {"notes.txt"}}); resp.StatusCode != 400 {
		t.Errorf("destination outside the home: %d", resp.StatusCode)
	}
	if resp, _ := e.send(t, "POST", "/sites/blog/files/paste", strings.NewReader("dir=dest&from=&item=notes.txt"), "application/x-www-form-urlencoded"); resp.StatusCode != 403 {
		t.Errorf("without the token: %d", resp.StatusCode)
	}
}
