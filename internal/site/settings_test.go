package site

import (
	"strings"
	"testing"
)

func TestSettingsOnTheListAreAcceptedWithTheirValues(t *testing.T) {
	ok := map[string]string{
		"memory_limit": "128M", "max_execution_time": "0", "max_input_time": "-1", "max_input_vars": "100", "post_max_size": "1G", "upload_max_filesize": "2M",
		"display_errors": "Off", "error_reporting": "24575", "date.timezone": "UTC", "session.gc_maxlifetime": "86400", "max_file_uploads": "1000", "short_open_tag": "On",
	}
	if err := ValidatePHPIni(ok); err != nil {
		t.Errorf("%v", err)
	}
	if err := ValidatePHPIni(nil); err != nil {
		t.Errorf("no settings: %v", err)
	}
}

func TestSettingsThatCouldBreakTheIsolationAreNotOnTheList(t *testing.T) {
	for _, name := range []string{"open_basedir", "disable_functions", "auto_prepend_file", "auto_append_file", "extension", "zend_extension", "user_ini.filename",
		"sys_temp_dir", "upload_tmp_dir", "session.save_path", "allow_url_include", "allow_url_fopen", "enable_dl", "doc_root", "user_dir", "error_log", "mail.log",
		"sendmail_path", "opcache.preload", "memory_limit ", " memory_limit", "MEMORY_LIMIT", "memory_limit]", ""} {
		if err := ValidatePHPIni(map[string]string{name: "1"}); err == nil {
			t.Errorf("%q must not be settable", name)
		}
	}
}

func TestValuesThatAreNotWhatTheyClaimAreRefused(t *testing.T) {
	for name, vals := range map[string][]string{
		"memory_limit":       {"", "0", "-1", "256", "256m", "256MB", "5G", "4097M", "0M", "1.5G", " 256M", "256M ", "256M\n", "1e3M", "ten"},
		"max_execution_time": {"", "-1", "3601", "1e3", "01", "1.5", "x", " 5", "5 "},
		"max_input_time":     {"", "-2", "3601", "x"},
		"max_input_vars":     {"", "99", "100001", "x"},
		"post_max_size":      {"", "0", "256", "4097M", "x"},
		"display_errors":     {"", "on", "1", "true", "stderr", "On\n"},
		"error_reporting":    {"", "E_ALL", "-1", "32768", "x"},
		"date.timezone":      {"", "Asia/Ho Chi Minh", "../x", "/etc/passwd", "asia/saigon", "A/B;C", "Asia/Ho_Chi_Minh\nopen_basedir=/"},
		"max_file_uploads":   {"", "0", "1001"},
		"short_open_tag":     {"", "1", "off"},
	} {
		for _, v := range vals {
			if err := ValidatePHPIni(map[string]string{name: v}); err == nil {
				t.Errorf("%s = %q must be refused", name, v)
			}
		}
	}
}

func TestAnUploadCannotBeLargerThanARequest(t *testing.T) {
	if err := ValidatePHPIni(map[string]string{"upload_max_filesize": "100M", "post_max_size": "50M"}); err == nil || !strings.Contains(err.Error(), "may not be larger") {
		t.Errorf("%v", err)
	}
	// the defaults count too: a post size below the default upload size is a mistake
	if err := ValidatePHPIni(map[string]string{"post_max_size": "8M"}); err == nil {
		t.Error("post_max_size below the default upload_max_filesize")
	}
	if err := ValidatePHPIni(map[string]string{"post_max_size": "8M", "upload_max_filesize": "2M"}); err != nil {
		t.Errorf("%v", err)
	}
}

func TestEveryErrorIsReportedNotOnlyTheFirst(t *testing.T) {
	err := ValidatePHPIni(map[string]string{"memory_limit": "x", "nope": "1", "max_input_vars": "5"})
	if err == nil || strings.Count(err.Error(), ";") != 2 {
		t.Errorf("%v", err)
	}
}

func TestTheWebRootBelowHtdocs(t *testing.T) {
	for _, ok := range []string{"", "public", "app/public", "a.b", "a_b-c", "v1.2/web", "A1"} {
		if !ValidSubdir(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{".", "..", "../x", "a/../b", "/abs", "a/", "/a", "a//b", ".ssh", "a/.git", ".hidden/x", "a b", "a\nb", "a;b", "a$b", "a\\b", "a:b",
		"é", strings.Repeat("a", 65), strings.Repeat("a/", 120), "-x"} {
		if ValidSubdir(bad) {
			t.Errorf("%q must be refused", bad)
		}
	}
	if RootFor("blog", "") != "/home/blog/htdocs" || RootFor("blog", "public") != "/home/blog/htdocs/public" {
		t.Error("RootFor")
	}
	s := Site{User: "blog", Root: "/home/blog/htdocs/app/public"}
	if sub, ok := s.RootSub(); !ok || sub != "app/public" {
		t.Errorf("%q %v", sub, ok)
	}
	if sub, ok := (Site{User: "blog", Root: "/home/blog/htdocs"}).RootSub(); !ok || sub != "" {
		t.Errorf("%q %v", sub, ok)
	}
	for _, outside := range []string{"/var/www/blog", "/home/blog", "/home/blog/htdocs2", "/home/other/htdocs"} {
		if _, ok := (Site{User: "blog", Root: outside}).RootSub(); ok {
			t.Errorf("%s is not below htdocs", outside)
		}
	}
}

func TestPortsOfTheServerItselfCannotBeGivenToAnApp(t *testing.T) {
	for _, p := range []int{0, 22, 80, 443, 1023, 3306, 8780, 2610, 2611, 2612, 8781, 65536} {
		if ValidAppPort(p) {
			t.Errorf("%d must be refused", p)
		}
	}
	for _, p := range []int{1024, 3000, 8080, 20000, 29999, 65535} {
		if !ValidAppPort(p) {
			t.Errorf("%d must be accepted", p)
		}
	}
}

func TestUpstreamsAreOnlyPlainUrls(t *testing.T) {
	for _, ok := range []string{"http://127.0.0.1:3000", "https://example.com", "http://localhost:8080/app"} {
		if !ValidUpstream(ok) {
			t.Errorf("%q", ok)
		}
	}
	for _, bad := range []string{"", "ftp://x", "http://", "//x", "http://a b", "http://a;b", "http://a{b", "http://a\"b", "http://a'b", "http://a\\b", "http://a$b", "http://a\nb", "javascript:alert(1)"} {
		if ValidUpstream(bad) {
			t.Errorf("%q", bad)
		}
	}
}

func TestMemoryOfANodeApp(t *testing.T) {
	for m, want := range map[int]bool{0: true, 63: false, 64: true, 8192: true, 8193: false, -1: false} {
		if ValidMemMB(m) != want {
			t.Errorf("%d", m)
		}
	}
}

func TestAPhpSiteWithBadSettingsDoesNotValidate(t *testing.T) {
	s := Site{Name: "a", Kind: PHP, User: "a", Root: "/home/a/htdocs", Hostnames: []string{"a.example.com"}, PHPVersion: "8.4", PHPIni: map[string]string{"open_basedir": "/"}}
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "php settings") {
		t.Errorf("%v", err)
	}
	s.PHPIni = nil
	if err := s.Validate(); err != nil {
		t.Errorf("%v", err)
	}
}
