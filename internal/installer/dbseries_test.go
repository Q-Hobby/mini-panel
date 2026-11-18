package installer

import (
	"strings"
	"testing"

	"mpn/internal/osinfo"
)

var (
	debian12 = osinfo.Target{ID: "debian", Version: "12", Codename: "bookworm"}
	debian13 = osinfo.Target{ID: "debian", Version: "13", Codename: "trixie"}
	ubuntu22 = osinfo.Target{ID: "ubuntu", Version: "22.04", Codename: "jammy"}
)

func optsFor(tg osinfo.Target, engine, series string, php ...string) Options {
	o := opts(engine, 1024)
	o.Target, o.DBSeries, o.PHPVersions = tg, series, php
	return o
}

func TestNoSeriesMeansTheEnginesDefault(t *testing.T) {
	p, err := Build(optsFor(debian12, "mariadb", ""))
	if err != nil || !strings.Contains(p.String(), "mirror.mariadb.org/repo/11.8/debian") && !hasWrite(p, "11.8/debian") {
		t.Fatalf("MariaDB default: err=%v\n%s", err, p)
	}
	p, err = Build(optsFor(debian12, "mysql", ""))
	if err != nil || !hasWrite(p, "Components: mysql-8.4-lts") {
		t.Fatalf("MySQL default: err=%v", err)
	}
}

// hasWrite reports whether any file in the plan contains s.
func hasWrite(p Plan, s string) bool {
	for _, st := range p {
		for _, a := range st.Actions {
			if a.Args == nil && strings.Contains(a.Content, s) {
				return true
			}
		}
	}
	return false
}

func TestMariaDBSeriesFollowTheRelease(t *testing.T) {
	if p, err := Build(optsFor(ubuntu22, "mariadb", "10.6")); err != nil || !hasWrite(p, "repo/10.6/ubuntu") {
		t.Errorf("MariaDB 10.6 on Ubuntu 22.04 must work: %v", err)
	}
	_, err := Build(optsFor(ubuntu22, "mariadb", "12.3"))
	if err == nil || !strings.Contains(err.Error(), "choose one of: 10.6, 10.11, 11.4, 11.8") {
		t.Errorf("12.3 is not offered on Ubuntu 22.04: %v", err)
	}
	if _, err := Build(optsFor(debian13, "mariadb", "10.11")); err == nil {
		t.Error("MariaDB 10.11 has no packages for Debian 13")
	}
	if p, err := Build(optsFor(debian13, "mariadb", "12.3")); err != nil || !hasWrite(p, "repo/12.3/debian") {
		t.Errorf("MariaDB 12.3 on Debian 13 must work: %v", err)
	}
}

func TestMySQL80OnlyWhereTheOracleRepositoryHasIt(t *testing.T) {
	if p, err := Build(optsFor(debian12, "mysql", "8.0")); err != nil || !hasWrite(p, "Components: mysql-8.0") {
		t.Errorf("MySQL 8.0 on Debian 12 must work: %v", err)
	}
	if _, err := Build(optsFor(debian13, "mysql", "8.0")); err == nil {
		t.Error("the Oracle repository has no MySQL 8.0 for Debian 13")
	}
}

func TestLegacyLoginDependsOnTheSeries(t *testing.T) {
	c84 := dbConf(t, optsFor(debian12, "mysql", "8.4", "7.0"))
	if !strings.Contains(c84, "mysql_native_password = ON") || !strings.Contains(c84, "authentication_policy = mysql_native_password") {
		t.Errorf("8.4 needs the plugin switched on and named in the policy:\n%s", c84)
	}
	c80 := dbConf(t, optsFor(debian12, "mysql", "8.0", "7.0"))
	if !strings.Contains(c80, "default_authentication_plugin = mysql_native_password") || strings.Contains(c80, "mysql_native_password = ON") {
		t.Errorf("8.0 has no such option, only the default plugin:\n%s", c80)
	}
	if c := dbConf(t, optsFor(debian12, "mysql", "8.0", "7.4", "8.4")); strings.Contains(c, "native_password") {
		t.Errorf("PHP 7.4 and newer need no legacy login:\n%s", c)
	}
	if c := dbConf(t, optsFor(debian12, "mariadb", "10.11", "7.0")); strings.Contains(c, "native_password") || strings.Contains(c, "authentication_policy") {
		t.Errorf("MariaDB takes none of the MySQL login settings:\n%s", c)
	}
}
