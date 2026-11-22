package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"mpn/internal/audit"
	"mpn/internal/notify"
	"mpn/internal/security"
	"mpn/internal/store"
	"mpn/internal/update"
)

// notifier is the mpn-notify service. On the hub it watches this machine, takes the reports of the agents (through the panel, which
// passes them on to its socket), sends every message to Telegram and answers the bot. On an agent it watches this machine and reports
// to the hub every minute; when the hub stays silent it sends to Telegram itself.
type notifier struct {
	db    string
	cfg   notify.Config
	mach  *machine
	watch notify.Watcher

	mu     sync.Mutex
	state  notify.State
	remote notify.Remote
	events []string // an agent's messages waiting for the next report
	nodes  map[int64]*notify.Watcher
	report notify.Report
}

func runNotifyService(db string) {
	requireLinuxRoot()
	n := &notifier{db: db, mach: newMachine(db), nodes: map[int64]*notify.Watcher{}}
	if err := notify.Load(notify.ConfigFile, &n.cfg); err != nil {
		fail(err)
	}
	_ = notify.Load(notify.StateFile, &n.state)
	_ = notify.Load(notify.RemoteFile, &n.remote)
	if n.cfg.Role == "" {
		log.Printf("alerts are not set up (mpn notify); waiting")
		waitForSignal()
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	n.seed()
	log.Printf("alerts: this machine is %s, %s", n.cfg.Name, n.cfg.Role)
	if n.cfg.Role == notify.RoleHub {
		go n.serveHub(ctx)
		go n.botLoop(ctx)
		go n.sendLoop(ctx)
	}
	tick := time.NewTicker(notify.BeatEvery)
	defer tick.Stop()
	for {
		n.check(time.Now())
		select {
		case <-ctx.Done():
			n.saveState()
			return
		case <-tick.C:
		}
	}
}

func waitForSignal() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}

// seed remembers what is there when the service first starts, so the first minute does not report every ban and sign-in of the past.
func (n *notifier) seed() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.state.Seeded {
		return
	}
	if st, err := store.Open(n.db); err == nil {
		n.state.LastAudit, _ = st.LastAuditID()
		st.Close()
	}
	n.state.Bans = currentBans()
	n.state.Release = version
	now := time.Now().Unix()
	n.state.BackupAt, n.state.RenewAt = now, now
	n.state.Seeded = true
	n.saveStateLocked()
}

func (n *notifier) saveState() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.saveStateLocked()
}

func (n *notifier) saveStateLocked() {
	if err := notify.Save(notify.StateFile, n.state); err != nil {
		log.Printf("alerts: could not save the state: %v", err)
	}
}

// lang, rules and name are the hub's for the hub, and what the hub last said for an agent.
func (n *notifier) lang() string {
	if n.cfg.Role == notify.RoleAgent && n.remote.Lang != "" {
		return n.remote.Lang
	}
	if n.cfg.Lang != "" {
		return n.cfg.Lang
	}
	return "en"
}

func (n *notifier) rules() notify.Rules {
	if n.cfg.Role == notify.RoleAgent {
		if n.remote.Rules.CPU == 0 {
			return notify.DefaultRules()
		}
		return n.remote.Rules
	}
	if n.cfg.Rules.CPU == 0 {
		return notify.DefaultRules()
	}
	return n.cfg.Rules
}

// check is one minute: read this machine, find what to tell, and tell it (on the hub: queue it; on an agent: report it).
func (n *notifier) check(now time.Time) {
	r := n.mach.read(now)
	n.mu.Lock()
	lang, rules, name := n.lang(), n.rules(), n.cfg.Name
	msgs := n.watch.Check(now, name, lang, rules, r)
	msgs = append(msgs, n.events_(now, lang, rules, name)...)
	n.report = notify.Report{Version: version, Stats: r.Stats(now), Alerts: n.watch.Track.Open()}
	n.state.Report = n.report
	n.saveStateLocked()
	n.mu.Unlock()
	if n.cfg.Role == notify.RoleHub {
		n.queue(msgs...)
		n.checkNodes(now)
		return
	}
	n.beat(now, msgs)
}

