package installer

import (
	"strings"
	"testing"
)

func TestPMAFilesAreWhatAFirstInstallWrites(t *testing.T) {
	files := PMAFiles(2610)
	for _, name := range []string{"config.inc.php", "signon.php", "logout.php"} {
		if _, ok := files[pmaDir+"/"+name]; !ok {
			t.Errorf("%s is missing", name)
		}
	}
	for path, body := range files {
		if strings.Contains(body, "@BACKEND@") || strings.Contains(body, "@PANELPORT@") {
			t.Errorf("%s: a placeholder was left", path)
		}
	}
	if !strings.Contains(files[pmaDir+"/logout.php"], "':2610'") {
		t.Error("the sign-out page leads back to the panel's own port when the browser is reached by its port")
	}
	if PMAFiles(2700)[pmaDir+"/logout.php"] == files[pmaDir+"/logout.php"] {
		t.Error("the port of the panel is part of the sign-out page")
	}
	// the same text the first install writes, so that mpn panel sync finds nothing to change on a machine installed with this version
	o := panelOpts()
	o.Engine = "mariadb" // the browser is installed only on a machine with a database server
	step, err := pmaStep(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range step.Actions {
		if want, ok := files[a.Path]; ok && a.Content != want {
			t.Errorf("%s differs from the first install", a.Path)
		}
	}
}
