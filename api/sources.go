package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// The watcher re-checks known sources on a schedule. Feeds (ICS) yield real
// events that go straight into the moderation queue; plain web pages can only
// be hashed, so a change produces a "look at this" line for the admin. A
// feed that covers the whole province can carry a "match" pattern (a
// case-insensitive regular expression) and then only events whose title,
// description, location or link matches it are queued; the rest are counted
// and dropped, and never offered again unless the admin uses Forget. It
// never publishes anything on its own.

//go:embed sources.json
var sourcesJSON []byte

//go:embed seed-events.json
var seedEventsJSON []byte

type sourceDef struct {
	URL      string `json:"url"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Listing  string `json:"listing"`
	Category string `json:"category"`
	Town     string `json:"town"`
	Match    string `json:"match,omitempty"`
}

// matchLimit caps a source filter; anything longer is a mistake, not a pattern.
const matchLimit = 300

// compileMatch turns a source's filter into a case-insensitive regexp, or
// nil for an empty filter. A bad pattern is an error the watcher reports on
// that source rather than something it guesses around.
func compileMatch(m string) (*regexp.Regexp, error) {
	m = strings.TrimSpace(m)
	if m == "" {
		return nil, nil
	}
	if len(m) > matchLimit {
		return nil, fmt.Errorf("filter longer than %d characters", matchLimit)
	}
	return regexp.Compile("(?i)" + m)
}

func (a *App) seedSources() error {
	var defs []sourceDef
	if err := json.Unmarshal(sourcesJSON, &defs); err != nil {
		return fmt.Errorf("sources.json: %w", err)
	}
	for _, d := range defs {
		if d.Kind != "ics" && d.Kind != "html" {
			return fmt.Errorf("sources.json: %s has kind %q", d.URL, d.Kind)
		}
		if _, err := compileMatch(d.Match); err != nil {
			return fmt.Errorf("sources.json: %s has a bad match pattern: %w", d.URL, err)
		}
		if _, err := a.db.Exec(`INSERT INTO sources(url, kind, label, listing, category, town, match) VALUES(?,?,?,?,?,?,?)
			ON CONFLICT(url) DO UPDATE SET kind=excluded.kind, label=excluded.label, listing=excluded.listing, category=excluded.category, town=excluded.town, match=excluded.match`,
			d.URL, d.Kind, d.Label, d.Listing, d.Category, d.Town, d.Match); err != nil {
			return err
		}
	}
	return nil
}

// seedEvents loads the curated events shipped with the build as approved
// entries. Existing rows are left alone so admin decisions survive deploys.
func (a *App) seedEvents() error {
	var evs []Event
	if err := json.Unmarshal(seedEventsJSON, &evs); err != nil {
		return fmt.Errorf("seed-events.json: %w", err)
	}
	for _, e := range evs {
		if !slugRe.MatchString(e.ID) || !towns[e.Town] || !categories[e.Category] {
			return fmt.Errorf("seed event %q has bad id/town/category", e.ID)
		}
		if _, ok := validDate(e.Date); !ok {
			return fmt.Errorf("seed event %q has bad date", e.ID)
		}
		if e.Cost == "" {
			e.Cost = "varies"
		}
		e.Status, e.Origin = "approved", "seed"
		var n int
		_ = a.db.QueryRow(`SELECT COUNT(*) FROM events WHERE id = ?`, e.ID).Scan(&n)
		if n > 0 {
			continue
		}
		if err := a.insertEvent(e, "", "", nil); err != nil {
			return err
		}
		_, _ = a.db.Exec(`UPDATE events SET decided_at = ? WHERE id = ?`, now(), e.ID)
	}
	return nil
}

type sourceRow struct {
	ID                                int64
	URL, Kind, Label, Status, Checked string
	Changed                           string
}

func (a *App) sourceRows() ([]sourceRow, error) {
	rows, err := a.db.Query(`SELECT id, url, kind, label, COALESCE(last_status,''), COALESCE(last_checked_at,''), COALESCE(last_changed_at,'') FROM sources WHERE enabled = 1 ORDER BY label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sourceRow
	for rows.Next() {
		var s sourceRow
		if err := rows.Scan(&s.ID, &s.URL, &s.Kind, &s.Label, &s.Status, &s.Checked, &s.Changed); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

var (
	tagRe    = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</\s*(script|style|noscript)\s*>`)
	anyTagRe = regexp.MustCompile(`(?s)<[^>]+>`)
	wsRe     = regexp.MustCompile(`\s+`)
	// Things that change on every load and would make every check a "change".
	noiseRe = regexp.MustCompile(`(?i)(nonce|csrf|token|_wpnonce|session|cache|timestamp)[^\s"']*`)
)

func pageHash(body []byte) string {
	s := tagRe.ReplaceAllString(string(body), " ")
	s = anyTagRe.ReplaceAllString(s, " ")
	s = noiseRe.ReplaceAllString(s, "")
	s = wsRe.ReplaceAllString(s, " ")
	sum := sha256.Sum256([]byte(strings.TrimSpace(s)))
	return hex.EncodeToString(sum[:])
}

func (a *App) fetch(url string) ([]byte, error) {
	client := &http.Client{Timeout: 25 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "HelderbergSocialBot/1.0 (+"+a.cfg.SiteURL+"/about.html)")
	req.Header.Set("Accept", "text/calendar, text/html;q=0.9, */*;q=0.5")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// 8 MB: a club's Google Calendar with a decade of history is around 4 MB,
	// and cutting it short would silently drop whichever events came last.
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// runWatch checks every enabled source once and returns a one-line summary.
// The admin gets an email only when there is something to act on.
func (a *App) runWatch(reason string) string { return a.runWatchWhere(reason, "enabled = 1") }

// runWatchOne checks a single source regardless of its enabled flag.
func (a *App) runWatchOne(id string) string { return a.runWatchWhere("manual", "id = ?", id) }

func (a *App) runWatchWhere(reason, where string, args ...any) string {
	if !a.watchMu.TryLock() {
		return "A check is already running."
	}
	defer a.watchMu.Unlock()
	rows, err := a.db.Query(`SELECT id, url, kind, label, listing, category, town, match, last_hash FROM sources WHERE `+where, args...)
	if err != nil {
		a.logf("watch: %v", err)
		return "error: " + err.Error()
	}
	type src struct {
		id                                                         int64
		url, kind, label, listing, category, town, match, lastHash string
	}
	var list []src
	for rows.Next() {
		var s src
		if err := rows.Scan(&s.id, &s.url, &s.kind, &s.label, &s.listing, &s.category, &s.town, &s.match, &s.lastHash); err == nil {
			list = append(list, s)
		}
	}
	rows.Close()

	var report []string
	newEvents, changed, errs := 0, 0, 0
	today := time.Now().In(a.cfg.TZ).Truncate(24 * time.Hour)
	for _, s := range list {
		filter, err := compileMatch(s.match)
		var body []byte
		if err == nil {
			body, err = a.fetch(s.url)
		}
		status := "ok"
		if err != nil {
			status = "error: " + clean(err.Error(), 120)
			errs++
			report = append(report, fmt.Sprintf("✗ %s: %s", s.label, status))
			_, _ = a.db.Exec(`UPDATE sources SET last_checked_at=?, last_status=? WHERE id=?`, now(), status, s.id)
			continue
		}
		switch s.kind {
		case "ics":
			evs := parseICS(string(body), a.cfg.TZ)
			added, skipped := 0, 0
			for _, ev := range evs {
				if ev.Start.Before(today.AddDate(0, 0, -1)) || ev.Start.After(today.AddDate(1, 0, 0)) {
					continue
				}
				if filter != nil && !filter.MatchString(ev.Summary+"\n"+ev.Description+"\n"+ev.Location+"\n"+ev.URL) {
					skipped++
					continue
				}
				res, err := a.db.Exec(`INSERT OR IGNORE INTO seen_uids(source_id, uid, seen_at) VALUES(?,?,?)`, s.id, ev.UID, now())
				if err != nil {
					continue
				}
				if n, _ := res.RowsAffected(); n == 0 {
					continue // already offered to the admin once
				}
				e := Event{
					Title:    clean(ev.Summary, 120),
					Date:     ev.Start.Format("2006-01-02"),
					Town:     s.town,
					Category: s.category,
					Listing:  s.listing,
					Summary:  cleanMulti(ev.Description, 800),
					Cost:     "varies",
					Source:   s.url,
					Status:   "pending_review",
					Origin:   "auto",
				}
				if w, ok := validURL(ev.URL); ok {
					e.Website = w
				}
				if !ev.AllDay {
					e.Time = ev.Start.Format("15:04")
					if !ev.End.IsZero() {
						e.EndTime = ev.End.Format("15:04")
					}
				}
				if !ev.End.IsZero() {
					end := ev.End
					if ev.AllDay {
						end = end.AddDate(0, 0, -1) // ICS all-day DTEND is exclusive
					}
					if ed := end.Format("2006-01-02"); ed > e.Date {
						e.EndDate = ed
					}
				}
				sid := s.id
				e.ID = a.uniqueEventID(slugify(e.Title) + "-" + e.Date)
				if err := a.insertEvent(e, "", "", &sid); err == nil {
					added++
					report = append(report, fmt.Sprintf("• NEW from %s: %s on %s\n    approve %s\n    reject  %s", s.label, e.Title, e.Date,
						a.moderateURL("event", e.ID, "approve"), a.moderateURL("event", e.ID, "reject")))
				}
			}
			newEvents += added
			status = fmt.Sprintf("ok, %d events in feed, %d new", len(evs), added)
			if filter != nil {
				status += fmt.Sprintf(", %d outside the filter", skipped)
			}
			_, _ = a.db.Exec(`UPDATE sources SET last_checked_at=?, last_status=? WHERE id=?`, now(), status, s.id)
		case "html":
			h := pageHash(body)
			if s.lastHash != "" && h != s.lastHash {
				changed++
				report = append(report, fmt.Sprintf("~ CHANGED: %s\n    %s", s.label, s.url))
				_, _ = a.db.Exec(`UPDATE sources SET last_checked_at=?, last_status='changed', last_hash=?, last_changed_at=? WHERE id=?`, now(), h, now(), s.id)
			} else {
				_, _ = a.db.Exec(`UPDATE sources SET last_checked_at=?, last_status='ok', last_hash=? WHERE id=?`, now(), h, s.id)
			}
		}
		time.Sleep(1500 * time.Millisecond) // one request at a time, politely spaced
	}
	if where == "enabled = 1" {
		_ = a.metaSet("last:watch", now())
	}
	summary := fmt.Sprintf("Checked %d sources (%s): %d new events queued, %d pages changed, %d errors.", len(list), reason, newEvents, changed, errs)
	a.logf("watch: %s", summary)
	if newEvents > 0 || changed > 0 || (errs > 0 && reason != "scheduled") {
		body := summary + "\n\n" + strings.Join(report, "\n") + "\n\nQueue: " + a.adminURL() + "\n"
		for _, to := range a.notifyList() {
			_ = a.send(Message{To: to, Kind: "watch", Subject: fmt.Sprintf("[HS] Source check: %d new, %d changed", newEvents, changed), Text: body})
		}
	}
	return summary
}
