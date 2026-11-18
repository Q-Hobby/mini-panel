package installer

import (
	"errors"
	"os"
	"strings"

	"mpn/internal/distro"
)

// Moving a machine installed before 1.2.0 to the panel's socket. install.sh replaces only the program on an update, so the files the
// plan of a first install writes are brought up to date here: the socket and the group, the panel's unit, and the nginx files that
// point at the panel. It can run any number of times; on a machine that is up to date it changes nothing.

const (
	WebUnitFile   = "/etc/systemd/system/mpn-web.service"
	SocketFile    = "/etc/systemd/system/mpn-web.socket"
	PanelConfFile = "/etc/nginx/conf.d/01-mpn-panel.conf"
	PMAConfFile   = "/etc/nginx/conf.d/02-mpn-pma.conf"
)

// MigrateWebUnit returns the panel's unit with the listening address replaced by the socket. Everything else in it (the names of the
// panel, the port of the database browser) stays as it is. ok is false when the unit is neither the old form nor the new one.
func MigrateWebUnit(unit string) (string, bool) {
	if strings.Contains(unit, "-listen systemd") && strings.Contains(unit, "Requires=mpn-web.socket") {
		return unit, true
	}
	old := "-listen 127.0.0.1:" + itoa(PanelBackendPort)
	if !strings.Contains(unit, old) || !strings.Contains(unit, "Wants=mpn-root.service\n") {
		return unit, false
	}
	unit = strings.Replace(unit, old, "-listen systemd", 1)
	unit = strings.Replace(unit, "Wants=mpn-root.service\n", "Wants=mpn-root.service\nRequires=mpn-web.socket\nAfter=mpn-web.socket\n", 1)
	return unit, true
}

func itoa(n int) string {
	var b [12]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			return string(b[i:])
		}
	}
}

// The SELinux domain of the web panel. Connecting to a Unix socket is checked against the domain of the process that listens, not
// only against the label of the socket file. Without a domain of its own the panel runs as unconfined_service_t, like any service
// systemd starts from /usr/local/bin, and nginx (httpd_t) may not connect to it (502, seen on AlmaLinux 9). Letting httpd_t connect
// to every such service would open them all to the web server, so the panel gets mpn_web_t, and httpd_t may connect to that alone.
//
// mpn_web_t is unconfined like the domain it replaces: the panel is already held in by its unit (its own account, no capabilities, a
// read-only system, ProtectHome), and confining it further here would only break it on the next change. The rules for init_t are what
// systemd needs to start a service in a named domain and to make its listening socket in that domain (SELinuxContextFromNet=no).
//
// It is CIL, which semodule takes as it is, so nothing needs compiling. The text carries a version line: a machine with an older
// module gets this one.
const panelSELinuxPolicy = `; mpn_panel 2: the web panel of mpn
(type mpn_web_t)
(roletype system_r mpn_web_t)
(typeattributeset domain (mpn_web_t))
(typeattributeset unconfined_domain_type (mpn_web_t))
(typeattributeset corenet_unconfined_type (mpn_web_t))
(typeattributeset files_unconfined_type (mpn_web_t))
(typeattributeset filesystem_unconfined_type (mpn_web_t))
(typeattributeset devices_unconfined_type (mpn_web_t))
(typeattributeset kern_unconfined (mpn_web_t))
(typeattributeset named_filetrans_domain (mpn_web_t))
(allow init_t mpn_web_t (process (transition siginh rlimitinh noatsecure)))
(allow init_t mpn_web_t (process2 (nnp_transition nosuid_transition)))
(allow init_t mpn_web_t (unix_stream_socket (create bind listen setopt getopt getattr setattr accept read write ioctl shutdown)))
(allow mpn_web_t init_t (unix_stream_socket (accept read write getattr getopt setopt ioctl shutdown)))
(allow mpn_web_t init_t (fd (use)))
(allow mpn_web_t bin_t (file (entrypoint)))
(allow httpd_t mpn_web_t (unix_stream_socket (connectto)))
`

// PanelSELinuxDropins put the panel and its socket in mpn_web_t. They are drop-ins and not lines of the units, because systemd on a
// system without SELinux refuses to start a service whose SELinuxContext it cannot set.
const (
	panelSELinuxServiceDropin = "/etc/systemd/system/mpn-web.service.d/10-selinux.conf"
	panelSELinuxSocketDropin  = "/etc/systemd/system/mpn-web.socket.d/10-selinux.conf"
	panelSELinuxServiceText   = "[Service]\nSELinuxContext=system_u:system_r:mpn_web_t:s0\n"
	panelSELinuxSocketText    = "[Socket]\nSELinuxContextFromNet=no\n"
)

