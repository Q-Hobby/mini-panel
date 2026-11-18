package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Writes the real "add a website" page, with the real script and a small test harness, to the folder named by\n// MPN_BROWSER_DIR so that a real browser can run the script (see scripts/browser-check.ps1). Skipped otherwise.
func TestWriteFormForBrowserCheck(t *testing.T) {
	dir := os.Getenv("MPN_BROWSER_DIR")
	if dir == "" {
		t.Skip("MPN_BROWSER_DIR not set")
	}
	_ = os.MkdirAll(dir, 0o755)
	h, _, c, _ := setup(t, okAnswer(""))
	page := do(h, "GET", "/sites/new?kind=php", nil, cookie(c)).Body.String()
	page = regexp.MustCompile(`/static/(app\.css|app\.js|theme\.js)\?v=[0-9a-f]+`).ReplaceAllString(page, "$1")
	for _, f := range []string{"app.css", "app.js", "theme.js"} {
		b, err := os.ReadFile(filepath.Join("assets", f))
		if err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(dir, f), b, 0o644)
	}
	harness, err := os.ReadFile(filepath.Join("testdata", "harness.html"))
	if err != nil {
		t.Fatal(err)
	}
	withServerPassword := strings.Replace(page, "</body>", string(harness)+"</body>", 1)
	emptyPassword := regexp.MustCompile(`(name="password"[^>]*?) value="[A-Za-z0-9]*"`).ReplaceAllString(page, `$1 value=""`)
	withoutServerPassword := strings.Replace(emptyPassword, "</body>", string(harness)+"</body>", 1)
	_ = os.WriteFile(filepath.Join(dir, "form-server-password.html"), []byte(withServerPassword), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "form-empty-password.html"), []byte(withoutServerPassword), 0o644)
}
