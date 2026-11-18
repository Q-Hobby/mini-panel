package vhost

import (
	"strings"
	"testing"

	"mpn/internal/site"
)

func phpDomainSite(www string, allowHTTP bool) site.Site {
	return site.Site{Name: "blog", Kind: site.PHP, User: "blog", Root: "/home/blog/htdocs", Hostnames: site.NamesFor("example.com", www),
		PHPVersion: "8.4", WWW: www, AllowHTTP: allowHTTP}
}

func TestOneDomainWithoutHTTPSServesOnPort80Only(t *testing.T) {
	out, err := Render(phpDomainSite(site.WWWOff, false), Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Port 443 only sends the visitor back to http:// (see TestSiteWithoutTLSAnswersHTTPSByRedirect).
	if strings.Count(out, "fastcgi_pass") != 1 || strings.Count(out, "listen 443") != 1 || !strings.Contains(out, "return 302 http://") || !strings.Contains(out, "server_name example.com;") {
		t.Errorf("%s", out)
	}
}

func TestHTTPSSendsPlainHTTPToHTTPSUnlessTheSiteAllowsIt(t *testing.T) {
	out, _ := Render(phpDomainSite(site.WWWOff, false), Options{TLS: true, CertDir: "/etc/mpn/certs/blog"})
	if !strings.Contains(out, "return 301 https://$host$request_uri;") || strings.Count(out, "fastcgi_pass") != 1 || !strings.Contains(out, "listen 443 ssl http2;") {
		t.Errorf("redirecting config:\n%s", out)
	}
	out, _ = Render(phpDomainSite(site.WWWOff, true), Options{TLS: true, CertDir: "/etc/mpn/certs/blog"})
	if strings.Contains(out, "return 301") || strings.Count(out, "fastcgi_pass") != 2 || !strings.Contains(out, "/.well-known/acme-challenge/") {
		t.Errorf("a site that allows HTTP serves on both ports:\n%s", out)
	}
}

func TestTheOtherWWWNameOnlyRedirectsToTheAddress(t *testing.T) {
	for www, want := range map[string]struct{ primary, alias string }{
		site.WWWBare: {"example.com", "www.example.com"},
		site.WWWWith: {"www.example.com", "example.com"},
	} {
		// without HTTPS
		out, err := Render(phpDomainSite(www, false), Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range []string{"server_name " + want.primary + ";", "server_name " + want.alias + ";", "return 301 http://" + want.primary + "$request_uri;"} {
			if !strings.Contains(out, line) {
				t.Errorf("%s: missing %q in\n%s", www, line, out)
			}
		}
		if strings.Count(out, "fastcgi_pass") != 1 {
			t.Errorf("%s: only the address may run the site:\n%s", www, out)
		}
		// with HTTPS the alias redirects on both ports, and its own HTTPS block needs the certificate too
		out, _ = Render(phpDomainSite(www, false), Options{TLS: true, CertDir: "/etc/mpn/certs/blog"})
		if strings.Count(out, "return 301 https://"+want.primary+"$request_uri;") != 2 || strings.Count(out, "ssl_certificate ") != 2 || strings.Count(out, "fastcgi_pass") != 1 {
			t.Errorf("%s with https:\n%s", www, out)
		}
	}
}

func TestDomainChoicesAreChecked(t *testing.T) {
	bad := phpDomainSite(site.WWWBare, false)
	bad.Hostnames = []string{"example.com", "evil.org"}
	if _, err := Render(bad, Options{}); err == nil {
		t.Error("a second name that is not the www twin must be refused")
	}
	bad = phpDomainSite(site.WWWOff, false)
	bad.WWW = "both"
	if _, err := Render(bad, Options{}); err == nil {
		t.Error("an unknown www choice must be refused")
	}
}
