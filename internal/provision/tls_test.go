package provision

import (
	"strings"
	"testing"

	"mpn/internal/site"
)

func TestTLSPlanWithoutEnableOnlyWritesCertificates(t *testing.T) {
	p, err := TLSPlan(base(site.Static), []byte("CHAIN"), []byte("KEY"), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(p); got != "certificate files" {
		t.Errorf("steps=%s", got)
	}
	if out := p.String(); !strings.Contains(out, "write /etc/mpn/certs/demo/privkey.pem (3 bytes, 0600)") {
		t.Errorf("private key must be 0600:\n%s", out)
	}
}

func TestTLSPlanEnableSwitchesVhostToHTTPS(t *testing.T) {
	p, err := TLSPlan(base(site.Static), []byte("CHAIN"), []byte("KEY"), true)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(p), "certificate files,nginx vhost (https)"; got != want {
		t.Errorf("steps=%s want %s", got, want)
	}
	var vhostText string
	for _, a := range p[1].Actions {
		if strings.HasSuffix(a.Path, "demo.conf") {
			vhostText = a.Content
		}
	}
	for _, want := range []string{"listen 443 ssl http2;", "ssl_certificate /etc/mpn/certs/demo/fullchain.pem;", "return 301 https://"} {
		if !strings.Contains(vhostText, want) {
			t.Errorf("vhost missing %q:\n%s", want, vhostText)
		}
	}
}

func TestPlanKeepsHTTPSOnceEnabled(t *testing.T) {
	s := base(site.Static)
	s.TLS = true
	p, err := Plan(s, opts)
	if err != nil {
		t.Fatal(err)
	}
	var conf string
	for _, st := range p {
		for _, a := range st.Actions {
			if strings.HasSuffix(a.Path, "demo.conf") {
				conf = a.Content
			}
		}
	}
	if !strings.Contains(conf, "listen 443 ssl http2;") {
		t.Errorf("re-applying an HTTPS site must not drop HTTPS:\n%s", conf)
	}
}
