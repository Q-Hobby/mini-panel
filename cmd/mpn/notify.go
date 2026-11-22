package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"time"

	"mpn/internal/notify"
	"mpn/internal/panelssl"
	"mpn/internal/store"
)

func notifyUsage() {
	fmt.Fprintln(os.Stderr, `usage: mpn notify <command> [flags]
  status [-json]                       what this machine is in the alerts
  hub -name N -lang L -apply           make this machine the hub; the bot token is read from standard input
  chat -id ID -apply                   send the alerts to this chat (one that sent /start to the bot)
  test                                 send a test message
  rules [-cpu ...] -apply              what is reported and when (hub)
  pair -name N -url https://... -apply print a pairing code for a new machine (hub)
  node-rules -id ID [-reset] ... -apply  rules of one machine (hub)
  node-remove -id ID -apply            forget a machine (hub)
  agent -name N -apply                 connect this machine to a hub; the pairing code is read from standard input
  off -apply                           stop the alerts on this machine
  service                              run the alerts (the mpn-notify service)`)
	os.Exit(2)
}

func runNotify(args []string) {
	if len(args) == 0 {
		notifyUsage()
	}
	fs := flag.NewFlagSet("notify "+args[0], flag.ExitOnError)
	db := fs.String("db", defaultDB, "database path")
	apply := fs.Bool("apply", false, "make the change")
	asJSON := fs.Bool("json", false, "status: print JSON")
	name := fs.String("name", "", "the name of a machine")
	lang := fs.String("lang", "", "the language of the messages: en, vi or zh")
	hubURL := fs.String("url", "", "pair: the address agents reach this panel at, https://name-or-ip:port")
	chatID := fs.Int64("id", 0, "chat: the chat; node-rules, node-remove: the machine")
	reset := fs.Bool("reset", false, "node-rules: use the hub's rules again")
	var r notify.Rules
	ruleFlags(fs, &r)
	_ = fs.Parse(args[1:])
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	switch args[0] {
	case "service":
		runNotifyService(*db)
	case "status":
		s := notifyStatus(*db)
		if *asJSON {
			printJSON(s)
			return
		}
		fmt.Printf("role: %s\nname: %s\n", orNone(s.Role), s.Name)
		if s.Role == notify.RoleHub {
			fmt.Printf("bot: @%s\nchat: %s (%d)\nmachines: %d\n", s.Bot, s.ChatTitle, s.ChatID, len(s.Nodes))
		}
		if s.Role == notify.RoleAgent {
			fmt.Printf("hub: %s\nlast report: %s\n", s.HubURL, ago(s.Agent.LastOK))
		}
	case "hub":
		cfg := loadNotifyConfig()
		if cfg.Role == notify.RoleAgent {
			fail(errors.New("this machine reports to a hub; run mpn notify off -apply first"))
		}
		fail(notify.ValidName(*name))
		if *lang == "" {
			*lang = "en"
		}
		fail(notify.ValidLang(*lang))
		token := readSecret()
		if token == "" && cfg.Token != "" {
			token = cfg.Token // a change of the name or the language keeps the bot
		}
		fail(notify.ValidToken(token))
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		bot, err := notify.Bot{Token: token}.Me(ctx)
		fail(err)
		if !*apply {
			fmt.Printf("the bot is @%s; add -apply to use it\n", bot)
			return
		}
		requireLinuxRoot()
		if token != cfg.Token {
			cfg.ChatID, cfg.ChatTitle = 0, ""
			st := openStore(*db)
			_ = st.ClearNotifyChats()
			st.Close()
			// The new bot starts from its first update.
			var state notify.State
			_ = notify.Load(notify.StateFile, &state)
			state.Offset, state.BotError = 0, ""
			_ = notify.Save(notify.StateFile, state)
		}
		cfg.Role, cfg.Name, cfg.Lang, cfg.Token, cfg.Bot = notify.RoleHub, *name, *lang, token, bot
		if cfg.Rules.CPU == 0 {
			cfg.Rules = notify.DefaultRules()
		}
		saveNotifyConfig(cfg)
		fmt.Printf("this machine is the hub, with the bot @%s; send /start to the bot, then choose the chat\n", bot)
	case "chat":
		cfg := hubConfig()
		st := openStore(*db)
		chats, err := st.NotifyChats()
		st.Close()
		fail(err)
		title := ""
		for _, c := range chats {
			if c.ID == *chatID {
				title = c.Title
			}
		}
		if title == "" {
			fail(errors.New("this chat has not sent /start to the bot"))
		}
		if !*apply {
			fmt.Printf("would send the alerts to %s\n", title)
			return
		}
		requireLinuxRoot()
		cfg.ChatID, cfg.ChatTitle = *chatID, title
		saveNotifyConfig(cfg)
		sendTest(cfg)
		fmt.Printf("the alerts go to %s; a test message was sent\n", title)
	case "test":
		requireLinuxRoot()
		cfg := loadNotifyConfig()
		switch cfg.Role {
		case notify.RoleHub:
			if cfg.ChatID == 0 {
				fail(errors.New("choose the chat first"))
			}
			sendTest(cfg)
		case notify.RoleAgent:
			var rem notify.Remote
			_ = notify.Load(notify.RemoteFile, &rem)
			if rem.Token == "" || rem.ChatID == 0 {
				fail(errors.New("the hub has not given this machine its bot yet; wait for the first report"))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			fail(notify.Bot{Token: rem.Token}.Send(ctx, rem.ChatID, notify.Msg(rem.Lang, "test", notify.Escape(cfg.Name))))
		default:
			fail(errors.New("the alerts are not set up on this machine"))
		}
		fmt.Println("a test message was sent")
	case "rules":
		cfg := hubConfig()
		fail(r.Valid())
		if !*apply {
			fmt.Printf("%+v\n", r)
			return
		}
		requireLinuxRoot()
		cfg.Rules = r
		if set["lang"] {
			fail(notify.ValidLang(*lang))
			cfg.Lang = *lang
		}
		saveNotifyConfig(cfg)
		fmt.Println("the rules are saved; every machine gets them with its next report")
	case "pair":
		hubConfig()
		fail(notify.ValidName(*name))
		u, err := notify.ValidHubURL(*hubURL)
		fail(err)
		pin, err := notify.PinOf(panelssl.Dir + "/fullchain.pem")
		fail(err)
		st := openStore(*db)
		defer st.Close()
		nodes, err := st.NotifyNodes()
		fail(err)
		if *name == loadNotifyConfig().Name {
			fail(errors.New("this is the name of the hub"))
		}
		for _, nd := range nodes {
			if nd.Name == *name {
				fmt.Fprintf(os.Stderr, "%s is known already; pairing it again gives it a new key\n", *name)
			}
		}
		if !*apply {
			fmt.Println("would make a pairing code; add -apply")
			return
		}
		requireLinuxRoot()
		secret := notify.Token()
		fail(st.AddNotifyPair(notify.Hash(secret), *name, time.Now().Add(notify.PairValid)))
		fmt.Println(notify.MakeCode(u, pin, secret))
	case "node-rules":
		hubConfig()
		st := openStore(*db)
		defer st.Close()
		text := ""
		if !*reset {
			fail(r.Valid())
			b, _ := json.Marshal(r)
			text = string(b)
		}
		if !*apply {
			fmt.Println("would save the rules of the machine; add -apply")
			return
		}
		requireLinuxRoot()
		fail(st.SetNotifyNodeRules(*chatID, text))
		fmt.Println("saved; the machine gets them with its next report")
	case "node-remove":
		hubConfig()
		st := openStore(*db)
		defer st.Close()
		if !*apply {
			fmt.Println("would forget the machine; add -apply")
			return
		}
		requireLinuxRoot()
		fail(st.DeleteNotifyNode(*chatID))
		fmt.Println("the machine is forgotten; its reports are refused from now on")
	case "agent":
		cfg := loadNotifyConfig()
		if cfg.Role == notify.RoleHub {
			fail(errors.New("this machine is a hub; run mpn notify off -apply first"))
		}
		fail(notify.ValidName(*name))
		u, pin, secret, err := notify.ReadCode(readSecret())
		fail(err)
		if !*apply {
			fmt.Printf("would connect to the hub at %s; add -apply\n", u)
			return
		}
		requireLinuxRoot()
		ans, err := postHub[pairAnswer](u, pin, "/_notify/pair", map[string]string{"secret": secret})
		fail(err)
		cfg = notify.Config{Role: notify.RoleAgent, Name: ans.Remote.Name, HubURL: u, HubPin: pin, NodeID: ans.ID, NodeKey: ans.Key}
		if cfg.Name == "" {
			cfg.Name = *name
		}
		fail(notify.Save(notify.RemoteFile, ans.Remote))
		_ = os.Remove(notify.StateFile) // a fresh start: the state of an earlier role does not apply
		saveNotifyConfig(cfg)
		fmt.Printf("this machine reports to %s as %s\n", u, cfg.Name)
	case "off":
		if !*apply {
			fmt.Println("would stop the alerts on this machine; add -apply")
			return
		}
		requireLinuxRoot()
		_ = os.Remove(notify.RemoteFile)
		_ = os.Remove(notify.StateFile)
		saveNotifyConfig(notify.Config{})
		fmt.Println("the alerts are off on this machine")
	default:
		notifyUsage()
	}
}

func ruleFlags(fs *flag.FlagSet, r *notify.Rules) {
	d := notify.DefaultRules()
	fs.IntVar(&r.CPU, "cpu", d.CPU, "rules: CPU percent")
	fs.IntVar(&r.CPUMin, "cpu-min", d.CPUMin, "rules: for this many minutes")
	fs.IntVar(&r.Mem, "mem", d.Mem, "rules: memory percent")
	fs.IntVar(&r.MemMin, "mem-min", d.MemMin, "rules: for this many minutes")
	fs.IntVar(&r.Disk, "disk", d.Disk, "rules: disk percent")
	fs.IntVar(&r.CertDays, "cert-days", d.CertDays, "rules: certificates that end in fewer days")
	fs.IntVar(&r.Repeat, "repeat", d.Repeat, "rules: minutes between reminders; 0 never")
	fs.BoolVar(&r.Resources, "resources", d.Resources, "rules: report CPU, memory and disk")
	fs.BoolVar(&r.Services, "services", d.Services, "rules: report services that stop")
	fs.BoolVar(&r.Sites, "sites", d.Sites, "rules: report websites that do not answer")
	fs.BoolVar(&r.Security, "security", d.Security, "rules: report sign-ins and bans")
	fs.BoolVar(&r.Ops, "ops", d.Ops, "rules: report backups, certificates and releases")
	fs.BoolVar(&r.Danger, "danger", d.Danger, "rules: report important changes from the activity log")
}

// readSecret reads one line from standard input: the token or the pairing code never goes on the command line, where ps shows it.
func readSecret() string {
	s, err := bufio.NewReader(io.LimitReader(os.Stdin, 4096)).ReadString('\n')
	if err != nil && err != io.EOF {
		return ""
	}
	return strings.TrimSpace(s)
}

func loadNotifyConfig() notify.Config {
	var c notify.Config
	fail(notify.Load(notify.ConfigFile, &c))
	return c
}

func hubConfig() notify.Config {
	c := loadNotifyConfig()
	if c.Role != notify.RoleHub {
		fail(errors.New("this machine is not the hub"))
	}
	return c
}

// saveNotifyConfig writes the setting and restarts the service, which reads it only when it starts.
func saveNotifyConfig(c notify.Config) {
	fail(notify.Save(notify.ConfigFile, c))
	if err := exec.Command("systemctl", "restart", "mpn-notify").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "the setting is saved, but mpn-notify could not be restarted: %v\n", err)
	}
}

