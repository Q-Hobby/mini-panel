package vhost

import (
	"strings"
	"testing"

	"mpn/internal/site"
)

// Somebody opens the vHost tab and saves what mpn wrote, as it is: that must still be accepted, for every kind of site.
func TestTheDefaultConfigurationPassesTheChecksOfAHandWrittenOne(t *testing.T) {
	sites := []site.Site{phpDomainSite(site.WWWOff, false), phpDomainSite(site.WWWBare, true), phpDomainSite(site.WWWWith, false)}
	for _, s := range sites {
		for _, tls := range []bool{false, true} {
			o := Options{}
			if tls {
				o = Options{TLS: true, CertDir: "/etc/mpn/certs/blog"}
			}
			text, err := Render(s, o)
			if err != nil {
				t.Fatalf("tls=%v: %v", tls, err)
			}
			if err := CheckCustomFor(text, s.Name, tls); err != nil {
				t.Errorf("tls=%v: the configuration mpn writes is refused: %v", tls, err)
			}
		}
	}
}

func TestAHandWrittenConfigurationCannotReachOutsideTheSite(t *testing.T) {
	base := "server {\n    listen 80;\n    server_name blog.example.com;\n    root /home/blog/htdocs;\n%s\n}\n"
	for name, line := range map[string]string{
		"a log in /etc":                   "    access_log /etc/cron.d/x;",
		"a log of another site":           "    error_log /var/log/mpn/sites/shop/error.log;",
		"a log with a variable":           "    access_log /var/log/mpn/sites/blog/$host.log;",
		"a log that climbs out":           "    access_log /var/log/mpn/sites/blog/../../../../etc/passwd;",
		"a log in a subfolder":            "    access_log /var/log/mpn/sites/blog/sub/a.log;",
		"a log in quotes":                 `    access_log "/root/.ssh/authorized_keys";`,
		"a log after a semicolon":         "    root /home/blog/htdocs; access_log /etc/x;",
		"a log in a location":             "    location / { access_log /etc/x; }",
		"an include of another site":      "    include /home/shop/htdocs/x.conf;",
		"an include of a site's own file": "    include /home/blog/htdocs/x.conf;",
		"a module":                        "    load_module /tmp/x.so;",
		"a certificate of another site":   "    ssl_certificate /home/shop/key.pem;",
		"a password file":                 "    auth_basic_user_file /home/shop/.htpasswd;",
		"a temp folder":                   "    client_body_temp_path /etc;",
		"a cache folder":                  "    proxy_cache_path /etc/x levels=1;",
		"a certificate that climbs out":   "    ssl_certificate /etc/letsencrypt/live/../../shadow;",
	} {
		if err := CheckCustomFor(strings.Replace(base, "%s", line, 1), "blog", false); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	for name, line := range map[string]string{
		"the site's own log":      "    access_log /var/log/mpn/sites/blog/access.log;",
		"a second log of its own": "    access_log /var/log/mpn/sites/blog/api.log combined;",
		"no log":                  "    access_log off;",
		"a commented line":        "    # access_log /etc/x;",
		"a log to syslog":         "    error_log syslog:server=unix:/dev/log;",
		"fastcgi parameters":      "    location ~ \\.php$ { include fastcgi_params; }",
		"a variable named so":     "    set $include 1;",
	} {
		if err := CheckCustomFor(strings.Replace(base, "%s", line, 1), "blog", false); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
}
