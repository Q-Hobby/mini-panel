package notify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

// A problem is told once when it has been seen twice, reminded every repeat, and told once when it ends; one bad check is not a problem.
func TestTrackerTellsOnceRemindsAndSolves(t *testing.T) {
	var tr Tracker
	p := []Problem{{"service:nginx", "nginx has stopped"}}
	step := func(i int, seen []Problem) []Change {
		return tr.Check(t0.Add(time.Duration(i)*time.Minute), seen, 30*time.Minute)
	}
	if c := step(0, p); len(c) != 0 {
		t.Fatalf("told after one check: %v", c)
	}
	if c := step(1, nil); len(c) != 0 {
		t.Fatalf("a problem seen once must not be solved: %v", c)
	}
	step(2, p)
	if c := step(3, p); len(c) != 1 || c[0].Kind != "problem" {
		t.Fatalf("%v", c)
	}
	for i := 4; i < 33; i++ {
		if c := step(i, p); len(c) != 0 {
			t.Fatalf("minute %d: %v", i, c)
		}
	}
	if c := step(33, p); len(c) != 1 || c[0].Kind != "again" {
		t.Fatalf("no reminder after 30 minutes: %v", c)
	}
	if got := tr.Open(); len(got) != 1 {
		t.Errorf("open: %v", got)
	}
	if c := step(34, nil); len(c) != 1 || c[0].Kind != "solved" {
		t.Fatalf("%v", c)
	}
	var quiet Tracker
	quiet.Check(t0, p, 0)
	quiet.Check(t0.Add(time.Minute), p, 0)
	if c := quiet.Check(t0.Add(10*time.Hour), p, 0); len(c) != 0 {
		t.Errorf("repeat 0 must never remind: %v", c)
	}
}

func TestCPUMustStayHighForTheWholeWindow(t *testing.T) {
	var w Watcher
	r := DefaultRules() // 90% for 5 minutes
	var got []string
	for i := 0; i <= 7; i++ {
		cpu := 95.0
		if i == 3 {
			cpu = 40 // one quiet minute starts the window again
		}
		got = append(got, w.Check(t0.Add(time.Duration(i)*time.Minute), "web-1", "en", r, Readings{CPU: cpu})...)
	}
	if len(got) != 0 {
		t.Fatalf("told before 5 minutes in a row: %v", got)
	}
	for i := 8; i <= 10; i++ {
		got = append(got, w.Check(t0.Add(time.Duration(i)*time.Minute), "web-1", "en", r, Readings{CPU: 95})...)
	}
	if len(got) != 1 || !strings.Contains(got[0], "CPU 95%") || !strings.Contains(got[0], "<b>web-1</b>") {
		t.Fatalf("%v", got)
	}
}

// Names and errors come from outside; they must not make HTML of their own in a message.
func TestMessagesEscapeWhatComesFromOutside(t *testing.T) {
	var w Watcher
	r := DefaultRules()
	in := Readings{SitesDown: []Down{{Name: "<b>x</b>", Why: "a & b"}}}
	w.Check(t0, "m<1>", "vi", r, in)
	got := w.Check(t0.Add(time.Minute), "m<1>", "vi", r, in)
	if len(got) != 1 || strings.Contains(got[0], "<b>x</b>") || !strings.Contains(got[0], "&lt;b&gt;x&lt;/b&gt;") || !strings.Contains(got[0], "a &amp; b") ||
		!strings.Contains(got[0], "m&lt;1&gt;") || !strings.Contains(got[0], "không trả lời") {
		t.Fatalf("%v", got)
	}
}

func TestRulesSwitchKindsOff(t *testing.T) {
	var w Watcher
	r := DefaultRules()
	r.Services, r.Ops = false, false
	in := Readings{Down: []Down{{"nginx", "failed"}}, Certs: []Cert{{"blog", 3}}, Disks: []DiskUse{{"/", 99}}}
	w.Check(t0, "a", "en", r, in)
	got := w.Check(t0.Add(time.Minute), "a", "en", r, in)
	if len(got) != 1 || !strings.Contains(got[0], "disk / is 99% full") {
		t.Fatalf("%v", got)
	}
}

func TestEveryMessageHasEveryLanguage(t *testing.T) {
	for key := range messages["en"] {
		for _, l := range []string{"vi", "zh"} {
			m, ok := messages[l][key]
			if !ok {
				t.Errorf("%s has no %s", l, key)
				continue
			}
			if strings.Count(m, "%")-2*strings.Count(m, "%%") != strings.Count(messages["en"][key], "%")-2*strings.Count(messages["en"][key], "%%") {
				t.Errorf("%s/%s: not the same arguments as English", l, key)
			}
		}
	}
	if got := Msg("zh", "hub.lost", "a", "b", 3); !strings.Contains(got, "<b>a</b>") || !strings.Contains(got, "3 分钟") || !strings.Contains(got, "（b）") {
		t.Errorf("%q", got)
	}
}

