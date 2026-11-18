package distro

import (
	"slices"
	"strings"
	"testing"
)

func TestFromOSRelease(t *testing.T) {
	for content, want := range map[string]Family{
		"NAME=\"AlmaLinux\"\nID=\"almalinux\"\nVERSION_ID=\"9.8\"\n": EL,
		"ID=\"rocky\"\nVERSION_ID=\"10.0\"\n":                        EL,
		"ID=\"centos\"\nVERSION_ID=\"9\"\n":                          EL,
		"ID=debian\nVERSION_ID=\"13\"\n":                             Debian,
		"ID=ubuntu\nID_LIKE=debian\nVERSION_ID=\"24.04\"\n":          Debian,
		"":                                      Debian,
		"ID_LIKE=\"rhel centos fedora\"\n":      Debian, // only ID counts
		"  ID = almalinux\n":                    Debian, // not the format of os-release
		"ID='rocky'\n":                          EL,
		"PRETTY_NAME=\"Rocky Linux\"\nID=rocky": EL,
	} {
		if got := FromOSRelease(content); got != want {
			t.Errorf("%q: %v, want %v", content, got, want)
		}
	}
}

func use(t *testing.T, f Family) {
	t.Helper()
	SetForTest(f)
	t.Cleanup(func() { SetForTest(Debian) })
}

// The values on Debian are the paths and names the panel wrote before this package existed.
func TestDebianKeepsItsPaths(t *testing.T) {
	use(t, Debian)
	for _, c := range [][2]string{
		{WebUser(), "www-data"},
		{LogGroup(), "adm"},
		{NginxModuleDir(), "/usr/lib/nginx/modules"},
		{PHPPoolDir("8.4"), "/etc/php/8.4/fpm/pool.d/"},
		{PHPPoolDir("7.3"), "/etc/php/7.3/fpm/pool.d/"},
		{PHPConfDir("8.4"), "/etc/php/8.4/fpm/conf.d/"},
		{PHPDefaultPool("8.4"), "/etc/php/8.4/fpm/pool.d/www.conf"},
		{PHPService("8.4"), "php8.4-fpm"},
		{PHPService("7.0"), "php7.0-fpm"},
		{PHPFPMBinary("8.4"), "/usr/sbin/php-fpm8.4"},
		{PHPCLIBinary("8.4"), "/usr/bin/php8.4"},
		{DBConfFile("mariadb"), "/etc/mysql/mariadb.conf.d/99-mpn.cnf"},
		{DBConfFile("mysql"), "/etc/mysql/conf.d/99-mpn.cnf"},
		{DBService("mariadb"), "mariadb"},
		{DBService("mysql"), "mysql"},
		{DBPackage("mariadb"), "mariadb-server"},
		{DBPackage("mysql"), "mysql-community-server"},
		{CronService(), "cron"},
		{CronPackage(), "cron"},
		{strings.Join(InstallCommand("a", "b"), " "), "env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 install -y a b"},
		{strings.Join(RemoveCommand("a"), " "), "env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 purge -y a"},
	} {
		if got, want := c[0], c[1]; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if OwnPHP("7.0") || IsEL() {
		t.Error("Debian has every PHP version as packages")
	}
	if got := PHPPackages("8.4", []string{"fpm", "cli", "mysql", "curl", "zip"}); !slices.Equal(got, []string{"php8.4-fpm", "php8.4-cli", "php8.4-mysql", "php8.4-curl", "php8.4-zip"}) {
		t.Errorf("packages: %v", got)
	}
	if !strings.Contains(RefreshScript(), "apt-get -o DPkg::Lock::Timeout=300 update") || !strings.Contains(InstalledScript("x"), "install ok installed") {
		t.Error("the Debian scripts use apt and dpkg")
	}
}

func TestELPaths(t *testing.T) {
	use(t, EL)
	for _, c := range [][2]string{
		{WebUser(), "nginx"},
		{LogGroup(), "root"},
		{NginxModuleDir(), "/usr/lib64/nginx/modules"},
		{PHPPoolDir("8.4"), "/etc/opt/remi/php84/php-fpm.d/"},
		{PHPConfDir("8.4"), "/etc/opt/remi/php84/php.d/"},
		{PHPDefaultPool("8.4"), "/etc/opt/remi/php84/php-fpm.d/www.conf"},
		{PHPService("8.4"), "php84-php-fpm"},
		{PHPService("7.4"), "php74-php-fpm"},
		{PHPFPMBinary("8.4"), "/opt/remi/php84/root/usr/sbin/php-fpm"},
		{PHPCLIBinary("8.4"), "/opt/remi/php84/root/usr/bin/php"},
		{PHPPoolDir("7.3"), "/opt/mpn/php/7.3/etc/php-fpm.d/"},
		{PHPConfDir("7.3"), "/opt/mpn/php/7.3/etc/conf.d/"},
		{PHPService("7.3"), "mpn-php73-fpm"},
		{PHPFPMBinary("7.3"), "/opt/mpn/php/7.3/sbin/php-fpm"},
		{PHPCLIBinary("7.0"), "/opt/mpn/php/7.0/bin/php"},
		{DBConfFile("mariadb"), "/etc/my.cnf.d/99-mpn.cnf"},
		{DBConfFile("mysql"), "/etc/my.cnf.d/99-mpn.cnf"},
		{DBService("mariadb"), "mariadb"},
		{DBService("mysql"), "mysqld"},
		{DBPackage("mariadb"), "MariaDB-server"},
		{DBPackage("mysql"), "mysql-community-server"},
		{CronService(), "crond"},
		{CronPackage(), "cronie"},
	} {
		if got, want := c[0], c[1]; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if !OwnPHP("7.3") || OwnPHP("7.4") || !IsEL() {
		t.Error("PHP before 7.4 comes from mpn-packages on EL, 7.4 and newer from Remi")
	}
	got := PHPPackages("8.4", []string{"fpm", "cli", "mysql", "mbstring", "xml", "curl", "zip", "gd"})
	want := []string{"php84-php-fpm", "php84-php-cli", "php84-php-mysqlnd", "php84-php-mbstring", "php84-php-xml", "php84-php-common", "php84-php-pecl-zip", "php84-php-gd"}
	if !slices.Equal(got, want) {
		t.Errorf("packages: %v\nwant %v", got, want)
	}
	if p := PHPPackages("7.3", []string{"fpm"}); len(p) != 0 {
		t.Errorf("PHP 7.3 on EL has no packages: %v", p)
	}
	if c := strings.Join(InstallCommand("a"), " "); !strings.HasPrefix(c, "dnf -y") || !strings.HasSuffix(c, "install a") {
		t.Errorf("install: %s", c)
	}
	if c := strings.Join(RemoveCommand("a"), " "); c != "dnf -y -q remove a" {
		t.Errorf("remove: %s", c)
	}
	if !strings.Contains(RefreshScript(), "dnf -q makecache") || !strings.HasPrefix(InstalledScript("x"), "rpm -q x") {
		t.Error("the EL scripts use dnf and rpm")
	}
}
