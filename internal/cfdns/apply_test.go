package cfdns

import (
	"context"
	"strings"
	"testing"
)

func pri(n int) *int { return &n }

func apply(c Client, want ...Record) []Result { return c.Apply(context.Background(), zid, want) }

func statuses(rs []Result) string {
	var s []string
	for _, r := range rs {
		s = append(s, string(r.Status))
	}
	return strings.Join(s, ",")
}

func TestApplyAddsTheMailRecords(t *testing.T) {
	f, c, _ := newFake(t)
	dkim := "v=DKIM1; k=rsa; p=" + strings.Repeat("A", 392)
	rs := apply(c,
		Record{Type: "MX", Name: "example.com", Content: "mail.example.com", Priority: pri(10)},
		Record{Type: "A", Name: "mail.example.com", Content: "203.0.113.10", Proxied: true}, // asked proxied; it must not be
		Record{Type: "TXT", Name: "example.com", Content: "v=spf1 mx -all"},
		Record{Type: "TXT", Name: "_dmarc.example.com", Content: "v=DMARC1; p=none"},
		Record{Type: "TXT", Name: "mail._domainkey.example.com", Content: dkim},
	)
	if statuses(rs) != "added,added,added,added,added" {
		t.Fatalf("%s: %+v", statuses(rs), rs)
	}
	if len(f.posts) != 5 || f.bad != 0 {
		t.Fatalf("posts %d, other calls %d", len(f.posts), f.bad)
	}
	mx, a, key := f.posts[0], f.posts[1], f.posts[4]
	if mx.Priority == nil || *mx.Priority != 10 || mx.Proxied != nil || mx.TTL != 1 {
		t.Errorf("mx: %+v", mx)
	}
	if a.Proxied == nil || *a.Proxied {
		t.Errorf("the address of a mail host is DNS only: %+v", a)
	}
	if key.Content != dkim || len(key.Content) < 400 {
		t.Errorf("the long TXT value goes whole: %d characters", len(key.Content))
	}
	for _, h := range f.auths {
		if h != "Bearer "+tok {
			t.Errorf("header %q", h)
		}
	}
}

func TestRecordsThatAreAlreadyThereAreLeftAlone(t *testing.T) {
	f, c, _ := newFake(t)
	f.recs = []Record{
		{ID: "1", Type: "MX", Name: "example.com", Content: "MAIL.example.com.", Priority: pri(10)},
		{ID: "2", Type: "TXT", Name: "example.com", Content: `"v=spf1 mx -all"`},
		{ID: "3", Type: "TXT", Name: "_dmarc.example.com", Content: `"v=DMARC1; " "p=none"`}, // long values come back in pieces
		{ID: "4", Type: "A", Name: "mail.example.com", Content: "203.0.113.10"},
	}
	rs := apply(c,
		Record{Type: "mx", Name: "Example.com.", Content: "mail.example.com", Priority: pri(10)},
		Record{Type: "TXT", Name: "example.com", Content: "v=spf1  mx -all"},
		Record{Type: "TXT", Name: "_dmarc.example.com", Content: "v=DMARC1; p=none"},
		Record{Type: "A", Name: "mail.example.com", Content: "203.0.113.10"},
	)
	if statuses(rs) != "exists,exists,exists,exists" || len(f.posts) != 0 {
		t.Errorf("%s, posts %d: %+v", statuses(rs), len(f.posts), rs)
	}
}

func TestADifferentRecordIsAConflictAndNothingIsChanged(t *testing.T) {
	for name, c := range map[string]struct {
		have Record
		want Record
	}{
		"address":          {Record{Type: "A", Name: "mail.example.com", Content: "203.0.113.9"}, Record{Type: "A", Name: "mail.example.com", Content: "203.0.113.10"}},
		"mx target":        {Record{Type: "MX", Name: "example.com", Content: "mx.other.net", Priority: pri(10)}, Record{Type: "MX", Name: "example.com", Content: "mail.example.com", Priority: pri(10)}},
		"mx priority":      {Record{Type: "MX", Name: "example.com", Content: "mail.example.com", Priority: pri(20)}, Record{Type: "MX", Name: "example.com", Content: "mail.example.com", Priority: pri(10)}},
		"spf":              {Record{Type: "TXT", Name: "example.com", Content: "v=spf1 include:_spf.google.com ~all"}, Record{Type: "TXT", Name: "example.com", Content: "v=spf1 mx -all"}},
		"dmarc":            {Record{Type: "TXT", Name: "_dmarc.example.com", Content: "v=DMARC1; p=reject"}, Record{Type: "TXT", Name: "_dmarc.example.com", Content: "v=DMARC1; p=none"}},
		"dkim selector":    {Record{Type: "TXT", Name: "mail._domainkey.example.com", Content: "v=DKIM1; p=OLD"}, Record{Type: "TXT", Name: "mail._domainkey.example.com", Content: "v=DKIM1; p=NEW"}},
		"cname in the way": {Record{Type: "CNAME", Name: "mail.example.com", Content: "host.other.net"}, Record{Type: "A", Name: "mail.example.com", Content: "203.0.113.10"}},
	} {
		f, cl, _ := newFake(t)
		c.have.ID = "1"
		f.recs = []Record{c.have}
		rs := apply(cl, c.want)
		if rs[0].Status != Conflict || rs[0].Note == "" {
			t.Errorf("%s: %+v", name, rs[0])
		}
		if len(f.posts) != 0 || f.bad != 0 || len(f.recs) != 1 {
			t.Errorf("%s: a conflict must change nothing (posts %d, other calls %d)", name, len(f.posts), f.bad)
		}
	}
}