func sendTest(cfg notify.Config) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	fail(notify.Bot{Token: cfg.Token}.Send(ctx, cfg.ChatID, notify.Msg(cfg.Lang, "test", notify.Escape(cfg.Name))))
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func ago(t int64) string {
	if t == 0 {
		return "never"
	}
	return notify.Duration(time.Since(time.Unix(t, 0))) + " ago"
}

func lookupGID(account string) (int, error) {
	u, err := user.Lookup(account)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(u.Gid)
}

func notifyStatus(db string) notify.Status {
	cfg := loadNotifyConfig()
	var state notify.State
	_ = notify.Load(notify.StateFile, &state)
	s := notify.Status{Role: cfg.Role, Name: cfg.Name, Lang: cfg.Lang, Bot: cfg.Bot, ChatID: cfg.ChatID, ChatTitle: cfg.ChatTitle,
		Rules: cfg.Rules, HubURL: cfg.HubURL, Agent: state.Agent, HubLost: state.HubLost, BotError: state.BotError, Report: state.Report,
		Nodes: []notify.NodeView{}, Chats: []notify.Chat{}, Active: unitActive("mpn-notify.service")}
	if s.Name == "" {
		if h, err := os.Hostname(); err == nil {
			s.Name, _, _ = strings.Cut(h, ".")
		}
	}
	if s.Rules.CPU == 0 {
		s.Rules = notify.DefaultRules()
	}
	if cfg.Role == notify.RoleAgent {
		var rem notify.Remote
		_ = notify.Load(notify.RemoteFile, &rem)
		if rem.Rules.CPU != 0 {
			s.Rules = rem.Rules
		}
		s.Lang = rem.Lang
	}
	if cfg.Role != notify.RoleHub {
		return s
	}
	st, err := store.Open(db)
	if err != nil {
		return s
	}
	defer st.Close()
	nodes, _ := st.NotifyNodes()
	for _, n := range nodes {
		v := notify.NodeView{ID: n.ID, Name: n.Name, LastSeen: n.LastSeen, Lost: n.Lost}
		_ = json.Unmarshal([]byte(n.Report), &v.Report)
		if n.Rules != "" {
			var r notify.Rules
			if json.Unmarshal([]byte(n.Rules), &r) == nil {
				v.OwnRules = &r
			}
		}
		s.Nodes = append(s.Nodes, v)
	}
	chats, _ := st.NotifyChats()
	for _, c := range chats {
		s.Chats = append(s.Chats, notify.Chat{ID: c.ID, Title: c.Title, At: c.At})
	}
	return s
}
