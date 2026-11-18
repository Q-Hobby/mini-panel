package installer

import (
	"strings"
	"testing"
)

func TestMigrateWebUnitKeepsTheSettingsAndMovesToTheSocket(t *testing.T) {
	old := strings.Replace(webService([]string{"panel.example.com"}, 2611), "-listen systemd", "-listen 127.0.0.1:8780", 1)
	old = strings.Replace(old, "Requires=mpn-web.socket\nAfter=mpn-web.socket\n", "", 1)
	if strings.Contains(old, "mpn-web.socket") {
		t.Fatal("the old unit for the test still names the socket")
	}
	got, ok := MigrateWebUnit(old)
	if !ok {
		t.Fatal("the old unit was not recognised")
	}
	if got != webService([]string{"panel.example.com"}, 2611) {
		t.Errorf("migrated unit differs from a new one:\n%s\n---\n%s", got, webService([]string{"panel.example.com"}, 2611))
	}
	if again, ok := MigrateWebUnit(got); !ok || again != got {
		t.Error("running it again must change nothing")
	}
	if _, ok := MigrateWebUnit("[Service]\nExecStart=/bin/true\n"); ok {
		t.Error("a unit mpn did not write must be left alone")
	}
}

// The panel has an SELinux domain of its own, and the web server may connect to that domain alone: the broad rule of the first try
// (httpd_t to every unconfined service) must not come back.
func TestThePanelHasItsOwnSELinuxDomain(t *testing.T) {
	for _, want := range []string{"(type mpn_web_t)", "(roletype system_r mpn_web_t)", "(allow httpd_t mpn_web_t (unix_stream_socket (connectto)))",
		"(allow init_t mpn_web_t (process (transition", "(allow mpn_web_t bin_t (file (entrypoint)))"} {
		if !strings.Contains(panelSELinuxPolicy, want) {
			t.Errorf("policy lacks %q", want)
		}
	}
	if strings.Contains(panelSELinuxPolicy, "unconfined_service_t") {
		t.Error("the policy must not open unconfined_service_t to the web server")
	}
	s := selinuxScript([]int{2610})
	for _, want := range []string{"semodule -i /etc/mpn/selinux/mpn_panel.cil", "SELinuxContext=system_u:system_r:mpn_web_t:s0", "SELinuxContextFromNet=no"} {
		if !strings.Contains(s, want) {
			t.Errorf("the SELinux step lacks %q", want)
		}
	}
	// Only on EL with SELinux: the units themselves never name the domain, which would stop the panel where there is no SELinux.
	if strings.Contains(webService(nil, 0), "SELinux") || strings.Contains(panelSocketUnit, "SELinux") {
		t.Error("the units must not name the domain; the drop-ins of the SELinux step do")
	}
}

func TestMigrateStartsTheSocketBeforeNginxIsPointedAtIt(t *testing.T) {
	old := strings.Replace(webService(nil, 0), "-listen systemd", "-listen 127.0.0.1:8780", 1)
	old = strings.Replace(old, "Requires=mpn-web.socket\nAfter=mpn-web.socket\n", "", 1)
	acts, err := MigrateActions(MigrateInput{WebUnit: old, PanelPort: 2610, PMA: true, EL: true})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, a := range acts {
		b.WriteString(strings.Join(a.Args, " ") + " " + a.Path + "\n")
		if a.Path == PanelConfFile && !strings.Contains(a.Content, "proxy_pass "+PanelUpstream+";") {
			t.Error("the panel's nginx file still points at the port")
		}
		if a.Path == PMAConfFile && strings.Contains(a.Content, "127.0.0.1:8780") {
			t.Error("the browser's nginx file still asks the port")
		}
	}
	out := b.String()
	// The domain is loaded before the units that name it are started.
	order := []string{"groupadd --system mpn-front", "usermod -aG mpn-front mpn-pma", "semodule -i", " " + SocketFile + "\n", " " + WebUnitFile + "\n", "rmdir /run/mpn-web",
		"systemctl start mpn-web", PanelConfFile, PMAConfFile, "nginx -t", "systemctl restart nginx"}
	last := -1
	for _, w := range order {
		i := strings.Index(out, w)
		if i < 0 || i < last {
			t.Fatalf("%q missing or out of order:\n%s", w, out)
		}
		last = i
	}
	if _, err := MigrateActions(MigrateInput{WebUnit: old, PanelPort: 0}); err == nil {
		t.Error("an unknown panel port must stop it")
	}
}
