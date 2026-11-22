// Package notify sends the alerts of the panel to Telegram. One panel is the hub: it holds the bot, reads the messages people send
// it (/start, /status) and sends the alerts of every machine. The other panels are agents: they watch themselves, tell the hub what
// they see every minute, and send to Telegram themselves only when the hub has not answered for a while. The hub reports an agent
// that stops reporting. Everything runs in the mpn-notify service, as root, apart from the panel.
package notify

import (
	"errors"
	"regexp"
	"time"
)

const (
	RoleHub   = "hub"
	RoleAgent = "agent"
)

const (
	// BeatEvery is how often an agent reports to the hub.
	BeatEvery = 60 * time.Second
	// LostAfter is how long an agent (or, seen from the agent, the hub) may stay silent before it is reported.
	LostAfter = 3 * time.Minute
	// PairValid is how long a pairing code can be used.
	PairValid = time.Hour
)

// Rules are what is reported and when. The hub keeps them for every machine, and may keep other rules for one machine.
type Rules struct {
	CPU      int `json:"cpu"`       // percent of the processor busy...
	CPUMin   int `json:"cpu_min"`   // ...for this many minutes
	Mem      int `json:"mem"`       // percent of the memory in use...
	MemMin   int `json:"mem_min"`   // ...for this many minutes
	Disk     int `json:"disk"`      // percent of a filesystem used
	CertDays int `json:"cert_days"` // a certificate ends in fewer days than this
	Repeat   int `json:"repeat"`    // minutes between reminders of a problem that goes on; 0 never

	Resources bool `json:"resources"`
	Services  bool `json:"services"`
	Sites     bool `json:"sites"`
	Security  bool `json:"security"`
	Ops       bool `json:"ops"`    // backups, certificates, a new release
	Danger    bool `json:"danger"` // important changes from the activity log
}

// DefaultRules are the rules of a new hub.
func DefaultRules() Rules {
	return Rules{CPU: 90, CPUMin: 5, Mem: 90, MemMin: 5, Disk: 85, CertDays: 14, Repeat: 30,
		Resources: true, Services: true, Sites: true, Security: true, Ops: true, Danger: true}
}

// Valid checks that every number is in its range.
func (r Rules) Valid() error {
	for _, c := range []struct {
		v, lo, hi int
		what      string
	}{
		{r.CPU, 50, 100, "CPU"}, {r.CPUMin, 1, 60, "CPU minutes"}, {r.Mem, 50, 100, "memory"}, {r.MemMin, 1, 60, "memory minutes"},
		{r.Disk, 50, 99, "disk"}, {r.CertDays, 1, 60, "certificate days"}, {r.Repeat, 0, 1440, "reminder minutes"},
	} {
		if c.v < c.lo || c.v > c.hi {
			return errors.New(c.what + " is out of range")
		}
	}
	return nil
}

// Config is the setting of this machine. The command line writes it (mpn notify ...); the service only reads it.
type Config struct {
	Role string `json:"role"` // "", RoleHub or RoleAgent
	Name string `json:"name"` // what the messages call this machine
	Lang string `json:"lang"` // of the messages: en, vi or zh

	// The hub's bot and chat. An agent gets them from the hub (Remote).
	Token     string `json:"token,omitempty"`
	Bot       string `json:"bot,omitempty"` // the name of the bot, for the page
	ChatID    int64  `json:"chat_id,omitempty"`
	ChatTitle string `json:"chat_title,omitempty"`
	Rules     Rules  `json:"rules"`

	// An agent: where the hub is, the key of its certificate, and the name and key this machine has there.
	HubURL  string `json:"hub_url,omitempty"`
	HubPin  string `json:"hub_pin,omitempty"`
	NodeID  int64  `json:"node_id,omitempty"`
	NodeKey string `json:"node_key,omitempty"`
}

// Remote is what an agent last heard from the hub. The service writes it after every report.
type Remote struct {
	Name   string `json:"name"`
	Lang   string `json:"lang"`
	Token  string `json:"token,omitempty"`
	ChatID int64  `json:"chat_id,omitempty"`
	Rules  Rules  `json:"rules"`
	Pin    string `json:"pin,omitempty"`
}

// AgentState is how the reports of an agent go, for its page.
type AgentState struct {
	LastOK    int64  `json:"last_ok"`
	LastError string `json:"last_error,omitempty"`
	ErrorAt   int64  `json:"error_at,omitempty"`
}

// Chat is a chat that sent /start to the bot.
type Chat struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	At    int64  `json:"at"`
}

// Stats is the state of a machine at one moment.
type Stats struct {
	At     int64   `json:"at"`
	CPU    float64 `json:"cpu"`
	Mem    float64 `json:"mem"`  // percent
	Disk   float64 `json:"disk"` // percent of the fullest filesystem
	Load   float64 `json:"load"`
	Uptime int64   `json:"uptime"` // seconds
}

// Report is what a machine says about itself: on the hub for itself, and in every report of an agent.
type Report struct {
	Version string   `json:"version"`
	Stats   Stats    `json:"stats"`
	Alerts  []string `json:"alerts"` // the problems still open
}

// Beat is the report an agent sends every minute, with the messages of what happened since the last one it could send.
type Beat struct {
	ID     int64    `json:"id"`
	Key    string   `json:"key"`
	Report Report   `json:"report"`
	Events []string `json:"events"`
}

// MaxEvents is the most messages one report carries; older ones are dropped first.
const MaxEvents = 50

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)

// ValidName accepts the name of a machine.
func ValidName(s string) error {
	if !nameRe.MatchString(s) {
		return errors.New("a machine name has 1 to 40 letters, digits, dots, dashes or underscores")
	}
	return nil
}

// ValidLang accepts a language of the messages.
func ValidLang(s string) error {
	if s != "en" && s != "vi" && s != "zh" {
		return errors.New("unknown language")
	}
	return nil
}