// events_ reads what happened since the last check: the activity log, fail2ban, the nightly backup, the renewal of certificates and
// a new release. n.mu is held.
func (n *notifier) events_(now time.Time, lang string, r notify.Rules, name string) []string {
	var out []string
	host := notify.Escape(name)
	if st, err := store.Open(n.db); err == nil {
		rows, _ := st.AuditSince(n.state.LastAudit, 500)
		st.Close()
		fails, lastFailIP := 0, ""
		for _, e := range rows {
			n.state.LastAudit = e.ID
			switch {
			case e.Op == "login.ok":
				if n.state.KnowIP(e.IP) && r.Security && len(n.state.KnownIPs) > 1 {
					out = append(out, notify.Msg(lang, "login.new", host, notify.Escape(e.IP)))
				}
			case e.Op == "login.fail" || e.Op == "login.2fa_fail":
				fails, lastFailIP = fails+1, e.IP
			case e.Danger && r.Danger:
				what := audit.OpName(lang, e.Op)
				target := e.Site
				if target == "" {
					target = e.Detail
				} else if e.Detail != "" {
					target += " (" + e.Detail + ")"
				}
				key := "danger"
				if !e.OK {
					key = "danger.fail"
				}
				ip := e.IP
				if ip == "" {
					ip = "root"
				}
				out = append(out, notify.Msg(lang, key, host, notify.Escape(what), notify.Escape(target), notify.Escape(ip)))
			}
		}
		// Many wrong passwords are told once in ten minutes at most; fail2ban bans the address in the meantime.
		if fails >= 5 && r.Security && now.Unix()-n.state.FailsAt >= 600 {
			n.state.FailsAt = now.Unix()
			out = append(out, notify.Msg(lang, "login.fails", host, fails, notify.Escape(lastFailIP)))
		}
	}
	if r.Security {
		bans := currentBans()
		for jail, ips := range bans {
			old := map[string]bool{}
			for _, ip := range n.state.Bans[jail] {
				old[ip] = true
			}
			for _, ip := range ips {
				if !old[ip] {
					out = append(out, notify.Msg(lang, "banned", host, notify.Escape(ip), notify.Escape(jail)))
				}
			}
		}
		if bans != nil {
			n.state.Bans = bans
		}
	}
	if r.Ops {
		if res, at := unitResult("mpn-backup.service"); res != "" && at.Unix() > n.state.BackupAt+60 { // the time is worked out from the uptime and may move by a second
			n.state.BackupAt = at.Unix()
			if res == "success" {
				sites := backupSites(at.Add(-6 * time.Hour))
				out = append(out, notify.Msg(lang, "backup.ok", host, notify.Escape(strings.Join(sites, ", "))))
			} else {
				out = append(out, notify.Msg(lang, "backup.fail", host, "", notify.Escape(journalTail("mpn-backup.service", 5))))
			}
		}
		if res, at := unitResult("mpn-renew.service"); res != "" && at.Unix() > n.state.RenewAt+60 {
			n.state.RenewAt = at.Unix()
			if res != "success" {
				out = append(out, notify.Msg(lang, "renew.fail", host, notify.Escape(journalTail("mpn-renew.service", 5))))
			}
		}
		// A new release is looked for twice a day, and only on the hub: it is the same for every machine, and each machine's version is
		// in its report.
		if n.cfg.Role == notify.RoleHub && now.Unix()-n.state.ReleaseAt >= 12*3600 {
			n.state.ReleaseAt = now.Unix()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			tag, err := latestTag(ctx)
			cancel()
			if err == nil {
				if latest, err := update.ParseVersion(tag); err == nil && tag != n.state.Release {
					if cur, err := update.ParseVersion(version); err == nil && cur.Less(latest) {
						n.state.Release = tag
						out = append(out, notify.Msg(lang, "release", notify.Escape(tag), host, notify.Escape(version)))
					}
				}
			}
		}
	}
	return out
}

// currentBans are the addresses each jail of mpn bans now; nil when fail2ban does not answer.
func currentBans() map[string][]string {
	if !unitActive("fail2ban.service") {
		return nil
	}
	out := map[string][]string{}
	for _, j := range security.Jails {
		out[j] = readJail(j).IPs
	}
	return out
}

// queue keeps messages for the sender of the hub.
func (n *notifier) queue(msgs ...string) {
	if len(msgs) == 0 {
		return
	}
	st, err := store.Open(n.db)
	if err != nil {
		log.Printf("alerts: %v", err)
		return
	}
	defer st.Close()
	for _, m := range msgs {
		if err := st.QueueNotify(m); err != nil {
			log.Printf("alerts: could not queue a message: %v", err)
		}
	}
}

