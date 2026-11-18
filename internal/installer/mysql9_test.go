package installer

import (
	"strings"
	"testing"

	"mpn/internal/osinfo"
)

// MySQL 9.x removed the mysql_native_password plugin that PHP older than 7.4 needs, and CloudPanel does not offer it
// either, so no release lists it and every request for it is refused before anything changes.
func TestMySQL9IsRefusedOnEveryRelease(t *testing.T) {
	for _, tg := range osinfo.Supported() {
		for _, series := range []string{"9.7", "9.0", "9.7-lts"} {
			o := opts("mysql", 1024)
			o.Target, o.DBSeries = tg, series
			// DatabasePlan, not Build: the series check is the same, and Build would first stop at the nginx modules, which the
			// RHEL family does not have pinned yet.
			p, err := DatabasePlan(o)
			if err == nil || p != nil {
				t.Errorf("%s %s series %s: expected a refusal, got plan=%v err=%v", tg.ID, tg.Version, series, p, err)
				continue
			}
			if !strings.Contains(err.Error(), "choose one of:") || !strings.Contains(err.Error(), "8.4") {
				t.Errorf("the message should say what to pick instead: %v", err)
			}
		}
	}
}
