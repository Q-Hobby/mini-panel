package ctl

import (
	"reflect"
	"strings"
	"testing"
)

func TestPortOperationsAreFixedCommands(t *testing.T) {
	if IsWrite("port.status") || IsWrite("port.sshaddr") || !IsWrite("port.set") || !IsWrite("port.finish") || !IsWrite("port.cancel") {
		t.Fatal("reads and writes are mixed up")
	}
	for _, c := range []struct {
		r    Request
		want []string
	}{
		{Request{Op: "port.status"}, []string{"port", "status", "-json"}},
		{Request{Op: "port.sshaddr"}, []string{"port", "ssh-addr"}},
		{Request{Op: "port.set", SvcKey: "ssh", Port: 2222}, []string{"port", "set", "-service=ssh", "-port=2222", "-apply"}},
		{Request{Op: "port.set", SvcKey: "webmail", Port: 8443}, []string{"port", "set", "-service=webmail", "-port=8443", "-apply"}},
		{Request{Op: "port.finish", SvcKey: "panel"}, []string{"port", "finish", "-service=panel", "-apply"}},
		{Request{Op: "port.cancel", SvcKey: "ssh"}, []string{"port", "cancel", "-service=ssh", "-apply"}},
	} {
		if got, err := c.r.Argv(); err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: %v %v", c.r, got, err)
		}
	}
	for name, r := range map[string]Request{
		"mysql":          {Op: "port.set", SvcKey: "mysql", Port: 3307},
		"a flag":         {Op: "port.set", SvcKey: "-apply", Port: 2222},
		"port 0":         {Op: "port.set", SvcKey: "ssh"},
		"a web port":     {Op: "port.set", SvcKey: "panel", Port: 443},
		"finish webmail": {Op: "port.finish", SvcKey: "webmail"},
	} {
		if _, err := r.Argv(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestAPortChangeIsLoggedAndSSHAndThePanelAreImportant(t *testing.T) {
	r := Request{Op: "port.set", SvcKey: "ssh", Port: 2222}
	if d := r.AuditDetail(); !strings.Contains(d, "svc=ssh") || !strings.Contains(d, "port=2222") {
		t.Errorf("detail: %s", d)
	}
	if !r.Dangerous() || !(Request{Op: "port.finish", SvcKey: "panel"}).Dangerous() || (Request{Op: "port.set", SvcKey: "pma", Port: 8443}).Dangerous() {
		t.Error("Dangerous is wrong for ports")
	}
}