// sendLoop sends the queued messages in order, and waits when Telegram asks it to or cannot be reached.
func (n *notifier) sendLoop(ctx context.Context) {
	bot := notify.Bot{Token: n.cfg.Token}
	for ctx.Err() == nil {
		wait := 3 * time.Second
		if n.cfg.Token != "" && n.cfg.ChatID != 0 {
			if st, err := store.Open(n.db); err == nil {
				msgs, _ := st.NotifyOutbox(20)
				for _, m := range msgs {
					err := sendAll(ctx, bot, n.cfg.ChatID, m.Text)
					var retry notify.RetryError
					if errors.As(err, &retry) {
						wait = retry.After
						break
					}
					if err != nil {
						log.Printf("alerts: Telegram: %v", err)
						wait = 30 * time.Second
						break
					}
					_ = st.DoneNotify(m.ID)
					time.Sleep(1100 * time.Millisecond) // Telegram takes about one message a second in one chat
				}
				st.Close()
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

func sendAll(ctx context.Context, bot notify.Bot, chat int64, text string) error {
	for _, part := range notify.Split(text) {
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := bot.Send(c, chat, part)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

// botLoop reads what is sent to the bot: /start remembers a chat for the page, /status is answered in the chat chosen.
func (n *notifier) botLoop(ctx context.Context) {
	bot := notify.Bot{Token: n.cfg.Token}
	if n.cfg.Token == "" {
		return
	}
	for ctx.Err() == nil {
		ups, err := bot.Updates(ctx, n.state.Offset, 50*time.Second)
		if err != nil {
			n.mu.Lock()
			n.state.BotError = err.Error()
			n.mu.Unlock()
			if errors.Is(err, notify.ErrConflict) {
				log.Printf("alerts: %v; is a second hub using this bot?", err)
			}
			select {
			case <-ctx.Done():
			case <-time.After(30 * time.Second):
			}
			continue
		}
		n.mu.Lock()
		n.state.BotError = ""
		n.mu.Unlock()
		for _, u := range ups {
			n.mu.Lock()
			n.state.Offset = u.ID + 1
			n.mu.Unlock()
			if u.ChatID == 0 {
				continue
			}
			switch notify.Command(u.Text) {
			case "/start":
				if st, err := store.Open(n.db); err == nil {
					_ = st.AddNotifyChat(u.ChatID, u.Title)
					st.Close()
				}
				key := "start.no"
				if n.cfg.ChatID == 0 || n.cfg.ChatID == u.ChatID {
					key = "start.ok"
				}
				_ = bot.Send(ctx, u.ChatID, notify.Msg(n.lang(), key))
			case "/status":
				if u.ChatID == n.cfg.ChatID { // nobody else learns anything of the machines
					_ = sendAll(ctx, bot, u.ChatID, n.statusText(time.Now()))
				}
			case "/help":
				if u.ChatID == n.cfg.ChatID {
					_ = bot.Send(ctx, u.ChatID, notify.Msg(n.lang(), "help"))
				}
			}
		}
		n.saveState()
	}
}

// statusText is the answer to /status: every machine, with its numbers and its open problems.
func (n *notifier) statusText(now time.Time) string {
	lang := n.lang()
	n.mu.Lock()
	own := n.report
	n.mu.Unlock()
	type line struct {
		name string
		rep  notify.Report
		lost time.Duration
	}
	lines := []line{{n.cfg.Name, own, 0}}
	if st, err := store.Open(n.db); err == nil {
		nodes, _ := st.NotifyNodes()
		st.Close()
		for _, nd := range nodes {
			var rep notify.Report
			_ = json.Unmarshal([]byte(nd.Report), &rep)
			l := line{name: nd.Name, rep: rep}
			if d := now.Sub(time.Unix(nd.LastSeen, 0)); d >= notify.LostAfter {
				l.lost = d
			}
			lines = append(lines, l)
		}
	}
	sort.SliceStable(lines[1:], func(i, j int) bool { return lines[1+i].name < lines[1+j].name })
	var b strings.Builder
	b.WriteString(notify.Msg(lang, "status.title", len(lines)) + "\n")
	for _, l := range lines {
		if l.lost > 0 {
			b.WriteString(notify.Msg(lang, "status.lost", notify.Escape(l.name), notify.Duration(l.lost)) + "\n")
			continue
		}
		mark := "🟢"
		if len(l.rep.Alerts) > 0 {
			mark = "🔴"
		}
		s := l.rep.Stats
		b.WriteString(notify.Msg(lang, "status.line", mark, notify.Escape(l.name), s.CPU, s.Mem, s.Disk, notify.Duration(time.Duration(s.Uptime)*time.Second)) + "\n")
		for _, a := range l.rep.Alerts {
			b.WriteString(notify.Msg(lang, "status.alert", a) + "\n")
		}
	}
	return b.String()
}

// checkNodes reports the agents that went silent, once each until they are back.
func (n *notifier) checkNodes(now time.Time) {
	st, err := store.Open(n.db)
	if err != nil {
		return
	}
	defer st.Close()
	nodes, _ := st.NotifyNodes()
	for _, nd := range nodes {
		silent := now.Sub(time.Unix(nd.LastSeen, 0))
		if silent >= notify.LostAfter && !nd.Lost {
			_ = st.SetNotifyNodeLost(nd.ID, true)
			_ = st.QueueNotify(notify.Msg(n.lang(), "lost", notify.Escape(nd.Name), int(silent.Minutes())))
		}
	}
}

// serveHub answers the panel, which passes on to this socket what agents send to /_notify/ of the panel's port.
func (n *notifier) serveHub(ctx context.Context) {
	_ = os.MkdirAll("/run/mpn-notify", 0o750)
	_ = os.Remove(notify.Socket)
	l, err := net.Listen("unix", notify.Socket)
	if err != nil {
		log.Printf("alerts: cannot listen on %s: %v", notify.Socket, err)
		return
	}
	// The panel's account reaches the socket through the group of its folder.
	if err := chownToWebGroup("/run/mpn-notify", notify.Socket); err != nil {
		log.Printf("alerts: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /pair", n.hubPair)
	mux.HandleFunc("POST /beat", n.hubBeat)
	srv := &http.Server{Handler: http.MaxBytesHandler(mux, 256<<10), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second}
	go func() { <-ctx.Done(); srv.Close() }()
	_ = srv.Serve(l)
}

func chownToWebGroup(dir, sock string) error {
	gid, err := lookupGID("mpn-web")
	if err != nil {
		return err
	}
	if err := os.Chown(dir, 0, gid); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		return err
	}
	if err := os.Chown(sock, 0, gid); err != nil {
		return err
	}
	return os.Chmod(sock, 0o660)
}

type pairAnswer struct {
	ID     int64         `json:"id"`
	Key    string        `json:"key"`
	Remote notify.Remote `json:"remote"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// hubPair takes a pairing code once and gives the new agent its own key.
func (n *notifier) hubPair(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Secret string `json:"secret"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || in.Secret == "" {
		writeJSON(w, 400, map[string]string{"error": "bad request"})
		return
	}
	st, err := store.Open(n.db)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "the hub cannot open its database"})
		return
	}
	defer st.Close()
	key := notify.Token()
	node, err := st.UseNotifyPair(notify.Hash(in.Secret), notify.Hash(key))
	if err != nil {
		writeJSON(w, 403, map[string]string{"error": "this pairing code is not valid or was used already"})
		return
	}
	log.Printf("alerts: %s paired from %s", node.Name, r.Header.Get("X-Real-IP"))
	writeJSON(w, 200, pairAnswer{ID: node.ID, Key: key, Remote: n.remoteFor(node)})
}

// remoteFor is what an agent gets back: its name, the bot, the chat and its rules.
func (n *notifier) remoteFor(node store.NotifyNode) notify.Remote {
	rules := n.rules()
	if node.Rules != "" {
		var own notify.Rules
		if json.Unmarshal([]byte(node.Rules), &own) == nil && own.Valid() == nil {
			rules = own
		}
	}
	return notify.Remote{Name: node.Name, Lang: n.lang(), Token: n.cfg.Token, ChatID: n.cfg.ChatID, Rules: rules}
}

// hubBeat takes the report of an agent: its state for /status and the page, and its messages for Telegram.
func (n *notifier) hubBeat(w http.ResponseWriter, r *http.Request) {
	var b notify.Beat
	if json.NewDecoder(r.Body).Decode(&b) != nil || b.ID == 0 || b.Key == "" {
		writeJSON(w, 400, map[string]string{"error": "bad request"})
		return
	}
	st, err := store.Open(n.db)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "the hub cannot open its database"})
		return
	}
	defer st.Close()
	node, err := st.NotifyNode(b.ID)
	if err != nil || !notify.SameHash(b.Key, node.KeyHash) {
		writeJSON(w, 403, map[string]string{"error": "this machine is not known to the hub; pair it again"})
		return
	}
	if len(b.Events) > notify.MaxEvents {
		b.Events = b.Events[len(b.Events)-notify.MaxEvents:]
	}
	if len(b.Report.Alerts) > 50 {
		b.Report.Alerts = b.Report.Alerts[:50]
	}
	rep, _ := json.Marshal(b.Report)
	wasLost, err := st.SeeNotifyNode(node.ID, string(rep))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if wasLost {
		_ = st.QueueNotify(notify.Msg(n.lang(), "back", notify.Escape(node.Name)))
	}
	for _, e := range b.Events {
		// The messages are written by the agent, which escapes what came from outside; a message that is too long is cut.
		if len(e) > 3500 {
			e = e[:3500]
		}
		_ = st.QueueNotify(e)
	}
	writeJSON(w, 200, n.remoteFor(node))
}