func TestAnUnrelatedTXTDoesNotBlockSPF(t *testing.T) {
	f, c, _ := newFake(t)
	f.recs = []Record{{ID: "1", Type: "TXT", Name: "example.com", Content: `"google-site-verification=abc"`}}
	rs := apply(c, Record{Type: "TXT", Name: "example.com", Content: "v=spf1 mx -all"})
	if rs[0].Status != Added || len(f.posts) != 1 {
		t.Errorf("%+v", rs[0])
	}
}

func TestARecordAddedEarlierInTheCallIsKnownLater(t *testing.T) {
	f, c, _ := newFake(t)
	spf := Record{Type: "TXT", Name: "example.com", Content: "v=spf1 mx -all"}
	rs := apply(c, spf, spf, Record{Type: "TXT", Name: "example.com", Content: "v=spf1 a -all"})
	if statuses(rs) != "added,exists,conflict" || len(f.posts) != 1 {
		t.Errorf("%s, posts %d", statuses(rs), len(f.posts))
	}
}

func TestARecordThatAppearsMeanwhileCountsAsThere(t *testing.T) {
	f, c, _ := newFake(t)
	f.mode = "dup"
	rs := apply(c, Record{Type: "TXT", Name: "example.com", Content: "v=spf1 mx -all"})
	if rs[0].Status != Exists {
		t.Errorf("%+v", rs[0])
	}
}

func TestARefusedTokenOrTheRateLimitStopsTheCall(t *testing.T) {
	for _, mode := range []string{"forbid", "rate"} {
		f, c, _ := newFake(t)
		f.mode = mode
		rs := apply(c,
			Record{Type: "TXT", Name: "example.com", Content: "v=spf1 mx -all"},
			Record{Type: "TXT", Name: "_dmarc.example.com", Content: "v=DMARC1; p=none"},
			Record{Type: "TXT", Name: "mail._domainkey.example.com", Content: "v=DKIM1; p=X"})
		if statuses(rs) != "failed,failed,failed" || f.hits != 1 {
			t.Errorf("%s: %s after %d requests: %+v", mode, statuses(rs), f.hits, rs)
		}
		if !strings.Contains(rs[1].Note, "not tried") {
			t.Errorf("%s: %q", mode, rs[1].Note)
		}
	}
}

func TestRecordsThatCannotBeRightAreFailedWithoutAskingTheAPI(t *testing.T) {
	f, c, _ := newFake(t)
	rs := apply(c,
		Record{Type: "A", Name: "mail.example.com", Content: "2001:db8::1"},
		Record{Type: "AAAA", Name: "mail.example.com", Content: "203.0.113.10"},
		Record{Type: "MX", Name: "example.com", Content: "mail.example.com"},
		Record{Type: "SRV", Name: "example.com", Content: "x"},
		Record{Type: "TXT", Name: "example.com", Content: strings.Repeat("a", 3000)},
		Record{Type: "TXT", Name: "bad name.example.com", Content: "x"},
		Record{Type: "TXT", Name: "example.com", Content: ""},
	)
	for i, r := range rs {
		if r.Status != Failed || r.Note == "" {
			t.Errorf("%d: %+v", i, r)
		}
	}
	if f.hits != 0 {
		t.Errorf("%d requests for records that cannot be right", f.hits)
	}
}

func TestAFailedAddSaysSoWithoutTheToken(t *testing.T) {
	f, c, _ := newFake(t)
	f.echo, f.mode = true, "badpost"
	rs := apply(c, Record{Type: "TXT", Name: "example.com", Content: "v=spf1 mx -all"})
	if rs[0].Status != Failed || strings.Contains(rs[0].Note, tok) || !strings.Contains(rs[0].Note, "invalid content") {
		t.Errorf("%+v", rs[0])
	}
}

func TestNormalizingAndFamilies(t *testing.T) {
	for in, want := range map[string]string{
		`"abc" "def"`:     "abcdef",
		`  v=spf1   mx  `: "v=spf1 mx",
		`"a \"b\" c"`:     `a "b" c`,
		"plain":           "plain",
		`"v=DKIM1; p=Ab"`: "v=DKIM1; p=Ab", // case is kept: a key is case-sensitive
	} {
		if got := normalizeTXT(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	for _, c := range []struct{ name, content, want string }{
		{"example.com", "v=spf1 mx -all", "spf"},
		{"example.com", `"V=SPF1 mx"`, "spf"},
		{"_dmarc.example.com", "v=DMARC1; p=none", "dmarc"},
		{"mail._domainkey.example.com", "anything", "dkim"},
		{"example.com", "google-site-verification=1", "other"},
	} {
		if got := familyOf(c.name, c.content); got != c.want {
			t.Errorf("%s %q: %s, want %s", c.name, c.content, got, c.want)
		}
	}
}
