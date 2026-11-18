package tune

import (
	"strings"
	"testing"
)

func TestSmallHost(t *testing.T) {
	p := New(Input{RAMMB: 1024, CPUs: 1})
	want := Profile{
		Tier: "small", SwapMB: 1024, JournalMaxMB: 50,
		BufferPoolMB: 128, RedoMB: 48, MaxConnections: 40, TmpTableMB: 16, PerfSchema: false,
		PHPMaxChildren: 8, PHPMemoryLimitMB: 256, OpcacheMB: 64,
		NginxWorkers: 1, WorkerConnections: 1024,
	}
	if p != want {
		t.Errorf("got %+v\nwant %+v", p, want)
	}
}

func TestLargeHostIsCapped(t *testing.T) {
	p := New(Input{RAMMB: 8192, CPUs: 4})
	if p.BufferPoolMB != 1024 || p.MaxConnections != 300 || p.TmpTableMB != 128 {
		t.Errorf("db values: %+v", p)
	}
	if p.PHPMaxChildren != 64 || p.OpcacheMB != 256 || !p.PerfSchema || p.NginxWorkers != 4 {
		t.Errorf("php/nginx values: %+v", p)
	}
}

func TestBelowMinimumIsRaised(t *testing.T) {
	if got, want := New(Input{RAMMB: 100, CPUs: 0}), New(Input{RAMMB: 512, CPUs: 1}); got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestDBConfEngines(t *testing.T) {
	p := New(Input{RAMMB: 1024, CPUs: 1})
	my, err := p.DBConf("mariadb")
	if err != nil || !strings.Contains(my, "innodb_log_file_size = 48M") || !strings.Contains(my, "performance_schema = OFF") {
		t.Errorf("mariadb conf: %q err=%v", my, err)
	}
	ms, err := p.DBConf("mysql")
	if err != nil || !strings.Contains(ms, "innodb_redo_log_capacity = 48M") || strings.Contains(ms, "innodb_log_file_size") {
		t.Errorf("mysql conf: %q err=%v", ms, err)
	}
	if _, err := p.DBConf("postgres"); err == nil {
		t.Error("unknown engine must return an error")
	}
}

func TestPHPPool(t *testing.T) {
	out := New(Input{RAMMB: 1024, CPUs: 1}).PHPPool("a.com", "acom", "/run/php/a.sock")
	for _, s := range []string{"[a.com]", "user = acom", "listen = /run/php/a.sock", "pm = ondemand", "pm.max_children = 8"} {
		if !strings.Contains(out, s) {
			t.Errorf("pool missing %q:\n%s", s, out)
		}
	}
}
