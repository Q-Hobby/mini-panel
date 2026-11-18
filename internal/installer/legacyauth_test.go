package installer

import (
	"strings"
	"testing"
)

// dbConf returns the content of the database settings file written by the plan.
func dbConf(t *testing.T, o Options) string {
	t.Helper()
	p, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range p {
		for _, a := range s.Actions {
			if strings.HasSuffix(a.Path, "99-mpn.cnf") {
				return a.Content
			}
		}
	}
	t.Fatal("no database settings file in the plan")
	return ""
}

func TestMySQLWithOldPHPGetsTheLegacyLogin(t *testing.T) {
	o := opts("mysql", 1024)
	o.PHPVersions = []string{"7.0", "8.4"}
	if c := dbConf(t, o); !strings.Contains(c, "authentication_policy = mysql_native_password") {
		t.Errorf("PHP 7.0 cannot log in without it:\n%s", c)
	}
}

func TestMySQLWithModernPHPKeepsTheStrongerLogin(t *testing.T) {
	o := opts("mysql", 1024)
	o.PHPVersions = []string{"7.4", "8.4", "8.5"}
	if c := dbConf(t, o); strings.Contains(c, "native_password") {
		t.Errorf("7.4 and newer support the default login:\n%s", c)
	}
}

func TestMariaDBNeverGetsMySQLOnlySettings(t *testing.T) {
	o := opts("mariadb", 1024)
	o.PHPVersions = []string{"7.0"}
	c := dbConf(t, o)
	if strings.Contains(c, "authentication_policy") || strings.Contains(c, "mysqlx") {
		t.Errorf("MariaDB would refuse to start with MySQL-only options:\n%s", c)
	}
	if !strings.Contains(c, "bind-address = 127.0.0.1") {
		t.Errorf("MariaDB must be bound to loopback too:\n%s", c)
	}
}

func TestHasPHPBefore74(t *testing.T) {
	for in, want := range map[string]bool{"7.0": true, "7.3": true, "7.4": false, "8.0": false, "8.5": false} {
		if got := hasPHPBefore74([]string{in}); got != want {
			t.Errorf("%s: got %v want %v", in, got, want)
		}
	}
	if hasPHPBefore74(nil) {
		t.Error("no PHP versions means no legacy login")
	}
}
