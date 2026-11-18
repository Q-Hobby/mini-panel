package installer

import (
	"errors"
	"fmt"
	"strings"

	"mpn/internal/distro"
	"mpn/internal/security"
	"mpn/internal/site"
)

// PanelBackendPort is where the web panel listened before 1.2.0. Port 8780 of the loopback address could be taken by the program of any
// website while the panel was restarting, and any account of the machine could send it a forged client address. It is kept only so that
// a site may not take it while a machine is being moved to the socket, and so that the move can find the old configuration.
const PanelBackendPort = 8780

// The web panel listens on a Unix socket that systemd makes and keeps (mpn-web.socket), so it exists while the panel restarts and no
// program can take its place. Only root and the members of FrontGroup may connect: nginx and the pool of the database browser.
const (
	PanelSocketDir = "/run/mpn-web"
	PanelSocket    = PanelSocketDir + "/web.sock"
	// FrontGroup is the group of the accounts that may talk to the panel: the web server's account and mpn-pma.
	FrontGroup = "mpn-front"
)

const (
	webMark   = "mpn web panel"
	panelCert = "/etc/mpn/certs/_panel"
)

// panelSocketUnit is the socket of the panel. The socket is open only to root and the group. Its folder belongs to root:root (systemd
// makes it so, whatever SocketGroup says) and may only be written by root, so no other program can put a socket in its place; others
// may pass through it (0755), which they need to reach a socket they are allowed to open, and which opens nothing by itself.
const panelSocketUnit = `[Unit]
Description=mpn web panel socket

[Socket]
ListenStream=` + PanelSocket + `
SocketUser=root
SocketGroup=` + FrontGroup + `
SocketMode=0660
DirectoryMode=0755
RemoveOnStop=yes

[Install]
WantedBy=sockets.target
`

// frontGroupScript makes the group of the accounts that may connect to the panel's socket and puts the web server's account in it.
// The account of the database browser joins it when the browser is installed (pmaUserScript). nginx must be restarted, not
// reloaded, to see a new group: the master forks workers with the groups it started with.
func frontGroupScript(webUser string) string {
	return `getent group ` + FrontGroup + ` >/dev/null || groupadd --system ` + FrontGroup + `
id -nG ` + webUser + ` | tr ' ' '\n' | grep -qx ` + FrontGroup + ` || { usermod -aG ` + FrontGroup + ` ` + webUser + ` && touch /run/mpn-nginx-restart; }`
}

// nginxRestartIfNeeded restarts nginx once when frontGroupScript gave its account a new group.
const nginxRestartIfNeeded = `[ ! -e /run/mpn-nginx-restart ] || { systemctl restart nginx && rm -f /run/mpn-nginx-restart; }`

// PanelCertScript makes the self-signed certificate of the panel when none is there; mpn panel ssl-off uses it to go back.
const PanelCertScript = panelCertScript

const rootService = `[Unit]
Description=mpn root helper for the web panel
After=network.target

[Service]
ExecStart=` + mpnBinary + ` root
Restart=on-failure
RestartSec=2
RuntimeDirectory=mpn
RuntimeDirectoryMode=0755
RuntimeDirectoryPreserve=restart

[Install]
WantedBy=multi-user.target
`

// webService runs the panel as its own account with almost no rights: it cannot gain any, sees no home directories,
// writes nowhere, and may use only Unix sockets and IP. Everything it asks for goes through the root helper's socket.
func webService(hosts []string, pmaPort int) string {
	var h strings.Builder
	for _, x := range hosts {
		h.WriteString(" -host " + x)
	}
	if pmaPort > 0 {
		h.WriteString(fmt.Sprintf(" -pma-port %d", pmaPort))
	}
	return fmt.Sprintf(`[Unit]
Description=mpn web panel
After=mpn-root.service
Wants=mpn-root.service
Requires=mpn-web.socket
After=mpn-web.socket

[Service]
User=mpn-web
Group=mpn-web
ExecStart=%s web -listen systemd -secure -trust-proxy%s
Restart=on-failure
RestartSec=2
NoNewPrivileges=yes
CapabilityBoundingSet=
PrivateTmp=yes
PrivateDevices=yes
ProtectSystem=strict
ReadWritePaths=/var/lib/mpn-web
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
RestrictNamespaces=yes
LockPersonality=yes
SystemCallArchitectures=native
UMask=0077
MemoryMax=96M

[Install]
WantedBy=multi-user.target
`, mpnBinary, h.String())
}

