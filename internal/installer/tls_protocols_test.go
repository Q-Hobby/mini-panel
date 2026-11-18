package installer

import (
	"strings"
	"testing"

	"mpn/internal/tune"
)

// nginx 1.18 (Ubuntu 22.04) does not enable TLS 1.3 unless told to, and relying on the distribution's OpenSSL policy
// to keep TLS 1.0 and 1.1 off is not a setting mpn controls. Both are therefore stated explicitly.
func TestNginxConfStatesTheTLSVersions(t *testing.T) {
	c := nginxConf(tune.New(tune.Input{RAMMB: 1024, CPUs: 1}))
	if !strings.Contains(c, "\n    ssl_protocols TLSv1.2 TLSv1.3;\n") {
		t.Errorf("nginx.conf must set ssl_protocols:\n%s", c)
	}
	if strings.Contains(c, "TLSv1 ") || strings.Contains(c, "TLSv1.1") {
		t.Errorf("TLS 1.0 and 1.1 must stay off:\n%s", c)
	}
	// It belongs in the http block so every server block inherits it.
	if strings.Index(c, "ssl_protocols") < strings.Index(c, "http {") {
		t.Error("ssl_protocols must be inside the http block")
	}
}
