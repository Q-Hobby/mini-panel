package vhost

import (
	"strings"
	"testing"
)

func TestTheWebMailLocationIsInTheSslBlockOnlyAndOnlyWhenTheSiteAsks(t *testing.T) {
	s := phpSite()
	opt := Options{TLS: true, CertDir: "/etc/mpn/certs/blog"}

	off, err := Render(s, opt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(off, "/webmail") {
		t.Errorf("a site that did not ask has no web mail:\n%s", off)
	}

	s.Webmail = true
	on, err := Render(s, opt)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, on, "location ^~ /webmail/ {", "location = /webmail { return 301 /webmail/; }", "proxy_pass http://127.0.0.1:8781/;",
		"proxy_set_header X-Forwarded-Prefix /webmail;", "proxy_set_header Host $http_host;", "Cross-Origin-Opener-Policy same-origin",
		"connect-src https://$http_host/webmail/;", "form-action https://$http_host/webmail/;")
	if n := strings.Count(on, "location ^~ /webmail/ {"); n != 1 {
		t.Errorf("the redirect block of port 80 must not carry it: %d times", n)
	}
	// it sits in the block that serves over SSL, after the port 80 block that only redirects
	if strings.Index(on, "location ^~ /webmail/ {") < strings.Index(on, "listen 443 ssl http2;") {
		t.Error("the location belongs to the server block of port 443")
	}

	// a site without SSL gets nothing: a password must not be typed on a plain connection
	plain, err := Render(s, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, "/webmail") {
		t.Errorf("no web mail on a plain connection:\n%s", plain)
	}
}

func TestOnPlainHttpTheWebMailLocationSendsVisitorsToHttps(t *testing.T) {
	s := phpSite()
	s.Webmail, s.AllowHTTP = true, true // plain HTTP is still served, so the same body is in the port 80 block too
	out, err := Render(s, Options{TLS: true, CertDir: "/etc/mpn/certs/blog"})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out, "location ^~ /webmail/ {"); n != 2 {
		t.Fatalf("both blocks serve the site: %d", n)
	}
	if !strings.Contains(WebmailLocations(), "if ($scheme != https) { return 301 https://$host$request_uri; }") {
		t.Error("a request that arrives over plain http must be sent to https before anything is typed")
	}
}

func TestTheWebMailLocationBeatsTheRegularExpressionOfPHP(t *testing.T) {
	// "^~" makes nginx stop at the prefix without looking at \.php$, so /webmail/x.php is the web mail's and never the site's PHP
	if !strings.Contains(WebmailLocations(), "location ^~ /webmail/ {") {
		t.Error("the prefix must be ^~")
	}
}
