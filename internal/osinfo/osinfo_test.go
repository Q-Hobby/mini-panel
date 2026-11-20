package osinfo

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		name, in, codename string
		wantErr            bool
	}{
		{"debian12", "ID=debian\nVERSION_ID=\"12\"\n", "bookworm", false},
		{"debian13", "ID=debian\nVERSION_ID=\"13\"\n", "trixie", false},
		{"ubuntu2204", "ID=ubuntu\nVERSION_ID=\"22.04\"\n", "jammy", false},
		{"ubuntu2604", "NAME=Ubuntu\nID=ubuntu\nVERSION_ID=\"26.04\"\n", "resolute", false},
		{"ubuntu2404", "ID=ubuntu\nVERSION_ID=\"24.04\"\n", "noble", false},
		{"debian11", "ID=debian\nVERSION_ID=\"11\"\n", "", true},
		{"ubuntu2510", "ID=ubuntu\nVERSION_ID=\"25.10\"\n", "", true},
		{"empty", "", "", true},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if (err != nil) != c.wantErr {
			t.Fatalf("%s: err=%v wantErr=%v", c.name, err, c.wantErr)
		}
		if got.Codename != c.codename {
			t.Errorf("%s: codename=%q want %q", c.name, got.Codename, c.codename)
		}
	}
}

// Every supported system takes nginx from nginx.org; Ubuntu 26.04 too since nginx.org packages it (1.30.7, November 2026).
func TestEverySupportedSystemTakesNginxFromNginxOrg(t *testing.T) {
	for _, s := range Supported() {
		if !s.NginxOrg() {
			t.Errorf("%s %s does not take nginx from nginx.org", s.ID, s.Version)
		}
	}
	if (Target{}).NginxOrg() {
		t.Error("a system the program does not know keeps the nginx of its distribution")
	}
}

func TestRepos(t *testing.T) {
	u := Target{ID: "ubuntu", Version: "24.04", Codename: "noble"}
	if got := u.MariaDBRepo("11.8"); got != "https://mirror.mariadb.org/repo/11.8/ubuntu" {
		t.Errorf("MariaDBRepo=%q", got)
	}
	if got := u.MySQLRepo(); got != "https://repo.mysql.com/apt/ubuntu" {
		t.Errorf("MySQLRepo=%q", got)
	}
}