func TestPairingCodesCarryTheHubAndRefuseJunk(t *testing.T) {
	pin := strings.Repeat("ab", 32)
	code := MakeCode("https://1.2.3.4:2610", pin, "s3cret")
	u, p, s, err := ReadCode(" " + code[:20] + "\n" + code[20:] + " ")
	if err != nil || u != "https://1.2.3.4:2610" || p != pin || s != "s3cret" {
		t.Fatalf("%q %q %q %v", u, p, s, err)
	}
	for _, bad := range []string{"", "mpn1.", "mpn1.!!!", "hello", MakeCode("http://1.2.3.4", pin, "x"), MakeCode("https://h/x", pin, "x"), MakeCode("https://h", "short", "x")} {
		if _, _, _, err := ReadCode(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if !SameHash("abc", Hash("abc")) || SameHash("abd", Hash("abc")) || Hash("abc") == "abc" {
		t.Error("hash")
	}
}

func TestTokenCheck(t *testing.T) {
	if ValidToken("123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw") != nil {
		t.Error("a real-looking token refused")
	}
	for _, bad := range []string{"", "abc", "123:abc", "123456789:AA x", "123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw/../x"} {
		if ValidToken(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// The agent trusts the hub by the key of its certificate (often self-signed), and refuses any other certificate.
func TestPinnedClientAcceptsOnlyTheHub(t *testing.T) {
	hub := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) }))
	defer hub.Close()
	h := sha256.Sum256(hub.Certificate().RawSubjectPublicKeyInfo)
	good := hex.EncodeToString(h[:])
	if resp, err := PinnedClient(hub.URL, good, 5*time.Second).Get(hub.URL); err != nil {
		t.Fatalf("pinned hub refused: %v", err)
	} else {
		resp.Body.Close()
	}
	_, err := PinnedClient(hub.URL, strings.Repeat("0", 64), 5*time.Second).Get(hub.URL)
	if err == nil || !errors.Is(err, ErrPin) && !strings.Contains(err.Error(), "another certificate") {
		t.Fatalf("another key accepted: %v", err)
	}
}

func TestBotTalksToTelegramAndHidesTheToken(t *testing.T) {
	const token = "123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw"
	var sent []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch {
		case r.URL.Path == "/bot"+token+"/getMe":
			w.Write([]byte(`{"ok":true,"result":{"username":"mpn_bot"}}`))
		case r.URL.Path == "/bot"+token+"/sendMessage":
			sent = append(sent, r.PostForm.Get("chat_id")+":"+r.PostForm.Get("parse_mode")+":"+r.PostForm.Get("text"))
			w.Write([]byte(`{"ok":true,"result":{}}`))
		case r.URL.Path == "/bot"+token+"/getUpdates":
			w.Write([]byte(`{"ok":true,"result":[{"update_id":7,"message":{"text":"/status@mpn_bot","chat":{"id":-100,"type":"group","title":"Ops"}}},{"update_id":8,"message":{"text":"hi","chat":{"id":5,"type":"private","first_name":"An","username":"an"}}}]}`))
		case strings.HasSuffix(r.URL.Path, "/conflict"):
			w.Write([]byte(`{"ok":false,"error_code":409,"description":"Conflict"}`))
		default:
			w.WriteHeader(401)
			w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
		}
	}))
	defer api.Close()
	b := Bot{Token: token, API: api.URL}
	ctx := context.Background()
	if me, err := b.Me(ctx); err != nil || me != "mpn_bot" {
		t.Fatalf("%q %v", me, err)
	}
	if err := b.Send(ctx, -100, "<b>x</b>"); err != nil || len(sent) != 1 || sent[0] != "-100:HTML:<b>x</b>" {
		t.Fatalf("%v %v", sent, err)
	}
	ups, err := b.Updates(ctx, 0, time.Second)
	if err != nil || len(ups) != 2 || Command(ups[0].Text) != "/status" || ups[0].ChatID != -100 || ups[0].Title != "Ops" || ups[1].Title != "An @an" || !ups[1].Private {
		t.Fatalf("%+v %v", ups, err)
	}
	if _, err := (Bot{Token: "1:bad", API: api.URL}).Me(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("%v", err)
	}
	if err := b.call(ctx, "conflict", nil, nil); !errors.Is(err, ErrConflict) {
		t.Errorf("%v", err)
	}
	// A failure to connect repeats the address, which holds the token.
	_, err = (Bot{Token: token, API: "http://127.0.0.1:1"}).Me(ctx)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Errorf("the token leaked: %v", err)
	}
}

func TestSplitKeepsEveryPartUnderTheLimit(t *testing.T) {
	text := strings.Repeat("a line of the status\n", 400)
	parts := Split(text)
	if len(parts) < 2 || strings.Join(parts, "") != text {
		t.Fatalf("%d parts", len(parts))
	}
	for _, p := range parts {
		if len(p) > 4096 {
			t.Errorf("%d bytes", len(p))
		}
	}
}

func TestStateRemembersAddresses(t *testing.T) {
	var s State
	if !s.KnowIP("1.1.1.1") || s.KnowIP("1.1.1.1") || !s.KnowIP("2.2.2.2") {
		t.Fatal("KnowIP")
	}
	for i := 0; i < 300; i++ {
		s.KnowIP("10.0.0." + string(rune('0'+i%10)) + strings.Repeat("x", i/10))
	}
	if len(s.KnownIPs) != 200 {
		t.Errorf("%d", len(s.KnownIPs))
	}
}

func TestRulesAreChecked(t *testing.T) {
	if DefaultRules().Valid() != nil {
		t.Fatal("the defaults are not valid")
	}
	r := DefaultRules()
	r.Disk = 100
	if r.Valid() == nil {
		t.Error("disk 100% accepted")
	}
	b, _ := json.Marshal(DefaultRules())
	var back Rules
	if json.Unmarshal(b, &back) != nil || back != DefaultRules() {
		t.Error("rules do not survive JSON")
	}
}