// panelSELinuxModule loads the policy when it is missing or older, and writes the drop-ins. The domain must exist before systemd is
// told to use it, or the panel would not start.
const panelSELinuxModule = `mkdir -p /etc/mpn/selinux /etc/systemd/system/mpn-web.service.d /etc/systemd/system/mpn-web.socket.d
cat > /etc/mpn/selinux/mpn_panel.cil <<'MPN_CIL'
` + panelSELinuxPolicy + `MPN_CIL
if ! semodule -l | grep -qx mpn_panel || ! cmp -s /etc/mpn/selinux/mpn_panel.cil /etc/mpn/selinux/mpn_panel.loaded; then
  semodule -i /etc/mpn/selinux/mpn_panel.cil
  cp /etc/mpn/selinux/mpn_panel.cil /etc/mpn/selinux/mpn_panel.loaded
fi
printf '%s' '` + panelSELinuxServiceText + `' > ` + panelSELinuxServiceDropin + `
printf '%s' '` + panelSELinuxSocketText + `' > ` + panelSELinuxSocketDropin + `
`

// PanelPorts are the ports of the panel the SELinux step labels, from the panel's port.
func PanelPorts(panelPort int) []int {
	if panelPort == 0 {
		panelPort = 2610
	}
	return elPanelPorts(Options{PanelPort: panelPort})
}

// SELinuxUpToDate says whether the panel's domain is as this version writes it. On a machine without SELinux in use it is true.
func SELinuxUpToDate() bool {
	if b, err := os.ReadFile("/sys/fs/selinux/enforce"); err != nil || len(b) == 0 {
		return true
	}
	loaded, err := os.ReadFile("/etc/mpn/selinux/mpn_panel.loaded")
	if err != nil || string(loaded) != panelSELinuxPolicy {
		return false
	}
	d1, err1 := os.ReadFile(panelSELinuxServiceDropin)
	d2, err2 := os.ReadFile(panelSELinuxSocketDropin)
	return err1 == nil && err2 == nil && string(d1) == panelSELinuxServiceText && string(d2) == panelSELinuxSocketText
}

// SocketUnit is the text mpn-web.socket must have; a machine whose file differs is migrated again.
func SocketUnit() string { return panelSocketUnit }

// MigrateInput is what the machine has now.
type MigrateInput struct {
	WebUnit   string // the text of mpn-web.service
	PanelPort int    // from 01-mpn-panel.conf
	PMA       bool   // the database browser is installed (02-mpn-pma.conf exists)
	EL        bool
	Ports     []int // the panel's own ports, which the SELinux step labels (panel, database browser, web mail)
}

// MigrateActions are the steps that move the machine to the socket, in an order in which the panel stays reachable as long as it can:
// the group and the socket first, then the panel on the socket, then nginx pointed at it.
func MigrateActions(in MigrateInput) ([]Action, error) {
	unit, ok := MigrateWebUnit(in.WebUnit)
	if !ok {
		return nil, errors.New("mpn-web.service is not in a form mpn wrote; leave it, or install the panel again")
	}
	if in.PanelPort < 1024 || in.PanelPort > 65535 {
		return nil, errors.New("the panel's port could not be read from " + PanelConfFile)
	}
	acts := []Action{Run("sh", "-c", frontGroupScript(distro.WebUser()))}
	if in.PMA {
		acts = append(acts, Run("sh", "-c", `! id -u `+pmaAccount+` >/dev/null 2>&1 || id -nG `+pmaAccount+` | tr ' ' '\n' | grep -qx `+FrontGroup+` || usermod -aG `+FrontGroup+` `+pmaAccount))
	}
	if in.EL {
		// The whole SELinux step again: it can run any number of times, and it brings the label of the socket's folder, the
		// panel's domain and its drop-ins, and the label of phpMyAdmin's folder (restorecon of /usr/share/mpn).
		acts = append(acts, Run("sh", "-c", selinuxScript(in.Ports)))
	}
	acts = append(acts,
		WriteFile(SocketFile, panelSocketUnit, 0o644),
		WriteFile(WebUnitFile, unit, 0o644),
		Run("systemctl", "daemon-reload"),
		Run("systemctl", "enable", "mpn-web.socket"),
		Run("systemctl", "stop", "mpn-web"), // it holds the TCP port; the socket takes over
		// systemd sets the mode of the folder only when it makes it, so a folder left from an earlier socket is removed first.
		Run("sh", "-c", "systemctl stop mpn-web.socket; rmdir "+PanelSocketDir+" 2>/dev/null; systemctl start mpn-web.socket"),
		Run("sh", "-c", "command -v restorecon >/dev/null && [ -d /run/mpn-web ] && restorecon -R /run/mpn-web || true"),
		Run("systemctl", "start", "mpn-web"),
		WriteFile(PanelConfFile, panelNginx(in.PanelPort), 0o644),
	)
	if in.PMA {
		acts = append(acts, WriteFile(PMAConfFile, pmaNginx(in.PanelPort), 0o644))
	}
	acts = append(acts,
		Run("nginx", "-t"),
		Run("sh", "-c", nginxRestartIfNeeded),
		ReloadNginx(),
	)
	return acts, nil
}
