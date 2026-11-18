package ctl

import (
	"reflect"
	"strings"
	"testing"
)

func TestTheDatabaseBrowserOpsAreKnownAndWrite(t *testing.T) {
	for _, op := range []string{"db.pma", "db.pmadrop", "db.pmaclean"} {
		if !Known(op) || !IsWrite(op) {
			t.Errorf("%s must be a known operation that changes the machine (the helper runs those one at a time)", op)
		}
	}
}

func TestOpeningTheBrowserNeedsAValidDatabase(t *testing.T) {
	got, err := Request{Op: "db.pma", Name: "my_blog"}.Argv()
	if err != nil || !reflect.DeepEqual(got, []string{"db", "pma", "-name", "my_blog", "-apply"}) {
		t.Errorf("%v %v", got, err)
	}
	for _, bad := range []string{"", "Blog", "a b", "x'; DROP DATABASE mysql; --", "a`b", "-apply", "../x", strings.Repeat("a", 33)} {
		if _, err := (Request{Op: "db.pma", Name: bad}).Argv(); err == nil {
			t.Errorf("database %q must be refused", bad)
		}
	}
}

// Only a user shaped like a temporary one may be dropped: a request can name nothing else, so it cannot be used to
// drop a real user of the database server.
func TestOnlyTemporaryUsersCanBeDroppedThroughTheHelper(t *testing.T) {
	got, err := Request{Op: "db.pmadrop", User: "pma_rdwxyz_ab12cd34"}.Argv()
	if err != nil || !reflect.DeepEqual(got, []string{"db", "pmadrop", "-user", "pma_rdwxyz_ab12cd34", "-apply"}) {
		t.Errorf("%v %v", got, err)
	}
	for _, bad := range []string{"", "root", "mysql", "blog", "pma_", "pma_x", "pma_rdwxyz_ab12cd3", "pma_rdwxyz_ab12cd345", "PMA_rdwxyz_ab12cd34",
		"pma_rdwxyz_ab12cd34'", "pma_rdwxyz_ab12cd34; DROP USER root", "pma_rdwxyz_ab12cd34\n", "-apply", "pma_rdwx yz_ab12cd34"} {
		if _, err := (Request{Op: "db.pmadrop", User: bad}).Argv(); err == nil {
			t.Errorf("user %q must be refused", bad)
		}
	}
}

func TestTheSweepTakesNoValues(t *testing.T) {
	got, err := Request{Op: "db.pmaclean", Name: "ignored", User: "root"}.Argv()
	if err != nil || !reflect.DeepEqual(got, []string{"db", "pmaclean", "-apply"}) {
		t.Errorf("%v %v", got, err)
	}
}
