package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// Schema is applied in order; each statement is idempotent so a restart on a
// populated database is a no-op. Bump schemaVersion when appending.
const schemaVersion = 3

var schema = []string{
	`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS subscribers (
		id INTEGER PRIMARY KEY,
		email TEXT UNIQUE,
		phone TEXT UNIQUE,
		channel TEXT NOT NULL DEFAULT 'email' CHECK (channel IN ('email','whatsapp')),
		frequency TEXT NOT NULL CHECK (frequency IN ('daily','weekly')),
		horizon INTEGER NOT NULL CHECK (horizon IN (7,14,30)),
		towns TEXT NOT NULL DEFAULT '[]',
		categories TEXT NOT NULL DEFAULT '[]',
		created_at TEXT NOT NULL,
		confirmed_at TEXT,
		last_sent_at TEXT,
		ip_hash TEXT NOT NULL DEFAULT '',
		CHECK ((channel = 'email' AND email IS NOT NULL) OR (channel = 'whatsapp' AND phone IS NOT NULL))
	)`,
	`CREATE TABLE IF NOT EXISTS wa_seen (id TEXT PRIMARY KEY, seen_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS events (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		date TEXT NOT NULL,
		end_date TEXT NOT NULL DEFAULT '',
		time TEXT NOT NULL DEFAULT '',
		end_time TEXT NOT NULL DEFAULT '',
		town TEXT NOT NULL,
		category TEXT NOT NULL,
		listing TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL DEFAULT '',
		cost TEXT NOT NULL DEFAULT 'varies',
		website TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL CHECK (status IN ('pending_email','pending_review','approved','rejected')),
		origin TEXT NOT NULL CHECK (origin IN ('user','auto','admin','seed')),
		submitter_name TEXT NOT NULL DEFAULT '',
		submitter_email TEXT NOT NULL DEFAULT '',
		source_id INTEGER,
		created_at TEXT NOT NULL,
		verified_at TEXT,
		decided_at TEXT,
		ip_hash TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS events_status_date ON events(status, date)`,
	`CREATE TABLE IF NOT EXISTS listing_submissions (
		id INTEGER PRIMARY KEY,
		kind TEXT NOT NULL,
		existing_id TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL,
		category TEXT NOT NULL,
		town TEXT NOT NULL,
		schedule TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL,
		cost TEXT NOT NULL,
		website TEXT NOT NULL DEFAULT '',
		audience TEXT NOT NULL DEFAULT '[]',
		submitter_name TEXT NOT NULL,
		submitter_email TEXT NOT NULL,
		status TEXT NOT NULL CHECK (status IN ('pending_email','pending_review','accepted','rejected')),
		created_at TEXT NOT NULL,
		verified_at TEXT,
		decided_at TEXT,
		ip_hash TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS sources (
		id INTEGER PRIMARY KEY,
		url TEXT NOT NULL UNIQUE,
		kind TEXT NOT NULL CHECK (kind IN ('ics','html')),
		label TEXT NOT NULL,
		listing TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT 'community',
		town TEXT NOT NULL DEFAULT 'somerset-west',
		enabled INTEGER NOT NULL DEFAULT 1,
		last_checked_at TEXT,
		last_hash TEXT NOT NULL DEFAULT '',
		last_status TEXT NOT NULL DEFAULT '',
		last_changed_at TEXT
	)`,
	`CREATE TABLE IF NOT EXISTS seen_uids (source_id INTEGER NOT NULL, uid TEXT NOT NULL, seen_at TEXT NOT NULL, PRIMARY KEY (source_id, uid))`,
	`CREATE TABLE IF NOT EXISTS tokens_used (jti TEXT PRIMARY KEY, used_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS mail_log (id INTEGER PRIMARY KEY, to_hash TEXT NOT NULL, kind TEXT NOT NULL, sent_at TEXT NOT NULL, ok INTEGER NOT NULL, err TEXT NOT NULL DEFAULT '')`,
	`CREATE INDEX IF NOT EXISTS mail_log_to ON mail_log(to_hash, sent_at)`,
	// v2: admin console
	`CREATE TABLE IF NOT EXISTS sessions (id_hash TEXT PRIMARY KEY, created_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, expires_at TEXT NOT NULL, ip_hash TEXT NOT NULL DEFAULT '', ua TEXT NOT NULL DEFAULT '', revoked INTEGER NOT NULL DEFAULT 0)`,
	`CREATE TABLE IF NOT EXISTS audit_log (id INTEGER PRIMARY KEY, at TEXT NOT NULL, action TEXT NOT NULL, target TEXT NOT NULL DEFAULT '', detail TEXT NOT NULL DEFAULT '', ip_hash TEXT NOT NULL DEFAULT '')`,
	`CREATE TABLE IF NOT EXISTS req_stats (day TEXT NOT NULL, route TEXT NOT NULL, status INTEGER NOT NULL, n INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (day, route, status))`,
	`CREATE TABLE IF NOT EXISTS pageviews (day TEXT NOT NULL, path TEXT NOT NULL, views INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (day, path))`,
	`CREATE TABLE IF NOT EXISTS pv_uniques (day TEXT NOT NULL, path TEXT NOT NULL, iph TEXT NOT NULL, PRIMARY KEY (day, path, iph))`,
	`CREATE TABLE IF NOT EXISTS blocklist (id INTEGER PRIMARY KEY, kind TEXT NOT NULL CHECK (kind IN ('ip','email')), value TEXT NOT NULL, note TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, UNIQUE (kind, value))`,
}

