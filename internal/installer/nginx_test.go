package installer

import (
	"strings"
	"testing"
)

func TestNginxStepClosesUnknownHosts(t *testing.T) {
	p, err := Build(opts("", 1024))
	if err != nil {
		t.Fatal(err)
	}
	out := p.String()
	for _, want := range []string{"rm -f /etc/nginx/sites-enabled/default", "write /etc/nginx/conf.d/00-default.conf"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// The catch-all must exist before nginx -t runs, or a clash would only show up at reload.
	if strings.Index(out, "00-default.conf") > strings.Index(out, "run   nginx -t") {
		t.Error("default server must be written before nginx -t")
	}
	if strings.Contains(defaultServer, "return 444;") || strings.Count(defaultServer, "include /etc/nginx/mpn-default-page.conf;") != 2 {
		t.Error("both default servers must answer with the page, not close the connection")
	}
	// The page and its configuration are written before the server that includes them, or nginx -t would fail.
	for _, f := range []string{"write " + defaultPageDir + "/index.html", "write /etc/nginx/mpn-default-page.conf"} {
		if i := strings.Index(out, f); i < 0 || i > strings.Index(out, "00-default.conf") {
			t.Errorf("%q must be written before 00-default.conf", f)
		}
	}
	// The catch-all serves only its own folder: no site's files, no PHP, and only reading.
	for _, want := range []string{"root " + defaultPageDir + ";", "try_files /index.html =404;", "limit_except GET { deny all; }", "default-src 'none'"} {
		if !strings.Contains(defaultPageConf, want) {
			t.Errorf("default page configuration lacks %q", want)
		}
	}
	if strings.Contains(defaultPageConf, "fastcgi") || strings.Contains(defaultPageConf, "proxy_pass") {
		t.Error("the default page must be static")
	}
	if !strings.Contains(defaultPage, "<title>") || strings.Contains(defaultPage, "src=\"http") {
		t.Error("the default page must be a complete page that loads nothing")
	}
}
