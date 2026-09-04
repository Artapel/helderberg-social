package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Posting to the Facebook page through the Graph API.
//
// Nothing here writes a word a person has not already approved: every post is
// built from an approved event, from the approved-events list, or typed by
// the admin in the console. Posts go through a queue (fb_posts) that the
// scheduler drains one item a minute, so a burst of approvals never floods
// the page and an admin can still cancel a post in the delay window. Posting
// is off until HS_FB_PAGE_ID and HS_FB_PAGE_TOKEN are set, and even then the
// automatic kinds are each behind a setting that defaults to off.
//
// The token is a Page access token (docs/facebook.md explains how to get a
// non-expiring one from a system user). It is never logged and only its
// length is ever shown.

type fbClient struct {
	pageID, token, version string
	http                   *http.Client
}

// fbEndpoint is a var so tests can point the client at a local server.
var fbEndpoint = "https://graph.facebook.com"

func newFBClient(cfg *Config) *fbClient {
	if cfg.FBPageID == "" {
		return nil
	}
	return &fbClient{pageID: cfg.FBPageID, token: cfg.FBToken, version: cfg.FBVersion, http: &http.Client{Timeout: 20 * time.Second}}
}

func (a *App) fbEnabled() bool { return a.fb != nil }

// fbError is what Meta answers with; Code is what decides whether a retry
// can help (a bad token never fixes itself).
type fbError struct {
	Code int
	Msg  string
}

func (e *fbError) Error() string { return fmt.Sprintf("facebook %d: %s", e.Code, e.Msg) }

// fbPermanent reports errors a retry cannot fix: expired or invalid token
// (190), missing permission (200-299), an id that is not a page (100 with
// the id in the text), or the app being in development mode (10).
func fbPermanent(err error) bool {
	e, ok := err.(*fbError)
	if !ok {
		return false
	}
	return e.Code == 190 || e.Code == 10 || e.Code == 100 || (e.Code >= 200 && e.Code < 300)
}

func (f *fbClient) call(method, path string, form url.Values) (map[string]any, error) {
	var rd io.Reader
	if method == "POST" {
		rd = strings.NewReader(form.Encode())
	} else if len(form) > 0 {
		path += "?" + form.Encode()
	}
	req, err := http.NewRequest(method, fbEndpoint+"/"+f.version+"/"+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+f.token)
	if method == "POST" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, err := f.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("facebook: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if e, ok := out["error"].(map[string]any); ok {
		code, _ := e["code"].(float64)
		msg, _ := e["message"].(string)
		return out, &fbError{Code: int(code), Msg: clean(msg, 200)}
	}
	if res.StatusCode >= 300 {
		return out, fmt.Errorf("facebook: HTTP %d", res.StatusCode)
	}
	return out, nil
}

// pageInfo asks the page what it is called and where it lives; it doubles
// as the connection test, since it fails the same way a post would.
func (f *fbClient) pageInfo() (name, link string, err error) {
	out, err := f.call("GET", f.pageID, url.Values{"fields": {"name,link"}})
	if err != nil {
		return "", "", err
	}
	name, _ = out["name"].(string)
	link, _ = out["link"].(string)
	return name, link, nil
}

// publish creates one post on the page's timeline and returns its id, which
// is "<pageid>_<postid>" and makes the permalink.
func (f *fbClient) publish(message, link string) (string, error) {
	form := url.Values{"message": {message}}
	if link != "" {
		form.Set("link", link)
	}
	out, err := f.call("POST", f.pageID+"/feed", form)
	if err != nil {
		return "", err
	}
	id, _ := out["id"].(string)
	if id == "" {
		return "", fmt.Errorf("facebook: no post id in reply")
	}
	return id, nil
}

/* ---------- queue ---------- */

type fbPost struct {
	ID        int64
	Kind      string // event, weekend, manual
	Ref       string // event id or weekend date; makes automatic posts idempotent
	Message   string
	Link      string
	DueAt     string
	Status    string // queued, posted, failed, cancelled
	FBID      string
	Err       string
	CreatedAt string
	PostedAt  string
	Tries     int
}

const fbCols = `id, kind, ref, message, link, due_at, status, fb_id, err, created_at, posted_at, tries`

func (p fbPost) Permalink() string {
	if p.FBID == "" {
		return ""
	}
	return "https://www.facebook.com/" + p.FBID
}

