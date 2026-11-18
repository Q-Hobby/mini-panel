package ctl

import (
	"reflect"
	"strings"
	"testing"
)

func TestFirewallOperationsAreFixedCommands(t *testing.T) {
	if IsWrite("firewall.status") || !IsWrite("firewall.enable") || !IsWrite("firewall.disable") || !IsWrite("firewall.add") || !IsWrite("firewall.del") {
		t.Fatal("reads and writes are mixed up")
	}
	for op, want := range map[string][]string{
		"firewall.status":  {"firewall", "status", "-json"},
		"firewall.enable":  {"firewall", "enable", "-apply"},
		"firewall.disable": {"firewall", "disable", "-apply"},
	} {
		if got, err := (Request{Op: op}).Argv(); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v %v", op, got, err)
		}
	}
	got, _ := Request{Op: "firewall.del", RuleID: 7}.Argv()
	if !reflect.DeepEqual(got, []string{"firewall", "remove", "-id", "7", "-apply"}) {
		t.Errorf("%v", got)
	}
	if _, err := (Request{Op: "firewall.del"}).Argv(); err == nil {
		t.Error("rule zero must be refused")
	}
}

func TestARuleBecomesFlagsJoinedToTheirValues(t *testing.T) {
	got, err := Request{Op: "firewall.add", FwAction: "deny", FwProto: "both", FwSource: "203.0.113.9/24", FwComment: " -x spammer ", Client: "198.51.100.4"}.Argv()
	want := []string{"firewall", "add", "-action=deny", "-proto=both", "-port=", "-source=203.0.113.0/24", "-comment=-x spammer", "-client=198.51.100.4", "-apply"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("%v\n%v\n%v", got, want, err)
	}
	for name, r := range map[string]Request{
		"an action that does not exist": {Op: "firewall.add", FwAction: "reject", FwProto: "tcp", FwPort: "80"},
		"a port with a command":         {Op: "firewall.add", FwAction: "allow", FwProto: "tcp", FwPort: "80; flush ruleset"},
		"a source that is a name":       {Op: "firewall.add", FwAction: "deny", FwProto: "both", FwSource: "evil.example"},
		"a comment with a line break":   {Op: "firewall.add", FwAction: "allow", FwProto: "tcp", FwPort: "80", FwComment: "a\nb"},
		"allow everything":              {Op: "firewall.add", FwAction: "allow", FwProto: "both"},
		"a client that is not an IP":    {Op: "firewall.add", FwAction: "allow", FwProto: "tcp", FwPort: "80", Client: "me"},
	} {
		if _, err := r.Argv(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if argv, _ := (Request{Op: "firewall.add", FwAction: "allow", FwProto: "tcp", FwPort: "80", FwComment: "SECRET"}).Argv(); !strings.Contains(strings.Join(argv, " "), "-comment=SECRET") {
		t.Error("the comment is an argument of its own flag")
	}
}
