package provision

import (
	"strings"
	"testing"

	"mpn/internal/installer"
	"mpn/internal/site"
	"mpn/internal/tune"
)

func pySite() site.Site {
	return site.Site{Name: "py", Kind: site.Python, User: "py", Root: "/home/py/htdocs", Hostnames: []string{"py.example.com"},
		NodePort: 20007, PyVersion: "3.12", Entry: "app.py"}
}

// everything returns the full text of a plan: the arguments of every command and the content of every file written, in order.
func everything(p installer.Plan) string {
	var b strings.Builder
	for _, st := range p {
		for _, a := range st.Actions {
			b.WriteString(strings.Join(a.Args, " ") + a.Path + "\n" + a.Content + "\n")
		}
	}
	return b.String()
}

func TestPythonPlanMakesTheEnvironmentTheServiceAndTheProxy(t *testing.T) {
	plan, err := Plan(pySite(), Options{Profile: tune.New(tune.Input{RAMMB: 1024, CPUs: 1}), NodeMemMB: 192})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(plan), "account and directories,welcome page,python service,nginx vhost"; got != want {
		t.Errorf("steps=%s want %s", got, want)
	}
	text := everything(plan)
	for _, want := range []string{"/usr/bin/python3.12", "runuser -u py --", "/home/py/venv", "python3.12-venv",
		"ExecStart=/home/py/venv/bin/python app.py", "MemoryMax=192M", "systemctl enable app-py.service", "systemctl restart app-py.service",
		"proxy_pass http://127.0.0.1:20007;"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	if strings.Index(text, "runuser") > strings.Index(text, "systemctl restart app-py.service") {
		t.Error("the virtual environment must be made before the service starts")
	}
}

func TestPythonRemovalStopsTheService(t *testing.T) {
	plan, err := RemovePlan(pySite(), false)
	if err != nil {
		t.Fatal(err)
	}
	text := everything(plan)
	if !strings.Contains(text, "systemctl disable --now app-py.service") || !strings.Contains(text, "rm -f /etc/systemd/system/app-py.service") {
		t.Errorf("the service is not removed:\n%s", text)
	}
}
