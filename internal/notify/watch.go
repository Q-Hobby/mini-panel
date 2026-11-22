package notify

import (
	"fmt"
	"time"
)

// Readings are what a machine sees on one check. The service fills them from the machine; a test fills them by hand.
type Readings struct {
	CPU, Mem, Load float64
	Uptime         time.Duration
	Disks          []DiskUse
	Down           []Down // services that should run and do not
	SitesDown      []Down // websites that do not answer
	Certs          []Cert // certificates that end soon
}

type DiskUse struct {
	Mount string
	Pct   float64
}

// Down is a service or a website that is not working, and what was seen.
type Down struct{ Name, Why string }

type Cert struct {
	Site string
	Days int
}

// Watcher keeps what one machine needs between checks.
type Watcher struct {
	Track    Tracker
	cpu, mem Sustained
}

// Check turns readings into the problems of the rules, and the problems into messages for machine name.
func (w *Watcher) Check(now time.Time, name, lang string, r Rules, in Readings) []string {
	var seen []Problem
	if r.Resources {
		if w.cpu.Add(now, in.CPU, r.CPU, time.Duration(r.CPUMin)*time.Minute) {
			seen = append(seen, Problem{"cpu", Msg(lang, "cpu", in.CPU, r.CPUMin)})
		}
		if w.mem.Add(now, in.Mem, r.Mem, time.Duration(r.MemMin)*time.Minute) {
			seen = append(seen, Problem{"mem", Msg(lang, "mem", in.Mem, r.MemMin)})
		}
		for _, d := range in.Disks {
			if d.Pct >= float64(r.Disk) {
				seen = append(seen, Problem{"disk:" + d.Mount, Msg(lang, "disk", Escape(d.Mount), d.Pct)})
			}
		}
	}
	if r.Services {
		for _, d := range in.Down {
			seen = append(seen, Problem{"service:" + d.Name, Msg(lang, "service", Escape(d.Name), Escape(d.Why))})
		}
	}
	if r.Sites {
		for _, d := range in.SitesDown {
			seen = append(seen, Problem{"site:" + d.Name, Msg(lang, "site", Escape(d.Name), Escape(d.Why))})
		}
	}
	if r.Ops {
		for _, c := range in.Certs {
			if c.Days < r.CertDays {
				// The number of days is left out of the key, so a certificate that ends a day sooner is the same problem.
				seen = append(seen, Problem{"cert:" + c.Site, Msg(lang, "cert", Escape(c.Site), c.Days)})
			}
		}
	}
	var out []string
	for _, c := range w.Track.Check(now, seen, time.Duration(r.Repeat)*time.Minute) {
		out = append(out, Msg(lang, c.Kind, Escape(name), c.Text))
	}
	return out
}

// Stats is the summary of readings for /status.
func (in Readings) Stats(now time.Time) Stats {
	s := Stats{At: now.Unix(), CPU: in.CPU, Mem: in.Mem, Load: in.Load, Uptime: int64(in.Uptime.Seconds())}
	for _, d := range in.Disks {
		if d.Pct > s.Disk {
			s.Disk = d.Pct
		}
	}
	return s
}

// Duration says a span of time briefly: 3d 4h, 5h 10m, 12m.
func Duration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
