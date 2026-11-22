// Package svcport changes the ports of the services the panel looks after: SSH, the panel itself, the database browser and the web
// mail. It holds the parts that need no machine: which ports are allowed, the scripts that rewrite the configuration of sshd, and
// which address the panel dials to reach this machine's SSH server.
//
// SSH and the panel are changed in two steps, so a mistake never cuts the person off: the new port is added next to the old one,
// the person checks that the new one works, and only then is the old one closed. Until then both stay open.
package svcport

import (
	"fmt"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	SSH     = "ssh"
	Panel   = "panel"
	PMA     = "pma"
	Webmail = "webmail"
)

// Services are the services whose port can be changed, in the order the page shows them.
var Services = []string{SSH, Panel, PMA, Webmail}

// TwoStep says whether a change of the service's port keeps the old port until the person finishes it.
func TwoStep(service string) bool { return service == SSH || service == Panel }

// ValidService accepts one of Services.
func ValidService(s string) error {
	if !slices.Contains(Services, s) {
		return fmt.Errorf("unknown service %q: ssh, panel, pma or webmail", s)
	}
	return nil
}

// reserved are ports other programs of the server use, or that the panel uses behind nginx: none of the four may move there.
var reserved = map[int]string{
	25: "SMTP", 80: "HTTP", 110: "POP3", 143: "IMAP", 443: "HTTPS", 465: "SMTP", 587: "SMTP", 993: "IMAP", 995: "POP3",
	3306: "MySQL", 33060: "MySQL", 5432: "PostgreSQL", 6379: "Redis", 9000: "PHP-FPM", 11211: "Memcached",
	8780: "the panel behind nginx", 8781: "the web mail behind nginx",
}

// Valid checks a port for a service before anything on the machine is looked at. SSH may use a port below 1024 (22 is one); the
// others are served by nginx next to the websites and stay at 1024 or above, like the panel's own port at install.
func Valid(service string, port int) error {
	if err := ValidService(service); err != nil {
		return err
	}
	lo := 1024
	if service == SSH {
		lo = 1
	}
	if port < lo || port > 65535 {
		return fmt.Errorf("the port of %s must be a number from %d to 65535", service, lo)
	}
	if what, ok := reserved[port]; ok {
		return fmt.Errorf("port %d is used by %s", port, what)
	}
	return nil
}

// Status is the answer of "mpn port status": the ports in use now, and a change that waits to be finished.
type Status struct {
	SSH       []int `json:"ssh"`        // every port sshd listens on now
	SSHOld    []int `json:"ssh_old"`    // while a change waits: the ports that close when it is finished
	Panel     int   `json:"panel"`      // the panel's port
	PanelNew  int   `json:"panel_new"`  // while a change waits: the new port, open next to Panel
	PMA       int   `json:"pma"`        // 0 when phpMyAdmin is not installed
	Webmail   int   `json:"webmail"`    // the port of the web mail, also while it is off (it is used when it is turned on)
	WebmailOn bool  `json:"webmail_on"` // the web mail is turned on
}

// SSHNew are the ports a waiting change of SSH adds.
func (s Status) SSHNew() []int {
	var out []int
	for _, p := range s.SSH {
		if !slices.Contains(s.SSHOld, p) {
			out = append(out, p)
		}
	}
	return out
}

// InUse lists the ports the four services hold, with the service of each, for the check that a new port is free.
func (s Status) InUse() map[int]string {
	m := map[int]string{}
	for _, p := range s.SSH {
		m[p] = SSH
	}
	if s.Panel > 0 {
		m[s.Panel] = Panel
	}
	if s.PanelNew > 0 {
		m[s.PanelNew] = Panel
	}
	if s.PMA > 0 {
		m[s.PMA] = PMA
	}
	if s.Webmail > 0 {
		m[s.Webmail] = Webmail
	}
	return m
}

// List reads "22,2222" and returns the ports sorted, without repeats; anything that is not a port is dropped.
func List(s string) []int {
	var out []int
	for _, f := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(f)); err == nil && n >= 1 && n <= 65535 {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return slices.Compact(out)
}

