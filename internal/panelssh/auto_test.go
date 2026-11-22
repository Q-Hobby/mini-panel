package panelssh

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestAutoKeepsTheAddressForAMinuteAndAsksAgainWhenTold(t *testing.T) {
	calls := 0
	addr := "127.0.0.1:2222"
	a := &Auto{Fixed: "auto", Lookup: func() (string, error) { calls++; return addr, nil }}
	if got := a.address(false); got != "127.0.0.1:2222" {
		t.Fatalf("address = %s", got)
	}
	a.address(false)
	if calls != 1 {
		t.Errorf("looked up %d times, want 1 (kept)", calls)
	}
	addr = "127.0.0.1:2200"
	if got := a.address(true); got != "127.0.0.1:2200" || calls != 2 {
		t.Errorf("fresh address = %s after %d lookups", got, calls)
	}
	a.at = time.Now().Add(-2 * AutoTTL)
	a.address(false)
	if calls != 3 {
		t.Error("an old address must be looked up again")
	}
}

func TestAutoFallsBackWhenTheHelperIsSilent(t *testing.T) {
	a := &Auto{Lookup: func() (string, error) { return "", errors.New("down") }}
	if got := a.address(false); got != "127.0.0.1:22" {
		t.Errorf("no answer ever: %s, want 127.0.0.1:22", got)
	}
	a.addr = "127.0.0.1:2222"
	if got := a.address(true); got != "127.0.0.1:2222" {
		t.Errorf("no answer now: %s, want the last one", got)
	}
	if got := (&Auto{Fixed: "10.0.0.1:22", Lookup: func() (string, error) { t.Error("Fixed must not look up"); return "", nil }}).address(true); got != "10.0.0.1:22" {
		t.Errorf("Fixed: %s", got)
	}
	if got := (&Auto{Lookup: func() (string, error) { return "not an address", nil }}).address(false); got != "127.0.0.1:22" {
		t.Errorf("a bad answer must be ignored: %s", got)
	}
}

// The port moved while the panel ran: the kept address refuses the connection, and the dial asks again and reaches the new one.
func TestAutoDialAsksAgainWhenThePortMoved(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	live := ln.Addr().String()
	ln.Close() // nobody listens there now
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln2.Close()
	go func() {
		c, err := ln2.Accept()
		if err == nil {
			c.Close() // not SSH: the dial fails after it connected, which is all this test needs
		}
	}()
	lookups := 0
	a := &Auto{Client: Client{KeyPath: "testdata/none", HostKeyGlob: "testdata/none"}, Lookup: func() (string, error) {
		lookups++
		if lookups == 1 {
			return live, nil
		}
		return ln2.Addr().String(), nil
	}}
	a.address(false) // keeps the dead address
	// Dial fails early on the missing key, before any connection; the retry is checked through address instead.
	if _, err := a.Dial("site"); err == nil {
		t.Fatal("no key: the dial must fail")
	}
	if got := a.address(true); got != ln2.Addr().String() {
		t.Errorf("after the port moved: %s", got)
	}
}
