package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// The Facebook page of the console: connection state, what is queued, what
// went out, and three ways to put something in the queue (type a post, pick
// an approved event, or queue this weekend's list now). The automatic
// switches live with the other settings so reset-to-defaults covers them.

type fbPageData struct {
	On                      bool
	PageID, Version         string
	EventsOn, WeekendOn     bool
	Delay, WeekendHour      int
	WeekendDay, NextWeekend string
	LastWeekend             string
	WeekendPreview          string
	WeekendRef              string
	WeekendQueued           bool
	Queue, History          []fbPost
	Candidates              []Event
	Posted                  map[string]bool
	Now                     string
	Counts                  map[string]int
}

func (a *App) facebookPage(w http.ResponseWriter, r *http.Request) {
	n := time.Now().In(a.cfg.TZ)
	d := fbPageData{On: a.fbEnabled(), Version: a.cfg.FBVersion, PageID: a.cfg.FBPageID,
		EventsOn: a.settingBool("fb_events_on"), WeekendOn: a.settingBool("fb_weekend_on"),
		Delay: a.settingInt("fb_events_delay"), WeekendHour: a.settingInt("fb_weekend_hour"),
		WeekendDay: time.Weekday(a.settingInt("fb_weekend_day")).String(), LastWeekend: a.metaGet("last:fb:weekend"),
		Now: n.Format("2006-01-02T15:04"), Posted: map[string]bool{}, Counts: map[string]int{}}
	d.WeekendPreview, d.WeekendRef = a.fbWeekendText(n)
	d.WeekendQueued = a.count(`SELECT COUNT(*) FROM fb_posts WHERE kind='weekend' AND ref=?`, d.WeekendRef) > 0
	// next weekend post slot
	day := time.Weekday(a.settingInt("fb_weekend_day"))
	next := time.Date(n.Year(), n.Month(), n.Day(), d.WeekendHour, 0, 0, 0, a.cfg.TZ)
	for next.Weekday() != day || !next.After(n) {
		next = next.AddDate(0, 0, 1)
	}
	d.NextWeekend = next.Format("Mon 2 Jan 15:04")
	d.Queue = a.fbPosts(`status='queued'`, 100)
	d.History = a.fbPosts(`status<>'queued'`, 50)
	for _, st := range []string{"queued", "posted", "failed", "cancelled"} {
		d.Counts[st] = a.count(`SELECT COUNT(*) FROM fb_posts WHERE status=?`, st)
	}
	today := n.Format("2006-01-02")
	if evs, err := a.approvedEvents(time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, a.cfg.TZ), 60); err == nil {
		for _, e := range evs {
			if e.Date >= today || e.EndDate >= today {
				d.Candidates = append(d.Candidates, e)
			}
		}
	}
	rows, err := a.db.Query(`SELECT ref FROM fb_posts WHERE kind='event' AND status IN ('queued','posted')`)
	if err == nil {
		for rows.Next() {
			var ref string
			if rows.Scan(&ref) == nil {
				d.Posted[ref] = true
			}
		}
		rows.Close()
	}
	a.renderConsole(w, r, "p_facebook", "Facebook", d)
}

// facebookAction handles the page's POSTs. Every one is audited by the
// caller through the normal /admin/do path.
func (a *App) facebookAction(r *http.Request, action, id string) (string, error) {
	f := r.PostForm
	if !a.fbEnabled() && action != "fb-cancel" {
		return "", fmt.Errorf("Facebook posting is not configured (HS_FB_PAGE_ID and HS_FB_PAGE_TOKEN)")
	}
	switch action {
	case "fb-check":
		name, link, err := a.fb.pageInfo()
		if err != nil {
			return "", fmt.Errorf("the page did not answer: %v", err)
		}
		a.audit(r, "facebook.check", a.cfg.FBPageID, name)
		return fmt.Sprintf("Connected: the token is for %q (%s).", name, link), nil
	case "fb-compose":
		msg := strings.TrimSpace(strings.ReplaceAll(f.Get("message"), "\r\n", "\n"))
		if len(msg) < 10 {
			return "", fmt.Errorf("write at least a sentence")
		}
		link := strings.TrimSpace(f.Get("link"))
		if link != "" {
			u, ok := validURL(link)
			if !ok {
				return "", fmt.Errorf("the link must be a full http(s) address")
			}
			link = u
		}
		due := time.Now()
		if w := strings.TrimSpace(f.Get("when")); w != "" {
			t, err := time.ParseInLocation("2006-01-02T15:04", w, a.cfg.TZ)
			if err != nil {
				return "", fmt.Errorf("the time must look like 2026-09-06T17:00")
			}
			if t.After(time.Now().AddDate(0, 0, 60)) {
				return "", fmt.Errorf("schedule at most 60 days ahead")
			}
			if t.After(time.Now()) {
				due = t
			}
		}
		if _, err := a.fbQueue("manual", "", msg, link, due); err != nil {
			return "", err
		}
		a.audit(r, "facebook.compose", "", clean(msg, 80))
		if due.After(time.Now().Add(time.Minute)) {
			return "Post scheduled for " + due.In(a.cfg.TZ).Format("Mon 2 Jan 15:04") + ".", nil
		}
		return "Post queued; it goes out within a minute.", nil
	case "fb-event":
		evs, err := a.queryEvents(`id = ? AND status = 'approved'`, id)
		if err != nil || len(evs) != 1 {
			return "", fmt.Errorf("that event is not approved or does not exist")
		}
		e := evs[0]
		ok, err := a.fbQueue("event", e.ID, a.fbEventText(e), a.eventURL(e), time.Now())
		if err != nil {
			return "", err
		}
		if !ok {
			return "That event has already been queued or posted; cancel or wait for it below.", nil
		}
		a.audit(r, "facebook.event", e.ID, "")
		return fmt.Sprintf("Event %q queued; it goes out within a minute.", e.Title), nil
	case "fb-weekend":
		text, ref := a.fbWeekendText(time.Now().In(a.cfg.TZ))
		if text == "" {
			return "", fmt.Errorf("there is nothing approved for this weekend, so there is nothing to post")
		}
		ok, err := a.fbQueue("weekend", ref, text, a.cfg.SiteURL+"/events.html", time.Now())
		if err != nil {
			return "", err
		}
		if !ok {
			return "This weekend's list has already been queued or posted.", nil
		}
		a.audit(r, "facebook.weekend", ref, "")
		return "Weekend list queued; it goes out within a minute.", nil
	case "fb-cancel":
		res, err := a.db.Exec(`UPDATE fb_posts SET status='cancelled' WHERE id=? AND status='queued'`, id)
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return "That post was not in the queue.", nil
		}
		a.audit(r, "facebook.cancel", id, "")
		return "Post cancelled.", nil
	case "fb-retry", "fb-now":
		res, err := a.db.Exec(`UPDATE fb_posts SET status='queued', due_at=?, tries=0, err='' WHERE id=? AND status IN ('queued','failed','cancelled')`, now(), id)
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return "That post is not one that can be re-queued.", nil
		}
		a.audit(r, "facebook.requeue", id, "")
		return "Post goes out within a minute.", nil
	}
	return "", fmt.Errorf("unknown action")
}
