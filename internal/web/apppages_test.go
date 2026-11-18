package web

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"mpn/internal/ctl"
)

const (
	blogPlain = `[{"name":"blog","kind":"php","user":"blog","hostnames":["blog.example.com"],"detail":"php 8.4","tls":false,"root":"/home/blog/htdocs"}]`
	blogWP    = `[{"name":"blog","kind":"php","user":"blog","hostnames":["blog.example.com"],"detail":"php 8.4","tls":false,"root":"/home/blog/htdocs","app":"wordpress"}]`
	blogOld   = `[{"name":"blog","kind":"php","user":"blog","hostnames":["blog.example.com"],"detail":"php 7.4","tls":false,"root":"/home/blog/htdocs"}]`
	wpStatus  = `{"version":"6.9.1","url":"http://blog.example.com","core_update":"6.9.2","plugins":[` +
		`{"name":"akismet","title":"Akismet Anti-spam","status":"active","version":"5.3","update":"none","update_version":""},` +
		`{"name":"seo-x","title":"SEO <b>X</b>","status":"inactive","version":"1.0","update":"available","update_version":"1.1"}],` +
		`"themes":[{"name":"twentytwentyfour","title":"Twenty Twenty-Four","status":"active","version":"1.2","update":"none","update_version":""},` +
		`{"name":"astra","title":"Astra","status":"inactive","version":"4.0","update":"none","update_version":""}]}`
)

func appAnswers(list string) func(ctl.Request) (ctl.Response, error) {
	return byOp(map[string]string{"site.list": list, "site.add": "added", "site.apply": "applied", "site.access": "login enabled", "site.info": phpInfo(),
		"app.install": "WordPress is installed\n  admin:      chusite\n  password:   Mk9Wq3Lp7Rt2Xv5Bn8Hd", "wp.status": wpStatus, "wp.run": "Success: done", "wp.search": `[{"name":"Yoast SEO","slug":"wordpress-seo","rating":96,"version":"23.0","short_description":"Improve your SEO"}]`,
		"backup.list": `[]`, "db.list": `[]`})
}

func wpCreate(tok string) url.Values {
	return url.Values{"csrf": {tok}, "kind": {"php"}, "hosts": {"blog.example.com"}, "php": {"8.4"}, "app": {"wordpress"},
		"wp_title": {"Blog của tôi"}, "wp_admin": {"chusite"}, "wp_email": {"chu@example.com"}, "wp_locale": {"vi"}}
}

func TestOnlyThePHPFormOffersTheCode(t *testing.T) {
	h, _, c, _ := setupWith(t, appAnswers("[]"), nil)
	php := do(h, "GET", "/sites/new?kind=php", nil, cookie(c)).Body.String()
	for _, want := range []string{`name="app"`, `data-app-select`, `value="wordpress"`, `value="laravel"`, `value="codeigniter"`, `value="cakephp"`, `value="yii"`, "Trống, tôi tự đưa mã lên",
		`name="wp_title"`, `name="wp_admin"`, `name="wp_email"`, `name="wp_locale"`, `data-app-wp`, "cần PHP 8.2 trở lên"} {
		if !strings.Contains(php, want) {
			t.Errorf("php form lacks %q", want)
		}
	}
	for _, kind := range []string{"node", "python", "static", "proxy"} {
		if body := do(h, "GET", "/sites/new?kind="+kind, nil, cookie(c)).Body.String(); strings.Contains(body, `name="app"`) || strings.Contains(body, "wp_title") {
			t.Errorf("%s: a form for code that is not PHP must not offer an application", kind)
		}
	}
}

