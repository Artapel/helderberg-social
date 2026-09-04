package main

import (
	"context"
	"fmt"
	"time"
)

// scheduler runs the clock-driven jobs. Each job records the local date it
// last ran in the meta table, so a restart never sends a digest twice and a
// missed minute is caught up within the same hour.
func (a *App) scheduler(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	watch := time.NewTicker(a.cfg.WatchInterval)
	defer watch.Stop()
	first := time.After(2 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return
		case <-first:
			a.runWatch("startup")
		case <-watch.C:
			a.runWatch("scheduled")
		case <-tick.C:
			n := time.Now().In(a.cfg.TZ)
			day := n.Format("2006-01-02")
			if n.Hour() == a.cfg.DigestHour && a.metaGet("last:digest:daily") != day {
				_ = a.metaSet("last:digest:daily", day)
				if c, err := a.runDigest("daily", false); err != nil {
					a.logf("daily digest: %v", err)
				} else {
					a.logf("daily digest sent to %d subscribers", c)
				}
			}
			if n.Hour() == a.cfg.DigestHour && n.Weekday() == a.cfg.WeeklyDay && a.metaGet("last:digest:weekly") != day {
				_ = a.metaSet("last:digest:weekly", day)
				if c, err := a.runDigest("weekly", false); err != nil {
					a.logf("weekly digest: %v", err)
				} else {
					a.logf("weekly digest sent to %d subscribers", c)
				}
			}
			if n.Hour() == 3 && a.metaGet("last:housekeeping") != day {
				_ = a.metaSet("last:housekeeping", day)
				a.housekeeping()
			}
		}
	}
}

// runDigest mails every confirmed subscriber on the given frequency the
// approved events inside their horizon. Subscribers with nothing to read get
// nothing: an empty digest trains people to ignore the real ones.
// preview=true sends one sample (7-day horizon, all towns) to the admin only.
func (a *App) runDigest(freq string, preview bool) (int, error) {
	today := time.Now().In(a.cfg.TZ)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, a.cfg.TZ)
	var subs []Subscriber
	if preview {
		subs = []Subscriber{{ID: 0, Email: a.cfg.AdminEmail, Frequency: freq, Horizon: 7}}
	} else {
		var err error
		subs, err = a.subscribers(`confirmed_at IS NOT NULL AND frequency = ?`, freq)
		if err != nil {
			return 0, err
		}
	}
	all, err := a.approvedEvents(today, 30)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, s := range subs {
		evs := filterEvents(all, today, s)
		if len(evs) == 0 {
			continue
		}
		unsub := a.cfg.APIURL + "/api/unsubscribe?t=" + a.sign("unsub", fmt.Sprint(s.ID), 365*24*time.Hour)
		subject := fmt.Sprintf("What's on in the Helderberg: next %d days", s.Horizon)
		if preview {
			subject = "[preview] " + subject
		}
		m := Message{To: s.Email, Kind: "digest-" + freq, Subject: subject,
			Text: a.textDigest(evs, s, unsub), HTML: a.htmlDigest(evs, s, unsub),
			Headers: map[string]string{
				"List-Unsubscribe":      "<" + unsub + ">",
				"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
				"Precedence":            "bulk",
			}}
		if err := a.send(m); err != nil {
			continue
		}
		sent++
		if !preview {
			_, _ = a.db.Exec(`UPDATE subscribers SET last_sent_at = ? WHERE id = ?`, now(), s.ID)
			time.Sleep(400 * time.Millisecond) // be gentle with the relay
		}
	}
	if preview && sent == 0 {
		return 0, fmt.Errorf("no approved events in the next 7 days, nothing to preview")
	}
	return sent, nil
}

func filterEvents(all []Event, today time.Time, s Subscriber) []Event {
	limit := today.AddDate(0, 0, s.Horizon).Format("2006-01-02")
	tw := set(s.Towns...)
	ct := set(s.Categories...)
	var out []Event
	for _, e := range all {
		if e.Date > limit {
			continue
		}
		if len(s.Towns) > 0 && !tw[e.Town] {
			continue
		}
		if len(s.Categories) > 0 && !ct[e.Category] {
			continue
		}
		out = append(out, e)
	}
	return out
}
