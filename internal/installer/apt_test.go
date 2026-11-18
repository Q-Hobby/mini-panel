package installer

import (
	"strings"
	"testing"
)

// unattended-upgrade-shutdown runs all the time on Debian and Ubuntu, so the installer must not wait for it.
func TestInstallerDoesNotWaitForPermanentDaemons(t *testing.T) {
	o := opts("mariadb", 1024)
	o.PHPVersions = []string{"8.4"}
	p, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range p {
		for _, a := range s.Actions {
			joined := strings.Join(a.Args, " ")
			if strings.Contains(joined, "pgrep") || strings.Contains(joined, "unattended-upgr") {
				t.Errorf("step %q waits on a process by name: %s", s.Name, joined)
			}
		}
	}
}

// A plan that reloads nginx must wait until the old workers stop taking connections, or the first request after "apply"
// can be cut off. Every reload in every plan has to be that waiting step.
func TestNginxIsOnlyEverReloadedByTheWaitingStep(t *testing.T) {
	o := opts("mariadb", 1024)
	o.PHPVersions = []string{"8.4"}
	p, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range p {
		for _, a := range s.Actions {
			j := strings.Join(a.Args, " ")
			if len(a.Args) == 3 && a.Args[0] == "systemctl" && a.Args[1] == "reload" && a.Args[2] == "nginx" {
				t.Errorf("step %q reloads nginx without waiting", s.Name)
			}
			if strings.Contains(j, "systemctl reload nginx") {
				n++
			}
		}
	}
	if n == 0 {
		t.Error("the plan never reloads nginx")
	}
	for _, want := range []string{"systemctl reload nginx || exit 1", "shutting down", "--ppid", "sleep 0.1"} {
		if !strings.Contains(nginxReloadScript, want) {
			t.Errorf("the waiting step lacks %q", want)
		}
	}
	if strings.Contains(nginxReloadScript, "pgrep") {
		t.Error("the workers must be found by their parent, not by name")
	}
}

func TestAptUpdateRetriesAndAptInstallWaitsForTheLock(t *testing.T) {
	u := strings.Join(aptUpdate().Args, " ")
	if !strings.Contains(u, "seq 1 30") || !strings.Contains(u, "apt-get -o DPkg::Lock::Timeout=300 update") {
		t.Errorf("update must retry and wait for the lock: %s", u)
	}
	if i := strings.Join(aptInstall("x").Args, " "); !strings.Contains(i, "DPkg::Lock::Timeout=300") {
		t.Errorf("install must wait for the lock: %s", i)
	}
}

func TestEveryAptUpdateInAPlanIsTheRetryingOne(t *testing.T) {
	o := opts("mariadb", 1024)
	o.PHPVersions = []string{"8.4"}
	p, _ := Build(o)
	for _, s := range p {
		for _, a := range s.Actions {
			if len(a.Args) > 1 && a.Args[0] == "apt-get" && a.Args[len(a.Args)-1] == "update" {
				t.Errorf("step %q runs a bare apt-get update: %v", s.Name, a.Args)
			}
		}
	}
}