// panelNginx makes nginx answer the public port over TLS and hand requests to the panel. The Host header keeps its port
// (the panel compares it with the Origin of a form), and X-Real-IP is set here, replacing whatever a client sent.
func panelNginx(port int) string {
	return fmt.Sprintf(`server {
    listen %[1]d ssl http2;
    listen [::]:%[1]d ssl http2;
    server_name _;
    ssl_certificate %[2]s/fullchain.pem;
    ssl_certificate_key %[2]s/privkey.pem;
    client_max_body_size 1m;
    gzip on;
    gzip_types text/css text/javascript application/javascript;
    # The panel is not a website: the rules of the web application firewall (once installed) would block saving code in the editor.
    %[4]s
    %[3]s
}
`, port, panelCert, panelLocations(), security.OffInclude)
}

// panelLocations are the locations every server block that serves the panel has, on its port and on its name.
func panelLocations() string {
	return fmt.Sprintf(`# The setup link carries a one-time key in its query; it is not written to the log.
    location = /setup {
        access_log off;
        %[2]s
    }
    # What the database browser and nginx ask of the panel on this machine is not for anybody on the Internet.
    location ^~ /_pma/ { return 404; }
    # An upload is passed on as it arrives instead of being stored by nginx first, and may be large; the panel stops it at its own limit.
    location ~ ^/sites/[^/]+/files/upload$ {
        client_max_body_size %[1]dm;
        proxy_request_buffering off;
        client_body_timeout 2h;
        proxy_read_timeout 2h;
        proxy_send_timeout 2h;
        %[2]s
    }
    # The text of the code editor can be a few megabytes.
    location ~ ^/sites/[^/]+/files/save$ {
        client_max_body_size 8m;
        %[2]s
    }
    location / {
        %[2]s
        proxy_read_timeout 1500s;
        proxy_send_timeout 60s;
    }`, UploadLimitMB, panelProxy())
}

// panelAuthLocation is nginx's question to the panel whether a visitor is signed in, which the database browser and the web mail ask
// before every request. The answer is 204 or 401 from the cookie the panel sees.
func panelAuthLocation() string {
	return `location = /_mpn_auth {
        internal;
        proxy_pass ` + PanelUpstream + `/_pma/auth;
        proxy_pass_request_body off;
        proxy_set_header Content-Length "";
        proxy_set_header Host localhost;
        proxy_set_header Cookie $http_cookie;
    }`
}

// UploadLimitMB is the largest upload nginx lets through to the panel (1 GiB, the panel's own default limit).
const UploadLimitMB = 1100

// PanelUpstream is how nginx reaches the panel.
const PanelUpstream = "http://unix:" + PanelSocket + ":"

// panelProxy is what every location that hands a request to the panel has in common.
func panelProxy() string {
	return `proxy_pass ` + PanelUpstream + `;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header Host $http_host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-Proto https;`
}

// sshdExemptScript keeps sshd from penalising this machine's own address. OpenSSH 9.8 and later slows down, then drops,
// connections from an address after failed logins, and every login of the panel comes from 127.0.0.1: one failed check
// would otherwise shut the terminal and the file manager of every website for seconds to minutes. Older OpenSSH has no
// such penalty and does not know the option, and a configuration with an unknown option stops sshd from starting, so
// the file is written only when sshd itself lists the option, and is removed again if sshd does not accept the result.
const sshdExemptScript = `S=$(command -v sshd || echo /usr/sbin/sshd); [ -x "$S" ] || exit 0
"$S" -T 2>/dev/null | grep -qi '^persourcepenaltyexemptlist' || exit 0
f=/etc/ssh/sshd_config.d/10-mpn-panel.conf
mkdir -p /etc/ssh/sshd_config.d
printf '# mpn: the web panel logs in from this machine, so this machine must not be penalised\nPerSourcePenaltyExemptList 127.0.0.1/32,::1/128\n' > "$f"
if "$S" -t 2>/dev/null; then systemctl reload ssh 2>/dev/null || systemctl reload sshd; else rm -f "$f"; echo 'sshd did not accept the exemption; removed it' >&2; fi`
const panelCertScript = `d=` + panelCert + `
[ -s "$d/fullchain.pem" ] || { mkdir -p "$d" && openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 3650 -subj /CN=mpn-panel -keyout "$d/privkey.pem" -out "$d/fullchain.pem" && chmod 600 "$d/privkey.pem"; }`