func (a *App) fbPosts(where string, limit int, args ...any) []fbPost {
	rows, err := a.db.Query(`SELECT `+fbCols+` FROM fb_posts WHERE `+where+` ORDER BY due_at DESC, id DESC LIMIT `+fmt.Sprint(limit), args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []fbPost
	for rows.Next() {
		var p fbPost
		if rows.Scan(&p.ID, &p.Kind, &p.Ref, &p.Message, &p.Link, &p.DueAt, &p.Status, &p.FBID, &p.Err, &p.CreatedAt, &p.PostedAt, &p.Tries) == nil {
			out = append(out, p)
		}
	}
	return out
}

// fbQueue adds a post. A kind+ref pair is queued at most once ever (the
// unique index), so approving, editing and re-approving an event yields one
// post; manual posts have no ref and always go in.
func (a *App) fbQueue(kind, ref, message, link string, due time.Time) (bool, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return false, fmt.Errorf("the post is empty")
	}
	if len(message) > 5000 {
		return false, fmt.Errorf("the post is too long (5000 characters max)")
	}
	res, err := a.db.Exec(`INSERT OR IGNORE INTO fb_posts(kind, ref, message, link, due_at, status, created_at) VALUES(?,?,?,?,?,'queued',?)`,
		kind, ref, message, link, due.UTC().Format(time.RFC3339), now())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// fbQueueEvent is the approval hook: when automatic event posts are on, the
// event goes into the queue after the configured delay. Past events and
// events outside the public window are skipped.
func (a *App) fbQueueEvent(id string) {
	if !a.fbEnabled() || !a.settingBool("fb_events_on") {
		return
	}
	evs, err := a.queryEvents(`id = ? AND status = 'approved'`, id)
	if err != nil || len(evs) != 1 {
		return
	}
	e := evs[0]
	today := time.Now().In(a.cfg.TZ).Format("2006-01-02")
	if end := e.EndDate; (end != "" && end < today) || (end == "" && e.Date < today) {
		return
	}
	due := time.Now().Add(time.Duration(a.settingInt("fb_events_delay")) * time.Minute)
	if ok, err := a.fbQueue("event", e.ID, a.fbEventText(e), a.eventURL(e), due); err != nil {
		a.logf("facebook: queue event %s: %v", e.ID, err)
	} else if ok {
		a.logf("facebook: event %s queued for %s", e.ID, due.In(a.cfg.TZ).Format("02 Jan 15:04"))
	}
}

// fbCancelRef withdraws a queued automatic post when its event is taken
// back, rejected or deleted. Posts already on the page stay; taking them
// down is a decision for a person.
func (a *App) fbCancelRef(kind, ref string) {
	_, _ = a.db.Exec(`UPDATE fb_posts SET status='cancelled' WHERE kind=? AND ref=? AND status='queued'`, kind, ref)
}

/* ---------- texts ---------- */

// fbEventText is the post for one event. Facebook shows the link as a card
// below the text, so the text itself carries what the card cannot: when,
// where, what it costs, and the summary.
func (a *App) fbEventText(e Event) string {
	var b strings.Builder
	b.WriteString(e.Title)
	b.WriteString("\n")
	when := fmtDate(e.Date)
	if e.EndDate != "" && e.EndDate != e.Date {
		when += " to " + fmtDate(e.EndDate)
	}
	if e.Time != "" {
		when += ", " + e.Time
		if e.EndTime != "" {
			when += " to " + e.EndTime
		}
	}
	b.WriteString(when)
	b.WriteString(" · " + townName(e.Town))
	if e.Cost != "" {
		b.WriteString(" · " + e.Cost)
	}
	if s := strings.TrimSpace(e.Summary); s != "" {
		b.WriteString("\n\n")
		if len(s) > 400 {
			cut := s[:400]
			if i := strings.LastIndex(cut, " "); i > 200 {
				cut = cut[:i]
			}
			s = strings.TrimRight(cut, " .,;") + "…"
		}
		b.WriteString(s)
	}
	fmt.Fprintf(&b, "\n\nMore events across the Helderberg: %s/events.html", a.cfg.SiteURL)
	return b.String()
}

// weekendOf returns the Saturday and Sunday the next "this weekend" post is
// about: the coming weekend, or the current one on a Saturday or Sunday.
func weekendOf(n time.Time) (sat, sun time.Time) {
	d := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location())
	switch d.Weekday() {
	case time.Sunday:
		sat = d.AddDate(0, 0, -1)
	default:
		sat = d.AddDate(0, 0, int(time.Saturday-d.Weekday()))
	}
	return sat, sat.AddDate(0, 0, 1)
}

