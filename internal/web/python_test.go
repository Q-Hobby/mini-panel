package web

import (
	"net/url"
	"strings"
	"testing"
)

const pyList = `[{"name":"py","kind":"python","user":"py","hostnames":["py.example.com"],"detail":"python 3.12","tls":false,"root":"/home/py/htdocs"}]`
const pyInfo = `{"name":"py","kind":"python","user":"py","hostnames":["py.example.com"],"root":"/home/py/htdocs","root_sub":"","tls":false,"running":true,"run_detail":"app-py.service",
"disk_bytes":1000,"python_version":"3.12","entry":"app.py","port":20007,"mem_mb":0,"installed_php":[],"installed_node":[],"installed_python":["3.11","3.12"],"nginx":"server {}"}`

func TestTheOverviewOfAPythonSiteShowsItsOwnSettings(t *testing.T) {
	h, _, c, _ := setupWith(t, answersFor(pyList, pyInfo), nil)
	body := do(h, "GET", "/sites/py", nil, cookie(c)).Body.String()
	for _, want := range []string{"Đang chạy", "Python 3.12", `name="python"`, `<option>3.11</option>`, `<option selected>3.12</option>`,
		`name="entry"`, `value="app.py"`, `name="port"`, `value="20007"`, `name="mem"`, `name="root_sub"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, not := range []string{`name="ini_memory_limit"`, `name="upstream"`, `name="node"`, `name="php"`} {
		if strings.Contains(body, not) {
			t.Errorf("a python site must not show %q", not)
		}
	}
}

func TestSavingPythonSettingsSendsThemAndRefusesWrongOnes(t *testing.T) {
	h, f, c, tok := setupWith(t, answersFor(pyList, pyInfo), nil)
	form := url.Values{"csrf": {tok}, "root_sub": {"src"}, "python": {"3.11"}, "entry": {"-m myapp"}, "port": {"20009"}, "mem": {"256"}}
	followed(h, do(h, "POST", "/sites/py/settings", form, cookie(c)), c)
	req, ok := lastSet(f)
	if !ok || req.Kind != "python" || req.Python != "3.11" || req.Entry != "-m myapp" || req.Port != 20009 || req.Mem != 256 || req.RootSub != "src" {
		t.Fatalf("%+v", req)
	}
	for name, change := range map[string]url.Values{
		"a version that is not installed": {"python": {"3.9"}},
		"an entry that goes up":           {"entry": {"../x.py"}},
		"a shell command as the entry":    {"entry": {"app.py; id"}},
		"the panel's own port":            {"port": {"8780"}},
		"too little memory":               {"mem": {"5"}},
	} {
		h, f, c, tok = setupWith(t, answersFor(pyList, pyInfo), nil)
		form := url.Values{"csrf": {tok}, "python": {"3.12"}, "entry": {"app.py"}, "port": {"20007"}}
		for k, v := range change {
			form[k] = v
		}
		if w := do(h, "POST", "/sites/py/settings", form, cookie(c)); w.Code != 400 {
			t.Errorf("%s: %d", name, w.Code)
		}
		if _, sent := lastSet(f); sent {
			t.Errorf("%s reached the helper", name)
		}
	}
}

func TestThePageForANewPythonSiteAsksForTheStartScriptOnly(t *testing.T) {
	h, _, c, _ := setupWith(t, answersFor(pyList, pyInfo), nil)
	if body := do(h, "GET", "/sites/new", nil, cookie(c)).Body.String(); !strings.Contains(body, "/sites/new?kind=python") {
		t.Error("the list of kinds must offer Python")
	}
	body := do(h, "GET", "/sites/new?kind=python", nil, cookie(c)).Body.String()
	if !strings.Contains(body, `name="entry"`) || !strings.Contains(body, `value="app.py"`) || strings.Contains(body, `name="node"`) || strings.Contains(body, `name="php"`) {
		t.Errorf("form:\n%s", body)
	}
}
