package tune

import (
	"strings"
	"testing"
)

func TestDatabaseListensOnLocalhostOnly(t *testing.T) {
	p := New(Input{RAMMB: 1024, CPUs: 1})
	mariadb, err := p.DBConf("mariadb")
	if err != nil {
		t.Fatal(err)
	}
	mysql, err := p.DBConf("mysql")
	if err != nil {
		t.Fatal(err)
	}
	for name, conf := range map[string]string{"mariadb": mariadb, "mysql": mysql} {
		if !strings.Contains(conf, "\nbind-address = 127.0.0.1\n") {
			t.Errorf("%s must listen on loopback only:\n%s", name, conf)
		}
	}
	if strings.Contains(mariadb, "mysqlx") {
		t.Error("MariaDB does not know the mysqlx options and would refuse to start")
	}
	if !strings.Contains(mysql, "\nmysqlx-bind-address = 127.0.0.1\n") {
		t.Errorf("MySQL also opens the X protocol port 33060:\n%s", mysql)
	}
}

func TestLegacyAuthSettingsAreSeparateFromTheDefaults(t *testing.T) {
	conf, _ := New(Input{RAMMB: 1024, CPUs: 1}).DBConf("mysql")
	if strings.Contains(conf, "authentication_policy") {
		t.Error("the weaker login plugin must not be part of the default settings")
	}
	c84, c80 := LegacyAuthConf("8.4"), LegacyAuthConf("8.0")
	if !strings.Contains(c84, "authentication_policy = mysql_native_password") || !strings.Contains(c84, "mysql_native_password = ON") {
		t.Errorf("8.4: %q", c84)
	}
	// 8.0 always loads the plugin and has no such option; setting it would stop the server.
	if !strings.Contains(c80, "default_authentication_plugin = mysql_native_password") || strings.Contains(c80, "mysql_native_password = ON") {
		t.Errorf("8.0: %q", c80)
	}
	for _, s := range []string{"9.7", "11.8", "10.6", ""} {
		if LegacyAuthConf(s) != "" {
			t.Errorf("series %q must get no legacy settings", s)
		}
	}
}
