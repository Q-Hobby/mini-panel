package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// A database created by the first release (schema version 1, no tls column) must upgrade without losing sites.
func TestUpgradeFromVersion1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		schemaV1, "PRAGMA user_version = 1",
		`INSERT INTO sites (name, kind, system_user, root_dir, php_version, created_at) VALUES ('old','php','old','/home/old/htdocs','8.4',1)`,
		`INSERT INTO hostnames (hostname, site_id, position) VALUES ('old.example.com', 1, 0)`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetSite("old")
	if err != nil || got.TLS || len(got.Hostnames) != 1 || got.PHPVersion != "8.4" {
		t.Fatalf("site after upgrade: %+v err=%v", got, err)
	}
	if err := s.SetTLS("old", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetSite("old"); !got.TLS {
		t.Error("TLS flag was not saved")
	}
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != schemaVersion {
		t.Errorf("user_version=%d err=%v", v, err)
	}
	if err := s.SetTLS("missing", true); err == nil {
		t.Error("unknown site must be an error")
	}
}
