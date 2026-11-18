package dbadmin

import "testing"

// blog_shop fits both the site blog and the site blog-shop; it belongs to the names of blog-shop, so blog may not make it.
func TestCloserOwnerFindsTheSiteWhoseNamesFitBetter(t *testing.T) {
	sites := []string{"blog", "blog-shop", "shop"}
	for _, c := range []struct{ name, site, want string }{
		{"blog_shop", "blog", "blog-shop"},
		{"blog_shop_x", "blog", "blog-shop"},
		{"blog_shop", "blog-shop", ""},
		{"blog_cache", "blog", ""},
		{"blog", "blog", ""},
		{"shop_1", "shop", ""},
	} {
		if got := CloserOwner(c.name, c.site, sites); got != c.want {
			t.Errorf("CloserOwner(%q, %q) = %q, want %q", c.name, c.site, got, c.want)
		}
	}
}
