package installer

import (
	"regexp"
	"strings"
	"testing"
)

func pmaOpts() Options {
	o := panelOpts()
	o.Engine, o.PHPVersions, o.PMA = "mariadb", []string{"8.4"}, true
	return o
}

func stepNamed(p Plan, prefix string) (Step, bool) {
	for _, s := range p {
		if strings.HasPrefix(s.Name, prefix) {
			return s, true
		}
	}
	return Step{}, false
}

// stepText is everything a step would run or write, as one string.
func stepText(s Step) string {
	var b strings.Builder
	for _, a := range s.Actions {
		b.WriteString(strings.Join(a.Args, " ") + " " + a.Path + "\n")
	}
	return b.String()
}

// writtenFile returns the content the plan writes to a path, or "".
func writtenFile(p Plan, path string) string {
	for _, s := range p {
		for _, a := range s.Actions {
			if a.Path == path {
				return a.Content
			}
		}
	}
	return ""
}

func mustBuild(t *testing.T, o Options) Plan {
	t.Helper()
	p, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTheBrowserNeedsThePanelAndADatabaseEngine(t *testing.T) {
	o := pmaOpts()
	o.Panel = false
	if _, err := Build(o); err == nil || !strings.Contains(err.Error(), "needs the web panel") {
		t.Errorf("without the panel: %v", err)
	}
	o = pmaOpts()
	o.Engine = ""
	if _, err := Build(o); err == nil || !strings.Contains(err.Error(), "needs a database engine") {
		t.Errorf("without an engine: %v", err)
	}
	o = pmaOpts()
	o.PanelPort = 8779 // the browser would take 8780, where the panel listens
	if _, err := Build(o); err == nil {
		t.Error("a panel port whose next port is the panel's own must be refused")
	}
	o = pmaOpts()
	o.PanelPort = 65535
	if _, err := Build(o); err == nil {
		t.Error("there is no port after 65535")
	}
	o = pmaOpts()
	o.PMA = false
	p, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stepNamed(p, "database browser"); ok || strings.Contains(p.String(), "mpn-pma") || strings.Contains(p.String(), "-pma-port") {
		t.Error("nothing of the browser may be installed unless asked for")
	}
}

func TestTheBrowserStepComesAfterThePanelAndThePhpStep(t *testing.T) {
	p, err := Build(pmaOpts())
	if err != nil {
		t.Fatal(err)
	}
	n := names(p)
	i, j, k := strings.Index(n, "web panel"), strings.Index(n, "php "), strings.Index(n, "database browser")
	if i < 0 || j < 0 || k < 0 || !(i < k && j < k) {
		t.Errorf("steps: %s", n)
	}
	if !strings.HasSuffix(n, "database browser (phpMyAdmin "+pmaVersion+")") && !strings.Contains(n, "database browser") {
		t.Errorf("steps: %s", n)
	}
}

func TestThePinnedReleaseIsCheckedBeforeAnythingIsUnpacked(t *testing.T) {
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(pmaSHA256) {
		t.Fatalf("the hash must be 64 hexadecimal characters: %q", pmaSHA256)
	}
	if !strings.Contains(pmaURL, "https://files.phpmyadmin.net/phpMyAdmin/"+pmaVersion+"/") {
		t.Errorf("the download must come from the project's own site over HTTPS: %s", pmaURL)
	}
	s := pmaInstallScript
	check, unpack := strings.Index(s, "sha256sum -c"), strings.Index(s, "tar -xJf")
	if check < 0 || unpack < 0 || check > unpack {
		t.Errorf("the hash check (%d) must come before the unpacking (%d)", check, unpack)
	}
	if !strings.Contains(s, pmaSHA256) || !strings.Contains(s, "exit 1") {
		t.Error("a file that does not match must stop the plan")
	}
	for _, gone := range []string{`"$t/x/setup"`, `"$t/x/examples"`} {
		if !strings.Contains(s, gone) {
			t.Errorf("%s must be removed: the setup wizard is a way in", gone)
		}
	}
	if !strings.Contains(s, ".mpn-version") {
		t.Error("applying the plan again must not download again")
	}
}

func TestTheBrowserRunsAsItsOwnAccountWithATightPool(t *testing.T) {
	pool := pmaPool()
	for _, want := range []string{"[mpn-pma]", "user = mpn-pma", "group = mpn-pma", "listen = /run/php/mpn-pma.sock", "pm = ondemand",
		"open_basedir] = " + pmaDir, "session.save_path] = " + pmaState + "/sessions", "disable_functions] = exec,passthru,shell_exec,system,proc_open,popen",
		"allow_url_fopen] = off", "session.use_strict_mode] = 1", "session.cookie_httponly] = 1"} {
		if !strings.Contains(pool, want) {
			t.Errorf("pool lacks %q:\n%s", want, pool)
		}
	}
	if strings.Contains(pool, "user = www-data") || strings.Contains(pool, "user = root") {
		t.Error("it must not run as the web server's account or as root")
	}
	if !strings.Contains(pmaUserScript, "was not created by mpn") {
		t.Error("an account of that name that mpn did not make must stop the plan")
	}
	if !strings.Contains(pmaUserScript, "chmod 0640") || !strings.Contains(pmaUserScript, "root:"+pmaAccount) {
		t.Error("the key must be readable by the browser and the panel only")
	}
	if !strings.Contains(pmaUserScript, "usermod -aG "+pmaAccount+" mpn-web") {
		t.Error("the panel's account must share the key's group")
	}
}

func TestTheConfigurationAllowsNoWayInButTheTicket(t *testing.T) {
	c := pmaConfig()
	for _, want := range []string{"'auth_type'] = 'signon'", "'AllowNoPassword'] = false", "'AllowRoot'] = false", "$cfg['AllowArbitraryServer'] = false",
		"'host'] = 'localhost'", "$cfg['ShowChgPassword'] = false", "'LogoutURL'] = 'logout.php'", "'secure' => true"} {
		if !strings.Contains(c, want) {
			t.Errorf("configuration lacks %q", want)
		}
	}
	if strings.Contains(c, "'cookie'") || strings.Contains(c, "'config'") {
		t.Error("only signing on with a ticket may be possible")
	}
}

func TestTheSignOnPageTradesTheTicketAndSaysNothingElse(t *testing.T) {
	files := PMAFiles(2610)
	s := files[pmaDir+"/signon.php"]
	for _, want := range []string{"preg_match('/^[A-Za-z0-9_-]{43}$/'", "CURLOPT_UNIX_SOCKET_PATH => '/run/mpn-web/web.sock'", "http://localhost/_pma/ticket?t=", "X-Mpn-Key", "session_regenerate_id(true)",
		"PMA_single_signon_user", "pma_[0-9a-z]{6,8}_[0-9a-z]{8}", "no-store"} {
		if !strings.Contains(s, want) {
			t.Errorf("sign-on page lacks %q", want)
		}
	}
	if strings.Contains(s, "@BACKEND@") || strings.Contains(s, "@PANELPORT@") {
		t.Error("a placeholder was left")
	}
	if strings.Contains(s, "$_GET['user']") || strings.Contains(s, "$_POST") || strings.Contains(s, "$_REQUEST") {
		t.Error("the user and password may come from the panel only, never from the request")
	}
	if strings.Contains(s, "127.0.0.1") {
		t.Error("the sign-on page must ask the panel on its socket, not on a port any program could take")
	}
	lo := files[pmaDir+"/logout.php"]
	for _, want := range []string{"/_pma/end?u=", "CURLOPT_UNIX_SOCKET_PATH => '/run/mpn-web/web.sock'", "session_destroy()", "':2610'", "header('Location: https://' . $host . $port . '/databases');", "'/pma/'"} {
		if !strings.Contains(lo, want) {
			t.Errorf("sign-out page lacks %q", want)
		}
	}
}

func TestTheBrowsersServerChecksThePanelFirstAndClosesItsFolders(t *testing.T) {
	c := pmaNginx(2610)
	for _, want := range []string{"listen 2611 ssl http2;", "listen [::]:2611 ssl http2;", "root " + pmaDir + ";", "location = /_mpn_auth {", "internal;",
		"proxy_pass " + PanelUpstream + "/_pma/auth;", "error_page 401 = @mpn_login;", "return 302 https://$host:2610/login;",
		"fastcgi_pass unix:/run/php/mpn-pma.sock;", "location ~ \\.php$ { return 404; }"} {
		if !strings.Contains(c, want) {
			t.Errorf("server lacks %q:\n%s", want, c)
		}
	}
	// every place that reaches a file or runs PHP asks the panel first
	for _, loc := range []string{"location / {", "location ~ ^/(index|signon|logout|url|js/messages)\\.php$ {"} {
		i := strings.Index(c, loc)
		if i < 0 {
			t.Errorf("no %q", loc)
			continue
		}
		end := strings.Index(c[i:], "\n    }")
		if !strings.Contains(c[i:i+end], "auth_request /_mpn_auth;") {
			t.Errorf("%q does not ask the panel first", loc)
		}
	}
	for _, closed := range []string{"libraries", "templates", "vendor", "setup", "config", "tmp"} {
		if !regexp.MustCompile(`location ~ \^/\([^)]*\b` + closed + `\b[^)]*\)/ \{ return 404; \}`).MatchString(c) {
			t.Errorf("the folder %s must be closed", closed)
		}
	}
	if strings.Contains(c, "alias ") || strings.Contains(c, "autoindex on") {
		t.Error("no alias and no listing of folders")
	}
	if !strings.Contains(c, "Strict-Transport-Security") || !strings.Contains(c, "X-Content-Type-Options") {
		t.Error("security headers")
	}
}

func TestThePublicPanelServerNeverAnswersTheInternalPath(t *testing.T) {
	c := panelNginx(2610)
	i, j := strings.Index(c, "location ^~ /_pma/ { return 404; }"), strings.Index(c, "location / {")
	if i < 0 || j < 0 || i > j {
		t.Errorf("/_pma/ must be closed on the public server, before the general location:\n%s", c)
	}
}

func TestThePanelStartsWithTheBrowserOnlyWhenItIsInstalled(t *testing.T) {
	p, err := Build(pmaOpts())
	if err != nil {
		t.Fatal(err)
	}
	if u := writtenFile(p, "/etc/systemd/system/mpn-web.service"); !strings.Contains(u, "-pma-port 2611") {
		t.Errorf("the panel's unit must name the browser's port:\n%s", u)
	}
	if u := writtenFile(mustBuild(t, panelOpts()), "/etc/systemd/system/mpn-web.service"); u == "" || strings.Contains(u, "-pma-port") {
		t.Errorf("without the browser the unit has no such flag:\n%s", u)
	}
	// the group and the key must exist before the panel starts and reads the key
	s, _ := stepNamed(p, "web panel")
	prep, start := -1, -1
	for i, a := range s.Actions {
		j := strings.Join(a.Args, " ")
		if strings.Contains(j, "usermod -aG "+pmaAccount) {
			prep = i
		}
		if strings.Contains(j, "restart mpn-web") {
			start = i
		}
	}
	if prep < 0 || start < 0 || prep > start {
		t.Errorf("the key (%d) must be made before the panel is restarted (%d)", prep, start)
	}
	if !strings.Contains(webService(nil, 0), "-trust-proxy") || strings.Contains(webService(nil, 0), "-pma-port") {
		t.Error("without the browser the unit has no such flag")
	}
}

func TestExpiredTemporaryUsersAreSweptEveryFiveMinutes(t *testing.T) {
	if !strings.Contains(pmaCleanTimer, "OnCalendar=*:0/5") || !strings.Contains(pmaCleanService, "db pmaclean -apply") {
		t.Error("the sweep")
	}
	p, _ := Build(pmaOpts())
	if !strings.Contains(p.String(), "mpn-pma-clean.timer") {
		t.Error("the plan must start the sweep")
	}
}

func TestPhp83IsInstalledForTheBrowserOnlyWhenNothingElseDidIt(t *testing.T) {
	o := pmaOpts()
	p, _ := Build(o)
	s, _ := stepNamed(p, "database browser")
	if !strings.Contains(stepText(s), "php8.3-fpm") {
		t.Errorf("php 8.3 must be installed for the browser:\n%s", p)
	}
	o.PHPVersions = []string{"8.3", "8.4"}
	p, _ = Build(o)
	s, _ = stepNamed(p, "database browser")
	if strings.Contains(stepText(s), "apt-get") {
		t.Errorf("php 8.3 is already installed by the php step:\n%s", stepText(s))
	}
}
