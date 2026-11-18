package panelssl

import (
	"strings"
	"testing"
)

func TestValidDomain(t *testing.T) {
	for _, ok := range []string{"panel.example.com", "a.bc", "p-1.example.co.uk"} {
		if err := ValidDomain(ok); err != nil {
			t.Errorf("%q should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "localhost", "panel", "203.0.113.7", "::1", "[::1]", "Panel.Example.com", "panel.example.com; reboot",
		"a b.com", "-x.com", "panel.example.com/x", "panel.example.com:2610", strings.Repeat("a", 254) + ".com"} {
		if ValidDomain(bad) == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestACMEConfigAnswersTheChallengeAndSendsVisitorsToThePanel(t *testing.T) {
	c, err := ACMEConfig("panel.example.com", 2610)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"listen 80;", "server_name panel.example.com;", "location /.well-known/acme-challenge/ { root /var/lib/mpn/acme; }",
		"return 301 https://$host:2610$request_uri;", "include /etc/mpn/modsec/off.d/*.conf;"} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q in:\n%s", want, c)
		}
	}
	if _, err := ACMEConfig("panel.example.com; }", 2610); err == nil {
		t.Error("a name with nginx syntax must be refused")
	}
	for _, p := range []int{80, 70000} {
		if _, err := ACMEConfig("panel.example.com", p); err == nil {
			t.Errorf("port %d must be refused", p)
		}
	}
	// Port 0 says the name has a server block of its own on 443: visitors of port 80 are sent to the plain https address.
	plain, err := ACMEConfig("panel.example.com", 0)
	if err != nil || !strings.Contains(plain, "return 301 https://$host$request_uri;") || strings.Contains(plain, "$host:") {
		t.Errorf("without a port: %v\n%s", err, plain)
	}
}

func TestParseHostsKeepsOnlyValidNames(t *testing.T) {
	got := ParseHosts("panel.example.com\n\n  Other.Example.com  \n203.0.113.7\nbad name\nlocalhost\n}\n")
	if strings.Join(got, ",") != "panel.example.com,other.example.com" {
		t.Errorf("got %v", got)
	}
	if len(ParseHosts("")) != 0 {
		t.Error("an empty file adds nothing")
	}
	if ParseHosts(HostsText("panel.example.com"))[0] != "panel.example.com" {
		t.Error("what HostsText writes, ParseHosts reads")
	}
}

// Both ports read the same directory: this is what makes one certificate serve the panel (2610) and phpMyAdmin (2611).
func TestOneDirectoryServesBothPorts(t *testing.T) {
	if Dir != "/etc/mpn/certs/_panel" {
		t.Errorf("the nginx files of the panel and of the database browser read /etc/mpn/certs/_panel, not %s", Dir)
	}
}
