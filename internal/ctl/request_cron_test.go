package ctl

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCronOperationsAreFixedCommandsAndTheCommandTravelsOnStandardInput(t *testing.T) {
	if IsWrite("site.cronlist") || !IsWrite("site.cronadd") || !IsWrite("site.cronset") || !IsWrite("site.crondel") {
		t.Fatal("reads and writes are mixed up")
	}
	got, _ := Request{Op: "site.cronlist", Site: "blog"}.Argv()
	if !reflect.DeepEqual(got, []string{"cron", "list", "-site", "blog", "-json"}) {
		t.Errorf("%v", got)
	}
	r := Request{Op: "site.cronadd", Site: "blog", Schedule: "*/5 * * * *", Command: "mysql -pSECRET -e 'x'"}
	argv, err := r.Argv()
	if err != nil || !reflect.DeepEqual(argv, []string{"cron", "add", "-site", "blog", "-stdin", "-apply"}) {
		t.Fatalf("%v %v", argv, err)
	}
	for _, a := range argv {
		if strings.Contains(a, "SECRET") {
			t.Error("the command must not be an argument: it can hold a password")
		}
	}
	var in struct{ Schedule, Command string }
	if err := json.Unmarshal([]byte(r.Stdin()), &in); err != nil || in.Schedule != "*/5 * * * *" || !strings.Contains(in.Command, "SECRET") {
		t.Errorf("stdin %q %v", r.Stdin(), err)
	}
	if (Request{Op: "site.cronlist", Site: "blog", Command: "x"}).Stdin() != "" {
		t.Error("only site.cronadd sends text")
	}
	got, _ = Request{Op: "site.cronset", Site: "blog", JobID: 7, On: true}.Argv()
	if !reflect.DeepEqual(got, []string{"cron", "enable", "-site", "blog", "-id", "7", "-apply"}) {
		t.Errorf("%v", got)
	}
	got, _ = Request{Op: "site.cronset", Site: "blog", JobID: 7}.Argv()
	if !reflect.DeepEqual(got, []string{"cron", "disable", "-site", "blog", "-id", "7", "-apply"}) {
		t.Errorf("%v", got)
	}
	got, _ = Request{Op: "site.crondel", Site: "blog", JobID: 7}.Argv()
	if !reflect.DeepEqual(got, []string{"cron", "remove", "-site", "blog", "-id", "7", "-apply"}) {
		t.Errorf("%v", got)
	}
}

func TestACronRequestThatIsNotValidIsRefusedBeforeAnythingRuns(t *testing.T) {
	for name, r := range map[string]Request{
		"a bad schedule":      {Op: "site.cronadd", Site: "blog", Schedule: "* * * *", Command: "id"},
		"a macro that is not": {Op: "site.cronadd", Site: "blog", Schedule: "@reboot", Command: "id"},
		"a second line":       {Op: "site.cronadd", Site: "blog", Schedule: "@daily", Command: "id\n* * * * * root id"},
		"an empty command":    {Op: "site.cronadd", Site: "blog", Schedule: "@daily", Command: ""},
		"a bad site":          {Op: "site.cronadd", Site: "../x", Schedule: "@daily", Command: "id"},
		"job zero":            {Op: "site.crondel", Site: "blog", JobID: 0},
		"a negative job":      {Op: "site.cronset", Site: "blog", JobID: -3, On: true},
	} {
		if _, err := r.Argv(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}
