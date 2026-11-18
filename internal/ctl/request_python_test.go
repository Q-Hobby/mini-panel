package ctl

import (
	"reflect"
	"testing"
)

func TestAPythonSiteIsAddedWithItsInterpreterAndEntry(t *testing.T) {
	got, err := Request{Op: "site.add", Site: "py", Kind: "python", Hosts: []string{"py.example.com"}, Python: "3.12", Entry: "-m myapp"}.Argv()
	want := []string{"site", "add", "-name", "py", "-kind", "python", "-user", "py", "-entry", "-m myapp", "-python", "3.12", "-host", "py.example.com"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("%v\n%v\n%v", got, want, err)
	}
	// without a version mpn picks the system's python3, and the entry defaults to app.py
	got, err = Request{Op: "site.add", Site: "py", Kind: "python", Hosts: []string{"py.example.com"}}.Argv()
	if err != nil || !reflect.DeepEqual(got, []string{"site", "add", "-name", "py", "-kind", "python", "-user", "py", "-entry", "app.py", "-host", "py.example.com"}) {
		t.Errorf("defaults: %v %v", got, err)
	}
	for _, bad := range []Request{
		{Op: "site.add", Site: "py", Kind: "python", Hosts: []string{"py.example.com"}, Python: "3.12; id"},
		{Op: "site.add", Site: "py", Kind: "python", Hosts: []string{"py.example.com"}, Entry: "app.py\nx"},
		{Op: "site.add", Site: "py", Kind: "python", Hosts: []string{"py.example.com"}, Entry: "-c import os"},
	} {
		if _, err := bad.Argv(); err == nil {
			t.Errorf("%+v must be refused", bad)
		}
	}
}

func TestPythonSettingsBecomeFlagsAndAreChecked(t *testing.T) {
	got, err := Request{Op: "site.set", Site: "py", Kind: "python", RootSub: "src", Python: "3.11", Entry: "app.py", Port: 20007, Mem: 256}.Argv()
	want := []string{"site", "set", "-name", "py", "-root-sub", "src", "-python", "3.11", "-entry", "app.py", "-port", "20007", "-mem", "256", "-apply"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("%v\n%v\n%v", got, want, err)
	}
	ok := Request{Op: "site.set", Site: "py", Kind: "python", Python: "3.11", Entry: "app.py", Port: 20007}
	for name, mutate := range map[string]func(*Request){
		"a version that is not a version": func(r *Request) { r.Python = "latest" },
		"python 2":                        func(r *Request) { r.Python = "2.7" },
		"an entry that goes up":           func(r *Request) { r.Entry = "../x.py" },
		"a port the server uses":          func(r *Request) { r.Port = 3306 },
		"too little memory":               func(r *Request) { r.Mem = 8 },
		"a web root going up":             func(r *Request) { r.RootSub = "../x" },
	} {
		r := ok
		mutate(&r)
		if _, err := r.Argv(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}
