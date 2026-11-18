package unit

import (
	"strings"
	"testing"

	"mpn/internal/site"
)

func nodeSite() site.Site {
	return site.Site{Name: "api", Kind: site.Node, User: "api", Root: "/home/api/app",
		Hostnames: []string{"api.example.com"}, NodePort: 20000, NodeVersion: "22", Entry: "server.js"}
}

func TestNodeUnit(t *testing.T) {
	out, err := Node(nodeSite(), 256)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + out)
	for _, want := range []string{
		"User=api\n", "WorkingDirectory=/home/api/app\n", "Environment=PORT=20000\n",
		"ExecStart=/opt/node/22/bin/node server.js\n", "MemoryMax=256M\n", "Restart=always\n",
		"[Install]\nWantedBy=multi-user.target\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestNoMemoryCapWhenZero(t *testing.T) {
	out, err := Node(nodeSite(), 0)
	if err != nil || strings.Contains(out, "MemoryMax") {
		t.Errorf("err=%v out=%s", err, out)
	}
}

func TestRejectsWrongKindAndBadInput(t *testing.T) {
	php := nodeSite()
	php.Kind = site.PHP
	if _, err := Node(php, 0); err == nil {
		t.Error("php site must be rejected")
	}
	bad := nodeSite()
	bad.Entry = "x.js\nExecStart=/bin/sh"
	if _, err := Node(bad, 0); err == nil {
		t.Error("entry with a newline must be rejected")
	}
	if got := Name(nodeSite()); got != "app-api.service" {
		t.Errorf("Name=%q", got)
	}
}
