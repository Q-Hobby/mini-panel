package security

import (
	"regexp"
	"strings"
	"testing"
)

func have(paths ...string) func(string) bool {
	m := map[string]bool{}
	for _, p := range paths {
		m[p] = true
	}
	return func(p string) bool { return m[p] }
}

func TestMainConfigModes(t *testing.T) {
	all := have(CRSRules, CRSSetup, "/etc/nginx/modsecurity.conf", "/etc/modsecurity/crs/REQUEST-900-EXCLUSION-RULES-BEFORE-CRS.conf", "/etc/modsecurity/crs/RESPONSE-999-EXCLUSION-RULES-AFTER-CRS.conf")
	detect, err := MainConfig(ModeDetect, all)
	if err != nil || !strings.Contains(detect, "SecRuleEngine DetectionOnly\n") || strings.Contains(detect, "SecRuleEngine On") {
		t.Errorf("detect: %v\n%s", err, detect)
	}
	block, err := MainConfig(ModeBlock, all)
	if err != nil || !strings.Contains(block, "SecRuleEngine On\n") {
		t.Errorf("block: %v\n%s", err, block)
	}
	// The packaged base file must come before the engine line, or its own SecRuleEngine would win.
	if strings.Index(block, "Include /etc/nginx/modsecurity.conf") > strings.Index(block, "SecRuleEngine On") {
		t.Errorf("the engine line must follow the packaged base file:\n%s", block)
	}
	if !strings.Contains(block, "Include "+CRSRules+"/*.conf") || !strings.Contains(block, "SecAuditEngine RelevantOnly") || !strings.Contains(block, "SecAuditLog "+AuditLog+"\n") {
		t.Errorf("rules or audit setting missing:\n%s", block)
	}
	if _, err := MainConfig(ModeOff, all); err == nil {
		t.Error("the off mode has no rule file")
	}
}

func TestMainConfigNamesOnlyFilesThatExist(t *testing.T) {
	c, err := MainConfig(ModeBlock, have(CRSRules, CRSSetup))
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"/etc/nginx/modsecurity.conf", "REQUEST-900", "RESPONSE-999"} {
		if strings.Contains(c, gone) {
			t.Errorf("%q does not exist on this machine and must not be included:\n%s", gone, c)
		}
	}
	if _, err := MainConfig(ModeBlock, have(CRSSetup)); err == nil {
		t.Error("a machine without the rule files must be refused")
	}
}

func TestJailConfig(t *testing.T) {
	c, err := JailConfig([]int{2222, 22}, []int{2610, 2611}, []string{"203.0.113.7", "198.51.100.9/24", "203.0.113.7"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ignoreip = 127.0.0.0/8 ::1 203.0.113.7 198.51.100.0/24\n", "[sshd]\nenabled = true\nbackend = systemd\nport = 22,2222\n",
		"port = 2610,2611\n", "filter = mpn-panel", "filter = mpn-modsec", "banaction = nftables\n", "logpath = /var/log/mpn/sites/*/error.log"} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q in:\n%s", want, c)
		}
	}
	if strings.Count(c, "203.0.113.7") != 1 {
		t.Errorf("a repeated address must be listed once:\n%s", c)
	}
	if _, err := JailConfig(nil, []int{2610}, nil); err == nil {
		t.Error("a jail without ports must be refused")
	}
	if _, err := JailConfig([]int{22}, []int{70000}, nil); err == nil {
		t.Error("a port out of range must be refused")
	}
}

