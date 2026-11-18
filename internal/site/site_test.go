package site

import "testing"

func valid() Site {
	return Site{Name: "blog", Kind: PHP, User: "blog", Root: "/home/blog/htdocs",
		Hostnames: []string{"blog.example.com"}, PHPVersion: "8.4"}
}

func TestValidSites(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
	n := valid()
	n.Kind, n.PHPVersion, n.NodePort, n.NodeVersion, n.Entry = Node, "", 3001, "22", "server.js"
	p := valid()
	p.Kind, p.PHPVersion, p.Upstream = Proxy, "", "https://10.0.0.5:8443"
	for _, s := range []Site{n, p} {
		if err := s.Validate(); err != nil {
			t.Errorf("%s: %v", s.Kind, err)
		}
	}
}

func TestRejects(t *testing.T) {
	cases := map[string]func(*Site){
		"bad name":        func(s *Site) { s.Name = "Blog!" },
		"bad user":        func(s *Site) { s.User = "root;" },
		"root traversal":  func(s *Site) { s.Root = "/home/blog/../etc" },
		"root with space": func(s *Site) { s.Root = "/home/my site" },
		"relative root":   func(s *Site) { s.Root = "htdocs" },
		"no hostnames":    func(s *Site) { s.Hostnames = nil },
		"host injection":  func(s *Site) { s.Hostnames = []string{"a.com; } server {"} },
		"upper host":      func(s *Site) { s.Hostnames = []string{"Blog.Example.com"} },
		"php 5.4":         func(s *Site) { s.PHPVersion = "5.4" },
		"php 8.6":         func(s *Site) { s.PHPVersion = "8.6" },
		"unknown kind":    func(s *Site) { s.Kind = "ruby" },
		"root account":    func(s *Site) { s.User = "root" },
		"web server user": func(s *Site) { s.User = "www-data" },
		"systemd account": func(s *Site) { s.User = "systemd-network" },
		"database user":   func(s *Site) { s.User = "mysql" },
		"node low port": func(s *Site) {
			s.Kind, s.PHPVersion, s.NodePort, s.NodeVersion, s.Entry = Node, "", 80, "22", "server.js"
		},
		"node bad version": func(s *Site) {
			s.Kind, s.PHPVersion, s.NodePort, s.NodeVersion, s.Entry = Node, "", 3001, "9", "server.js"
		},
		"entry traversal": func(s *Site) {
			s.Kind, s.PHPVersion, s.NodePort, s.NodeVersion, s.Entry = Node, "", 3001, "22", "../x.js"
		},
		"absolute entry": func(s *Site) {
			s.Kind, s.PHPVersion, s.NodePort, s.NodeVersion, s.Entry = Node, "", 3001, "22", "/etc/x.js"
		},
		"ftp upstream":   func(s *Site) { s.Kind, s.Upstream = Proxy, "ftp://x.com" },
		"space upstream": func(s *Site) { s.Kind, s.Upstream = Proxy, "http://a b" },
		"semicolon":      func(s *Site) { s.Kind, s.Upstream = Proxy, "http://x.com;y" },
	}
	for name, mutate := range cases {
		s := valid()
		mutate(&s)
		if s.Validate() == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestProxyTarget(t *testing.T) {
	s := Site{Kind: Node, NodePort: 3001}
	if got := s.ProxyTarget(); got != "http://127.0.0.1:3001" {
		t.Errorf("got %q", got)
	}
	if got := valid().ProxyTarget(); got != "" {
		t.Errorf("php must not be proxied, got %q", got)
	}
}
