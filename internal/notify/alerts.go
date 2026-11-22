package notify

import (
	"sort"
	"time"
)

// Problem is a condition that is true now: its key names it for as long as it lasts (cpu, disk:/var, service:nginx.service), and Text
// says it in the language of the messages.
type Problem struct {
	Key  string
	Text string
}

// Tracker turns the problems seen on each check into messages: one when a problem starts, a reminder every Repeat while it lasts, and
// one when it ends. A problem must be seen twice in a row before it is told, so one slow answer of a website is not an alert.
type Tracker struct {
	open    map[string]*openProblem
	pending map[string]int // seen once, not told yet
}

type openProblem struct {
	text   string
	since  time.Time
	toldAt time.Time
}

// Change is a message the tracker wants sent.
type Change struct {
	Kind string // "problem", "again" or "solved"
	Text string
}

// Check takes the problems of this check and returns what to send.
func (t *Tracker) Check(now time.Time, seen []Problem, repeat time.Duration) []Change {
	if t.open == nil {
		t.open, t.pending = map[string]*openProblem{}, map[string]int{}
	}
	var out []Change
	present := map[string]bool{}
	for _, p := range seen {
		present[p.Key] = true
		if o, ok := t.open[p.Key]; ok {
			o.text = p.Text
			if repeat > 0 && now.Sub(o.toldAt) >= repeat {
				o.toldAt = now
				out = append(out, Change{"again", p.Text})
			}
			continue
		}
		t.pending[p.Key]++
		if t.pending[p.Key] >= 2 {
			delete(t.pending, p.Key)
			t.open[p.Key] = &openProblem{text: p.Text, since: now, toldAt: now}
			out = append(out, Change{"problem", p.Text})
		}
	}
	for k := range t.pending {
		if !present[k] {
			delete(t.pending, k)
		}
	}
	var gone []string
	for k := range t.open {
		if !present[k] {
			gone = append(gone, k)
		}
	}
	sort.Strings(gone)
	for _, k := range gone {
		out = append(out, Change{"solved", t.open[k].text})
		delete(t.open, k)
	}
	return out
}

// Open is the text of every problem still open, for /status.
func (t *Tracker) Open() []string {
	keys := make([]string, 0, len(t.open))
	for k := range t.open {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, t.open[k].text)
	}
	return out
}

// Sustained says whether a value stayed at or over a limit for the whole of a window, from samples taken every step.
type Sustained struct {
	since time.Time
	over  bool
}

// Add records a value and says whether it has been at or over the limit for at least d.
func (s *Sustained) Add(now time.Time, v float64, limit int, d time.Duration) bool {
	if v < float64(limit) {
		s.over = false
		return false
	}
	if !s.over {
		s.over, s.since = true, now
	}
	return now.Sub(s.since) >= d
}
