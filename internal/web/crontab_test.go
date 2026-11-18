package web

import (
	"net/url"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

const cronJobsJSON = `[{"id":3,"schedule":"*/5 * * * *","command":"php8.4 /home/shop/htdocs/cron.php","enabled":true},{"id":4,"schedule":"0 3 * * *","command":"echo 100%","enabled":false},{"id":5,"schedule":"15 8-18 * * 1-5","command":"x","enabled":true}]`

func cronAnswers(jobs string) func(ctl.Request) (ctl.Response, error) {
	return byOp(map[string]string{"site.list": siteListJSON("", "", false), "site.cronlist": jobs, "site.cronadd": "added", "site.cronset": "set", "site.crondel": "gone", "site.info": phpInfo()})
}

func TestTheCronTabListsJobsInWordsAndOffersTheActions(t *testing.T) {
	h, _, c, _ := setupWith(t, cronAnswers(cronJobsJSON), nil)
	body := do(h, "GET", "/sites/shop/cron", nil, cookie(c)).Body.String()
	for _, want := range []string{"Mỗi 5 phút", "Hằng ngày lúc 03:00", "15 8-18 * * 1-5", "php8.4 /home/shop/htdocs/cron.php", "echo 100%",
		`action="/sites/shop/cron/3/toggle"`, `action="/sites/shop/cron/4/delete"`, `name="on" value="0"`, `name="on" value="1"`, "Tạm dừng", "Bật lại",
		`action="/sites/shop/cron"`, `name="preset"`, `name="custom"`, `name="command"`, `href="/sites/shop/cron"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	h, _, c, _ = setupWith(t, cronAnswers("[]"), nil)
	if body = do(h, "GET", "/sites/shop/cron", nil, cookie(c)).Body.String(); !strings.Contains(body, "Chưa có công việc định kỳ nào") {
		t.Error("an empty list says so")
	}
}

func TestAddingAJobSendsTheScheduleAndTheCommand(t *testing.T) {
	h, f, c, tok := setupWith(t, cronAnswers("[]"), nil)
	form := url.Values{"csrf": {tok}, "preset": {"0 3 * * *"}, "custom": {""}, "command": {"  php8.4 /home/shop/htdocs/cron.php \r\n"}}
	w := followed(h, do(h, "POST", "/sites/shop/cron", form, cookie(c)), c)
	req, ok := lastOp(f, "site.cronadd")
	if !ok || req.Site != "shop" || req.Schedule != "0 3 * * *" || req.Command != "php8.4 /home/shop/htdocs/cron.php" {
		t.Fatalf("%+v\n%s", req, w.Body.String())
	}
	if backOf(t, w.Body.String()) != "/sites/shop/cron" {
		t.Errorf("back: %s", w.Body.String())
	}
	// a schedule written by hand wins over the preset, and its spaces are tidied
	form.Set("custom", "  */10   8-18 * * 1-5 ")
	followed(h, do(h, "POST", "/sites/shop/cron", form, cookie(c)), c)
	if req, _ := lastOp(f, "site.cronadd"); req.Schedule != "*/10 8-18 * * 1-5" {
		t.Errorf("custom schedule: %q", req.Schedule)
	}
}

func TestAJobThatCouldNotRunNeverReachesTheHelper(t *testing.T) {
	for name, form := range map[string]url.Values{
		"a schedule that does not exist": {"custom": {"61 * * * *"}, "command": {"id"}},
		"words in the schedule":          {"custom": {"every day"}, "command": {"id"}},
		"a preset that was not offered":  {"preset": {"* * * * * root"}, "command": {"id"}},
		"no command":                     {"preset": {"* * * * *"}, "command": {"  "}},
		"a second line":                  {"preset": {"* * * * *"}, "command": {"id\n* * * * * root id"}},
	} {
		h, f, c, tok := setupWith(t, cronAnswers("[]"), nil)
		form.Set("csrf", tok)
		w := do(h, "POST", "/sites/shop/cron", form, cookie(c))
		if _, sent := lastOp(f, "site.cronadd"); sent || w.Code != 400 || !strings.Contains(w.Body.String(), "Chưa thực hiện") {
			t.Errorf("%s: sent=%v code=%d", name, sent, w.Code)
		}
	}
}

func TestToggleAndDeleteNeedAValidJobNumberAndTheToken(t *testing.T) {
	h, f, c, tok := setupWith(t, cronAnswers(cronJobsJSON), nil)
	followed(h, do(h, "POST", "/sites/shop/cron/3/toggle", url.Values{"csrf": {tok}, "on": {"0"}}, cookie(c)), c)
	if req, ok := lastOp(f, "site.cronset"); !ok || req.JobID != 3 || req.On {
		t.Errorf("pause: %+v", req)
	}
	followed(h, do(h, "POST", "/sites/shop/cron/4/toggle", url.Values{"csrf": {tok}, "on": {"1"}}, cookie(c)), c)
	if req, _ := lastOp(f, "site.cronset"); req.JobID != 4 || !req.On {
		t.Errorf("resume: %+v", req)
	}
	followed(h, do(h, "POST", "/sites/shop/cron/5/delete", url.Values{"csrf": {tok}}, cookie(c)), c)
	if req, ok := lastOp(f, "site.crondel"); !ok || req.JobID != 5 {
		t.Errorf("delete: %+v", req)
	}
	before := len(f.reqs)
	for _, p := range []string{"/sites/shop/cron/x/delete", "/sites/shop/cron/0/delete", "/sites/shop/cron/-1/toggle"} {
		if w := do(h, "POST", p, url.Values{"csrf": {tok}}, cookie(c)); w.Code != 400 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	if w := do(h, "POST", "/sites/shop/cron/3/delete", url.Values{"csrf": {"wrong"}}, cookie(c)); w.Code != 403 {
		t.Errorf("wrong token: %d", w.Code)
	}
	if w := do(h, "POST", "/sites/nope/cron", url.Values{"csrf": {tok}, "preset": {"* * * * *"}, "command": {"id"}}, cookie(c)); w.Code != 404 {
		t.Errorf("unknown site: %d", w.Code)
	}
	for _, r := range f.reqs[before:] {
		if r.Op == "site.cronset" || r.Op == "site.crondel" || r.Op == "site.cronadd" {
			t.Errorf("%s must not reach the helper", r.Op)
		}
	}
}

func TestDescribeSchedule(t *testing.T) {
	for in, want := range map[string]string{
		"* * * * *": "Mỗi phút", "*/15 * * * *": "Mỗi 15 phút", "0 * * * *": "Mỗi giờ, phút thứ 0", "30 2 * * *": "Hằng ngày lúc 02:30",
		"@daily": "Hằng ngày lúc 00:00", "@weekly": "Hằng tuần", "0 3 * * 0": "0 3 * * 0", "1 2 3 4 5": "1 2 3 4 5",
	} {
		if got := describeSchedule(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
