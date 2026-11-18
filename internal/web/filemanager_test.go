package web

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fmEnv is the panel over real HTTP with a fake SSH server whose SFTP serves a real temporary directory.
type fmEnv struct {
	termEnv
	home string
}

func startFM(t *testing.T) fmEnv {
	t.Helper()
	e := startTerminalEnv(t, nil)
	home := filepath.FromSlash(e.ssh.Home)
	for name, body := range map[string]string{"index.php": "<?php echo 1;\n", "css/site.css": "a{}\n", "notes.txt": "hello"} {
		p := filepath.Join(home, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return fmEnv{e, home}
}

var noFollow = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func (e fmEnv) send(t *testing.T, method, target string, body io.Reader, ctype string) (*http.Response, string) {
	t.Helper()
	r, _ := http.NewRequest(method, e.url+target, body)
	r.Header.Set("Cookie", e.cookie)
	if method == http.MethodPost {
		r.Header.Set("Origin", e.url)
	}
	if ctype != "" {
		r.Header.Set("Content-Type", ctype)
	}
	resp, err := noFollow.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (e fmEnv) get(t *testing.T, target string) (*http.Response, string) {
	return e.send(t, "GET", target, nil, "")
}

func (e fmEnv) post(t *testing.T, target string, form url.Values) (*http.Response, string) {
	t.Helper()
	if form.Get("csrf") == "" && form["nocsrf"] == nil {
		form.Set("csrf", e.token)
	}
	return e.send(t, "POST", target, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
}

func (e fmEnv) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(e.home, filepath.FromSlash(rel)))
	return err == nil
}

func (e fmEnv) read(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.home, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (e fmEnv) upload(t *testing.T, dir string, csrf string, overwrite bool, files map[string]string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if overwrite {
		_ = mw.WriteField("overwrite", "on")
	}
	for name, content := range files {
		fw, _ := mw.CreateFormFile("files", name)
		_, _ = fw.Write([]byte(content))
	}
	_ = mw.Close()
	resp, _ := e.send(t, "POST", "/sites/blog/files/upload?dir="+url.QueryEscape(dir)+"&csrf="+url.QueryEscape(csrf), &buf, mw.FormDataContentType())
	return resp
}

func TestTheFilesTabListsFoldersFirstAndShowsThePath(t *testing.T) {
	e := startFM(t)
	resp, body := e.get(t, "/sites/blog/files")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if i, j := strings.Index(body, `data-name="css"`), strings.Index(body, `data-name="index.php"`); i < 0 || j < 0 || i > j {
		t.Errorf("folders should come first (%d, %d)", i, j)
	}
	_, sub := e.get(t, "/sites/blog/files?path=css")
	for _, want := range []string{"site.css", `href="/sites/blog/files/edit?path=css%2fsite.css"`, `href="/sites/blog/files"`} {
		if !strings.Contains(sub, want) {
			t.Errorf("missing %q in:\n%s", want, sub)
		}
	}
}

func TestAPathThatLeavesTheHomeDirectoryIsRefused(t *testing.T) {
	e := startFM(t)
	for _, p := range []string{"../etc", "css/../../x", "a%5Cb", "%2e%2e"} {
		if resp, _ := e.get(t, "/sites/blog/files?path="+p); resp.StatusCode != 400 {
			t.Errorf("path %q: status %d", p, resp.StatusCode)
		}
	}
	if resp, _ := e.get(t, "/sites/blog/files/download?path=../x"); resp.StatusCode != 400 {
		t.Errorf("download: status %d", resp.StatusCode)
	}
}

func TestNothingChangesWithoutTheFormToken(t *testing.T) {
	e := startFM(t)
	resp, _ := e.send(t, "POST", "/sites/blog/files/mkdir", strings.NewReader("dir=&name=evil"), "application/x-www-form-urlencoded")
	if resp.StatusCode != 403 || e.exists("evil") {
		t.Errorf("mkdir without token: %d", resp.StatusCode)
	}
	if resp := e.upload(t, "", "wrong", false, map[string]string{"x.txt": "x"}); resp.StatusCode != 403 || e.exists("x.txt") {
		t.Errorf("upload with a wrong token: %d", resp.StatusCode)
	}
}

func TestMakingAFolderAndAFileShowsAMessageOnce(t *testing.T) {
	e := startFM(t)
	resp, _ := e.post(t, "/sites/blog/files/mkdir", url.Values{"dir": {"css"}, "name": {"img"}})
	if resp.StatusCode != 303 || !e.exists("css/img") {
		t.Fatalf("mkdir: %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if _, page := e.get(t, loc); !strings.Contains(page, "Đã tạo thư mục.") {
		t.Errorf("no message after redirect:\n%s", page)
	}
	if _, page := e.get(t, loc); strings.Contains(page, "Đã tạo thư mục.") {
		t.Error("the message was shown twice")
	}
	e.post(t, "/sites/blog/files/newfile", url.Values{"dir": {""}, "name": {"new.txt"}})
	if !e.exists("new.txt") {
		t.Error("file not created")
	}
	_, page := e.post(t, "/sites/blog/files/mkdir", url.Values{"dir": {""}, "name": {"../x"}})
	_ = page
	if e.exists("../x") {
		t.Error("a name with a slash was accepted")
	}
}

func TestUploadKeepsAnExistingFileUnlessToldToReplaceIt(t *testing.T) {
	e := startFM(t)
	if resp := e.upload(t, "css", e.token, false, map[string]string{"site.css": "NEW", "extra.css": "b{}"}); resp.StatusCode != 303 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if e.read(t, "css/site.css") != "a{}\n" || e.read(t, "css/extra.css") != "b{}" {
		t.Error("an existing file was replaced without being asked, or the new one is missing")
	}
	e.upload(t, "css", e.token, true, map[string]string{"site.css": "NEW"})
	if e.read(t, "css/site.css") != "NEW" {
		t.Error("overwrite did not replace the file")
	}
	e.upload(t, "", e.token, false, map[string]string{"../../evil.txt": "x"})
	if !e.exists("evil.txt") || e.exists("../evil.txt") {
		t.Error("an upload name with directories must be reduced to its last part inside the folder")
	}
}

func TestADownloadIsNeverShownInsideThePanel(t *testing.T) {
	e := startFM(t)
	_ = os.WriteFile(filepath.Join(e.home, "x.html"), []byte("<script>alert(1)</script>"), 0o644)
	resp, body := e.get(t, "/sites/blog/files/download?path=x.html")
	if resp.Header.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") || body != "<script>alert(1)</script>" {
		t.Errorf("headers %v", resp.Header)
	}
}

func TestTheEditPageEscapesTheFileAndHasItsOwnPolicy(t *testing.T) {
	e := startFM(t)
	_ = os.WriteFile(filepath.Join(e.home, "evil.txt"), []byte("</textarea><script>alert(1)</script>"), 0o644)
	resp, body := e.get(t, "/sites/blog/files/edit?path=evil.txt")
	if strings.Contains(body, "<script>alert(1)") || !strings.Contains(body, "&lt;/textarea&gt;") {
		t.Errorf("the file was not escaped:\n%s", body)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "'nonce-") || strings.Contains(csp, "unsafe") {
		t.Errorf("policy %q", csp)
	}
	_ = os.WriteFile(filepath.Join(e.home, "bin.dat"), []byte{1, 0, 2}, 0o644)
	if resp, _ := e.get(t, "/sites/blog/files/edit?path=bin.dat"); resp.StatusCode != 303 {
		t.Errorf("a binary file should go back to the list, got %d", resp.StatusCode)
	}
}

func mtimeOf(t *testing.T, p string) string {
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return strconv.FormatInt(fi.ModTime().Unix(), 10)
}

func TestSavingKeepsLineEndingsAndRefusesAFileThatChanged(t *testing.T) {
	e := startFM(t)
	p := filepath.Join(e.home, "notes.txt")
	past := time.Now().Add(-time.Hour)
	_ = os.Chtimes(p, past, past)
	mt := mtimeOf(t, p)
	resp, _ := e.post(t, "/sites/blog/files/save", url.Values{"path": {"notes.txt"}, "mtime": {mt}, "eol": {"crlf"}, "content": {"a\r\nb\r\n"}})
	if resp.StatusCode != 303 || e.read(t, "notes.txt") != "a\r\nb\r\n" {
		t.Fatalf("save: %d %q", resp.StatusCode, e.read(t, "notes.txt"))
	}
	e.post(t, "/sites/blog/files/save", url.Values{"path": {"css/site.css"}, "mtime": {mtimeOf(t, filepath.Join(e.home, "css", "site.css"))}, "eol": {"lf"}, "content": {"x\r\ny"}})
	if e.read(t, "css/site.css") != "x\ny" {
		t.Errorf("lf file: %q", e.read(t, "css/site.css"))
	}
	// somebody else changed notes.txt after it was opened
	_ = os.WriteFile(p, []byte("theirs"), 0o644)
	_ = os.Chtimes(p, time.Now().Add(time.Hour), time.Now().Add(time.Hour))
	resp, body := e.post(t, "/sites/blog/files/save", url.Values{"path": {"notes.txt"}, "mtime": {mt}, "eol": {"lf"}, "content": {"mine"}})
	if resp.StatusCode != 409 || e.read(t, "notes.txt") != "theirs" || !strings.Contains(body, "mine") {
		t.Errorf("conflict: status %d, file %q", resp.StatusCode, e.read(t, "notes.txt"))
	}
}

// The dialog of the list sends the new name in the field "name", the same as the dialogs for a new file and a new folder.
// A test that posted "newname" (the field of the page for browsers without script) missed that the dialog sent nothing
// the server read.
func TestRenamingWorksWithTheFieldTheDialogSends(t *testing.T) {
	e := startFM(t)
	resp, _ := e.post(t, "/sites/blog/files/rename", url.Values{"dir": {""}, "path": {"notes.txt"}, "name": {"memo.txt"}})
	if resp.StatusCode != 303 || e.exists("notes.txt") || !e.exists("memo.txt") {
		t.Fatalf("rename with the dialog's field: %d, notes %v, memo %v", resp.StatusCode, e.exists("notes.txt"), e.exists("memo.txt"))
	}
	_, page := e.get(t, resp.Header.Get("Location"))
	if !strings.Contains(page, "Đã đổi tên.") || strings.Contains(page, "Tên không hợp lệ") {
		t.Errorf("message after a good rename:\n%s", page)
	}
	e.post(t, "/sites/blog/files/rename", url.Values{"dir": {""}, "path": {"memo.txt"}, "newname": {"old.txt"}})
	if !e.exists("old.txt") {
		t.Error("the page for browsers without script no longer works")
	}
	// a failure must not say that it worked
	resp, _ = e.post(t, "/sites/blog/files/rename", url.Values{"dir": {""}, "path": {"old.txt"}, "name": {"a/b"}})
	_, page = e.get(t, resp.Header.Get("Location"))
	if !strings.Contains(page, "Tên không hợp lệ") || strings.Contains(page, "Đã đổi tên.") || !strings.Contains(page, "Không thực hiện được") {
		t.Errorf("message after a bad rename:\n%s", page)
	}
	resp, _ = e.post(t, "/sites/blog/files/rename", url.Values{"dir": {""}, "path": {"old.txt"}, "name": {"index.php"}})
	_, page = e.get(t, resp.Header.Get("Location"))
	if !strings.Contains(page, "Đã có mục trùng tên") || !e.exists("old.txt") {
		t.Errorf("renaming onto an existing name:\n%s", page)
	}
}

func TestDeletingAsksFirstAndTakesFoldersWithIt(t *testing.T) {
	e := startFM(t)
	resp, body := e.post(t, "/sites/blog/files/confirm-delete", url.Values{"dir": {""}, "item": {"css", "notes.txt"}})
	if resp.StatusCode != 200 || !strings.Contains(body, "css") || !e.exists("css/site.css") {
		t.Fatalf("confirm: %d", resp.StatusCode)
	}
	if strings.Contains(body, `name="confirm"`) {
		t.Error("nothing needs to be typed to delete files")
	}
	e.post(t, "/sites/blog/files/delete", url.Values{"dir": {""}, "items": {"css\nnotes.txt"}})
	if e.exists("css") || e.exists("notes.txt") || !e.exists("index.php") {
		t.Error("wrong things were deleted")
	}
	if resp, _ := e.post(t, "/sites/blog/files/confirm-delete", url.Values{"dir": {""}, "item": {"../x"}}); resp.StatusCode != 400 {
		t.Errorf("a name with .. : %d", resp.StatusCode)
	}
}

func TestZipAndUnzipFromThePage(t *testing.T) {
	e := startFM(t)
	resp, _ := e.post(t, "/sites/blog/files/zip", url.Values{"dir": {""}, "zipname": {"all.zip"}, "item": {"css", "index.php"}})
	if resp.StatusCode != 303 || !e.exists("all.zip") {
		t.Fatalf("zip: %d", resp.StatusCode)
	}
	_ = os.RemoveAll(filepath.Join(e.home, "css"))
	e.post(t, "/sites/blog/files/unzip", url.Values{"dir": {""}, "zip": {"all.zip"}})
	if e.read(t, "css/site.css") != "a{}\n" {
		t.Error("css/site.css was not restored")
	}
	e.post(t, "/sites/blog/files/unzip", url.Values{"dir": {""}, "zip": {"../all.zip"}})
}

func TestChangingPermissionsRefusesWhatItShould(t *testing.T) {
	e := startFM(t)
	chmod := func(mode string, items ...string) (int, string) {
		resp, _ := e.post(t, "/sites/blog/files/chmod", url.Values{"dir": {""}, "mode": {mode}, "item": items})
		if resp.StatusCode != 303 {
			return resp.StatusCode, ""
		}
		_, page := e.get(t, resp.Header.Get("Location"))
		return 303, page
	}
	for name, c := range map[string]struct {
		mode  string
		items []string
		want  string
	}{
		"setuid digit":   {"4755", []string{"notes.txt"}, "Quyền không hợp lệ"},
		"letters":        {"rwx", []string{"notes.txt"}, "Quyền không hợp lệ"},
		"too short":      {"64", []string{"notes.txt"}, "Quyền không hợp lệ"},
		"digit 8":        {"688", []string{"notes.txt"}, "Quyền không hợp lệ"},
		"parent":         {"644", []string{"../x"}, "Tên không hợp lệ"},
		"slash in name":  {"644", []string{"css/site.css"}, "Tên không hợp lệ"},
		"nothing chosen": {"644", nil, "Không tìm thấy"},
		"missing file":   {"644", []string{"nope.txt"}, "Không tìm thấy"},
	} {
		if code, page := chmod(c.mode, c.items...); code != 303 || !strings.Contains(page, c.want) || !strings.Contains(page, "Không đổi được quyền") {
			t.Errorf("%s: %d, wanted %q in the message", name, code, c.want)
		}
	}
	if resp, _ := e.send(t, "POST", "/sites/blog/files/chmod", strings.NewReader("dir=&mode=600&item=notes.txt"), "application/x-www-form-urlencoded"); resp.StatusCode != 403 {
		t.Errorf("without the token: %d", resp.StatusCode)
	}
	if code, page := chmod("640", "notes.txt", "css"); code != 303 || !strings.Contains(page, "Đã đổi quyền cho 1 tệp và 1 thư mục") {
		t.Errorf("a good change: %d\n%s", code, page)
	}
	if code, page := chmod("755", ""); code != 303 || !strings.Contains(page, "Không đổi được quyền") {
		t.Errorf("an empty name must be refused: %d", code)
	}
}

func TestTheListShowsTheModeAndTheMenuCanDecideWhatToOffer(t *testing.T) {
	e := startFM(t)
	_, body := e.get(t, "/sites/blog/files")
	for _, want := range []string{`data-mode="`, `data-edit="1"`, `data-zip="0"`, `id="fm-menu"`, `data-m="chmod"`, `data-m="unzip"`, `id="dlg-chmod"`, `data-bit="400"`, `pattern="[0-7]{3}"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestTheEditorFilesAreServedWithTheRightCaching(t *testing.T) {
	e := startFM(t)
	resp, body := e.get(t, "/static/editor/editor.js")
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "javascript") || resp.Header.Get("Cache-Control") != "no-cache" || len(body) < 1000 {
		t.Fatalf("editor.js: %d %v", resp.StatusCode, resp.Header)
	}
	ents, _ := assetFS.ReadDir("assets/editor")
	for _, en := range ents {
		if strings.HasPrefix(en.Name(), "chunk-") {
			r, _ := e.get(t, "/static/editor/"+en.Name())
			if !strings.Contains(r.Header.Get("Cache-Control"), "immutable") {
				t.Errorf("%s: %v", en.Name(), r.Header)
			}
			break
		}
	}
	for _, bad := range []string{"..%2Fapp.js", "x.txt", "nope.js"} {
		if r, _ := e.get(t, "/static/editor/"+bad); r.StatusCode != 404 {
			t.Errorf("%s: %d", bad, r.StatusCode)
		}
	}
}

func TestWithoutALoginTheFilesTabSaysSo(t *testing.T) {
	h, _, c, _ := setupWith(t, sitePageAnswers(), func(s *Server) { s.PasswdFile = passwdWith(t, "/bin/bash") })
	w := do(h, "GET", "/sites/blog/files", nil, cookie(c))
	if w.Code != 502 || !strings.Contains(w.Body.String(), "Không đăng nhập được") {
		t.Errorf("%d: %s", w.Code, w.Body.String())
	}
	if w := do(h, "GET", "/sites/blog/files", nil); w.Code != 303 {
		t.Errorf("no cookie: %d", w.Code)
	}
}