// fbWeekendText lists what is on this weekend. Empty when there is nothing,
// and the caller then posts nothing: a "nothing on" post helps nobody.
func (a *App) fbWeekendText(n time.Time) (string, string) {
	sat, sun := weekendOf(n)
	evs, err := a.approvedEvents(sat, 1)
	if err != nil || len(evs) == 0 {
		return "", sat.Format("2006-01-02")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "What's on in the Helderberg this weekend (%s):\n", sat.Format("2 Jan")+" to "+sun.Format("2 Jan"))
	const max = 12
	for i, e := range evs {
		if i == max {
			fmt.Fprintf(&b, "…and %d more on the site.\n", len(evs)-max)
			break
		}
		day := "Sat"
		if e.Date > sat.Format("2006-01-02") {
			day = "Sun"
		} else if e.Date < sat.Format("2006-01-02") {
			day = "Sat/Sun" // a multi-day event that spans the weekend
		}
		line := "• " + day + ": " + e.Title + " (" + townName(e.Town) + ")"
		if e.Time != "" {
			line += " " + e.Time
		}
		if e.Cost != "" {
			line += " · " + e.Cost
		}
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "\nFull list with details: %s/events.html\nAdd your event, free: %s/submit.html?kind=event", a.cfg.SiteURL, a.cfg.SiteURL)
	return b.String(), sat.Format("2006-01-02")
}

/* ---------- scheduler ---------- */

// fbTick runs once a minute from the scheduler: queues the weekly weekend
// post when its slot comes round, then publishes at most one due post.
func (a *App) fbTick(n time.Time) {
	if !a.fbEnabled() {
		return
	}
	day := n.Format("2006-01-02")
	if a.settingBool("fb_weekend_on") && n.Weekday() == time.Weekday(a.settingInt("fb_weekend_day")) && n.Hour() == a.settingInt("fb_weekend_hour") && a.metaGet("last:fb:weekend") != day {
		_ = a.metaSet("last:fb:weekend", day)
		if text, ref := a.fbWeekendText(n); text != "" {
			if ok, err := a.fbQueue("weekend", ref, text, a.cfg.SiteURL+"/events.html", time.Now()); err != nil {
				a.logf("facebook: queue weekend post: %v", err)
			} else if ok {
				a.logf("facebook: weekend post for %s queued", ref)
			}
		} else {
			a.logf("facebook: nothing on this weekend, no post")
		}
	}
	a.fbPostDue()
}

// fbPostDue publishes the oldest due post. Transient failures retry three
// times a quarter-hour apart; permanent ones (bad token, no permission)
// fail at once so the queue does not sit there hammering Meta.
func (a *App) fbPostDue() {
	due := a.fbPosts(`status='queued' AND due_at <= ?`, 1, now())
	if len(due) == 0 {
		return
	}
	p := due[0]
	id, err := a.fb.publish(p.Message, p.Link)
	if err == nil {
		_, _ = a.db.Exec(`UPDATE fb_posts SET status='posted', fb_id=?, posted_at=?, err='', tries=tries+1 WHERE id=?`, id, now(), p.ID)
		a.logf("facebook: posted %s %s as %s", p.Kind, p.Ref, id)
		return
	}
	tries := p.Tries + 1
	if fbPermanent(err) || tries >= 3 {
		_, _ = a.db.Exec(`UPDATE fb_posts SET status='failed', err=?, tries=? WHERE id=?`, clean(err.Error(), 300), tries, p.ID)
		a.logf("facebook: post %d failed for good: %v", p.ID, err)
		return
	}
	_, _ = a.db.Exec(`UPDATE fb_posts SET err=?, tries=?, due_at=? WHERE id=?`, clean(err.Error(), 300), tries, time.Now().Add(15*time.Minute).UTC().Format(time.RFC3339), p.ID)
	a.logf("facebook: post %d attempt %d failed, retrying in 15 min: %v", p.ID, tries, err)
}