func openDB(dir string) (*sql.DB, error) {
	path := filepath.Join(dir, "helderberg.sqlite")
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: one writer, and the service is small
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			return nil, fmt.Errorf("schema: %w\n%s", err, stmt)
		}
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES('schema_version', ?)`, fmt.Sprint(schemaVersion)); err != nil {
		return nil, err
	}
	return db, nil
}

// migrate brings a database written by an older build up to the current
// schema. Each step checks the actual table shape, not just the stored
// version, so an interrupted upgrade is safe to rerun.
func migrate(db *sql.DB) error {
	// v3: subscribers gain a channel and a phone; email becomes optional.
	// SQLite cannot relax NOT NULL in place, so rebuild the table.
	if !hasColumn(db, "subscribers", "phone") {
		_, err := db.Exec(`BEGIN;
			CREATE TABLE subscribers_v3 (
				id INTEGER PRIMARY KEY,
				email TEXT UNIQUE,
				phone TEXT UNIQUE,
				channel TEXT NOT NULL DEFAULT 'email' CHECK (channel IN ('email','whatsapp')),
				frequency TEXT NOT NULL CHECK (frequency IN ('daily','weekly')),
				horizon INTEGER NOT NULL CHECK (horizon IN (7,14,30)),
				towns TEXT NOT NULL DEFAULT '[]',
				categories TEXT NOT NULL DEFAULT '[]',
				created_at TEXT NOT NULL,
				confirmed_at TEXT,
				last_sent_at TEXT,
				ip_hash TEXT NOT NULL DEFAULT '',
				CHECK ((channel = 'email' AND email IS NOT NULL) OR (channel = 'whatsapp' AND phone IS NOT NULL))
			);
			INSERT INTO subscribers_v3(id, email, frequency, horizon, towns, categories, created_at, confirmed_at, last_sent_at, ip_hash)
				SELECT id, email, frequency, horizon, towns, categories, created_at, confirmed_at, last_sent_at, ip_hash FROM subscribers;
			DROP TABLE subscribers;
			ALTER TABLE subscribers_v3 RENAME TO subscribers;
			COMMIT;`)
		if err != nil {
			_, _ = db.Exec(`ROLLBACK`)
			return fmt.Errorf("migrate subscribers to v3: %w", err)
		}
	}
	return nil
}

func hasColumn(db *sql.DB, table, col string) bool {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk) == nil && name == col {
			return true
		}
	}
	return false
}

func (a *App) metaGet(key string) string {
	var v string
	_ = a.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	return v
}

func (a *App) metaSet(key, value string) error {
	_, err := a.db.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES(?, ?)`, key, value)
	return err
}

