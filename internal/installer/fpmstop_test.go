package installer

import (
	"strings"
	"testing"
)

func TestFPMWithASitePoolIsLeftRunning(t *testing.T) {
	s := stopUnusedFPM("8.4")
	for _, want := range []string{
		"ls /etc/php/8.4/fpm/pool.d/*.conf",
		"grep -qv '/www\\.conf$'", // ignore the package's own pool
		"systemctl disable --now php8.4-fpm",
		"php8.4-fpm has a site pool, left running",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	// The stop must be the else branch: it may only run when no site pool was found.
	if strings.Index(s, "else systemctl disable") < strings.Index(s, "then echo") {
		t.Errorf("the stop must come after the keep branch:\n%s", s)
	}
}

func TestEveryPHPVersionStopIsConditional(t *testing.T) {
	o := opts("", 1024)
	o.PHPVersions = []string{"7.4", "8.4"}
	p, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range p {
		for _, a := range st.Actions {
			joined := strings.Join(a.Args, " ")
			if strings.Contains(joined, "disable --now php") && !strings.Contains(joined, "grep -qv") {
				t.Errorf("unconditional stop of a PHP-FPM service in step %q: %s", st.Name, joined)
			}
		}
	}
}
