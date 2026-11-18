package firewall

import (
	"strings"
	"testing"
)

func TestPorts(t *testing.T) {
	for _, ok := range []string{"", "1", "80", "65535", "8000-8100", "22-22"} {
		if err := ValidPort(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"0", "65536", "-1", "80-", "-80", "8100-8000", "a", "80 ", "08", "80,443", "80;drop", "1e3", "80-90-100", "٨٠"} {
		if err := ValidPort(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestSources(t *testing.T) {
	for in, want := range map[string]string{"": "", "203.0.113.7": "203.0.113.7", "203.0.113.0/24": "203.0.113.0/24", "2001:db8::1": "2001:db8::1", "2001:db8::/32": "2001:db8::/32", "203.0.113.9/24": "203.0.113.0/24", "::ffff:203.0.113.7": "203.0.113.7"} {
		got, _, err := ParseSource(in)
		if err != nil || got != want {
			t.Errorf("%q: %q %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"example.com", "1.2.3", "1.2.3.4/33", "1.2.3.4 ; drop", "1.2.3.4\n", "fe80::1%eth0", "any", "0.0.0.0/x", "{1.2.3.4}"} {
		if _, _, err := ParseSource(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestRuleChecks(t *testing.T) {
	ok := Rule{Action: Allow, Proto: "tcp", Port: "8080", Comment: "  app  "}
	got, err := ValidRule(ok)
	if err != nil || got.Comment != "app" {
		t.Errorf("%+v %v", got, err)
	}
	for name, r := range map[string]Rule{
		"an action that does not exist":  {Action: "reject", Proto: "tcp", Port: "80"},
		"a protocol that does not exist": {Action: Allow, Proto: "icmp", Port: "80"},
		"allow everything":               {Action: Allow, Proto: "both"},
		"block everyone":                 {Action: Deny, Proto: "both"},
		"a comment with a line break":    {Action: Allow, Proto: "tcp", Port: "80", Comment: "a\nb"},
		"a long comment":                 {Action: Allow, Proto: "tcp", Port: "80", Comment: strings.Repeat("a", MaxComment+1)},
		"a source that is a name":        {Action: Deny, Proto: "both", Source: "evil.example"},
	} {
		if _, err := ValidRule(r); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if _, err := ValidRule(Rule{Action: Deny, Proto: "both", Source: "203.0.113.7"}); err != nil {
		t.Errorf("blocking one address is the main use: %v", err)
	}
	if _, err := ValidRule(Rule{Action: Allow, Proto: "tcp", Source: "203.0.113.0/24"}); err != nil {
		t.Errorf("opening every port to one network is a choice a person may make: %v", err)
	}
}

func TestTheScriptReplacesOnlyItsOwnTableAndKeepsTheOrder(t *testing.T) {
	out := Render(Locked{SSH: []int{22, 2222, 22}, Panel: []int{2610, 2611}}, []Rule{
		{Action: Allow, Proto: "tcp", Port: "80"},
		{Action: Deny, Proto: "both", Source: "203.0.113.7"},
		{Action: Allow, Proto: "both", Port: "5000-5010", Source: "198.51.100.0/24"},
		{Action: Deny, Proto: "tcp", Port: "3306", Source: "2001:db8::/32"},
		{Action: Allow, Proto: "udp", Port: "53"},
	})
	for _, want := range []string{"table inet mpn\ndelete table inet mpn\n\ntable inet mpn {", "policy drop;", "iif lo accept", "ct state established,related accept",
		"ip6 nexthdr icmpv6 accept", "tcp dport { 22, 2222 } accept", "tcp dport { 2610, 2611 } accept",
		"ip saddr 203.0.113.7 drop", "tcp dport 80 accept", "ip saddr 198.51.100.0/24 tcp dport 5000-5010 accept", "ip saddr 198.51.100.0/24 udp dport 5000-5010 accept",
		"ip6 saddr 2001:db8::/32 tcp dport 3306 drop", "udp dport 53 accept"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	idx := func(s string) int { return strings.Index(out, s) }
	if !(idx("iif lo accept") < idx("ip saddr 203.0.113.7 drop") && idx("ip saddr 203.0.113.7 drop") < idx("tcp dport { 22, 2222 } accept") &&
		idx("tcp dport { 22, 2222 } accept") < idx("tcp dport 80 accept")) {
		t.Errorf("loopback, then blocked sources, then the ports that never close, then the rules that allow:\n%s", out)
	}
	if strings.Contains(out, "flush ruleset") {
		t.Error("the tables of other programs must stay")
	}
	if strings.Count(out, "delete table") != 1 {
		t.Error("only our own table is deleted")
	}
}

func TestWithoutRulesTheWayInStaysOpen(t *testing.T) {
	out := Render(Locked{SSH: []int{22}, Panel: []int{2610}}, nil)
	for _, want := range []string{"tcp dport { 22 } accept", "tcp dport { 2610 } accept", "policy drop;"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	// a comment is for the person; it never reaches the firewall, so it cannot break the script
	out = Render(Locked{SSH: []int{22}}, []Rule{{Action: Allow, Proto: "tcp", Port: "80", Comment: `evil" accept; drop`}})
	if strings.Contains(out, "evil") || strings.Contains(out, "comment") {
		t.Errorf("comments must not be written:\n%s", out)
	}
	if Unload != "table inet mpn\ndelete table inet mpn\n" {
		t.Error("unload")
	}
	if d := Defaults(); len(d) != 2 || d[0].Port != "80" || d[1].Port != "443" {
		t.Errorf("%+v", d)
	}
}

func TestCovers(t *testing.T) {
	for _, c := range []struct {
		src, ip string
		want    bool
	}{{"", "1.2.3.4", true}, {"1.2.3.4", "1.2.3.4", true}, {"1.2.3.4", "1.2.3.5", false}, {"1.2.3.0/24", "1.2.3.99", true}, {"1.2.3.0/24", "1.2.4.1", false},
		{"2001:db8::/32", "2001:db8::5", true}, {"2001:db8::/32", "1.2.3.4", false}, {"1.2.3.4", "not an ip", false}} {
		if got := Covers(c.src, c.ip); got != c.want {
			t.Errorf("%q covers %q: %v", c.src, c.ip, got)
		}
	}
}
