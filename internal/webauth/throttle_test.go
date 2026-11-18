package webauth

import (
	"testing"
	"time"
)

func clockedThrottle() (*Throttle, *time.Time) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	t := NewThrottle()
	t.now = func() time.Time { return now }
	return t, &now
}

func TestThrottleLocksAfterFreeFailuresAndGrows(t *testing.T) {
	th, now := clockedThrottle()
	for i := 0; i < 5; i++ {
		if th.Wait("1.2.3.4") != 0 {
			t.Fatalf("locked after only %d failures", i)
		}
		th.Fail("1.2.3.4")
	}
	if th.Wait("1.2.3.4") != 0 {
		t.Error("the fifth failure must not lock yet")
	}
	th.Fail("1.2.3.4") // sixth
	if w := th.Wait("1.2.3.4"); w != 30*time.Second {
		t.Errorf("first lock %v, want 30s", w)
	}
	if th.Wait("5.6.7.8") != 0 {
		t.Error("another address must not be affected")
	}
	*now = now.Add(31 * time.Second)
	th.Fail("1.2.3.4")
	if w := th.Wait("1.2.3.4"); w != time.Minute {
		t.Errorf("second lock %v, want 1m", w)
	}
	for i := 0; i < 30; i++ {
		*now = now.Add(time.Hour)
		th.Fail("1.2.3.4")
	}
	if w := th.Wait("1.2.3.4"); w != 15*time.Minute {
		t.Errorf("the lock must stop growing at 15m, got %v", w)
	}
}

// The case of the review: 16 tries sent at the same moment all passed Wait before the first one failed. Admit counts a try when it is
// let through, so only the free ones get in.
func TestAdmitCountsATryWhenItIsLetThrough(t *testing.T) {
	th, now := clockedThrottle()
	var releases []func()
	for i := 0; i < 16; i++ {
		if rel, d := th.Admit("1.2.3.4"); d == 0 {
			releases = append(releases, rel)
		}
	}
	if len(releases) != 5 {
		t.Fatalf("%d tries let through at once, want the 5 free ones", len(releases))
	}
	for _, rel := range releases {
		th.Fail("1.2.3.4")
		rel()
	}
	// After the free ones: one at a time, and its failure locks.
	rel, d := th.Admit("1.2.3.4")
	if d != 0 {
		t.Fatal("the sixth try must be let through, alone")
	}
	if _, d := th.Admit("1.2.3.4"); d == 0 {
		t.Error("a second try beside it must wait")
	}
	th.Fail("1.2.3.4")
	rel()
	if _, d := th.Admit("1.2.3.4"); d != 30*time.Second {
		t.Errorf("after the sixth failure the lock is 30s, got %v", d)
	}
	*now = now.Add(31 * time.Second)
	if rel, d := th.Admit("1.2.3.4"); d != 0 {
		t.Error("when the lock is over one more try is allowed")
	} else {
		rel()
	}
	// A success clears everything, also while another try is being checked.
	rel1, _ := th.Admit("9.9.9.9")
	rel2, _ := th.Admit("9.9.9.9")
	th.Reset("9.9.9.9")
	rel1()
	rel2()
	rel2() // twice does no harm
	if _, d := th.Admit("9.9.9.9"); d != 0 {
		t.Error("after a success the address is free")
	}
}

func TestThrottleResetAndOverflow(t *testing.T) {
	th, _ := clockedThrottle()
	for i := 0; i < 8; i++ {
		th.Fail("a")
	}
	th.Reset("a")
	if th.Wait("a") != 0 {
		t.Error("a successful login must clear the lock")
	}
	th.Cap = 2
	th.Fail("x")
	th.Fail("y")
	for i := 0; i < 8; i++ {
		th.Fail("new-1") // the table is full, so these share one bucket
	}
	if th.Wait("new-2") == 0 {
		t.Error("when the table is full, unknown keys must share a lock (fail closed)")
	}
	if th.Wait("x") != 0 {
		t.Error("a tracked key with few failures must stay usable")
	}
}