func TestCreatingAWordPressSiteInstallsItAfterTheSiteExists(t *testing.T) {
	h, f, c, tok := setupWith(t, appAnswers("[]"), nil)
	w := followed(h, do(h, "POST", "/sites/add", wpCreate(tok), cookie(c)), c)
	if got := f.ops(); !reflect.DeepEqual(got, []string{"site.list", "site.add", "site.apply", "site.access", "app.install"}) {
		t.Fatalf("ops %v", got)
	}
	req, _ := lastOp(f, "app.install")
	if req.Site != "blog-example-com" || req.App != "wordpress" || req.AppTitle != "Blog của tôi" || req.AppAdmin != "chusite" || req.AppEmail != "chu@example.com" || req.AppLocale != "vi" {
		t.Errorf("%+v", req)
	}
	body := w.Body.String()
	// the login of the site and the password of WordPress are both shown, once
	if !strings.Contains(body, "Mk9Wq3Lp7Rt2Xv5Bn8Hd") || !strings.Contains(body, "mật khẩu:") {
		t.Errorf("both logins must be on the result page:\n%s", body)
	}
	if backOf(t, body) != "/sites/blog-example-com" {
		t.Errorf("back: %s", backOf(t, body))
	}
}

func TestAFrameworkNeedsNoAdministratorDetails(t *testing.T) {
	h, f, c, tok := setupWith(t, appAnswers("[]"), nil)
	form := url.Values{"csrf": {tok}, "kind": {"php"}, "hosts": {"shop.example.com"}, "php": {"8.4"}, "app": {"laravel"}}
	followed(h, do(h, "POST", "/sites/add", form, cookie(c)), c)
	if req, ok := lastOp(f, "app.install"); !ok || req.App != "laravel" || req.AppTitle != "" {
		t.Errorf("%+v", req)
	}
}

func TestNoApplicationMeansNoInstallation(t *testing.T) {
	h, f, c, tok := setupWith(t, appAnswers("[]"), nil)
	form := url.Values{"csrf": {tok}, "kind": {"php"}, "hosts": {"plain.example.com"}, "php": {"8.4"}, "app": {""}}
	followed(h, do(h, "POST", "/sites/add", form, cookie(c)), c)
	if _, ok := lastOp(f, "app.install"); ok {
		t.Error("an empty choice installs nothing")
	}
	// an application sent with another kind of site is ignored
	h, f, c, tok = setupWith(t, appAnswers("[]"), nil)
	form = url.Values{"csrf": {tok}, "kind": {"static"}, "hosts": {"static.example.com"}, "app": {"wordpress"}, "wp_title": {"x"}}
	followed(h, do(h, "POST", "/sites/add", form, cookie(c)), c)
	if _, ok := lastOp(f, "app.install"); ok {
		t.Error("only a PHP site gets an application")
	}
}

func TestAChoiceThatCannotWorkCreatesNothing(t *testing.T) {
	for name, mut := range map[string]func(url.Values){
		"no title":      func(v url.Values) { v.Set("wp_title", "") },
		"bad admin":     func(v url.Values) { v.Set("wp_admin", "a b") },
		"bad email":     func(v url.Values) { v.Set("wp_email", "nobody") },
		"bad locale":    func(v url.Values) { v.Set("wp_locale", "xx") },
		"unknown app":   func(v url.Values) { v.Set("app", "wordpress;id") },
		"php too old":   func(v url.Values) { v.Set("app", "laravel"); v.Set("php", "7.4") },
		"title newline": func(v url.Values) { v.Set("wp_title", "a\nb") },
	} {
		h, f, c, tok := setupWith(t, appAnswers("[]"), nil)
		form := wpCreate(tok)
		mut(form)
		w := do(h, "POST", "/sites/add", form, cookie(c))
		if w.Code != 400 {
			t.Errorf("%s: %d", name, w.Code)
		}
		for _, op := range f.ops() {
			if op == "site.add" || op == "app.install" {
				t.Errorf("%s: %s reached the helper", name, op)
			}
		}
	}
}