// Join writes ports as List reads them.
func Join(ports []int) string {
	s := make([]string, len(ports))
	for i, p := range ports {
		s[i] = strconv.Itoa(p)
	}
	return strings.Join(s, ",")
}

// Listener is one listening TCP socket, as "ss -ltnH" prints it.
type Listener struct {
	Addr string // 0.0.0.0, ::, 127.0.0.1, * ...
	Port int
}

// Listeners reads the output of "ss -ltnH" (with or without -p). The local address is the fourth field.
func Listeners(ss string) []Listener {
	var out []Listener
	for _, line := range strings.Split(ss, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		local := f[3]
		i := strings.LastIndexByte(local, ':')
		if i < 0 {
			continue
		}
		n, err := strconv.Atoi(local[i+1:])
		if err != nil {
			continue
		}
		addr := strings.Trim(local[:i], "[]")
		if j := strings.IndexByte(addr, '%'); j >= 0 { // 127.0.0.53%lo
			addr = addr[:j]
		}
		out = append(out, Listener{Addr: addr, Port: n})
	}
	return out
}

// SocketPorts reads the ports of "systemctl show ssh.socket -p Listen": "Listen=0.0.0.0:2222 (Stream)", one line per address,
// or several on one line. A unit that does not exist gives "Listen=" and no port.
func SocketPorts(show string) []int {
	var out []int
	for _, f := range strings.Fields(strings.ReplaceAll(show, "Listen=", " ")) {
		if strings.HasPrefix(f, "(") {
			continue
		}
		if i := strings.LastIndexByte(f, ':'); i >= 0 {
			f = f[i+1:]
		}
		if n, err := strconv.Atoi(f); err == nil && n >= 1 && n <= 65535 {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return slices.Compact(out)
}

// DialAddr is the address the panel connects to for SSH: the first of ports that something listens on, on the loopback address of
// the family it listens on. ports are in the order to try (those sshd is known to listen on first). When nothing is found it falls
// back to the loopback address and the first port, or 22.
func DialAddr(ports []int, listeners []Listener) string {
	for _, p := range ports {
		var own string
		for _, l := range listeners {
			if l.Port != p {
				continue
			}
			switch l.Addr {
			case "0.0.0.0", "*", "127.0.0.1":
				return net.JoinHostPort("127.0.0.1", strconv.Itoa(p))
			case "::", "::1":
				if own == "" {
					own = net.JoinHostPort("::1", strconv.Itoa(p))
				}
			default:
				if own == "" { // sshd listens on one address only (ListenAddress)
					own = net.JoinHostPort(l.Addr, strconv.Itoa(p))
				}
			}
		}
		if own != "" {
			return own
		}
	}
	if len(ports) > 0 {
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(ports[0]))
	}
	return "127.0.0.1:22"
}

// SSHDropIn is the file in which mpn writes the ports of sshd. Every "Port" line adds a port, so the lines elsewhere are commented
// out: otherwise sshd would go on listening on them.
const SSHDropIn = "/etc/ssh/sshd_config.d/00-mpn-port.conf"

// SSHBackupDir keeps the configuration of sshd as it was before mpn first changed a port.
const SSHBackupDir = "/var/backups/mpn-ssh-port"

// SSHPortScript makes sshd listen on exactly ports. It refuses a configuration that does not read sshd_config.d (where the ports
// are written) or that names a port inside ListenAddress (where Port lines do not apply). sshd -t checks the result; when it fails,
// every file goes back as it was and the script fails. Nothing is restarted here.
func SSHPortScript(ports []int) (string, error) {
	if len(ports) == 0 {
		return "", fmt.Errorf("sshd needs at least one port")
	}
	var lines strings.Builder
	for _, p := range ports {
		if p < 1 || p > 65535 {
			return "", fmt.Errorf("port %d is out of range", p)
		}
		lines.WriteString("Port " + strconv.Itoa(p) + "\\n")
	}
	return `set -eu
S=$(command -v sshd || echo /usr/sbin/sshd)
main=/etc/ssh/sshd_config
drop=` + SSHDropIn + `
grep -qiE '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config\.d/\*\.conf' "$main" || { echo "$main does not include /etc/ssh/sshd_config.d/*.conf; change the port of SSH by hand" >&2; exit 1; }
if grep -hiE '^[[:space:]]*ListenAddress[[:space:]]+([0-9.]+|\[[0-9a-fA-F:]+\]):[0-9]+' "$main" /etc/ssh/sshd_config.d/*.conf 2>/dev/null | grep -q .; then
  echo "a ListenAddress line names its own port; change the port of SSH by hand" >&2; exit 1
fi
mkdir -p /etc/ssh/sshd_config.d ` + SSHBackupDir + `
# A copy of every file as it is now, to put back if sshd refuses the result.
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cp -p "$main" "$tmp/main"
mkdir "$tmp/d"
cp -p /etc/ssh/sshd_config.d/*.conf "$tmp/d/" 2>/dev/null || true
for f in "$main" /etc/ssh/sshd_config.d/*.conf; do
  [ -f "$f" ] && [ "$f" != "$drop" ] || continue
  if grep -qiE '^[[:space:]]*Port[[:space:]]' "$f"; then
    [ -e ` + SSHBackupDir + `/"$(basename "$f")" ] || cp -p "$f" ` + SSHBackupDir + `/
    sed -i -E 's/^([[:space:]]*)(Port[[:space:]])/\1# mpn moved the port to 00-mpn-port.conf: \2/I' "$f"
  fi
done
printf '# Written by mpn (Firewall page, service ports). Every Port line adds a port.\n` + lines.String() + `' > "$drop"
chmod 0644 "$drop"
# sshd -t needs its privilege separation folder, which is missing while ssh.socket waits for a connection.
mkdir -p -m 0755 /run/sshd
if ! "$S" -t; then
  cp -p "$tmp/main" "$main"
  rm -f "$drop"
  cp -p "$tmp/d/"*.conf /etc/ssh/sshd_config.d/ 2>/dev/null || true
  echo "sshd did not accept the new port; the configuration was put back" >&2
  exit 1
fi`, nil
}

// SSHRestartScript makes sshd take its new ports. Sessions that are open stay open. Where ssh.socket listens (Ubuntu 22.10 and
// later), its addresses are made from sshd_config by a generator when systemd reloads, and the socket is restarted.
//
// systemd refuses a unit restarted more than a few times in a short while ("start request repeated too quickly", seen on Ubuntu 24.04
// when a change was cancelled and made again within a minute): then nothing listens at all. reset-failed clears that count before
// every restart, and a socket that is not listening afterwards is started once more.
const SSHRestartScript = `systemctl reset-failed ssh.socket ssh.service sshd.service 2>/dev/null || true
if systemctl is-enabled -q ssh.socket 2>/dev/null || systemctl is-active -q ssh.socket 2>/dev/null; then
  systemctl daemon-reload
  systemctl restart ssh.socket || { sleep 2; systemctl reset-failed ssh.socket; systemctl restart ssh.socket; }
  systemctl try-restart ssh.service 2>/dev/null || true
  systemctl is-active -q ssh.socket || { systemctl reset-failed ssh.socket; systemctl start ssh.socket; }
elif systemctl cat ssh.service >/dev/null 2>&1; then
  systemctl restart ssh.service || { sleep 2; systemctl reset-failed ssh.service; systemctl restart ssh.service; }
else
  systemctl restart sshd.service || { sleep 2; systemctl reset-failed sshd.service; systemctl restart sshd.service; }
fi`

// SELinuxPortScript gives a TCP port the SELinux type a service may listen on (ssh_port_t for sshd, http_port_t for nginx). It
// does nothing where SELinux is not in use.
func SELinuxPortScript(typ string, port int) string {
	p := strconv.Itoa(port)
	return `command -v getenforce >/dev/null || exit 0
[ "$(getenforce)" != Disabled ] || exit 0
command -v semanage >/dev/null || dnf -y -q install policycoreutils-python-utils
semanage port -a -t ` + typ + ` -p tcp ` + p + ` 2>/dev/null || semanage port -m -t ` + typ + ` -p tcp ` + p
}
