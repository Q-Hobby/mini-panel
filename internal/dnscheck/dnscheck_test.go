package dnscheck

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func deps(records map[string][]string, local string, probeOK map[string]bool) Deps {
	return Deps{
		Lookup: func(_ context.Context, h string) ([]net.IP, error) {
			if h == "broken.example" {
				return nil, errors.New("timeout")
			}
			var out []net.IP
			for _, s := range records[h] {
				out = append(out, net.ParseIP(s))
			}
			return out, nil
		},
		Local: func(ip net.IP) bool { return ip.String() == local },
		Probe: func(_ context.Context, ip net.IP, host string) bool { return probeOK[host+"@"+ip.String()] },
	}
}

func TestNamesThatLeadHerePass(t *testing.T) {
	d := deps(map[string][]string{"a.example": {"203.0.113.5"}, "www.a.example": {"203.0.113.5", "2001:db8::5"}}, "203.0.113.5", map[string]bool{"www.a.example@2001:db8::5": true})
	if f := Check(context.Background(), d, []string{"a.example", "www.a.example"}); f != nil {
		t.Errorf("%v", f)
	}
}

func TestAWrongNoneOrBrokenRecordFailsWithTheName(t *testing.T) {
	d := deps(map[string][]string{"other.example": {"198.51.100.9"}, "half.example": {"203.0.113.5", "2001:db8::99"}}, "203.0.113.5", nil)
	for name, want := range map[string]string{
		"other.example":  "points to 198.51.100.9",
		"half.example":   "points to 2001:db8::99",
		"empty.example":  "no A or AAAA record",
		"broken.example": "could not be looked up",
	} {
		f := Check(context.Background(), d, []string{name})
		if f == nil || f.Name != name || !strings.Contains(f.Error(), want) || !strings.HasPrefix(f.Error(), Marker) {
			t.Errorf("%s: %v", name, f)
		}
	}
}

func TestTheProbeSettlesAMachineBehindNAT(t *testing.T) {
	// the public address is on no network card, but the web server at that address answers with our file
	d := deps(map[string][]string{"nat.example": {"198.51.100.7"}}, "10.0.0.4", map[string]bool{"nat.example@198.51.100.7": true})
	if f := Check(context.Background(), d, []string{"nat.example"}); f != nil {
		t.Errorf("%v", f)
	}
	d.Probe = nil
	if f := Check(context.Background(), d, []string{"nat.example"}); f == nil {
		t.Error("without a probe an address that is not local does not lead here")
	}
}

func TestTheFirstFailingNameIsReported(t *testing.T) {
	d := deps(map[string][]string{"ok.example": {"203.0.113.5"}, "bad.example": {"198.51.100.1"}}, "203.0.113.5", nil)
	if f := Check(context.Background(), d, []string{"ok.example", "bad.example"}); f == nil || f.Name != "bad.example" {
		t.Errorf("%v", f)
	}
}
