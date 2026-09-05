package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"time"
)

// The watcher re-checks known sources on a schedule. Feeds (ICS) yield real
// events that go straight into the moderation queue; plain web pages can only
// be hashed, so a change produces a "look at this" line for the admin; a
// "list" page (an aggregator's RSS or a what's-on index that changes every
// day) is read for its links and only links never seen before are reported,
// so it never cries wolf. A feed that covers the whole province can carry a
// "match" pattern (a case-insensitive regular expression) and then only
// events whose title, description, location or link matches it are queued;
// the rest are counted and dropped, and never offered again unless the admin
// uses Forget. A recurring series in a feed (a weekly service, a monthly
// club night) is queued once, at its next occurrence, with the repeat rule
// written into the summary, rather than once per instance. It never
// publishes anything on its own.

// sourceKinds are the kinds a source can be; the sources table's CHECK
// constraint and the console's add form list the same three.
var sourceKinds = map[string]bool{"ics": true, "html": true, "list": true}

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
	// Retired is set in sources.json when the shipped list knows a source is
	// dead (bot-blocked, superseded, gone). The row is kept for its history
	// but switched off on every start, with the reason as its status.
	Retired string `json:"retired,omitempty"`
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
		if !sourceKinds[d.Kind] {
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
		if d.Retired != "" {
			if _, err := a.db.Exec(`UPDATE sources SET enabled = 0, last_status = ? WHERE url = ? AND (enabled = 1 OR last_status <> ?)`, "retired: "+d.Retired, d.URL, "retired: "+d.Retired); err != nil {
				return err
			}
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
	MemberID                          int64  // a promoter's connected calendar
	Org                               string // its organisation, for the console
	Enabled                           bool
}

func (a *App) sourceRows() ([]sourceRow, error) {
	return a.sourceRowsWhere(`s.enabled = 1`)
}

// sourceRowsWhere lists sources with their promoter's organisation, if any.
func (a *App) sourceRowsWhere(where string, args ...any) ([]sourceRow, error) {
	rows, err := a.db.Query(`SELECT s.id, s.url, s.kind, s.label, COALESCE(s.last_status,''), COALESCE(s.last_checked_at,''), COALESCE(s.last_changed_at,''), COALESCE(s.member_id, 0), COALESCE(p.org, ''), s.enabled FROM sources s LEFT JOIN promoters p ON p.member_id = s.member_id WHERE `+where+` ORDER BY s.label`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sourceRow
	for rows.Next() {
		var s sourceRow
		var enabled int
		if err := rows.Scan(&s.ID, &s.URL, &s.Kind, &s.Label, &s.Status, &s.Checked, &s.Changed, &s.MemberID, &s.Org, &enabled); err != nil {
			return nil, err
		}
		s.Enabled = enabled == 1
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

// fetch gets a source once, and once more after a pause when the first try
// failed on the network or with a 5xx: small WordPress sites here time out
// during their own night-time backups and answer fine a moment later. A 4xx
// is the site's answer and is not retried.
func (a *App) fetch(url string) ([]byte, error) {
	body, err := a.fetchOnce(url)
	if err != nil && retryableFetch(err) {
		time.Sleep(fetchRetryPause)
		body, err = a.fetchOnce(url)
	}
	return body, err
}

var fetchRetryPause = 3 * time.Second

type httpStatusError int

func (e httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", int(e)) }

func retryableFetch(err error) bool {
	if se, ok := err.(httpStatusError); ok {
		return int(se) >= 500 || int(se) == 429
	}
	return true // timeouts, resets, DNS blips
}

func (a *App) fetchOnce(url string) ([]byte, error) {
	// A redirect may not lead somewhere private: a promoter's calendar
	// address is checked when it is added (publicCalendarURL), and this
	// keeps a later redirect from undoing that check.
	client := &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if msg := privateTarget(req.URL); msg != "" {
			return errors.New("redirect refused: " + msg)
		}
		return nil
	}}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	// The "Mozilla/5.0 (compatible; Name/1.0; +url)" form is what the big
	// crawlers use and what the WAF in front of parkrun.co.za keys on: the
	// bare "Name/1.0" form got a 403 from it on every page (checked
	// 2026-09-05), this form is let through. Still says who we are.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HelderbergSocialBot/1.0; +"+a.cfg.SiteURL+"/about.html)")
	req.Header.Set("Accept", "text/calendar, text/html;q=0.9, */*;q=0.5")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, httpStatusError(resp.StatusCode)
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
	rows, err := a.db.Query(`SELECT id, url, kind, label, listing, category, town, match, last_hash, COALESCE(member_id, 0) FROM sources WHERE `+where, args...)
	if err != nil {
		a.logf("watch: %v", err)
		return "error: " + err.Error()
	}
	type src struct {
		id                                                         int64
		url, kind, label, listing, category, town, match, lastHash string
		memberID                                                   int64
	}
	var list []src
	for rows.Next() {
		var s src
		if err := rows.Scan(&s.id, &s.url, &s.kind, &s.label, &s.listing, &s.category, &s.town, &s.match, &s.lastHash, &s.memberID); err == nil {
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
			added, skipped, series := 0, 0, 0
			for _, ev := range evs {
				if ev.RecurrenceID {
					continue // one changed instance of a series; the series itself is offered
				}
				occ := ev.occurrences(today.AddDate(0, 0, -1), today.AddDate(1, 0, 0), 1)
				if len(occ) == 0 {
					continue
				}
				start := occ[0]
				if ev.RRule != "" {
					series++
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
				summary := ev.Description
				if rt := repeatText(ev.RRule, a.cfg.TZ); rt != "" {
					summary = strings.TrimSpace(rt + "\n\n" + summary)
				}
				e := Event{
					Title:    clean(ev.Summary, 120),
					Date:     start.Format("2006-01-02"),
					Town:     s.town,
					Category: s.category,
					Listing:  s.listing,
					Summary:  cleanMulti(summary, 800),
					Cost:     "varies",
					Source:   s.url,
					Status:   "pending_review",
					Origin:   "auto",
				}
				if w, ok := validURL(ev.URL); ok {
					e.Website = w
				}
				if !ev.AllDay {
					e.Time = start.Format("15:04")
					if !ev.End.IsZero() {
						e.EndTime = ev.End.Format("15:04")
					}
				}
				if !ev.End.IsZero() {
					end := start.Add(ev.End.Sub(ev.Start)) // same length as the first instance
					if ev.AllDay {
						end = end.AddDate(0, 0, -1) // ICS all-day DTEND is exclusive
					}
					if ed := end.Format("2006-01-02"); ed > e.Date {
						e.EndDate = ed
					}
				}
				sid := s.id
				e.ID = a.uniqueEventID(slugify(e.Title) + "-" + e.Date)
				if s.memberID != 0 {
					// A promoter's connected calendar: the event is theirs,
					// and skips the queue only when they are trusted.
					if m := a.memberByID(s.memberID); m != nil && m.IsPromoter() {
						a.stampPromoterEvent(&e, m)
						e.Origin = "auto"
					}
				}
				if err := a.insertEvent(e, "", "", &sid); err == nil {
					added++
					if e.Status == "approved" {
						report = append(report, fmt.Sprintf("• PUBLISHED from %s (trusted promoter): %s on %s", s.label, e.Title, e.Date))
						continue
					}
					report = append(report, fmt.Sprintf("• NEW from %s: %s on %s\n    approve %s\n    reject  %s", s.label, e.Title, e.Date,
						a.moderateURL("event", e.ID, "approve"), a.moderateURL("event", e.ID, "reject")))
				}
			}
			newEvents += added
			status = fmt.Sprintf("ok, %d events in feed, %d new", len(evs), added)
			if series > 0 {
				status += fmt.Sprintf(", %d recurring", series)
			}
			if filter != nil {
				status += fmt.Sprintf(", %d outside the filter", skipped)
			}
			_, _ = a.db.Exec(`UPDATE sources SET last_checked_at=?, last_status=? WHERE id=?`, now(), status, s.id)
		case "list":
			links := extractLinks(body, s.url)
			first := s.lastHash == "" // nothing remembered yet: learn the page, report nothing
			fresh := 0
			var lines []string
			for _, l := range links {
				if filter != nil && !filter.MatchString(l.Title+"\n"+l.URL) {
					continue
				}
				res, err := a.db.Exec(`INSERT OR IGNORE INTO seen_uids(source_id, uid, seen_at) VALUES(?,?,?)`, s.id, l.URL, now())
				if err != nil {
					continue
				}
				if n, _ := res.RowsAffected(); n == 0 {
					continue
				}
				fresh++
				if !first && len(lines) < 25 {
					lines = append(lines, fmt.Sprintf("• NEW on %s: %s\n    %s", s.label, l.Title, l.URL))
				}
			}
			switch {
			case first:
				status = fmt.Sprintf("ok, %d links remembered", fresh)
				_, _ = a.db.Exec(`UPDATE sources SET last_checked_at=?, last_status=?, last_hash='seen' WHERE id=?`, now(), status, s.id)
			case fresh > 0:
				changed++
				report = append(report, lines...)
				status = fmt.Sprintf("ok, %d links, %d new", len(links), fresh)
				_, _ = a.db.Exec(`UPDATE sources SET last_checked_at=?, last_status=?, last_changed_at=? WHERE id=?`, now(), status, now(), s.id)
			default:
				status = fmt.Sprintf("ok, %d links, nothing new", len(links))
				_, _ = a.db.Exec(`UPDATE sources SET last_checked_at=?, last_status=? WHERE id=?`, now(), status, s.id)
			}
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

/* ---------- link lists ---------- */

// pageLink is one entry of a list source: where it points and what it is called.
type pageLink struct{ URL, Title string }

var (
	rssItemRe   = regexp.MustCompile(`(?is)<(item|entry)\b.*?</(item|entry)>`)
	rssTitleRe  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	rssLinkRe   = regexp.MustCompile(`(?is)<link[^>]*>\s*(?:<!\[CDATA\[)?\s*(https?://[^<\]\s]+)`)
	atomLinkRe  = regexp.MustCompile(`(?is)<link[^>]*href="([^"]+)"`)
	cdataRe     = regexp.MustCompile(`(?s)<!\[CDATA\[(.*?)\]\]>`)
	anchorRe    = regexp.MustCompile(`(?is)<a\b[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	linkLimit   = 500
	skipLinkRe  = regexp.MustCompile(`(?i)^(mailto:|tel:|javascript:)|\.(png|jpe?g|gif|svg|webp|css|js|pdf|ico)(\?|$)`)
	unwrapCDATA = func(s string) string { return cdataRe.ReplaceAllString(s, "$1") }
)

// extractLinks reads the links of an RSS/Atom feed (item title + link) or,
// failing that, every anchor on an HTML page, resolved against the page's
// address. Order is the page's order; duplicates and non-page links
// (images, mail, scripts) are dropped.
func extractLinks(body []byte, base string) []pageLink {
	s := string(body)
	seen := map[string]bool{}
	var out []pageLink
	add := func(u, title string) {
		u = strings.TrimSpace(unwrapCDATA(u))
		if u == "" || strings.HasPrefix(u, "#") || skipLinkRe.MatchString(u) {
			return
		}
		if bu, err := neturl.Parse(base); err == nil {
			if ru, err := bu.Parse(u); err == nil {
				ru.Fragment = ""
				u = ru.String()
			}
		}
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return
		}
		if seen[u] || len(out) >= linkLimit {
			return
		}
		seen[u] = true
		title = clean(strings.TrimSpace(anyTagRe.ReplaceAllString(unwrapCDATA(title), " ")), 140)
		if title == "" {
			title = u
		}
		out = append(out, pageLink{URL: u, Title: title})
	}
	if items := rssItemRe.FindAllString(s, -1); len(items) > 0 {
		for _, it := range items {
			title := ""
			if m := rssTitleRe.FindStringSubmatch(it); m != nil {
				title = m[1]
			}
			if m := rssLinkRe.FindStringSubmatch(it); m != nil {
				add(m[1], title)
			} else if m := atomLinkRe.FindStringSubmatch(it); m != nil {
				add(m[1], title)
			}
		}
		return out
	}
	for _, m := range anchorRe.FindAllStringSubmatch(s, -1) {
		add(m[1], m[2])
	}
	return out
}
