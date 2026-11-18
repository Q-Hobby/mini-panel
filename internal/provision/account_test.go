package provision

import (
	"strings"
	"testing"

	"mpn/internal/site"
)

func accountScript(t *testing.T, s site.Site) string {
	t.Helper()
	p, err := Plan(s, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range actionsOf(p) {
		if len(a.Args) == 3 && a.Args[0] == "sh" && strings.Contains(a.Args[2], "useradd") {
			return a.Args[2]
		}
	}
	t.Fatal("no account script in the plan")
	return ""
}

func TestAccountIsCreatedFreshAndMarked(t *testing.T) {
	s := base(site.Static)
	sc := accountScript(t, s)
	for _, want := range []string{
		"useradd --create-home --home-dir /home/demo --shell /usr/sbin/nologin --comment 'mpn site demo' demo",
		"was not created by mpn for this site",
		"exit 1",
	} {
		if !strings.Contains(sc, want) {
			t.Errorf("missing %q in:\n%s", want, sc)
		}
	}
	// The old script reused any account with that name.
	if strings.Contains(sc, "|| useradd") {
		t.Errorf("an existing account must not be reused unchecked:\n%s", sc)
	}
}

func TestAnExistingAccountMustCarryThisSitesMark(t *testing.T) {
	sc := accountScript(t, base(site.Static))
	// the comparison is against this site's own mark, so a site cannot adopt another site's account either
	if !strings.Contains(sc, `getent passwd demo | cut -d: -f5)" = 'mpn site demo'`) {
		t.Errorf("the existing-account check must compare the mark:\n%s", sc)
	}
	other := base(site.Static)
	other.Name, other.User = "shop", "shop"
	if strings.Contains(accountScript(t, other), "mpn site demo") {
		t.Error("each site has its own mark")
	}
}

func TestAccountIsCheckedBeforeAnyDirectoryIsTouched(t *testing.T) {
	p, err := Plan(base(site.PHP), opts)
	if err == nil {
		out := p.String()
		if strings.Index(out, "useradd") > strings.Index(out, "chmod 711") {
			t.Errorf("the account check must come first:\n%s", out)
		}
	}
	s := base(site.PHP)
	s.PHPVersion = "8.4"
	p, _ = Plan(s, opts)
	out := p.String()
	if strings.Index(out, "useradd") > strings.Index(out, "chmod 711") || strings.Index(out, "useradd") > strings.Index(out, "install -d -o root") {
		t.Errorf("the account check must come before every chmod and install:\n%s", out)
	}
}

// userdel refuses an account that still has a process, and a session that has just ended may not be gone yet: purging a
// site right after its user logged out failed in two runs out of six on a real server.
func TestPurgeWaitsForTheAccountsProcessesBeforeDeletingIt(t *testing.T) {
	p, err := RemovePlan(base(site.Static), true)
	if err != nil {
		t.Fatal(err)
	}
	out := p.String()
	for _, want := range []string{"loginctl terminate-user demo", "pkill -u demo", "pgrep -u demo", "pkill -KILL -u demo", "sleep 0.5"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	kill, wait, del := strings.Index(out, "pkill -u demo"), strings.Index(out, "pgrep -u demo"), strings.Index(out, "userdel -r demo")
	if !(kill < wait && wait < del) {
		t.Errorf("the account must be deleted only after its processes were stopped and checked:\n%s", out)
	}
}

func TestPurgeDeletesOnlyAnAccountWithTheSitesMark(t *testing.T) {
	p, err := RemovePlan(base(site.Static), true)
	if err != nil {
		t.Fatal(err)
	}
	out := p.String()
	for _, want := range []string{
		`getent passwd demo | cut -d: -f5)" = 'mpn site demo'`, "pkill -u demo", "userdel -r demo", "left alone",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "'mpn site demo'") > strings.Index(out, "userdel -r demo") {
		t.Error("the mark must be checked before the delete")
	}
}
