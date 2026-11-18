package vhost

import (
	"strings"
	"testing"

	"mpn/internal/site"
)

func TestACustomConfigurationReplacesTheOneMpnMakes(t *testing.T) {
	s := phpDomainSite(site.WWWOff, false)
	s.CustomVhost = "server {\r\n    listen 80;\r\n    server_name example.com;\r\n    return 204;\r\n}\r\n\r\n\r\n"
	out, err := Render(s, Options{})
	if err != nil || !strings.Contains(out, "return 204;") || strings.Contains(out, "fastcgi_pass") || !strings.HasSuffix(out, "}\n") {
		t.Errorf("%v\n%q", err, out)
	}
	// with SSL the file must still answer the certificate authority
	s.CustomVhost = "server { listen 443 ssl; server_name example.com; }"
	if _, err := Render(s, Options{TLS: true, CertDir: "/etc/mpn/certs/blog"}); err == nil || !strings.Contains(err.Error(), "acme-challenge") {
		t.Errorf("a site with SSL must keep the challenge location: %v", err)
	}
	s.CustomVhost = "server { listen 80; location /.well-known/acme-challenge/ { root /var/lib/mpn/acme; } }"
	if _, err := Render(s, Options{TLS: true, CertDir: "/etc/mpn/certs/blog"}); err != nil {
		t.Errorf("%v", err)
	}
}

func TestCheckCustomRefusesWhatCannotBeAServerBlock(t *testing.T) {
	for name, c := range map[string]struct {
		text string
		ssl  bool
		want string
	}{
		"empty":            {"  \n", false, "empty"},
		"no server block":  {"listen 80;", false, "no server"},
		"a nul":            {"server {\x00}", false, "characters"},
		"invalid utf-8":    {"server {\xff}", false, "characters"},
		"too long":         {"server {" + strings.Repeat("#", MaxCustom) + "}", false, "longer than"},
		"ssl without acme": {"server { listen 443 ssl; }", true, "acme-challenge"},
	} {
		if err := CheckCustom(c.text, c.ssl); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := CheckCustom("# my site\n  server {\n listen 80; }", false); err != nil {
		t.Errorf("a comment before the block and an indented block are fine: %v", err)
	}
}
