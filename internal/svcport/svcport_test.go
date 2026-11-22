package svcport

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidPorts(t *testing.T) {
	for _, c := range []struct {
		svc  string
		port int
		ok   bool
	}{
		{SSH, 22, true}, {SSH, 2222, true}, {SSH, 0, false}, {SSH, 65536, false}, {SSH, 443, false}, {SSH, 3306, false},
		{Panel, 8443, true}, {Panel, 1023, false}, {Panel, 8780, false}, {PMA, 2611, true}, {Webmail, 8781, false},
		{"mysql", 3307, false},
	} {
		if err := Valid(c.svc, c.port); (err == nil) != c.ok {
			t.Errorf("Valid(%s, %d) = %v, want ok=%v", c.svc, c.port, err, c.ok)
		}
	}
}

// A provider image with SSH on 2222, as on the machines that started this: the panel must dial 2222, not 22.
func TestDialAddrFollowsTheDetectedPort(t *testing.T) {
	ss := `LISTEN 0 128 0.0.0.0:2222 0.0.0.0:* users:(("sshd",pid=1,fd=3))
LISTEN 0 128    [::]:2222    [::]:* users:(("sshd",pid=1,fd=4))
LISTEN 0 511 127.0.0.53%lo:53 0.0.0.0:*`
	l := Listeners(ss)
	if len(l) != 3 || l[2].Addr != "127.0.0.53" || l[1].Addr != "::" {
		t.Fatalf("Listeners = %+v", l)
	}
	for _, c := range []struct {
		ports []int
		ss    string
		want  string
	}{
		{[]int{2222}, ss, "127.0.0.1:2222"},
		{[]int{22, 2222}, ss, "127.0.0.1:2222"}, // nothing listens on 22
		{[]int{2222}, "LISTEN 0 128 [::]:2222 [::]:*", "[::1]:2222"},
		{[]int{2222}, "LISTEN 0 128 10.0.0.5:2222 0.0.0.0:*", "10.0.0.5:2222"}, // ListenAddress on one address
		{[]int{2222}, "", "127.0.0.1:2222"},
		{nil, "", "127.0.0.1:22"},
	} {
		if got := DialAddr(c.ports, Listeners(c.ss)); got != c.want {
			t.Errorf("DialAddr(%v, %q) = %s, want %s", c.ports, c.ss, got, c.want)
		}
	}
}

// While no session is open on Ubuntu 24.04, systemd holds the sockets of sshd; its unit says where they are.
func TestSocketPortsReadsTheAddressesOfSSHSocket(t *testing.T) {
	for show, want := range map[string][]int{
		"Listen=0.0.0.0:2222 (Stream)\nListen=[::]:2222 (Stream)":               {2222},
		"Listen=0.0.0.0:2201 (Stream) [::]:2201 (Stream) 0.0.0.0:2222 (Stream)": {2201, 2222},
		"Listen=[::]:22 (Stream)":                                               {22},
		"Listen=":                                                               nil,
		"":                                                                      nil,
	} {
		if got := SocketPorts(show); !reflect.DeepEqual(got, want) {
			t.Errorf("SocketPorts(%q) = %v, want %v", show, got, want)
		}
	}
}

// systemd prints the addresses of ssh.socket also while it is off (Debian, or after it was turned off); those ports are not open.
func TestASocketThatIsOffGivesNoPort(t *testing.T) {
	for show, want := range map[string][]int{
		"ActiveState=active\nListen=0.0.0.0:2222 (Stream)\nListen=[::]:2222 (Stream)": {2222},
		"Listen=0.0.0.0:2222 (Stream)\nActiveState=active":                            {2222},
		"Listen=[::]:22 (Stream)\nActiveState=inactive":                               nil,
		"ActiveState=failed\nListen=0.0.0.0:2222 (Stream)":                            nil,
		"ActiveState=inactive\nListen=":                                               nil,
		"":                                                                            nil,
	} {
		if got := ActiveSocketPorts(show); !reflect.DeepEqual(got, want) {
			t.Errorf("ActiveSocketPorts(%q) = %v, want %v", show, got, want)
		}
	}
}

func TestStatusNewAndInUse(t *testing.T) {
	s := Status{SSH: []int{22, 2222}, SSHOld: []int{22}, Panel: 2610, PanelNew: 8443, PMA: 2611, Webmail: 2612}
	if got := s.SSHNew(); !reflect.DeepEqual(got, []int{2222}) {
		t.Errorf("SSHNew = %v", got)
	}
	u := s.InUse()
	if u[2222] != SSH || u[8443] != Panel || u[2611] != PMA || u[2612] != Webmail || len(u) != 6 {
		t.Errorf("InUse = %v", u)
	}
	if got := List(" 2222,22,x,22,70000"); !reflect.DeepEqual(got, []int{22, 2222}) || Join(got) != "22,2222" {
		t.Errorf("List = %v", got)
	}
}

func TestSSHPortScriptRefusesWhatItCannotChange(t *testing.T) {
	if _, err := SSHPortScript(nil); err == nil {
		t.Error("no port must be refused")
	}
	s, err := SSHPortScript([]int{22, 2222})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Port 22\\nPort 2222\\n", `"$S" -t`, "sshd_config.d/*.conf", "ListenAddress", SSHBackupDir, `cp -p "$tmp/main" "$main"`} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q", want)
		}
	}
	if !strings.Contains(SSHRestartScript, "ssh.socket") || !strings.Contains(SELinuxPortScript("ssh_port_t", 2222), "-t ssh_port_t -p tcp 2222") {
		t.Error("restart or SELinux script is wrong")
	}
}
