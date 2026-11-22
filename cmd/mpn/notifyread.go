package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"mpn/internal/backup"
	"mpn/internal/monitor"
	"mpn/internal/notify"
	"mpn/internal/site"
	"mpn/internal/store"
)

// machine reads what the alerts need from this machine. It keeps the monitor's collector, which needs two readings for a rate.
type machine struct {
	db  string
	mon *monitor.Collector
	// The checks of the websites run every other minute, the certificates once an hour; the results are kept in between.
	mu        sync.Mutex
	sitesAt   time.Time
	sitesDown []notify.Down
	certsAt   time.Time
	certs     []notify.Cert
}

func newMachine(db string) *machine {
	return &machine{db: db, mon: monitor.New(monitor.ProcSource, "")}
}

// read takes the readings of one check.
func (m *machine) read(now time.Time) notify.Readings {
	_ = m.mon.Tick()
	var r notify.Readings
	if s, ok := m.mon.Latest(); ok {
		r.CPU, r.Load = s.CPU, s.Load
		if mt := m.mon.Machine(); mt.MemTotalMB > 0 {
			r.Mem = 100 * s.MemMB / mt.MemTotalMB
		}
	}
	r.Uptime = m.mon.Uptime()
	for _, d := range m.mon.Disks() {
		r.Disks = append(r.Disks, notify.DiskUse{Mount: d.Mount, Pct: d.Pct})
	}
	for _, s := range monitor.Services(monitor.SystemdState, site.InstalledPHP()) {
		// A unit that is installed but switched off by the owner (inactive and not enabled) is not a problem.
		if !s.Active && unitEnabled(s.Unit) {
			label := s.Label
			if strings.HasPrefix(label, monitor.LabelKeyPrefix) {
				label = s.Unit
			}
			r.Down = append(r.Down, notify.Down{Name: label, Why: s.State})
		}
	}
	for _, u := range []string{"postfix.service", "dovecot.service", "fail2ban.service"} {
		if unitEnabled(u) && !unitActive(u) {
			r.Down = append(r.Down, notify.Down{Name: strings.TrimSuffix(u, ".service"), Why: unitState(u)})
		}
	}
	st, err := store.Open(m.db)
	if err != nil {
		return r
	}
	sites, _ := st.ListSites()
	st.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	if now.Sub(m.sitesAt) >= 2*time.Minute {
		m.sitesAt, m.sitesDown = now, checkSites(sites)
	}
	if now.Sub(m.certsAt) >= time.Hour {
		m.certsAt, m.certs = now, nil
		for _, s := range sites {
			if exp := expiryOf(s); exp > 0 {
				m.certs = append(m.certs, notify.Cert{Site: s.Name, Days: int(time.Until(time.Unix(exp, 0)).Hours() / 24)})
			}
		}
	}
	r.SitesDown, r.Certs = m.sitesDown, m.certs
	return r
}

func unitState(u string) string {
	out, _ := exec.Command("systemctl", "is-active", u).Output()
	return strings.TrimSpace(string(out))
}

func unitActive(u string) bool { return unitState(u) == "active" }

func unitEnabled(u string) bool {
	out, _ := exec.Command("systemctl", "is-enabled", u).Output()
	s := strings.TrimSpace(string(out))
	return s == "enabled" || s == "static" || s == "alias"
}

// checkSites asks every website for its home page through nginx on this machine, by its first name, and returns those that do not
// answer or answer with a server error. The answer is read from this machine, so a website is checked even when the DNS of its name
// points elsewhere, and the check costs no traffic. A redirect, a 4xx (a login page, a forbidden folder) is an answer.
func checkSites(sites []site.Site) []notify.Down {
	var out []notify.Down
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, s := range sites {
		if len(s.Hostnames) == 0 {
			continue
		}
		wg.Add(1)
		go func(s site.Site) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			why := probe(s.Hostnames[0], s.TLS)
			if why == "" { // once more, so a single slow answer is not a problem
				return
			}
			time.Sleep(3 * time.Second)
			if why = probe(s.Hostnames[0], s.TLS); why != "" {
				mu.Lock()
				out = append(out, notify.Down{Name: s.Name, Why: why})
				mu.Unlock()
			}
		}(s)
	}
	wg.Wait()
	return out
}

func probe(host string, tlsOn bool) string {
	scheme, port := "http", "80"
	if tlsOn {
		scheme, port = "https", "443"
	}
	tr := &http.Transport{
		// Every name goes to nginx on this machine.
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, "127.0.0.1:"+port)
		},
		// The certificate is checked on its own (the date, by the certificate alert); a check of the website must not fail because
		// a self-signed certificate is in use while Let's Encrypt is not yet done.
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true, ServerName: host},
		DisableKeepAlives: true,
	}
	c := &http.Client{Timeout: 15 * time.Second, Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", scheme+"://"+host+"/", nil)
	req.Header.Set("User-Agent", "mpn-notify (website check)")
	resp, err := c.Do(req)
	if err != nil {
		if e, ok := err.(net.Error); ok && e.Timeout() {
			return "no answer in 15 s"
		}
		return "connection failed"
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return ""
}

// unitResult is how the last run of a oneshot unit ended: "success", "exit-code"... and when, "" when it never ran.
func unitResult(u string) (result string, at time.Time) {
	// The time is read as microseconds since boot, which every systemd prints the same way (--timestamp=unix needs systemd 251, and
	// Ubuntu 22.04 has 249), and turned into a date with the uptime.
	out, err := exec.Command("systemctl", "show", "-p", "Result", "-p", "ExecMainExitTimestampMonotonic", u).Output()
	if err != nil {
		return "", time.Time{}
	}
	up, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "", time.Time{}
	}
	var upSec float64
	if _, err := fmt.Sscanf(string(up), "%f", &upSec); err != nil {
		return "", time.Time{}
	}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch k {
		case "Result":
			result = v
		case "ExecMainExitTimestampMonotonic":
			var usec int64
			if _, err := fmt.Sscanf(v, "%d", &usec); err == nil && usec > 0 {
				at = time.Now().Add(-time.Duration(upSec*float64(time.Second)) + time.Duration(usec)*time.Microsecond).Truncate(time.Second)
			}
		}
	}
	if at.IsZero() {
		return "", at
	}
	return result, at
}

// journalTail is the last lines a unit wrote in its last run, for the message of a failure.
func journalTail(u string, n int) string {
	out, _ := exec.Command("journalctl", "-u", u, "-n", fmt.Sprint(n), "--no-pager", "-o", "cat").Output()
	s := strings.TrimSpace(string(out))
	if len(s) > 600 {
		s = "…" + s[len(s)-600:]
	}
	return s
}

// backupSites are the websites that have a backup newer than t, which a nightly backup that ended well made.
func backupSites(since time.Time) []string {
	var out []string
	entries, _ := os.ReadDir(backup.Base)
	for _, e := range entries {
		if !e.IsDir() || !site.ValidName(e.Name()) {
			continue
		}
		sub, _ := os.ReadDir(backup.Base + "/" + e.Name())
		for _, b := range sub {
			if fi, err := b.Info(); err == nil && fi.ModTime().After(since) {
				out = append(out, e.Name())
				break
			}
		}
	}
	return out
}
