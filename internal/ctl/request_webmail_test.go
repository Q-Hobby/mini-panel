package ctl

import (
	"reflect"
	"testing"
)

func TestSiteWebmailChangesTheMachineAndNeedsAValidSite(t *testing.T) {
	if !Known("site.webmail") || !IsWrite("site.webmail") {
		t.Fatal("it changes a file of nginx, so it is a write")
	}
	for _, c := range []struct {
		on   bool
		want []string
	}{
		{true, []string{"site", "webmail", "-name", "blog", "-on=true", "-apply"}},
		{false, []string{"site", "webmail", "-name", "blog", "-on=false", "-apply"}},
	} {
		got, err := Request{Op: "site.webmail", Site: "blog", Enable: c.on}.Argv()
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v %v", got, err)
		}
	}
	for _, bad := range []string{"", "Blog", "../x", "a b", "blog;id", "-name"} {
		if _, err := (Request{Op: "site.webmail", Site: bad, Enable: true}).Argv(); err == nil {
			t.Errorf("site %q must be refused", bad)
		}
	}
}
