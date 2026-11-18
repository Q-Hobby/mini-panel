package mailcf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"mpn/internal/cfdns"
	"mpn/internal/mail"
)

const testToken = "tokentokentokentokentoken123456"

// fakeCF is a Cloudflare with one zone and the records in it. It records what was created.
type fakeCF struct {
	mu      sync.Mutex
	zone    string
	records []map[string]any
	created []map[string]any
	auth    []string
}

func (f *fakeCF) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	reply := func(result any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": result, "result_info": map[string]any{"total_pages": 1}})
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/zones":
		if r.URL.Query().Get("name") == f.zone {
			reply([]map[string]any{{"id": "zone123", "name": f.zone}})
			return
		}
		reply([]any{})
	case r.Method == "GET" && r.URL.Path == "/zones/zone123/dns_records":
		var out []map[string]any
		for _, rec := range f.records {
			if t := r.URL.Query().Get("type"); t != "" && rec["type"] != t {
				continue
			}
			if n := r.URL.Query().Get("name"); n != "" && rec["name"] != n {
				continue
			}
			out = append(out, rec)
		}
		reply(out)
	case r.Method == "POST" && r.URL.Path == "/zones/zone123/dns_records":
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		b["id"] = "rec" + string(rune('a'+len(f.created)))
		f.created = append(f.created, b)
		f.records = append(f.records, b)
		reply(b)
	default:
		w.WriteHeader(404)
	}
}

func setup(t *testing.T, f *fakeCF) cfdns.Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return cfdns.Client{Token: testToken, BaseURL: srv.URL}
}

func recs() []mail.Record {
	return mail.Records("example.com", "mail.example.com", "mpn", "v=DKIM1; k=rsa; p="+strings.Repeat("B", 392))
}

func byStatus(rs []cfdns.Result) map[string]int {
	m := map[string]int{}
	for _, r := range rs {
		m[string(r.Status)]++
	}
	return m
}

func TestAMailDomainGetsItsFiveRecordsAndTheHostIsNotProxied(t *testing.T) {
	f := &fakeCF{zone: "example.com"}
	rep, err := Add(context.Background(), setup(t, f), "example.com", "mail.example.com", recs(), "203.0.113.7")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Zone != "example.com" || byStatus(rep.Results)["added"] != 5 || len(f.created) != 5 {
		t.Fatalf("%+v created %d", byStatus(rep.Results), len(f.created))
	}
	first := f.created[0]
	if first["type"] != "A" || first["name"] != "mail.example.com" || first["content"] != "203.0.113.7" || first["proxied"] != false {
		t.Errorf("the address of the mail host first, grey cloud: %+v", first)
	}
	for _, a := range f.auth {
		if a != "Bearer "+testToken {
			t.Errorf("every call carries the token: %q", a)
		}
	}
}

func TestARecordThatIsAlreadyThereIsLeftAndADifferentOneIsNotTouched(t *testing.T) {
	f := &fakeCF{zone: "example.com", records: []map[string]any{
		{"id": "x1", "type": "A", "name": "mail.example.com", "content": "198.51.100.1", "ttl": 1, "proxied": false}, // points elsewhere on purpose
		{"id": "x2", "type": "TXT", "name": "example.com", "content": "\"v=spf1 include:_spf.google.com ~all\"", "ttl": 1},
		{"id": "x3", "type": "MX", "name": "example.com", "content": "mail.example.com", "priority": float64(10), "ttl": 1},
	}}
	rep, err := Add(context.Background(), setup(t, f), "example.com", "mail.example.com", recs(), "203.0.113.7")
	if err != nil {
		t.Fatal(err)
	}
	got := byStatus(rep.Results)
	if got["conflict"] != 1 || got["exists"] != 1 || got["added"] != 2 {
		t.Errorf("the SPF of somebody else conflicts, the MX exists, DKIM and DMARC are added: %v", got)
	}
	for _, c := range f.created {
		if c["type"] == "A" {
			t.Errorf("the mail host already has an address and must be left alone: %+v", c)
		}
	}
	if len(f.records) != 3+2 {
		t.Errorf("nothing was deleted or changed: %d records", len(f.records))
	}
}

func TestTheHostInAnotherZoneIsNotAdded(t *testing.T) {
	f := &fakeCF{zone: "example.com"}
	rep, err := Add(context.Background(), setup(t, f), "example.com", "mail.other.net", recs(), "203.0.113.7")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range f.created {
		if c["type"] == "A" || c["type"] == "AAAA" {
			t.Errorf("this token may not reach another zone: %+v", c)
		}
	}
	if byStatus(rep.Results)["added"] != 4 {
		t.Errorf("%v", byStatus(rep.Results))
	}
}

func TestADomainThatIsNotAZoneOfTheAccountIsAnErrorWithoutTheToken(t *testing.T) {
	f := &fakeCF{zone: "other.com"}
	_, err := Add(context.Background(), setup(t, f), "example.com", "mail.example.com", recs(), "")
	if err == nil || !strings.Contains(err.Error(), "not a zone") {
		t.Fatalf("%v", err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Error("the token must never be in an error")
	}
}

func TestASubdomainFindsTheZoneOfItsParent(t *testing.T) {
	f := &fakeCF{zone: "example.com"}
	rep, err := Add(context.Background(), setup(t, f), "shop.example.com", "mail.example.com", mail.Records("shop.example.com", "mail.example.com", "mpn", "v=DKIM1; p=AAAA"), "")
	if err != nil || rep.Zone != "example.com" {
		t.Fatalf("%+v %v", rep, err)
	}
	for _, c := range f.created {
		if !strings.HasSuffix(c["name"].(string), "shop.example.com") {
			t.Errorf("the records are those of the subdomain: %+v", c)
		}
	}
}
