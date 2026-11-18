package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEditorInARealBrowser opens the editing page in headless Edge or Chrome, with the panel's real policy, and looks at
// what the browser made of it. It runs only when MPN_BROWSER names the browser program, so an ordinary test run needs none.
func TestEditorInARealBrowser(t *testing.T) {
	browser := os.Getenv("MPN_BROWSER")
	if browser == "" {
		t.Skip("set MPN_BROWSER to the path of Edge or Chrome to run this")
	}
	e := startFM(t)
	_ = os.WriteFile(filepath.Join(e.home, "index.php"), []byte("<?php\n$a = \"x\"; // note\necho $a;\n"), 0o644)
	target, _ := url.Parse(e.url)
	// The browser has no login, so a small proxy adds the session cookie to what it asks for.
	px := httputil.NewSingleHostReverseProxy(target)
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Cookie", e.cookie)
		r.Host = target.Host
		px.ServeHTTP(w, r)
	}))
	defer front.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	prof := t.TempDir()
	cmd := exec.CommandContext(ctx, browser, "--headless=new", "--disable-gpu", "--no-first-run", "--user-data-dir="+prof,
		"--enable-logging=stderr", "--v=0", "--virtual-time-budget=8000", "--dump-dom", front.URL+"/sites/blog/files/edit?path=index.php")
	out, err := cmd.CombinedOutput()
	if err != nil && ctx.Err() != nil {
		t.Fatalf("browser: %v", err)
	}
	dom := string(out)
	for _, bad := range []string{"Content Security Policy", "Refused to", "Uncaught"} {
		if i := strings.Index(dom, bad); i >= 0 {
			t.Errorf("browser complained (%q): %.300s", bad, dom[i:])
		}
	}
	for what, want := range map[string]string{
		"the editor was built":          `class="cm-editor`,
		"the text box was hidden":       `<textarea id="edit-content"`,
		"the language file was applied": `class="cm-line"`,
	} {
		if !strings.Contains(dom, want) {
			t.Errorf("%s: %q not found", what, want)
		}
	}
	if i := strings.Index(dom, `<textarea id="edit-content"`); i >= 0 && !strings.Contains(dom[i:i+strings.Index(dom[i:], ">")], " hidden") {
		t.Error("the text box should be hidden once the editor is shown")
	}
	if strings.Contains(dom, `id="edit-host" hidden`) {
		t.Error("the editor host is still hidden")
	}
	// a language adds <span class="ͼ…"> around tokens; with no language the text would be one plain run
	if !strings.Contains(dom, "ͼ") {
		t.Error("no highlighting classes: the PHP grammar did not load")
	}
}
