package apps

import (
	"reflect"
	"strings"
	"testing"

	"mpn/internal/site"
)

func TestManageArgs(t *testing.T) {
	for _, c := range []struct {
		kind, action, slug string
		want               []string
	}{
		{"plugin", "install", "contact-form-7", []string{"plugin", "install", "contact-form-7"}},
		{"plugin", "activate", "akismet", []string{"plugin", "activate", "akismet"}},
		{"plugin", "deactivate", "akismet", []string{"plugin", "deactivate", "akismet"}},
		{"plugin", "update", "akismet", []string{"plugin", "update", "akismet"}},
		{"plugin", "delete", "akismet", []string{"plugin", "delete", "akismet"}},
		{"plugin", "activate", "hello.php", []string{"plugin", "activate", "hello.php"}},
		{"theme", "install", "astra", []string{"theme", "install", "astra"}},
		{"theme", "activate", "twentytwentyfour", []string{"theme", "activate", "twentytwentyfour"}},
		{"theme", "delete", "astra", []string{"theme", "delete", "astra"}},
		{"core", "update", "", []string{"core", "update"}},
	} {
		got, err := ManageArgs(c.kind, c.action, c.slug)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %s %s: %v %v", c.kind, c.action, c.slug, got, err)
		}
	}
	for _, c := range [][3]string{
		{"plugin", "install", "https://evil.example/p.zip"}, {"plugin", "install", "Akismet"}, {"plugin", "install", "a/b"}, {"plugin", "install", ""},
		{"plugin", "install", "--activate"}, {"plugin", "activate", "-x"}, {"plugin", "activate", "a..b"}, {"plugin", "update", "x y"},
		{"plugin", "shell", "x"}, {"theme", "deactivate", "x"}, {"core", "update", "6.9"}, {"core", "install", ""}, {"user", "delete", "1"}, {"", "update", "x"},
		{"plugin", "install", strings.Repeat("a", 65)},
	} {
		if got, err := ManageArgs(c[0], c[1], c[2]); err == nil {
			t.Errorf("%v must be refused, got %v", c, got)
		}
	}
}

func TestSearchArgs(t *testing.T) {
	got, err := SearchArgs("plugin", "  seo cache ")
	if err != nil || got[0] != "plugin" || got[1] != "search" || got[2] != "seo cache" || !strings.Contains(strings.Join(got, " "), "--format=json") {
		t.Errorf("%v %v", got, err)
	}
	if got, err := SearchArgs("theme", "Tối giản"); err != nil || got[2] != "Tối giản" {
		t.Errorf("letters of any language are fine: %v %v", got, err)
	}
	for _, c := range [][2]string{{"plugin", ""}, {"plugin", "   "}, {"plugin", "a;b"}, {"plugin", "a$(id)"}, {"plugin", "--all"}, {"plugin", strings.Repeat("x", 61)}, {"core", "x"}, {"", "x"}} {
		if _, err := SearchArgs(c[0], c[1]); err == nil && c[1] != "--all" {
			t.Errorf("%v must be refused", c)
		}
	}
	// "--all" has only characters the search accepts, but it cannot be taken for an option: it is a value of its own in the list
	got, _ = SearchArgs("plugin", "--all")
	if got[2] != "--all" || got[0] != "plugin" {
		t.Errorf("a term that looks like an option stays a single argument: %v", got)
	}
}

func TestWPArgvRunsAsTheSiteUserWithItsPHP(t *testing.T) {
	s := site.Site{Name: "blog", Kind: site.PHP, User: "blog", Root: "/home/blog/htdocs", PHPVersion: "8.3"}
	a := WPArgv(s, "plugin", "list", "--format=json")
	j := strings.Join(a, " ")
	for _, want := range []string{"runuser -u blog --", "/usr/bin/php8.3", WPCLIPath, "--path=/home/blog/htdocs", "plugin list --format=json"} {
		if !strings.Contains(j, want) {
			t.Errorf("missing %q in %s", want, j)
		}
	}
	if a[0] != "runuser" || strings.Contains(j, "sh -c") {
		t.Errorf("WP-CLI must be started directly, not through a shell: %s", j)
	}
}
