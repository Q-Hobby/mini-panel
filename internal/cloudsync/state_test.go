package cloudsync

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestADownloadWhoseProcessIsGoneIsShownAsInterrupted(t *testing.T) {
	now := time.Now()
	running := FetchState{Stamp: "20261104-031500", At: now.Add(-10 * time.Minute).Unix(), Running: true}
	if e := running.Effective(now, true); !e.Running {
		t.Error("a download whose process holds the lock is running")
	}
	e := running.Effective(now, false)
	if e.Running || e.OK || e.Message == "" {
		t.Errorf("a download with no process must show as interrupted: %+v", e)
	}
	// a download that has only just been started has not taken the lock yet
	fresh := FetchState{At: now.Add(-2 * time.Second).Unix(), Running: true}
	if e := fresh.Effective(now, false); !e.Running {
		t.Error("a download that has just been asked for is still starting")
	}
	done := FetchState{At: now.Add(-time.Hour).Unix(), OK: true}
	if e := done.Effective(now, false); !e.OK {
		t.Error("a finished download stays as it was")
	}
}

func TestLockHeldReadsTheProcessInTheLockFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	if LockHeld(p, func(int) bool { return true }) {
		t.Error("no file, no lock")
	}
	_ = os.WriteFile(p, []byte("4242\n"), 0o600)
	var asked int
	if !LockHeld(p, func(pid int) bool { asked = pid; return true }) || asked != 4242 {
		t.Errorf("asked about %d", asked)
	}
	if LockHeld(p, func(int) bool { return false }) {
		t.Error("a process that is gone holds nothing")
	}
	for _, junk := range []string{"", "abc", "-5", "0"} {
		_ = os.WriteFile(p, []byte(junk), 0o600)
		if LockHeld(p, func(int) bool { return true }) {
			t.Errorf("%q is not a process", junk)
		}
	}
}
