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
const schemaVersion = 10

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
		ip_hash TEXT NOT NULL DEFAULT '',
		visible_from TEXT NOT NULL DEFAULT '',
		hidden INTEGER NOT NULL DEFAULT 0,
		promoted INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE INDEX IF NOT EXISTS events_status_date ON events(status, date)`,
	`CREATE TABLE IF NOT EXISTS members (
		id INTEGER PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		pw_hash TEXT NOT NULL,
		created_at TEXT NOT NULL,
		verified_at TEXT,
		last_login_at TEXT,
		status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
		ip_hash TEXT NOT NULL DEFAULT '',
		role TEXT NOT NULL DEFAULT 'member',
		trusted INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS promoters (
		member_id INTEGER PRIMARY KEY REFERENCES members(id) ON DELETE CASCADE,
		org TEXT NOT NULL,
		kind TEXT NOT NULL,
		website TEXT NOT NULL DEFAULT '',
		facebook TEXT NOT NULL DEFAULT '',
		instagram TEXT NOT NULL DEFAULT '',
		towns TEXT NOT NULL DEFAULT '[]',
		blurb TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL CHECK (status IN ('pending','approved','declined')),
		applied_at TEXT NOT NULL,
		decided_at TEXT,
		note TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS posts (
		id TEXT PRIMARY KEY,
		member_id INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
		title TEXT NOT NULL,
		body TEXT NOT NULL,
		link TEXT NOT NULL DEFAULT '',
		town TEXT NOT NULL,
		category TEXT NOT NULL,
		starts TEXT NOT NULL,
		ends TEXT NOT NULL,
		status TEXT NOT NULL CHECK (status IN ('pending_review','approved','rejected')),
		hidden INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		decided_at TEXT
	)`,
	`CREATE INDEX IF NOT EXISTS posts_member ON posts(member_id)`,
	`CREATE INDEX IF NOT EXISTS posts_live ON posts(status, hidden, starts, ends)`,
	`CREATE TABLE IF NOT EXISTS member_identities (
		provider TEXT NOT NULL,
		sub TEXT NOT NULL,
		member_id INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
		email TEXT NOT NULL DEFAULT '',
		linked_at TEXT NOT NULL,
		PRIMARY KEY (provider, sub)
	)`,
	`CREATE INDEX IF NOT EXISTS member_identities_member ON member_identities(member_id)`,
	`CREATE TABLE IF NOT EXISTS member_sessions (
		id_hash TEXT PRIMARY KEY,
		member_id INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		last_seen_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		ip_hash TEXT NOT NULL DEFAULT '',
		ua TEXT NOT NULL DEFAULT '',
		revoked INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE INDEX IF NOT EXISTS member_sessions_member ON member_sessions(member_id)`,
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
		ip_hash TEXT NOT NULL DEFAULT '',
		member_id INTEGER
	)`,
	`CREATE TABLE IF NOT EXISTS sources (
		id INTEGER PRIMARY KEY,
		url TEXT NOT NULL UNIQUE,
		kind TEXT NOT NULL CHECK (kind IN ('ics','html','list')),
		label TEXT NOT NULL,
		listing TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT 'community',
		town TEXT NOT NULL DEFAULT 'somerset-west',
		match TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		last_checked_at TEXT,
		last_hash TEXT NOT NULL DEFAULT '',
		last_status TEXT NOT NULL DEFAULT '',
		last_changed_at TEXT,
		member_id INTEGER
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
	`CREATE TABLE IF NOT EXISTS fb_posts (id INTEGER PRIMARY KEY, kind TEXT NOT NULL CHECK (kind IN ('event','weekend','manual')), ref TEXT NOT NULL DEFAULT '', message TEXT NOT NULL, link TEXT NOT NULL DEFAULT '', due_at TEXT NOT NULL, status TEXT NOT NULL CHECK (status IN ('queued','posted','failed','cancelled')), fb_id TEXT NOT NULL DEFAULT '', err TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, posted_at TEXT NOT NULL DEFAULT '', tries INTEGER NOT NULL DEFAULT 0)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS fb_posts_ref ON fb_posts (kind, ref) WHERE ref <> ''`,
	`CREATE INDEX IF NOT EXISTS fb_posts_due ON fb_posts (status, due_at)`,
	`CREATE TABLE IF NOT EXISTS blocklist (id INTEGER PRIMARY KEY, kind TEXT NOT NULL CHECK (kind IN ('ip','email')), value TEXT NOT NULL, note TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, UNIQUE (kind, value))`,
	// v7: Facebook groups the page posts in by hand, on a cadence the console tracks.
	`CREATE TABLE IF NOT EXISTS fb_groups (
		id INTEGER PRIMARY KEY,
		fb_id TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		kind TEXT NOT NULL DEFAULT 'community',
		town TEXT NOT NULL DEFAULT '',
		note TEXT NOT NULL DEFAULT '',
		cadence_days INTEGER NOT NULL DEFAULT 30,
		enabled INTEGER NOT NULL DEFAULT 1,
		skip_reason TEXT NOT NULL DEFAULT '',
		posts INTEGER NOT NULL DEFAULT 0,
		last_posted_at TEXT NOT NULL DEFAULT '',
		next_due TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS fb_groups_due ON fb_groups (enabled, next_due)`,
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
	// v5: events posted from a member account point back at it.
	if !hasColumn(db, "events", "member_id") {
		if _, err := db.Exec(`ALTER TABLE events ADD COLUMN member_id INTEGER`); err != nil {
			return fmt.Errorf("migrate events.member_id: %w", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS events_member ON events(member_id)`); err != nil {
			return fmt.Errorf("migrate events_member index: %w", err)
		}
	}
	// v6: a regional feed can carry a filter so only Helderberg events are queued.
	if !hasColumn(db, "sources", "match") {
		if _, err := db.Exec(`ALTER TABLE sources ADD COLUMN match TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migrate sources.match: %w", err)
		}
	}
	// v7: a third source kind, "list". The kind lives in a CHECK constraint,
	// which SQLite cannot alter in place, so the table is rebuilt once.
	var sourcesSQL string
	_ = db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='sources'`).Scan(&sourcesSQL)
	if sourcesSQL != "" && !strings.Contains(sourcesSQL, "'list'") {
		_, err := db.Exec(`BEGIN;
			CREATE TABLE sources_v7 (
				id INTEGER PRIMARY KEY,
				url TEXT NOT NULL UNIQUE,
				kind TEXT NOT NULL CHECK (kind IN ('ics','html','list')),
				label TEXT NOT NULL,
				listing TEXT NOT NULL DEFAULT '',
				category TEXT NOT NULL DEFAULT 'community',
				town TEXT NOT NULL DEFAULT 'somerset-west',
				match TEXT NOT NULL DEFAULT '',
				enabled INTEGER NOT NULL DEFAULT 1,
				last_checked_at TEXT,
				last_hash TEXT NOT NULL DEFAULT '',
				last_status TEXT NOT NULL DEFAULT '',
				last_changed_at TEXT
			);
			INSERT INTO sources_v7(id, url, kind, label, listing, category, town, match, enabled, last_checked_at, last_hash, last_status, last_changed_at)
				SELECT id, url, kind, label, listing, category, town, match, enabled, last_checked_at, last_hash, last_status, last_changed_at FROM sources;
			DROP TABLE sources;
			ALTER TABLE sources_v7 RENAME TO sources;
			COMMIT;`)
		if err != nil {
			_, _ = db.Exec(`ROLLBACK`)
			return fmt.Errorf("migrate sources to v7: %w", err)
		}
	}
	// v8 added members.google_sub; v9 moves it into member_identities, one
	// row per (provider, subject), so Microsoft and Yahoo fit alongside
	// Google. Rows are copied before the column and its index go.
	if hasColumn(db, "members", "google_sub") {
		for _, q := range []string{
			`INSERT OR IGNORE INTO member_identities(provider, sub, member_id, email, linked_at) SELECT 'google', google_sub, id, email, COALESCE(verified_at, created_at) FROM members WHERE google_sub IS NOT NULL AND google_sub <> ''`,
			`DROP INDEX IF EXISTS members_google`,
			`ALTER TABLE members DROP COLUMN google_sub`,
		} {
			if _, err := db.Exec(q); err != nil {
				return fmt.Errorf("migrate members.google_sub to member_identities: %w", err)
			}
		}
	}
	// v10: promoters. Members gain a role and a trust flag, events gain a
	// show-from date, a hidden switch and a promoted mark, sources and
	// listing submissions may belong to a member. The new tables (promoters,
	// posts) come from the base schema list.
	for _, c := range []struct{ table, col, ddl string }{
		{"members", "role", `ALTER TABLE members ADD COLUMN role TEXT NOT NULL DEFAULT 'member'`},
		{"members", "trusted", `ALTER TABLE members ADD COLUMN trusted INTEGER NOT NULL DEFAULT 0`},
		{"events", "visible_from", `ALTER TABLE events ADD COLUMN visible_from TEXT NOT NULL DEFAULT ''`},
		{"events", "hidden", `ALTER TABLE events ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0`},
		{"events", "promoted", `ALTER TABLE events ADD COLUMN promoted INTEGER NOT NULL DEFAULT 0`},
		{"sources", "member_id", `ALTER TABLE sources ADD COLUMN member_id INTEGER`},
		{"listing_submissions", "member_id", `ALTER TABLE listing_submissions ADD COLUMN member_id INTEGER`},
	} {
		if !hasColumn(db, c.table, c.col) {
			if _, err := db.Exec(c.ddl); err != nil {
				return fmt.Errorf("migrate %s.%s: %w", c.table, c.col, err)
			}
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
	Promoted bool   `json:"promoted,omitempty"` // posted by an approved promoter
	By       string `json:"by,omitempty"`       // the promoter's organisation, filled for the public feed
	// internal
	Status        string `json:"-"`
	Origin        string `json:"-"`
	SubmitterName string `json:"-"`
	CreatedAt     string `json:"-"`
	MemberID      int64  `json:"-"` // 0 when not posted from a member account
	VisibleFrom   string `json:"-"` // '' or a date before which the event stays off the site
	Hidden        bool   `json:"-"` // switched off by its promoter; keeps its approval
}

const eventCols = `id, title, date, end_date, time, end_time, town, category, listing, summary, cost, website, source, status, origin, submitter_name, created_at, COALESCE(member_id, 0), visible_from, hidden, promoted`

func scanEvent(rows interface{ Scan(...any) error }) (Event, error) {
	var e Event
	var hidden, promoted int
	err := rows.Scan(&e.ID, &e.Title, &e.Date, &e.EndDate, &e.Time, &e.EndTime, &e.Town, &e.Category, &e.Listing, &e.Summary, &e.Cost, &e.Website, &e.Source, &e.Status, &e.Origin, &e.SubmitterName, &e.CreatedAt, &e.MemberID, &e.VisibleFrom, &hidden, &promoted)
	e.Verified = e.Status == "approved" && e.Origin == "admin"
	e.Hidden, e.Promoted = hidden == 1, promoted == 1
	return e, err
}

// liveEventsWhere is the part of every public query that a promoter's
// switches control: not hidden, and past its show-from date.
const liveEventsWhere = `hidden = 0 AND (visible_from = '' OR visible_from <= ?)`

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
	return a.queryEvents(`status = 'approved' AND `+liveEventsWhere+` AND (CASE WHEN end_date = '' THEN date ELSE end_date END) >= ? AND date <= ?`, f, f, to)
}

func (a *App) insertEvent(e Event, ipHash string, submitterEmail string, sourceID *int64) error {
	var member *int64
	if e.MemberID > 0 {
		member = &e.MemberID
	}
	promoted, hidden := 0, 0
	if e.Promoted {
		promoted = 1
	}
	if e.Hidden {
		hidden = 1
	}
	var decided any
	if e.Status == "approved" || e.Status == "rejected" {
		decided = now()
	}
	_, err := a.db.Exec(`INSERT INTO events(id, title, date, end_date, time, end_time, town, category, listing, summary, cost, website, source, status, origin, submitter_name, created_at, member_id, submitter_email, source_id, ip_hash, visible_from, hidden, promoted, decided_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.Title, e.Date, e.EndDate, e.Time, e.EndTime, e.Town, e.Category, e.Listing, e.Summary, e.Cost, e.Website, e.Source, e.Status, e.Origin, e.SubmitterName, now(), member, submitterEmail, sourceID, ipHash, e.VisibleFrom, hidden, promoted, decided)
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
		{`DELETE FROM fb_posts WHERE status IN ('posted','failed','cancelled') AND created_at < ?`, []any{cut(180 * 24 * time.Hour)}},
		{`DELETE FROM subscribers WHERE confirmed_at IS NULL AND created_at < ?`, []any{cut(3 * 24 * time.Hour)}},
		{`DELETE FROM members WHERE verified_at IS NULL AND created_at < ?`, []any{cut(3 * 24 * time.Hour)}},
		{`DELETE FROM member_sessions WHERE expires_at < ? OR revoked = 1`, []any{cut(24 * time.Hour)}},
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
