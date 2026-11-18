package unit

import (
	"strings"
	"testing"

	"mpn/internal/site"
)

func TestAnAppWithoutAPortListensOnItsOwnSocket(t *testing.T) {
	s := site.Site{Name: "app", Kind: site.Node, User: "app", Root: "/home/app/htdocs", Hostnames: []string{"app.example.com"}, NodeVersion: "22", Entry: "server.js"}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if !s.UsesSocket() || s.ProxyTarget() != "http://unix:/run/mpn-app/app/app.sock:" {
		t.Fatalf("socket %v, target %q", s.UsesSocket(), s.ProxyTarget())
	}
	u, err := Node(s, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Environment=SOCKET=/run/mpn-app/app/app.sock", "Environment=PORT=/run/mpn-app/app/app.sock",
		"ExecStartPre=+/bin/sh -c 'install -d -m 2750 -o app -g ", "/run/mpn-app/app && rm -f /run/mpn-app/app/app.sock'", "UMask=0007"} {
		if !strings.Contains(u, want) {
			t.Errorf("the unit lacks %q:\n%s", want, u)
		}
	}
	// A site with a port keeps it, as before 1.2.0.
	s.NodePort = 3001
	u, _ = Node(s, 0)
	if !strings.Contains(u, "Environment=PORT=3001") || strings.Contains(u, "SOCKET") || s.ProxyTarget() != "http://127.0.0.1:3001" {
		t.Errorf("%s", u)
	}
	p := site.Site{Name: "py", Kind: site.Python, User: "py", Root: "/home/py/htdocs", Hostnames: []string{"py.example.com"}, PyVersion: "3.11", Entry: "app.py"}
	u, err = Python(p, 0)
	if err != nil || !strings.Contains(u, "Environment=SOCKET=/run/mpn-app/py/app.sock") {
		t.Errorf("%v\n%s", err, u)
	}
}
