package ctl

import (
	"reflect"
	"strings"
	"testing"

	"mpn/internal/vhost"
)

func TestVhostOperationsAreFixedCommandsAndTheTextGoesOnStandardInput(t *testing.T) {
	if IsWrite("site.vhost") || !IsWrite("site.vhostset") || !IsWrite("site.vhostreset") {
		t.Fatal("reads and writes are mixed up")
	}
	got, _ := Request{Op: "site.vhost", Site: "blog"}.Argv()
	if !reflect.DeepEqual(got, []string{"site", "vhost", "-name", "blog"}) {
		t.Errorf("%v", got)
	}
	got, _ = Request{Op: "site.vhostreset", Site: "blog"}.Argv()
	if !reflect.DeepEqual(got, []string{"site", "vhost", "-name", "blog", "-reset", "-apply"}) {
		t.Errorf("%v", got)
	}
	r := Request{Op: "site.vhostset", Site: "blog", Vhost: "server { listen 80; # SECRET\n}"}
	argv, err := r.Argv()
	if err != nil || !reflect.DeepEqual(argv, []string{"site", "vhost", "-name", "blog", "-stdin", "-apply"}) {
		t.Fatalf("%v %v", argv, err)
	}
	for _, a := range argv {
		if strings.Contains(a, "SECRET") {
			t.Error("the text must not be an argument")
		}
	}
	if r.Stdin() != r.Vhost {
		t.Error("the text is the standard input, unchanged")
	}
	if (Request{Op: "site.vhost", Site: "blog", Vhost: "x"}).Stdin() != "" {
		t.Error("only site.vhostset sends text")
	}
}

func TestAVhostThatIsEmptyHugeOrHasANulIsRefused(t *testing.T) {
	for name, r := range map[string]Request{
		"empty":    {Op: "site.vhostset", Site: "blog", Vhost: " \n"},
		"huge":     {Op: "site.vhostset", Site: "blog", Vhost: strings.Repeat("a", vhost.MaxCustom+1)},
		"a nul":    {Op: "site.vhostset", Site: "blog", Vhost: "server {\x00}"},
		"bad site": {Op: "site.vhostset", Site: "../x", Vhost: "server {}"},
	} {
		if _, err := r.Argv(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}
