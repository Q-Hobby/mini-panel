package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The database lists every site and its account; other accounts on the machine must not be able to read it.
func TestTheDatabaseFilesArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes mean nothing on Windows; checked on the servers")
	}
	path := filepath.Join(t.TempDir(), "mpn.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil { // as left by an earlier version
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		fi, err := os.Stat(path + suffix)
		if err != nil {
			continue // the journal files may not exist yet
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s has mode %v; it must be readable by its owner only", filepath.Base(path+suffix), fi.Mode().Perm())
		}
	}
	fresh := filepath.Join(t.TempDir(), "new.db")
	st2, err := Open(fresh)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	if fi, _ := os.Stat(fresh); fi.Mode().Perm() != 0o600 {
		t.Errorf("a new database has mode %v, want 0600", fi.Mode().Perm())
	}
}