// Event is the shape the site consumes; JSON field names match data/data.js.
type Event struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Date     string `json:"date"`
	EndDate  string `json:"endDate,omitempty"`
	Time     string `json:"time,omitempty"`
	EndTime  string `json:"endTime,omitempty"`
	Town     string `json:"town"`
	Category string `json:"category"`
	Listing  string `json:"listing,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Cost     string `json:"cost,omitempty"`
	Website  string `json:"website,omitempty"`
	Source   string `json:"source,omitempty"`
	Verified bool   `json:"verified"`
	// internal
	Status        string `json:"-"`
	Origin        string `json:"-"`
	SubmitterName string `json:"-"`
	CreatedAt     string `json:"-"`
}

const eventCols = `id, title, date, end_date, time, end_time, town, category, listing, summary, cost, website, source, status, origin, submitter_name, created_at`

func scanEvent(rows interface{ Scan(...any) error }) (Event, error) {
	var e Event
	err := rows.Scan(&e.ID, &e.Title, &e.Date, &e.EndDate, &e.Time, &e.EndTime, &e.Town, &e.Category, &e.Listing, &e.Summary, &e.Cost, &e.Website, &e.Source, &e.Status, &e.Origin, &e.SubmitterName, &e.CreatedAt)
	e.Verified = e.Status == "approved" && e.Origin == "admin"
	return e, err
}

func (a *App) queryEvents(where string, args ...any) ([]Event, error) {
	rows, err := a.db.Query(`SELECT `+eventCols+` FROM events WHERE `+where+` ORDER BY date, time, title`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// approvedEvents returns events that have not yet ended, from a given local date.
func (a *App) approvedEvents(from time.Time, days int) ([]Event, error) {
	f := from.Format("2006-01-02")
	to := from.AddDate(0, 0, days).Format("2006-01-02")
	return a.queryEvents(`status = 'approved' AND (CASE WHEN end_date = '' THEN date ELSE end_date END) >= ? AND date <= ?`, f, to)
}

func (a *App) insertEvent(e Event, ipHash string, submitterEmail string, sourceID *int64) error {
	_, err := a.db.Exec(`INSERT INTO events(`+eventCols+`, submitter_email, source_id, ip_hash) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.Title, e.Date, e.EndDate, e.Time, e.EndTime, e.Town, e.Category, e.Listing, e.Summary, e.Cost, e.Website, e.Source, e.Status, e.Origin, e.SubmitterName, now(), submitterEmail, sourceID, ipHash)
	return err
}

