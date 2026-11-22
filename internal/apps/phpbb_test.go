package apps

import (
	"strings"
	"testing"
)

func TestPHPBBIsUnpackedOnlyWhenItsChecksumMatches(t *testing.T) {
	p := fwParams(PHPBBID)
	p.DBName, p.DBUser, p.DBPass = "blog", "blog", "Abc123def456"
	prep, install, err := Plan(p)
	if err != nil {
		t.Fatal(err)
	}
	all := argsOnly(prep) + argsOnly(install)
	for _, want := range []string{PHPBBSHA256, PHPBBURL, "does not match its checksum; not installed", "runuser -u blog -- unzip", "phpBB3", "chmod 0640 config.php"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, ComposerPath) {
		t.Error("phpBB does not use Composer")
	}
	// The checksum is compared before anything is unpacked.
	if i, j := strings.Index(all, "sha256sum"), strings.Index(all, "unzip -q"); i < 0 || j < 0 || i > j {
		t.Error("the package must be checked before it is unpacked")
	}
}

func TestPHPBBConfigIsReadOnlyOnceTheInstallerFinished(t *testing.T) {
	done := "<?php\n$dbms = 'phpbb\\\\db\\\\driver\\\\mysqli';\n$dbhost = 'localhost';\n$dbname = 'forum_mpanel_biz';\n$table_prefix = 'phpbb_';\n\n@define('PHPBB_INSTALLED', true);\n"
	if db, pre, ok := PHPBBConfig(done); !ok || db != "forum_mpanel_biz" || pre != "phpbb_" {
		t.Errorf("got %q %q %v", db, pre, ok)
	}
	for name, text := range map[string]string{
		"empty file (as unpacked)": "",
		"not finished":             strings.Replace(done, "@define('PHPBB_INSTALLED', true);", "", 1),
		"name with a quote":        strings.Replace(done, "forum_mpanel_biz", "x`; DROP", 1),
		"prefix with a backtick":   strings.Replace(done, "'phpbb_'", "'a`b'", 1),
	} {
		if _, _, ok := PHPBBConfig(text); ok {
			t.Errorf("%s must not count as installed", name)
		}
	}
	if q := PHPBBFounderSQL("forum", "phpbb_"); q != "SELECT COUNT(*) FROM `forum`.`phpbb_users` WHERE user_type = 3;" {
		t.Error(q)
	}
}