// webUserScript creates the panel's account, or accepts it again if mpn made it before. Any other account of that name
// stops the plan.
const webUserScript = `if id -u mpn-web >/dev/null 2>&1; then [ "$(getent passwd mpn-web | cut -d: -f5)" = '` + webMark + `' ] || { echo 'account mpn-web already exists and was not created by mpn' >&2; exit 1; }; else useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin --comment '` + webMark + `' mpn-web; fi`

// panelStep installs the web panel: its account, the two services, the TLS front on o.PanelPort, and the first
// administrator password, which is printed once if none exists yet.
func panelStep(o Options) (Step, error) {
	if o.SelfPath == "" {
		return Step{}, errors.New("the web panel needs the mpn binary installed; run the plan from the mpn binary itself")
	}
	port := o.PanelPort
	if port == 0 {
		port = 2610
	}
	if port < 1024 || port > 65535 || port == PanelBackendPort {
		return Step{}, fmt.Errorf("panel port %d must be between 1024 and 65535 and not %d", port, PanelBackendPort)
	}
	for _, h := range o.PanelHosts {
		if !site.ValidHostname(h) {
			return Step{}, fmt.Errorf("panel host name %q is not a valid lower-case ASCII name", h)
		}
	}
	acts := []Action{
		Run("sh", "-c", webUserScript),
		Run("install", "-d", "-o", "mpn-web", "-g", "mpn-web", "-m", "0750", "/var/lib/mpn-web"),
		Run("sh", "-c", frontGroupScript(distro.WebUser())),
		WriteFile("/etc/systemd/system/mpn-root.service", rootService, 0o644),
		WriteFile("/etc/systemd/system/mpn-web.socket", panelSocketUnit, 0o644),
		WriteFile("/etc/systemd/system/mpn-web.service", webService(o.PanelHosts, pmaPortOf(o, port)), 0o644),
		Run("sh", "-c", panelCertScript),
		Run("sh", "-c", sshdExemptScript),
		// The socket first: nginx is pointed at it below, and it must be there when nginx starts to use it.
		Run("systemctl", "daemon-reload"),
		Run("systemctl", "enable", "mpn-root", "mpn-web.socket", "mpn-web"),
		Run("systemctl", "restart", "mpn-root"),
		// systemd sets the mode of the folder only when it makes it, so a folder left from an earlier socket is removed first.
		Run("sh", "-c", "systemctl stop mpn-web mpn-web.socket; rmdir "+PanelSocketDir+" 2>/dev/null; systemctl start mpn-web.socket"),
		Run("systemctl", "restart", "mpn-web"),
		WriteFile("/etc/nginx/conf.d/01-mpn-panel.conf", panelNginx(port), 0o644),
		Run("nginx", "-t"),
		Run("sh", "-c", nginxRestartIfNeeded),
		ReloadNginx(),
	}
	acts = append(acts, motdActions()...)
	if o.PMA { // the group and the key must exist before the panel starts with the browser switched on
		acts = append(acts[:2], append(pmaPrepareActions(), acts[2:]...)...)
	}
	return Step{"web panel", acts}, nil
}

// pmaPortOf is the port of the database browser for the web unit, 0 when it is not installed.
func pmaPortOf(o Options, panelPort int) int {
	if o.PMA {
		return PMAPort(panelPort)
	}
	return 0
}
