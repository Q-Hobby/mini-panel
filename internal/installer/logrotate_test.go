package installer

import (
	"strings"
	"testing"
)

func TestSiteLogRotationIsSetUpBeforeNginxIsChecked(t *testing.T) {
	p, err := Build(opts("", 1024))
	if err != nil {
		t.Fatal(err)
	}
	out := p.String()
	for _, want := range []string{
		"run   install -d -m 0755 /var/log/mpn/sites",
		"write /etc/logrotate.d/mpn-sites",
		"run   logrotate -d /etc/logrotate.d/mpn-sites",
		"install -y ca-certificates curl xz-utils openssl logrotate",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "logrotate -d") > strings.Index(out, "run   nginx -t") {
		t.Error("the logrotate syntax check must run before nginx is reloaded")
	}
}

func TestSiteLogRotationSettings(t *testing.T) {
	for _, want := range []string{
		"/var/log/mpn/sites/*/*.log {", "daily", "rotate 14", "maxsize 200M", "missingok", "notifempty",
		"compress", "delaycompress", "dateext", "sharedscripts", "create 0644 root root",
		`kill -HUP "$(cat /run/nginx.pid)"`,
	} {
		if !strings.Contains(siteLogRotation, want) {
			t.Errorf("missing %q in:\n%s", want, siteLogRotation)
		}
	}
	// Two rotations on one day must not collide on the file name.
	if !strings.Contains(siteLogRotation, "dateformat -%Y%m%d-%s") {
		t.Error("the rotated name needs more than the date, or a second rotation the same day is skipped")
	}
	// USR1 does not work here: the workers would have to reopen the files themselves as www-data.
	if strings.Contains(siteLogRotation, "USR1") {
		t.Error("nginx must be reloaded, not told to reopen its logs")
	}
	if strings.Count(siteLogRotation, "{") != strings.Count(siteLogRotation, "}") {
		t.Error("unbalanced braces")
	}
}
