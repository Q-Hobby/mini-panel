package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// NotifyNode is a machine that reports to this hub.
type NotifyNode struct {
	ID       int64
	Name     string
	KeyHash  string
	Created  int64
	LastSeen int64
	Report   string // the last report, JSON
	Rules    string // rules of its own, JSON; "" uses the hub's
	Lost     bool   // reported as silent and not back yet
}

func (s *Store) NotifyNodes() ([]NotifyNode, error) {
	rows, err := s.db.Query(`SELECT id, name, key_hash, created_at, last_seen, report, rules, lost FROM notify_nodes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotifyNode{}
	for rows.Next() {
		var n NotifyNode
		if err := rows.Scan(&n.ID, &n.Name, &n.KeyHash, &n.Created, &n.LastSeen, &n.Report, &n.Rules, &n.Lost); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) NotifyNode(id int64) (NotifyNode, error) {
	var n NotifyNode
	err := s.db.QueryRow(`SELECT id, name, key_hash, created_at, last_seen, report, rules, lost FROM notify_nodes WHERE id = ?`, id).
		Scan(&n.ID, &n.Name, &n.KeyHash, &n.Created, &n.LastSeen, &n.Report, &n.Rules, &n.Lost)
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	return n, err
}

// AddNotifyPair keeps a pairing code (its hash) for a machine name, until expires.
func (s *Store) AddNotifyPair(hash, name string, expires time.Time) error {
	if _, err := s.db.Exec(`DELETE FROM notify_pairs WHERE expires < ?`, time.Now().Unix()); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO notify_pairs (hash, name, expires) VALUES (?, ?, ?)`, hash, name, expires.Unix())
	return err
}

// UseNotifyPair takes a pairing code once: the machine it names is added with the key (its hash), or, when a machine of that name is
// there already, given the new key (a machine installed again pairs again under its name).
func (s *Store) UseNotifyPair(hash, keyHash string) (NotifyNode, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return NotifyNode{}, err
	}
	defer tx.Rollback()
	var name string
	var expires int64
	err = tx.QueryRow(`SELECT name, expires FROM notify_pairs WHERE hash = ?`, hash).Scan(&name, &expires)
	if errors.Is(err, sql.ErrNoRows) || err == nil && expires < time.Now().Unix() {
		return NotifyNode{}, ErrNotFound
	}
	if err != nil {
		return NotifyNode{}, err
	}
	if _, err := tx.Exec(`DELETE FROM notify_pairs WHERE hash = ?`, hash); err != nil {
		return NotifyNode{}, err
	}
	now := time.Now().Unix()
	if _, err := tx.Exec(`INSERT INTO notify_nodes (name, key_hash, created_at, last_seen) VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET key_hash = excluded.key_hash, last_seen = excluded.last_seen, lost = 0`, name, keyHash, now, now); err != nil {
		return NotifyNode{}, err
	}
	var n NotifyNode
	if err := tx.QueryRow(`SELECT id, name FROM notify_nodes WHERE name = ?`, name).Scan(&n.ID, &n.Name); err != nil {
		return NotifyNode{}, err
	}
	return n, tx.Commit()
}

// SeeNotifyNode records a report. It says whether the machine had been reported as lost, so the hub can say it is back.
func (s *Store) SeeNotifyNode(id int64, report string) (wasLost bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := tx.QueryRow(`SELECT lost FROM notify_nodes WHERE id = ?`, id).Scan(&wasLost); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE notify_nodes SET last_seen = ?, report = ?, lost = 0 WHERE id = ?`, time.Now().Unix(), report, id); err != nil {
		return false, err
	}
	return wasLost, tx.Commit()
}

func (s *Store) SetNotifyNodeLost(id int64, lost bool) error {
	_, err := s.db.Exec(`UPDATE notify_nodes SET lost = ? WHERE id = ?`, lost, id)
	return err
}

func (s *Store) SetNotifyNodeRules(id int64, rules string) error {
	res, err := s.db.Exec(`UPDATE notify_nodes SET rules = ? WHERE id = ?`, rules, id)
	return affected(res, err)
}

func (s *Store) DeleteNotifyNode(id int64) error {
	res, err := s.db.Exec(`DELETE FROM notify_nodes WHERE id = ?`, id)
	return affected(res, err)
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// QueueNotify keeps a message to send. The service sends them in order and removes each once Telegram took it.
func (s *Store) QueueNotify(text string) error {
	_, err := s.db.Exec(`INSERT INTO notify_outbox (text, created_at) VALUES (?, ?)`, text, time.Now().Unix())
	return err
}

type NotifyMessage struct {
	ID   int64
	Text string
	At   int64
}

// NotifyOutbox returns the oldest messages waiting, up to n. Messages older than a day are dropped first: an alert of yesterday that
// could not go out is no use now.
func (s *Store) NotifyOutbox(n int) ([]NotifyMessage, error) {
	if _, err := s.db.Exec(`DELETE FROM notify_outbox WHERE created_at < ?`, time.Now().Add(-24*time.Hour).Unix()); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id, text, created_at FROM notify_outbox ORDER BY id LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotifyMessage
	for rows.Next() {
		var m NotifyMessage
		if err := rows.Scan(&m.ID, &m.Text, &m.At); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DoneNotify(id int64) error {
	_, err := s.db.Exec(`DELETE FROM notify_outbox WHERE id = ?`, id)
	return err
}

// AddNotifyChat remembers a chat that sent /start, so the page can offer it.
func (s *Store) AddNotifyChat(id int64, title string) error {
	_, err := s.db.Exec(`INSERT INTO notify_chats (id, title, at) VALUES (?, ?, ?) ON CONFLICT(id) DO UPDATE SET title = excluded.title, at = excluded.at`,
		id, strings.TrimSpace(title), time.Now().Unix())
	return err
}

type NotifyChat struct {
	ID    int64
	Title string
	At    int64
}

func (s *Store) NotifyChats() ([]NotifyChat, error) {
	rows, err := s.db.Query(`SELECT id, title, at FROM notify_chats ORDER BY at DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotifyChat{}
	for rows.Next() {
		var c NotifyChat
		if err := rows.Scan(&c.ID, &c.Title, &c.At); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ClearNotifyChats() error {
	_, err := s.db.Exec(`DELETE FROM notify_chats`)
	return err
}

// AuditSince returns the entries of the activity log after id, oldest first, up to n. The alerts read the dangerous ones from it.
func (s *Store) AuditSince(id int64, n int) ([]AuditRow, error) {
	rows, err := s.db.Query(`SELECT id, at, ip, site, op, detail, ok, danger FROM audit_log WHERE id > ? ORDER BY id LIMIT ?`, id, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditRow
	for rows.Next() {
		var r AuditRow
		if err := rows.Scan(&r.ID, &r.At, &r.IP, &r.Site, &r.Op, &r.Detail, &r.OK, &r.Danger); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AuditRow is an entry of the activity log as the alerts read it.
type AuditRow struct {
	ID, At               int64
	IP, Site, Op, Detail string
	OK, Danger           bool
}

// LastAuditID is the newest entry of the activity log, 0 when there is none.
func (s *Store) LastAuditID() (int64, error) {
	var id sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(id) FROM audit_log`).Scan(&id)
	return id.Int64, err
}