// uniqueEventID appends -2, -3… until the slug is free.
func (a *App) uniqueEventID(base string) string {
	if base == "" {
		base = "event"
	}
	id := base
	for i := 2; ; i++ {
		var n int
		_ = a.db.QueryRow(`SELECT COUNT(*) FROM events WHERE id = ?`, id).Scan(&n)
		if n == 0 {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
}

type Subscriber struct {
	ID         int64
	Email      string // empty for WhatsApp subscriptions
	Phone      string // E.164 digits, empty for email subscriptions
	Channel    string // "email" or "whatsapp"
	Frequency  string
	Horizon    int
	Towns      []string
	Categories []string
	Confirmed  bool
}

func (a *App) subscribers(where string, args ...any) ([]Subscriber, error) {
	rows, err := a.db.Query(`SELECT id, COALESCE(email,''), COALESCE(phone,''), channel, frequency, horizon, towns, categories, confirmed_at IS NOT NULL FROM subscribers WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscriber
	for rows.Next() {
		var s Subscriber
		var t, c string
		if err := rows.Scan(&s.ID, &s.Email, &s.Phone, &s.Channel, &s.Frequency, &s.Horizon, &t, &c, &s.Confirmed); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(t), &s.Towns)
		_ = json.Unmarshal([]byte(c), &s.Categories)
		out = append(out, s)
	}
	return out, rows.Err()
}

func jsonList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// mailBudgetOK enforces per-recipient sending limits so the service cannot be
// used to bombard an address: 5 transactional mails per hour, 20 per day.
func (a *App) mailBudgetOK(toHash string) bool {
	var hour, day int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM mail_log WHERE to_hash = ? AND sent_at > ?`, toHash, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)).Scan(&hour)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM mail_log WHERE to_hash = ? AND sent_at > ?`, toHash, time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339)).Scan(&day)
	return hour < 5 && day < 20
}

func (a *App) logMail(toHash, kind string, err error) {
	msg := ""
	if err != nil {
		msg = clean(err.Error(), 300)
	}
	_, _ = a.db.Exec(`INSERT INTO mail_log(to_hash, kind, sent_at, ok, err) VALUES(?,?,?,?,?)`, toHash, kind, now(), boolInt(err == nil), msg)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// housekeeping runs daily: expired single-use token ids, old mail log rows,
// unconfirmed subscriptions and unverified submissions older than 3 days, and
// events long past. Data minimisation is a POPIA requirement, not a nicety.
func (a *App) housekeeping() {
	cut := func(d time.Duration) string { return time.Now().UTC().Add(-d).Format(time.RFC3339) }
	stmts := []struct {
		q    string
		args []any
	}{
		{`DELETE FROM tokens_used WHERE used_at < ?`, []any{cut(45 * 24 * time.Hour)}},
		{`DELETE FROM wa_seen WHERE seen_at < ?`, []any{cut(7 * 24 * time.Hour)}},
		{`DELETE FROM mail_log WHERE sent_at < ?`, []any{cut(30 * 24 * time.Hour)}},
		{`DELETE FROM subscribers WHERE confirmed_at IS NULL AND created_at < ?`, []any{cut(3 * 24 * time.Hour)}},
		{`DELETE FROM events WHERE status = 'pending_email' AND created_at < ?`, []any{cut(3 * 24 * time.Hour)}},
		{`DELETE FROM listing_submissions WHERE status = 'pending_email' AND created_at < ?`, []any{cut(3 * 24 * time.Hour)}},
		{`UPDATE events SET submitter_email = '', submitter_name = '', ip_hash = '' WHERE status IN ('approved','rejected') AND decided_at < ?`, []any{cut(90 * 24 * time.Hour)}},
		{`DELETE FROM events WHERE status IN ('rejected') AND decided_at < ?`, []any{cut(180 * 24 * time.Hour)}},
		{`DELETE FROM events WHERE status = 'approved' AND (CASE WHEN end_date = '' THEN date ELSE end_date END) < ?`, []any{time.Now().AddDate(-1, 0, 0).Format("2006-01-02")}},
		{`DELETE FROM seen_uids WHERE seen_at < ?`, []any{cut(400 * 24 * time.Hour)}},
		{`DELETE FROM sessions WHERE expires_at < ? OR revoked = 1`, []any{cut(24 * time.Hour)}},
		{`DELETE FROM audit_log WHERE at < ?`, []any{cut(365 * 24 * time.Hour)}},
		{`DELETE FROM req_stats WHERE day < ?`, []any{time.Now().AddDate(0, 0, -400).Format("2006-01-02")}},
		{`DELETE FROM pageviews WHERE day < ?`, []any{time.Now().AddDate(0, 0, -400).Format("2006-01-02")}},
		{`DELETE FROM pv_uniques WHERE day < ?`, []any{time.Now().AddDate(0, 0, -35).Format("2006-01-02")}},
	}
	for _, s := range stmts {
		if _, err := a.db.Exec(s.q, s.args...); err != nil {
			a.logf("housekeeping: %v (%s)", err, strings.Fields(s.q)[2])
		}
	}
}
