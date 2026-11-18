package executor

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mpn/internal/installer"
)

type fake struct {
	ops    []string
	failOn string
	rolled bool
}

func (f *fake) Run(a []string) error {
	s := strings.Join(a, " ")
	f.ops = append(f.ops, "run "+s)
	if s == f.failOn {
		return errors.New("boom")
	}
	return nil
}

func (f *fake) WriteFile(p string, c []byte, m uint32) error {
	f.ops = append(f.ops, "write "+p)
	return nil
}

func (f *fake) Rollback() error { f.rolled = true; return nil }

var plan = installer.Plan{
	{Name: "one", Actions: []installer.Action{installer.Run("a"), installer.WriteFile("/x/conf", "c", 0o644)}},
	{Name: "two", Actions: []installer.Action{installer.Run("nginx", "-t"), installer.Run("reload")}},
}

func TestApplyRunsInOrder(t *testing.T) {
	f := &fake{}
	if err := Apply(plan, f, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(f.ops, "|"), "run a|write /x/conf|run nginx -t|run reload"; got != want {
		t.Errorf("ops=%s want %s", got, want)
	}
	if f.rolled {
		t.Error("no rollback expected on success")
	}
}

func TestApplyStopsAndRollsBack(t *testing.T) {
	f := &fake{failOn: "nginx -t"}
	err := Apply(plan, f, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), `step "two" failed`) {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(strings.Join(f.ops, "|"), "run reload") {
		t.Errorf("must stop at the first failure: %v", f.ops)
	}
	if !f.rolled {
		t.Error("expected a rollback")
	}
}

func TestOSWriteAndRollback(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "etc", "app.conf")
	fresh := filepath.Join(dir, "etc", "sub", "new.conf")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := &OS{Out: &bytes.Buffer{}}
	if err := o.WriteFile(existing, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := o.WriteFile(fresh, []byte("brand new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(existing); string(b) != "new" {
		t.Errorf("existing=%q", b)
	}
	if err := o.Rollback(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(existing); string(b) != "old" {
		t.Errorf("after rollback existing=%q", b)
	}
	if _, err := os.Stat(fresh); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("new file should be removed, stat err=%v", err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(existing)); len(entries) != 2 { // app.conf and sub/
		t.Errorf("leftover temp files: %v", entries)
	}
}
