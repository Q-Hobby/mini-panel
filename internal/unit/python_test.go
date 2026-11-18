package unit

import (
	"strings"
	"testing"

	"mpn/internal/site"
)

func pythonSite() site.Site {
	return site.Site{Name: "py", Kind: site.Python, User: "py", Root: "/home/py/htdocs", Hostnames: []string{"py.example.com"},
		NodePort: 20007, PyVersion: "3.12", Entry: "app.py"}
}

func TestPythonUnitRunsTheAppWithTheSitesOwnVirtualEnvironment(t *testing.T) {
	out, err := Python(pythonSite(), 256)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"User=py\n", "WorkingDirectory=/home/py/htdocs\n", "Environment=PORT=20007\n",
		"ExecStart=/home/py/venv/bin/python app.py\n", "MemoryMax=256M\n", "Restart=always\n", "NoNewPrivileges=yes\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	m := pythonSite()
	m.Entry = "-m myapp.main"
	if out, err = Python(m, 0); err != nil || !strings.Contains(out, "ExecStart=/home/py/venv/bin/python -m myapp.main\n") || strings.Contains(out, "MemoryMax") {
		t.Errorf("module entry: %v\n%s", err, out)
	}
}

func TestPythonUnitRefusesWrongKindAndInjection(t *testing.T) {
	if _, err := Python(nodeSite(), 0); err == nil {
		t.Error("a node site must be rejected")
	}
	for _, e := range []string{"app.py\nExecStart=/bin/sh", "app.py && id", "../x.py", "-m a b", "-m a;b", "-c import os", "/etc/x.py", "-m", ""} {
		s := pythonSite()
		s.Entry = e
		if _, err := Python(s, 0); err == nil {
			t.Errorf("entry %q must be rejected", e)
		}
	}
	s := pythonSite()
	s.PyVersion = "3.12; id"
	if _, err := Python(s, 0); err == nil {
		t.Error("a version with a command in it must be rejected")
	}
}