func TestAFailedInstallationKeepsTheSiteAndSaysSoAndHowToRetry(t *testing.T) {
	h, f, c, tok := setupWith(t, func(r ctl.Request) (ctl.Response, error) {
		if r.Op == "app.install" {
			return ctl.Response{Output: "app-notempty: the web root has files in it", Error: "exit status 2"}, nil
		}
		return appAnswers("[]")(r)
	}, nil)
	w := followed(h, do(h, "POST", "/sites/add", wpCreate(tok), cookie(c)), c)
	body := w.Body.String()
	for _, want := range []string{"Website đã được tạo", "thư mục trống", "mật khẩu:"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q:\n%s", want, body)
		}
	}
	if backOf(t, body) != "/sites/blog-example-com" {
		t.Errorf("the person goes back to the site that exists: %s", backOf(t, body))
	}
	if got := f.ops(); got[len(got)-1] != "app.install" {
		t.Errorf("%v", got)
	}
}

func TestTheOverviewOffersTheCodeOfASiteThatHasNone(t *testing.T) {
	h, _, c, _ := setupWith(t, appAnswers(blogPlain), nil)
	body := do(h, "GET", "/sites/blog", nil, cookie(c)).Body.String()
	for _, want := range []string{`action="/sites/blog/app/install"`, `name="app"`, `value="laravel"`, "Cài mã nguồn"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, `href="/sites/blog/wordpress"`) {
		t.Error("a site without WordPress has no WordPress tab")
	}
}

func TestTheOverviewOfAWordPressSiteShowsItAndLinksToTheManager(t *testing.T) {
	h, _, c, _ := setupWith(t, appAnswers(blogWP), nil)
	body := do(h, "GET", "/sites/blog", nil, cookie(c)).Body.String()
	for _, want := range []string{"WordPress", `href="/sites/blog/wordpress"`, "Quản lý WordPress", "wp-config.php"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, `action="/sites/blog/app/install"`) {
		t.Error("a site that has an application cannot get another")
	}
	// the manager comes right after the overview
	tabs := body[strings.Index(body, `aria-label="`):]
	if i, j := strings.Index(tabs, `href="/sites/blog/wordpress"`), strings.Index(tabs, `href="/sites/blog/files"`); i < 0 || j < 0 || i > j {
		t.Errorf("the WordPress tab belongs right after the overview (%d, %d)", i, j)
	}
}

func TestTheWordPressTabListsPluginsAndThemesAndWhatCanBeDone(t *testing.T) {
	h, f, c, _ := setupWith(t, appAnswers(blogWP), nil)
	body := do(h, "GET", "/sites/blog/wordpress", nil, cookie(c)).Body.String()
	for _, want := range []string{"6.9.1", "Cập nhật lên 6.9.2", "Akismet Anti-spam", "SEO &lt;b&gt;X&lt;/b&gt;", "Twenty Twenty-Four", "có bản 1.1", `action="/sites/blog/wp/run"`,
		`name="action" value="deactivate"`, `name="action" value="activate"`, `name="action" value="delete"`, `name="action" value="update"`, `name="slug" value="astra"`,
		`href="http://blog.example.com/wp-admin/"`, `rel="noopener noreferrer"`, "Cài theo tên gói"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "<b>X</b>") {
		t.Error("a plugin title must be escaped")
	}
	if _, ok := lastOp(f, "wp.status"); !ok {
		t.Error("the page asks the helper for the state")
	}
}

func TestTheWordPressTabExistsOnlyForAWordPressSite(t *testing.T) {
	h, _, c, _ := setupWith(t, appAnswers(blogPlain), nil)
	if w := do(h, "GET", "/sites/blog/wordpress", nil, cookie(c)); w.Code != 404 {
		t.Errorf("a site without WordPress: %d", w.Code)
	}
	h, _, c, _ = setupWith(t, appAnswers(blogWP), nil)
	if w := do(h, "GET", "/sites/blog/wordpress", nil); w.Code != 303 {
		t.Errorf("without a session: %d", w.Code)
	}
	if w := do(h, "GET", "/sites/nope/wordpress", nil, cookie(c)); w.Code != 404 {
		t.Errorf("a site that does not exist: %d", w.Code)
	}
}

