package mail

import (
	"strings"
	"testing"
)

func TestWebmailNginxIsOnItsOwnPortAndSharesThePanelCertificate(t *testing.T) {
	c, err := WebmailNginx(2610)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"listen 2612 ssl http2;", "ssl_certificate /etc/mpn/certs/_panel/fullchain.pem;", "proxy_pass http://127.0.0.1:8781;", "proxy_buffering off;", "include /etc/mpn/modsec/off.d/*.conf;"} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q:\n%s", want, c)
		}
	}
	for _, port := range []int{80, 1023, 65534, WebmailBackendPort - 2} {
		if _, err := WebmailNginx(port); err == nil && (port < 1024 || port+2 > 65535 || port+2 == WebmailBackendPort) {
			t.Errorf("panel port %d must be refused", port)
		}
	}
}

// Found on a real server: sending a message from the web mail answered 403. The program checks that the Referer of a form matches the
// host name it sees; the Referer carries the port and nginx's $host does not, so on port 2612 every form was refused.
func TestWebmailNginxKeepsThePortInTheHostHeader(t *testing.T) {
	c, _ := WebmailNginx(2610)
	if !strings.Contains(c, "proxy_set_header Host $http_host;") || strings.Contains(c, "proxy_set_header Host $host;") {
		t.Errorf("the Host header must keep the port:\n%s", c)
	}
}

func TestWebmailListensOnTheLoopbackThroughASocketAndExitsWhenIdle(t *testing.T) {
	if !strings.Contains(WebmailSocket(), "ListenStream=127.0.0.1:8781") {
		t.Errorf("the socket must be on the loopback:\n%s", WebmailSocket())
	}
	svc := WebmailService()
	if strings.Contains(svc, "[Install]") || !strings.Contains(svc, "Requires=mpn-mail.socket") {
		t.Errorf("only the socket starts the program:\n%s", svc)
	}
	for _, want := range []string{"User=mpn-mail", "NoNewPrivileges=yes", "ProtectSystem=strict", "ReadWritePaths=/var/lib/mpn-mail", "MemoryMax=128M", "CapabilityBoundingSet="} {
		if !strings.Contains(svc, want) {
			t.Errorf("service lacks %q", want)
		}
	}
	cfg := WebmailConfig()
	for _, want := range []string{"idle_exit_seconds = 900", "port = 143", "tls = false", "allow_full_email_username = true", "secure_cookies = true", `server = "127.0.0.1"`} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config lacks %q:\n%s", want, cfg)
		}
	}
}

// The secrets are made on the machine and kept when the file is written again, so a change here does not sign everybody out.
func TestSecretsAreMadeOnTheMachineAndKept(t *testing.T) {
	cfg := WebmailConfig()
	if !strings.Contains(cfg, "@JWT@") || !strings.Contains(cfg, "@KEY@") || strings.Contains(cfg, "your-") {
		t.Errorf("the template holds markers, never a secret:\n%s", cfg)
	}
	if !strings.Contains(configScript, "openssl rand -hex 32") || !strings.Contains(configScript, "openssl rand -hex 16") {
		t.Error("secrets must come from the system's random source")
	}
	if !strings.Contains(configScript, `[ ${#jwt} -ge 32 ] ||`) || !strings.Contains(configScript, "chmod 0600") {
		t.Error("an existing secret is reused and the file is private")
	}
	// a 16-byte hex string is 32 characters, which is a valid AES-256 key length for the program
	if !strings.Contains(configScript, `[ ${#key} -eq 32 ]`) {
		t.Error("the encryption key must be 32 characters")
	}
}

func TestTheProgramIsInstalledOnlyWithItsFixedChecksum(t *testing.T) {
	s := binaryScript("")
	if !strings.Contains(s, WebmailSHA256) || !strings.Contains(s, "does not match the expected checksum; not installed") {
		t.Errorf("the download is not checked:\n%s", s)
	}
	if len(WebmailSHA256) != 64 || strings.Trim(WebmailSHA256, "0") == "" {
		t.Error("the pinned checksum must be a real SHA-256")
	}
	if strings.Index(s, "sha256sum \"$t\"") > strings.Index(s, "install -m 0755") {
		t.Error("the check must come before the install")
	}
	local := binaryScript("/tmp/mpn-mail")
	if !strings.Contains(local, "cp -- '/tmp/mpn-mail'") || strings.Contains(local, "curl") {
		t.Errorf("a local file replaces the download and is checked the same way:\n%s", local)
	}
	if !strings.Contains(local, WebmailSHA256) {
		t.Error("a local file is checked against the same checksum")
	}
}

func TestOnlyPlainLocalPathsAreAccepted(t *testing.T) {
	if err := ValidLocalBinary("/tmp/mpn-mail_linux_amd64"); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"", "relative/path", "/tmp/../etc/passwd", "/tmp/a b", "/tmp/a'; rm -rf /; '", "/tmp/a\nb", "/tmp/$(id)"} {
		if ValidLocalBinary(bad) == nil {
			t.Errorf("%q must be refused: it goes into a shell script", bad)
		}
		if _, err := WebmailPlan(2610, bad); err == nil && bad != "" {
			t.Errorf("the plan must refuse %q", bad)
		}
	}
}

func TestWebmailPlanOrderAndOff(t *testing.T) {
	p, err := WebmailPlan(2610, "")
	if err != nil {
		t.Fatal(err)
	}
	a := allArgs(p)
	order := []string{"useradd", "sha256sum", "openssl rand", "daemon-reload", "nginx -t", "enable --now mpn-mail.socket"}
	last := -1
	for _, o := range order {
		i := strings.Index(a, o)
		if i < 0 || i < last {
			t.Fatalf("%q must come, in order, after the previous step:\n%s", o, a)
		}
		last = i
	}
	off := allArgs(installerPlan(WebmailOffPlan()))
	if strings.Contains(off, WebmailDir) || strings.Contains(off, "userdel") {
		t.Errorf("turning the web mail off keeps the account and its settings:\n%s", off)
	}
}
