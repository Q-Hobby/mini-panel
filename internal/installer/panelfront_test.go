package installer

import (
	"strings"
	"testing"
)

func TestThePanelsNameIsServedOnPort443WithoutAPort(t *testing.T) {
	c := PanelDomainNginx("c.demo.com", false, false)
	for _, want := range []string{"listen 443 ssl http2;", "listen [::]:443 ssl http2;", "server_name c.demo.com;",
		"ssl_certificate " + panelCert + "/fullchain.pem;", "proxy_pass " + PanelUpstream + ";", "location ^~ /_pma/ { return 404; }",
		"location ~ ^/sites/[^/]+/files/upload$ {", "proxy_request_buffering off;"} {
		if !strings.Contains(c, want) {
			t.Errorf("lacks %q:\n%s", want, c)
		}
	}
	if strings.Contains(c, "/pma/") && strings.Contains(c, "alias") {
		t.Error("without the database browser there is nothing under /pma/")
	}
	if strings.Contains(c, "server_name _") {
		t.Error("the block is for one name; every other name on 443 still gets the catch-all")
	}
}

func TestPhpMyAdminUnderPmaIsClosedInAndChecksThePanelFirst(t *testing.T) {
	c := PanelDomainNginx("c.demo.com", true, false)
	for _, want := range []string{"location ^~ /pma/ {", "auth_request /_mpn_auth;", "error_page 401 = @mpn_login;", "location @mpn_login { return 302 https://$host/login; }",
		"proxy_pass " + PanelUpstream + "/_pma/auth;", "alias " + pmaDir + "/;", "fastcgi_pass unix:/run/php/mpn-pma.sock;",
		"location ~ ^/pma/((index|signon|logout|url|js/messages)\\.php)$ {", "location ~ \\.php$ { return 404; }",
		"location ~ ^/pma/(libraries|templates|vendor|sql|locale|tmp|config|setup|doc|examples|test|node_modules)/ { return 404; }",
		// it shares an origin with the panel, so the headers keep a script in it away from the panel
		"Cross-Origin-Opener-Policy same-origin", "connect-src https://$http_host/pma/;", "form-action https://$http_host/pma/;", "frame-ancestors 'none'"} {
		if !strings.Contains(c, want) {
			t.Errorf("lacks %q:\n%s", want, c)
		}
	}
	// the panel's own locations come after /pma/ and still catch everything else
	if strings.Index(c, "location / {") < strings.Index(c, "location ^~ /pma/ {") {
		t.Error("the panel's root location must not shadow /pma/")
	}
	// every place under /pma/ that reaches a file inherits the check of the panel
	pma := c[strings.Index(c, "location ^~ /pma/ {"):strings.Index(c, "# An upload is passed on")]
	if strings.Count(pma, "auth_request") != 1 {
		t.Errorf("the check belongs on the /pma/ location, which its nested locations inherit:\n%s", pma)
	}
}

func TestTheSameScriptsServeThePortAndThePath(t *testing.T) {
	cfg := pmaConfig()
	if !strings.Contains(cfg, "$mpnBase = str_starts_with((string) ($_SERVER['SCRIPT_NAME'] ?? ''), '/pma/')") || !strings.Contains(cfg, "'path' => $mpnBase") {
		t.Error("the cookie path of the sign-on session follows where the browser is reached")
	}
	for name, src := range map[string]string{"signon": pmaSignon, "logout": pmaLogout} {
		if !strings.Contains(src, "$base = str_starts_with((string) ($_SERVER['SCRIPT_NAME'] ?? ''), '/pma/')") || !strings.Contains(src, "'path' => $base") {
			t.Errorf("%s: the cookies must follow the path too", name)
		}
		if strings.Contains(src, "'path' => '/'") {
			t.Errorf("%s: a cookie for the whole name would also reach the panel", name)
		}
	}
	if !strings.Contains(pmaSignon, "'path' => $base") || strings.Count(pmaSignon, "setcookie('MpnPmaUser'") != 1 {
		t.Error("the user cookie is set once, on the same path")
	}
}
