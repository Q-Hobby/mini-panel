package dbadmin

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// listing answers the sweep's query with the given user names.
type listing struct {
	fake
	users []string
}

func (l *listing) Run(sql string) (string, error) {
	if strings.Contains(sql, "FROM mysql.user WHERE user LIKE") {
		l.sqls = append(l.sqls, sql)
		return strings.Join(l.users, "\n") + "\n", nil
	}
	return l.fake.Run(sql)
}

func TestTempNamesCarryTheirExpiryAndNothingElseIsAccepted(t *testing.T) {
	name, err := newTempName(t0.Add(TempTTL))
	if err != nil {
		t.Fatal(err)
	}
	if !ValidTempName(name) || len(name) > 20 {
		t.Fatalf("%q", name)
	}
	exp, ok := TempExpiry(name)
	if !ok || !exp.Equal(t0.Add(TempTTL)) {
		t.Errorf("expiry %v %v", exp, ok)
	}
	other, _ := newTempName(t0)
	if name == other {
		t.Error("two names must differ")
	}
	for _, bad := range []string{"", "pma_", "root", "pma_abc_12345678", "pma_rdwxyz_1234567", "pma_rdwxyz_123456789", "PMA_rdwxyz_12345678",
		"pma_rdwxyz_1234567'", "pma_rdwxyz_12345678;", "pma_rdwx-z_12345678", "xpma_rdwxyz_12345678", "pma_rdwxyz_12345678\n"} {
		if ValidTempName(bad) {
			t.Errorf("%q must not be taken for a temporary user", bad)
		}
		if _, ok := TempExpiry(bad); ok {
			t.Errorf("%q has no expiry", bad)
		}
	}
}

func TestCreatingATempUserGivesOneDatabaseAndAQuotedPassword(t *testing.T) {
	f := &fake{inUse: "1"}
	u, err := Admin{R: f}.CreateTemp("my_blog", t0)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidTempName(u.Name) || !ValidPassword(u.Password) || !u.Expires.Equal(t0.Add(30*time.Minute)) {
		t.Fatalf("%+v", u)
	}
	sql := f.last()
	for _, want := range []string{"CREATE USER '" + u.Name + "'@'localhost' IDENTIFIED BY '" + u.Password + "' WITH MAX_USER_CONNECTIONS 6",
		"GRANT ALL PRIVILEGES ON `my\\_blog`.* TO '" + u.Name + "'@'localhost'"} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "GRANT OPTION") || strings.Contains(sql, "*.*") {
		t.Errorf("the user must get no more than its database:\n%s", sql)
	}
}

func TestCreatingATempUserRefusesWhatItShould(t *testing.T) {
	if _, err := (Admin{R: &fake{inUse: "1"}}).CreateTemp("x'; DROP DATABASE mysql; --", t0); err == nil {
		t.Error("an unsafe database name")
	}
	f := &fake{inUse: "0"}
	if _, err := (Admin{R: f}).CreateTemp("ghost", t0); err == nil || len(f.sqls) != 1 {
		t.Errorf("a database that is not on the server must not get a user: %v, %d statements", err, len(f.sqls))
	}
	f = &fake{inUse: "1", failOn: "CREATE USER"}
	_, err := Admin{R: f}.CreateTemp("blog", t0)
	if err == nil || !strings.Contains(f.last(), "DROP USER IF EXISTS") {
		t.Errorf("a failed creation must clean up after itself: %v / %q", err, f.last())
	}
	if err != nil && strings.Contains(err.Error(), "IDENTIFIED BY '") && !strings.Contains(err.Error(), "********") {
		t.Errorf("the password must not be in the error: %v", err)
	}
}

func TestOnlyTempUsersCanBeDropped(t *testing.T) {
	f := &fake{}
	a := Admin{R: f}
	for _, bad := range []string{"root", "blog", "pma", "pma_x", "", "mysql'; --"} {
		if err := a.DropTemp(bad); err == nil {
			t.Errorf("%q must not be dropped", bad)
		}
	}
	if len(f.sqls) != 0 {
		t.Errorf("nothing may reach the server for a refused name: %v", f.sqls)
	}
	name, _ := newTempName(t0)
	if err := a.DropTemp(name); err != nil || f.last() != "DROP USER IF EXISTS '"+name+"'@'localhost';\n" {
		t.Errorf("%v %q", err, f.last())
	}
}

func TestTheSweepDropsOnlyUsersWhoseTimeHasPassed(t *testing.T) {
	old1, _ := newTempName(t0.Add(-time.Hour))
	old2, _ := newTempName(t0.Add(-time.Second))
	live, _ := newTempName(t0.Add(time.Minute))
	l := &listing{users: []string{old1, live, "pma_thing", "pma_looks_like_one_but_is_not", old2, "pmauser", ""}}
	dropped, err := Admin{R: l}.CleanTemp(t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(dropped) != 2 || dropped[0] != old1 || dropped[1] != old2 {
		t.Errorf("dropped %v", dropped)
	}
	joined := strings.Join(l.sqls, "")
	if strings.Contains(joined, live) || strings.Contains(joined, "pma_thing") || strings.Contains(joined, "pmauser") {
		t.Errorf("a user that is still valid, or is not a temporary one, was dropped:\n%s", joined)
	}
}

func TestTheSweepKeepsGoingWhenOneDropFails(t *testing.T) {
	a1, _ := newTempName(t0.Add(-time.Hour))
	a2, _ := newTempName(t0.Add(-2 * time.Hour))
	l := &listing{users: []string{a1, a2}}
	l.failOn = a1
	dropped, err := Admin{R: l}.CleanTemp(t0)
	if err == nil || len(dropped) != 1 || dropped[0] != a2 {
		t.Errorf("one failure must be reported and the other user still dropped: %v %v", dropped, err)
	}
	if _, err := (Admin{R: failing{}}).CleanTemp(t0); err == nil {
		t.Error("an unreachable server must be reported")
	}
}

type failing struct{}

func (failing) Run(string) (string, error) { return "", errors.New("down") }
