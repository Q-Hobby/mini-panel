package notify

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// The files of the alerts. All belong to root and only root reads them: the config and the remote copy hold the token of the bot.
const (
	ConfigFile = "/etc/mpn/notify.json"
	RemoteFile = "/var/lib/mpn/notify-remote.json"
	StateFile  = "/var/lib/mpn/notify-state.json"
	// Socket is where the service answers the panel, which passes on what agents send to the hub (/_notify/...).
	Socket = "/run/mpn-notify/hub.sock"
)

// Load reads a JSON file into v; a missing file leaves v as it is.
func Load(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Save writes v to a JSON file that only its owner reads, all at once.
func Save(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil { // a file left from before keeps its mode under WriteFile
		return err
	}
	return os.Rename(tmp, path)
}

// State is what the service remembers between runs.
type State struct {
	Seeded    bool                `json:"seeded"`
	LastAudit int64               `json:"last_audit"`
	KnownIPs  []string            `json:"known_ips"`
	Bans      map[string][]string `json:"bans"`
	BackupAt  int64               `json:"backup_at"`
	RenewAt   int64               `json:"renew_at"`
	Release   string              `json:"release"`
	ReleaseAt int64               `json:"release_at"`
	FailsAt   int64               `json:"fails_at"`
	Offset    int64               `json:"offset"` // of the updates of the bot, on the hub
	BotError  string              `json:"bot_error,omitempty"`
	Agent     AgentState          `json:"agent"`
	HubLost   bool                `json:"hub_lost"` // an agent told Telegram itself that the hub is silent
	Report    Report              `json:"report"`   // the last report of this machine, for the page
}

// KnowIP adds an address to those signed in from before, and says whether it is new. The list keeps the 200 most recent.
func (s *State) KnowIP(ip string) bool {
	for i, x := range s.KnownIPs {
		if x == ip {
			s.KnownIPs = append(append(s.KnownIPs[:i:i], s.KnownIPs[i+1:]...), ip)
			return false
		}
	}
	s.KnownIPs = append(s.KnownIPs, ip)
	if len(s.KnownIPs) > 200 {
		s.KnownIPs = s.KnownIPs[len(s.KnownIPs)-200:]
	}
	return true
}
