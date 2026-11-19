package installer

import (
	"os/exec"
	"strings"
	"testing"

	"mpn/internal/osinfo"
)

var (
	trixie   = osinfo.Target{ID: "debian", Version: "13", Codename: "trixie"}
	resolute = osinfo.Target{ID: "ubuntu", Version: "26.04", Codename: "resolute"}
	alma9    = osinfo.Target{ID: "almalinux", Version: "9", Codename: "el9"}
)

func TestNginxUpgradeOnlyWhereNginxComesFromNginxOrg(t *testing.T) {
	cases := []struct {
		t       osinfo.Target
		running string
		want    bool
	}{
		{trixie, "1.28.3", true},
		{alma9, "1.28.3", true},
		{trixie, NginxVersion, false},
		{trixie, "", false},         // nginx is not installed, or its version could not be read
		{resolute, "1.28.3", false}, // Ubuntu 26.04 keeps the nginx of the distribution
	}
	for _, c := range cases {
		if got := NginxUpgradeNeeded(c.t, c.running); got != c.want {
			t.Errorf("%s %s running %q: got %v, want %v", c.t.ID, c.t.Version, c.running, got, c.want)
		}
	}
	if acts, err := NginxUpgradeActions(NginxUpgradeInput{Target: trixie, Running: NginxVersion}); err != nil || acts != nil {
		t.Errorf("a machine on %s must not change: %v %v", NginxVersion, acts, err)
	}
	if _, err := NginxUpgradeActions(NginxUpgradeInput{Target: trixie, Running: "1.28.3; rm -rf /"}); err == nil {
		t.Error("a version that is not three numbers must stop it")
	}
}

// Every module is downloaded and checked before nginx changes, the new pin goes in before the install, and a failed check puts the
// old package and modules back.
func TestNginxUpgradeFetchesFirstAndCanGoBack(t *testing.T) {
	acts, err := NginxUpgradeActions(NginxUpgradeInput{Target: trixie, Running: "1.28.3", Modules: []string{ModPageSpeed, ModModSecurity}})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, a := range acts {
		b.WriteString(strings.Join(a.Args, " ") + "\n")
	}
	out := b.String()
	order := []string{
		"ngx_pagespeed_module-trixie-nginx" + NginxVersion,
		"ngx_http_modsecurity_module-trixie-nginx" + NginxVersion,
		"cp -a /etc/nginx",
		"apt-mark unhold nginx",
		"Pin: version " + NginxVersion + "-1~trixie",
		"apt-get -o DPkg::Lock::Timeout=300 update",
		"nginx=" + NginxVersion + "-1~trixie || restore",
		"/usr/lib/nginx/modules/ngx_pagespeed_module.so",
		"nginx -t || restore",
		"apt-mark hold nginx",
		"systemctl restart nginx || restore",
		"systemctl reload nginx",
	}
	last := -1
	for _, w := range order {
		i := strings.Index(out[last+1:], w)
		if i < 0 {
			t.Fatalf("%q missing or out of order:\n%s", w, out)
		}
		last += 1 + i
	}
	for _, w := range []string{"nginx=1.28.3-1~trixie", "--allow-downgrades", "cp -a $b/old/etc-nginx /etc/nginx"} {
		if !strings.Contains(out, w) {
			t.Errorf("going back lacks %q", w)
		}
	}
	if strings.Contains(out, "--force-confnew") {
		t.Error("the package must keep the panel's nginx.conf")
	}
}

// The scripts must be valid sh: the first try had a here-document inside an indented block, which sh reads as an unfinished
// block, so every machine failed the step (seen on Debian 13, 2026-11-19).
func TestNginxUpgradeScriptsParse(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on this machine")
	}
	for _, tg := range []osinfo.Target{trixie, alma9} {
		acts, err := NginxUpgradeActions(NginxUpgradeInput{Target: tg, Running: "1.28.3", Modules: []string{ModPageSpeed, ModModSecurity}})
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range acts {
			if len(a.Args) != 3 || a.Args[0] != "sh" {
				continue
			}
			cmd := exec.Command(sh, "-n")
			cmd.Stdin = strings.NewReader(a.Args[2])
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s: sh -n: %v %s\n%s", tg.Codename, err, out, a.Args[2])
			}
		}
	}
}

func TestNginxUpgradeOnELUsesVersionlock(t *testing.T) {
	acts, err := NginxUpgradeActions(NginxUpgradeInput{Target: alma9, Running: "1.28.3", Modules: []string{ModPageSpeed}})
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(acts[len(acts)-2].Args, " ")
	for _, w := range []string{"dnf -q versionlock delete nginx", "dnf -y -q install nginx-" + NginxVersion, "nginx-1.28.3", "dnf -q versionlock add nginx"} {
		if !strings.Contains(s, w) {
			t.Errorf("EL upgrade lacks %q:\n%s", w, s)
		}
	}
	if strings.Contains(s, "apt-") {
		t.Error("EL must not use apt")
	}
}