// beat is the agent's report to the hub. When the hub has not answered for LostAfter, the agent sends what it has to Telegram itself,
// with a message that the hub is silent; when the hub answers again, it says so once.
func (n *notifier) beat(now time.Time, msgs []string) {
	n.mu.Lock()
	n.events = append(n.events, msgs...)
	if len(n.events) > notify.MaxEvents {
		n.events = n.events[len(n.events)-notify.MaxEvents:]
	}
	b := notify.Beat{ID: n.cfg.NodeID, Key: n.cfg.NodeKey, Report: n.report, Events: append([]string{}, n.events...)}
	n.mu.Unlock()
	remote, err := postHub[notify.Remote](n.cfg.HubURL, n.cfg.HubPin, "/_notify/beat", b)
	n.mu.Lock()
	defer n.mu.Unlock()
	if err == nil {
		wasLost := n.state.HubLost
		n.events, n.remote = nil, remote
		n.state.Agent.LastOK, n.state.Agent.LastError, n.state.HubLost = now.Unix(), "", false
		_ = notify.Save(notify.RemoteFile, n.remote)
		if wasLost {
			n.direct(notify.Msg(n.lang(), "hub.back", notify.Escape(n.cfg.Name)))
		}
		n.saveStateLocked()
		return
	}
	n.state.Agent.LastError, n.state.Agent.ErrorAt = err.Error(), now.Unix()
	last := n.state.Agent.LastOK
	if last == 0 {
		last = now.Unix()
		n.state.Agent.LastOK = last
	}
	if now.Sub(time.Unix(last, 0)) >= notify.LostAfter {
		if !n.state.HubLost {
			n.state.HubLost = true
			n.direct(notify.Msg(n.lang(), "hub.lost", notify.Escape(n.cfg.Name), notify.Escape(hostOf(n.cfg.HubURL)), int(now.Sub(time.Unix(last, 0)).Minutes())))
		}
		for _, m := range n.events {
			n.direct(m)
		}
		n.events = nil
	}
	n.saveStateLocked()
}

// direct sends a message from an agent to Telegram with the bot the hub shared. n.mu is held; it is short.
func (n *notifier) direct(text string) {
	if n.remote.Token == "" || n.remote.ChatID == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := sendAll(ctx, notify.Bot{Token: n.remote.Token}, n.remote.ChatID, text); err != nil {
		log.Printf("alerts: Telegram: %v", err)
		return
	}
	log.Printf("alerts: sent to Telegram directly")
}

func hostOf(u string) string {
	s := strings.TrimPrefix(u, "https://")
	if h, _, err := net.SplitHostPort(s); err == nil {
		return h
	}
	return s
}

// postHub sends JSON to the hub and reads its answer.
func postHub[T any](hubURL, pin, path string, body any) (T, error) {
	var out T
	raw, _ := json.Marshal(body)
	c := notify.PinnedClient(hubURL, pin, 30*time.Second)
	resp, err := c.Post(hubURL+path, "application/json", strings.NewReader(string(raw)))
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return out, fmt.Errorf("the hub answered %d: %s", resp.StatusCode, e.Error)
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}
