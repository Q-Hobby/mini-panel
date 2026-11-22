package ctl

import (
	"fmt"
	"strconv"

	"mpn/internal/notify"
)

// The Telegram alerts (mpn notify). The token and the pairing code reach mpn on standard input only.
func (r Request) notifyArgv() ([]string, error) {
	switch r.Op {
	case "notify.status":
		return []string{"notify", "status", "-json"}, nil
	case "notify.test":
		return []string{"notify", "test"}, nil
	case "notify.off":
		return []string{"notify", "off", "-apply"}, nil
	case "notify.hub":
		if err := notify.ValidName(r.Name); err != nil {
			return nil, err
		}
		if err := notify.ValidLang(r.Lang); err != nil {
			return nil, err
		}
		if r.Token != "" {
			if err := notify.ValidToken(r.Token); err != nil {
				return nil, err
			}
		}
		return []string{"notify", "hub", "-name=" + r.Name, "-lang=" + r.Lang, "-apply"}, nil
	case "notify.chat":
		if r.ChatID == 0 {
			return nil, fmt.Errorf("choose a chat")
		}
		return []string{"notify", "chat", "-id=" + strconv.FormatInt(r.ChatID, 10), "-apply"}, nil
	case "notify.rules", "notify.noderules":
		a := []string{"notify", "rules"}
		if r.Op == "notify.noderules" {
			if r.NodeID <= 0 {
				return nil, fmt.Errorf("choose a machine")
			}
			a = []string{"notify", "node-rules", "-id=" + strconv.FormatInt(r.NodeID, 10)}
			if r.Rules == nil {
				return append(a, "-reset", "-apply"), nil
			}
		}
		if r.Rules == nil {
			return nil, fmt.Errorf("no rules")
		}
		if err := r.Rules.Valid(); err != nil {
			return nil, err
		}
		x := *r.Rules
		b := strconv.FormatBool
		a = append(a, "-cpu="+strconv.Itoa(x.CPU), "-cpu-min="+strconv.Itoa(x.CPUMin), "-mem="+strconv.Itoa(x.Mem), "-mem-min="+strconv.Itoa(x.MemMin),
			"-disk="+strconv.Itoa(x.Disk), "-cert-days="+strconv.Itoa(x.CertDays), "-repeat="+strconv.Itoa(x.Repeat),
			"-resources="+b(x.Resources), "-services="+b(x.Services), "-sites="+b(x.Sites), "-security="+b(x.Security), "-ops="+b(x.Ops), "-danger="+b(x.Danger))
		if r.Op == "notify.rules" && r.Lang != "" {
			if err := notify.ValidLang(r.Lang); err != nil {
				return nil, err
			}
			a = append(a, "-lang="+r.Lang)
		}
		return append(a, "-apply"), nil
	case "notify.pair":
		if err := notify.ValidName(r.Name); err != nil {
			return nil, err
		}
		u, err := notify.ValidHubURL(r.HubURL)
		if err != nil {
			return nil, err
		}
		return []string{"notify", "pair", "-name=" + r.Name, "-url=" + u, "-apply"}, nil
	case "notify.noderemove":
		if r.NodeID <= 0 {
			return nil, fmt.Errorf("choose a machine")
		}
		return []string{"notify", "node-remove", "-id=" + strconv.FormatInt(r.NodeID, 10), "-apply"}, nil
	case "notify.agent":
		if err := notify.ValidName(r.Name); err != nil {
			return nil, err
		}
		if _, _, _, err := notify.ReadCode(r.Code); err != nil {
			return nil, err
		}
		return []string{"notify", "agent", "-name=" + r.Name, "-apply"}, nil
	}
	return nil, fmt.Errorf("unknown operation %q", r.Op)
}
