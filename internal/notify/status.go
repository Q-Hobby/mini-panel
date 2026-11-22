package notify

// Status is what the page of the alerts shows (mpn notify status -json). It holds no secret: not the token, not a key.
type Status struct {
	Role      string     `json:"role"`
	Name      string     `json:"name"`
	Lang      string     `json:"lang"`
	Bot       string     `json:"bot,omitempty"`
	ChatID    int64      `json:"chat_id,omitempty"`
	ChatTitle string     `json:"chat_title,omitempty"`
	BotError  string     `json:"bot_error,omitempty"`
	Rules     Rules      `json:"rules"`
	Report    Report     `json:"report"`
	Nodes     []NodeView `json:"nodes"`
	Chats     []Chat     `json:"chats"`
	HubURL    string     `json:"hub_url,omitempty"`
	Agent     AgentState `json:"agent"`
	HubLost   bool       `json:"hub_lost,omitempty"`
	Active    bool       `json:"active"` // the service runs
}

// NodeView is an agent as the hub's page shows it.
type NodeView struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	LastSeen int64  `json:"last_seen"`
	Lost     bool   `json:"lost"`
	Report   Report `json:"report"`
	OwnRules *Rules `json:"own_rules,omitempty"`
}