func TestIgnoreListRefusesInjection(t *testing.T) {
	for _, bad := range []string{"1.2.3.4 evil", "1.2.3.4\n[sshd]", "$(id)", "1.2.3.4;", "fe80::1%eth0", "999.1.1.1", "1.2.3.4/33"} {
		if _, err := ValidIgnore([]string{bad}); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	long := make([]string, MaxIgnore+1)
	for i := range long {
		long[i] = "10.0.0." + string(rune('0'+i%10)) + "/32"
	}
	if got, err := ValidIgnore([]string{"10.0.0.1", "", "  "}); err != nil || len(got) != 1 {
		t.Errorf("blank entries are dropped: %v %v", got, err)
	}
}

func TestValidators(t *testing.T) {
	for _, j := range Jails {
		if ValidJail(j) != nil {
			t.Errorf("%s is a jail", j)
		}
	}
	for _, bad := range []string{"", "sshd; reboot", "recidive", "../x", "-all"} {
		if ValidJail(bad) == nil {
			t.Errorf("jail %q must be refused", bad)
		}
	}
	for _, m := range []string{ModeOff, ModeDetect, ModeBlock} {
		if ValidMode(m) != nil {
			t.Errorf("mode %s", m)
		}
	}
	if ValidMode("On") == nil || ValidMode("") == nil {
		t.Error("only the three modes are valid")
	}
	if a, err := ValidAddr("::ffff:1.2.3.4"); err != nil || a != "1.2.3.4" {
		t.Errorf("mapped address: %q %v", a, err)
	}
	if _, err := ValidAddr("1.2.3.0/24"); err == nil {
		t.Error("unban takes one address, not a network")
	}
}

// failregexOf turns the failregex of a filter into a Go expression with <HOST> as a group.
func failregexOf(t *testing.T, filter string) *regexp.Regexp {
	t.Helper()
	_, rest, _ := strings.Cut(filter, "failregex = ")
	line, _, _ := strings.Cut(rest, "\n")
	re, err := regexp.Compile(strings.ReplaceAll(line, "<HOST>", `(?P<host>\S+)`))
	if err != nil {
		t.Fatal(err)
	}
	return re
}

// The first line is the shape nginx 1.24 with libmodsecurity 3.0.12 writes (copied from a test server); the second is the same
// request in detect mode, which must not lead to a ban.
func TestModsecFilterMatchesTheRealLogLine(t *testing.T) {
	re := failregexOf(t, ModsecFilter)
	denied := `2026/11/08 22:05:22 [error] 514430#514430: *13176 [client 198.51.100.9] ModSecurity: Access denied with code 403 (phase 2). Matched "Operator ` + "`Ge'" + ` with parameter ` + "`5'" + ` against variable ` + "`TX:ANOMALY_SCORE'" + ` (Value: ` + "`20'" + ` ) [file "/usr/share/modsecurity-crs/rules/REQUEST-949-BLOCKING-EVALUATION.conf"] [line "80"] [id "949110"] [rev ""] [msg "Inbound Anomaly Score Exceeded (Total Score: 20)"] [data ""] [severity "2"] [ver "OWASP_CRS/3.3.5"] [maturity "0"] [accuracy "0"] [hostname "127.0.0.1"] [uri "/"] [unique_id "17941503226.347045"] [ref ""], client: 198.51.100.9, server: a.com, request: "GET /?id=1 HTTP/1.1", host: "a.com"`
	if m := re.FindStringSubmatch(denied); m == nil || m[re.SubexpIndex("host")] != "198.51.100.9," && m[re.SubexpIndex("host")] != "198.51.100.9" {
		t.Errorf("a refused request must match: %v", m)
	}
	warn := strings.Replace(strings.Replace(denied, "[error]", "[warn]", 1), "Access denied with code 403 (phase 2)", "Warning. Matched", 1)
	if re.MatchString(warn) {
		t.Error("a warning of the detect mode must not match")
	}
}

func TestFiltersMatchTheirLogLines(t *testing.T) {
	if !strings.Contains(PanelFilter, "login failed from <HOST>") {
		t.Error("the panel filter must match the line the panel writes")
	}
	if !strings.Contains(ModsecFilter, "Access denied with code") || !strings.Contains(ModsecFilter, "<HOST>") {
		t.Error("the ModSecurity filter must match a refused request only")
	}
}
