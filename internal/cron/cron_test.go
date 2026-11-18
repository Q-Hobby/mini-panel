package cron

import (
	"strings"
	"testing"
)

func TestSchedules(t *testing.T) {
	for _, ok := range []string{"* * * * *", "*/5 * * * *", "0 3 * * *", "30 2 1 * *", "0 0 * * 0", "0 0 * * 7", "15,45 8-18 * 1-6 1-5", "0 */2 * * *", "1-30/5 * * * *", "@daily", "@hourly", "@weekly", "@monthly", "@yearly"} {
		if err := ValidSchedule(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * 32 * *", "* * * 13 *", "* * * * 8", "*/0 * * * *", "5-1 * * * *", "mon * * * *", "* * * * mon", "@reboot", "@every", "0  3 * * *", " 0 3 * * *", "0 3 * * * ", "0 3 * * *\n* * * * *", "$(id) * * * *", "1-2-3 * * * *", "*/ * * * *", "-1 * * * *"} {
		if err := ValidSchedule(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestCommands(t *testing.T) {
	for _, ok := range []string{"php8.4 /home/blog/htdocs/cron.php", "/usr/bin/curl -s https://example.com/hook?a=1 >/dev/null 2>&1", "echo 100%", `cd ~/htdocs && php artisan schedule:run`} {
		if err := ValidCommand(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for name, bad := range map[string]string{"empty": "  ", "newline": "echo a\n* * * * * root id", "carriage return": "echo a\r* * * * * root id", "nul": "echo a\x00b", "edge space": " echo a", "trailing space": "echo a ", "huge": strings.Repeat("a", MaxCommand+1), "bad utf-8": "echo \xff"} {
		if err := ValidCommand(bad); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestTheFileHasOneLinePerEnabledJobRunsAsTheSiteAccountAndEscapesPercent(t *testing.T) {
	got, err := Render("blog", "blog", []Job{
		{1, "*/5 * * * *", "php8.4 /home/blog/htdocs/cron.php", true},
		{2, "0 3 * * *", "mysqldump x | gzip > /tmp/$(date +%F).sql.gz", false},
		{3, "@daily", "echo 100% done", true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"*/5 * * * * blog php8.4 /home/blog/htdocs/cron.php", `@daily blog echo 100\% done`, "MAILTO=\"\"", "SHELL=/bin/sh"}
	for _, w := range want {
		if !strings.Contains(got, w+"\n") {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
	if strings.Contains(got, "mysqldump") {
		t.Error("a disabled job must not be written")
	}
	lines := 0
	for _, l := range strings.Split(strings.TrimSpace(got), "\n") {
		if l != "" && !strings.HasPrefix(l, "#") && !strings.Contains(l, "=") {
			lines++
		}
	}
	if lines != 2 {
		t.Errorf("two jobs, %d job lines:\n%s", lines, got)
	}
	if empty, _ := Render("blog", "blog", []Job{{1, "@daily", "x", false}}); empty != "" {
		t.Error("with no enabled job there is no file")
	}
}

func TestNothingCanBeSmuggledIntoTheFileOfRoot(t *testing.T) {
	for name, j := range map[string]Job{
		"line break in the command":   {1, "@daily", "id\n* * * * * root rm -rf /", true},
		"line break in the schedule":  {1, "@daily\n* * * * * root id", "id", true},
		"another user in the command": {1, "* * * * *", "id", true},
	} {
		got, err := Render("blog", "blog", []Job{j})
		if name == "another user in the command" {
			if err != nil || !strings.Contains(got, "* * * * * blog id\n") {
				t.Errorf("%s: %v %q", name, err, got)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s was written:\n%s", name, got)
		}
	}
	for _, bad := range []string{"root; id", "Blog", "a b", "", "x\n* * * * * root id"} {
		if _, err := Render("blog", bad, []Job{{1, "@daily", "id", true}}); err == nil {
			t.Errorf("account %q must be refused", bad)
		}
	}
	if File("blog") != "/etc/cron.d/mpn-blog" {
		t.Error(File("blog"))
	}
}
