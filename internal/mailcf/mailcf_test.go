package mailcf

import (
	"strings"
	"testing"

	"mpn/internal/mail"
)

func records() []mail.Record {
	return mail.Records("example.com", "mail.example.com", "mpn", "v=DKIM1; k=rsa; p="+strings.Repeat("A", 392))
}

func TestTheRecordsOfTheMailServerBecomeRecordsOfCloudflare(t *testing.T) {
	got, err := Wanted("mail.example.com", records(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("MX, SPF, DKIM, DMARC: %+v", got)
	}
	mx := got[0]
	if mx.Type != "MX" || mx.Name != "example.com" || mx.Content != "mail.example.com" || mx.Priority == nil || *mx.Priority != 10 || mx.TTL != 1 {
		t.Errorf("MX: %+v", mx)
	}
	if got[1].Type != "TXT" || got[1].Content != "v=spf1 mx -all" || got[1].Name != "example.com" {
		t.Errorf("SPF: %+v", got[1])
	}
	if got[2].Name != "mpn._domainkey.example.com" || !strings.HasPrefix(got[2].Content, "v=DKIM1; k=rsa; p=AAAA") || len(got[2].Content) < 400 {
		t.Errorf("DKIM keeps the whole key: %d characters", len(got[2].Content))
	}
	if got[3].Name != "_dmarc.example.com" || !strings.HasPrefix(got[3].Content, "v=DMARC1;") {
		t.Errorf("DMARC: %+v", got[3])
	}
}

func TestTheAddressOfTheMailHostIsAddedOnlyWhenAsked_AndNeverProxied(t *testing.T) {
	got, _ := Wanted("MAIL.example.com.", records(), "203.0.113.7")
	if len(got) != 5 || got[0].Type != "A" || got[0].Name != "mail.example.com" || got[0].Content != "203.0.113.7" || got[0].Proxied {
		t.Errorf("A record first, grey cloud: %+v", got[0])
	}
	v6, _ := Wanted("mail.example.com", records(), "2001:db8::7")
	if v6[0].Type != "AAAA" {
		t.Errorf("an IPv6 address is AAAA: %+v", v6[0])
	}
	for _, r := range got {
		if r.Proxied {
			t.Errorf("mail does not pass through the proxy: %+v", r)
		}
	}
	if _, err := Wanted("mail.example.com; x", records(), "203.0.113.7"); err == nil {
		t.Error("a mail host that is not a host name is refused")
	}
}

func TestADamagedRecordIsRefusedInsteadOfSentOn(t *testing.T) {
	for _, bad := range [][]mail.Record{
		{{Type: "MX", Name: "example.com", Value: "mail.example.com."}},
		{{Type: "MX", Name: "example.com", Value: "x mail.example.com."}},
		{{Type: "MX", Name: "example.com", Value: "10 "}},
		{{Type: "TXT", Name: "example.com", Value: "  "}},
		{{Type: "CNAME", Name: "example.com", Value: "x"}},
		{},
	} {
		if _, err := Wanted("mail.example.com", bad, ""); err == nil {
			t.Errorf("%+v must be refused", bad)
		}
	}
}