// wordpress.org sends its texts HTML-encoded already; shown as they come they would read "&quot;classic&quot;" on the page.
func TestTextsThatWordPressOrgSendsEncodedAreShownOnce(t *testing.T) {
	h, _, c, _ := setupWith(t, byOp(map[string]string{"site.list": blogWP, "wp.status": wpStatus, "backup.list": `[]`, "db.list": `[]`, "site.info": phpInfo(),
		"wp.search": `[{"name":"Classic &amp; Co","slug":"classic","rating":98,"version":"1.7","short_description":"Turn on the &quot;classic&quot; editor &lt;b&gt;now"}]`}), nil)
	body := do(h, "GET", "/sites/blog/wordpress?k=plugin&q=classic", nil, cookie(c)).Body.String()
	if !strings.Contains(body, "Classic &amp; Co") || !strings.Contains(body, "&#34;classic&#34; editor &lt;b&gt;now") {
		t.Errorf("encoded once, not twice:\n%s", body[strings.Index(body, "Classic"):])
	}
	if strings.Contains(body, "&amp;quot;") || strings.Contains(body, "<b>now") {
		t.Error("double-encoded, or markup from wordpress.org reached the page")
	}
}

func TestSearchingShowsWhatWordPressOrgHasAndOffersToInstallIt(t *testing.T) {
	h, f, c, _ := setupWith(t, appAnswers(blogWP), nil)
	body := do(h, "GET", "/sites/blog/wordpress?k=plugin&q=seo", nil, cookie(c)).Body.String()
	for _, want := range []string{"Yoast SEO", "wordpress-seo", "Improve your SEO", `name="slug" value="wordpress-seo"`, `name="kind" value="plugin"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if req, ok := lastOp(f, "wp.search"); !ok || req.WPKind != "plugin" || req.WPTerm != "seo" {
		t.Errorf("%+v", req)
	}
	n := len(f.reqs)
	bad := do(h, "GET", "/sites/blog/wordpress?k=plugin&q="+url.QueryEscape("a;rm -rf /"), nil, cookie(c)).Body.String()
	if !strings.Contains(bad, "Từ khóa chỉ gồm chữ") {
		t.Error("a search that is refused says why")
	}
	for _, r := range f.reqs[n:] {
		if r.Op == "wp.search" {
			t.Errorf("a refused search reached the helper: %+v", r)
		}
	}
}

func TestTheManagerSendsOnlyWhatIsAllowed(t *testing.T) {
	h, f, c, tok := setupWith(t, appAnswers(blogWP), nil)
	post := func(v url.Values) int {
		v.Set("csrf", tok)
		return do(h, "POST", "/sites/blog/wp/run", v, cookie(c)).Code
	}
	ok := url.Values{"kind": {"plugin"}, "action": {"install"}, "slug": {"contact-form-7"}}
	w := followed(h, do(h, "POST", "/sites/blog/wp/run", withCSRF(ok, tok), cookie(c)), c)
	if req, found := lastOp(f, "wp.run"); !found || req.WPKind != "plugin" || req.WPAction != "install" || req.WPSlug != "contact-form-7" {
		t.Fatalf("%+v", req)
	}
	if backOf(t, w.Body.String()) != "/sites/blog/wordpress" {
		t.Errorf("back: %s", backOf(t, w.Body.String()))
	}
	followed(h, do(h, "POST", "/sites/blog/wp/run", withCSRF(url.Values{"kind": {"core"}, "action": {"update"}}, tok), cookie(c)), c)
	if req, _ := lastOp(f, "wp.run"); req.WPKind != "core" || req.WPAction != "update" {
		t.Errorf("core update: %+v", req)
	}
	runs := 0
	for _, o := range f.ops() {
		if o == "wp.run" {
			runs++
		}
	}
	for name, v := range map[string]url.Values{
		"a link instead of a name": {"kind": {"plugin"}, "action": {"install"}, "slug": {"https://evil.example/p.zip"}},
		"capital letters":          {"kind": {"plugin"}, "action": {"install"}, "slug": {"Akismet"}},
		"unknown action":           {"kind": {"plugin"}, "action": {"eval"}, "slug": {"x"}},
		"unknown kind":             {"kind": {"option"}, "action": {"update"}, "slug": {"x"}},
		"theme deactivate":         {"kind": {"theme"}, "action": {"deactivate"}, "slug": {"x"}},
		"core with a version":      {"kind": {"core"}, "action": {"update"}, "slug": {"6.9"}},
		"nothing":                  {},
	} {
		if code := post(v); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	after := 0
	for _, o := range f.ops() {
		if o == "wp.run" {
			after++
		}
	}
	if after != runs {
		t.Errorf("refused requests reached the helper: %d → %d", runs, after)
	}
	if code := do(h, "POST", "/sites/blog/wp/run", url.Values{"csrf": {"wrong"}, "kind": {"plugin"}, "action": {"update"}, "slug": {"x"}}, cookie(c)).Code; code != 403 {
		t.Errorf("wrong token: %d", code)
	}
	if code := do(h, "POST", "/sites/blog/wp/run", ok).Code; code != 303 {
		t.Errorf("no session: %d", code)
	}
}

func withCSRF(v url.Values, tok string) url.Values {
	out := url.Values{}
	for k, x := range v {
		out[k] = x
	}
	out.Set("csrf", tok)
	return out
}

func TestTheManagerRefusesASiteWithoutWordPress(t *testing.T) {
	h, f, c, tok := setupWith(t, appAnswers(blogPlain), nil)
	w := do(h, "POST", "/sites/blog/wp/run", withCSRF(url.Values{"kind": {"plugin"}, "action": {"update"}, "slug": {"akismet"}}, tok), cookie(c))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "không có WordPress") {
		t.Errorf("%d", w.Code)
	}
	if _, ok := lastOp(f, "wp.run"); ok {
		t.Error("nothing may reach the helper")
	}
}

func TestInstallingLaterWorksOnceAndOnlyOnAPHPSiteWithNoApplication(t *testing.T) {
	h, f, c, tok := setupWith(t, appAnswers(blogPlain), nil)
	form := withCSRF(url.Values{"app": {"wordpress"}, "wp_title": {"Blog"}, "wp_admin": {"chusite"}, "wp_email": {"chu@example.com"}, "wp_locale": {"vi"}}, tok)
	w := followed(h, do(h, "POST", "/sites/blog/app/install", form, cookie(c)), c)
	if req, ok := lastOp(f, "app.install"); !ok || req.Site != "blog" || req.App != "wordpress" {
		t.Fatalf("%+v", req)
	}
	if !strings.Contains(w.Body.String(), "Mk9Wq3Lp7Rt2Xv5Bn8Hd") || backOf(t, w.Body.String()) != "/sites/blog" {
		t.Errorf("the password is shown once, back to the site:\n%s", w.Body.String())
	}
	// a site that has an application, a php version too old, and bad details are all refused before the helper is asked
	for name, c := range map[string]struct {
		list string
		form url.Values
	}{
		"has one already": {blogWP, withCSRF(url.Values{"app": {"laravel"}}, tok)},
		"php too old":     {blogOld, withCSRF(url.Values{"app": {"laravel"}}, tok)},
		"no choice":       {blogPlain, withCSRF(url.Values{"app": {""}}, tok)},
		"bad details":     {blogPlain, withCSRF(url.Values{"app": {"wordpress"}, "wp_title": {""}}, tok)},
	} {
		h2, f2, c2, tok2 := setupWith(t, appAnswers(c.list), nil)
		c.form.Set("csrf", tok2)
		if w := do(h2, "POST", "/sites/blog/app/install", c.form, cookie(c2)); w.Code != 400 {
			t.Errorf("%s: %d", name, w.Code)
		}
		if _, ok := lastOp(f2, "app.install"); ok {
			t.Errorf("%s: the helper was asked", name)
		}
	}
	if code := do(h, "POST", "/sites/blog/app/install", url.Values{"csrf": {"wrong"}, "app": {"laravel"}}, cookie(c)).Code; code != 403 {
		t.Errorf("wrong token: %d", code)
	}
}
