package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Moderation. Nothing a member of the public submits is ever shown on the
// site until the admin approves it. Links in notification emails land in
// the console, which needs a signed-in session (see auth.go): a forwarded
// email can never approve anything on its own.

func (a *App) moderateURL(kind, id, action string) string {
	return a.cfg.APIURL + "/admin/moderate?t=" + a.sign("moderate", kind+"|"+id+"|"+action, 30*24*time.Hour)
}

func (a *App) adminURL() string { return a.cfg.APIURL + "/admin/queue" }

// notify sends a moderation email to the admin and any extra addresses.
func (a *App) notify(kind, subject, body string) {
	for _, to := range a.notifyList() {
		_ = a.send(Message{To: to, Kind: kind, Subject: subject, Text: body})
	}
}

func (a *App) notifyAdminEvent(id string) {
	evs, err := a.queryEvents(`id = ?`, id)
	if err != nil || len(evs) != 1 {
		return
	}
	e := evs[0]
	body := a.textEvent(e) + "\n\nApprove: " + a.moderateURL("event", e.ID, "approve") +
		"\nReject:  " + a.moderateURL("event", e.ID, "reject") + "\n\nQueue: " + a.adminURL() + "\n"
	a.notify("admin-event", "[HS] Event to review: "+e.Title, body)
}

type listingSub struct {
	ID                             int64
	Kind, Existing, Name, Category string
	Town, Schedule, Summary, Cost  string
	Website, Audience, Submitter   string
	Status, CreatedAt              string
}

func (a *App) listingSubs(where string, args ...any) ([]listingSub, error) {
	rows, err := a.db.Query(`SELECT id, kind, existing_id, name, category, town, schedule, summary, cost, website, audience, submitter_name, status, created_at FROM listing_submissions WHERE `+where+` ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []listingSub
	for rows.Next() {
		var s listingSub
		if err := rows.Scan(&s.ID, &s.Kind, &s.Existing, &s.Name, &s.Category, &s.Town, &s.Schedule, &s.Summary, &s.Cost, &s.Website, &s.Audience, &s.Submitter, &s.Status, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// dataJS renders the block the admin pastes into data/data.js. Listings stay
// in the repo on purpose: they are curated content, not user state.
func (s listingSub) DataJS() string {
	var aud []string
	_ = json.Unmarshal([]byte(s.Audience), &aud)
	q := func(v string) string { b, _ := json.Marshal(v); return string(b) }
	lines := []string{
		"    {",
		fmt.Sprintf("      id: %s,", q(slugify(s.Name))),
		fmt.Sprintf("      type: %s,", q(s.Kind)),
		fmt.Sprintf("      name: %s,", q(s.Name)),
		fmt.Sprintf("      category: %s,", q(s.Category)),
		fmt.Sprintf("      town: %s,", q(s.Town)),
		fmt.Sprintf("      summary: %s,", q(s.Summary)),
	}
	if s.Schedule != "" {
		lines = append(lines, fmt.Sprintf("      schedule: { days: [], text: %s },", q(s.Schedule)))
	}
	lines = append(lines,
		fmt.Sprintf("      cost: %s,", q(s.Cost)),
		fmt.Sprintf("      audience: %s,", jsonList(aud)),
		"      tags: [],",
		fmt.Sprintf("      website: %s,", q(s.Website)),
		fmt.Sprintf("      source: %s,", q(s.Website)),
		"      verified: false",
		"    },")
	return strings.Join(lines, "\n")
}

func (a *App) notifyAdminListing(id string) {
	subs, err := a.listingSubs(`id = ?`, id)
	if err != nil || len(subs) != 1 {
		return
	}
	s := subs[0]
	kind := s.Kind
	if kind == "update" {
		kind = "update to " + s.Existing
	}
	body := fmt.Sprintf("Listing submission #%d (%s)\nName: %s\nCategory: %s · Town: %s\nWhen: %s\nCost: %s\nWebsite: %s\nAudience: %s\nSubmitted by: %s\n\n%s\n\nBlock for data/data.js:\n\n%s\n\nMark handled: %s\nReject: %s\n\nQueue: %s\n",
		s.ID, kind, s.Name, s.Category, s.Town, s.Schedule, s.Cost, s.Website, s.Audience, s.Submitter, s.Summary, s.DataJS(),
		a.moderateURL("listing", fmt.Sprint(s.ID), "accept"), a.moderateURL("listing", fmt.Sprint(s.ID), "reject"), a.adminURL())
	a.notify("admin-listing", "[HS] Listing to review: "+s.Name, body)
}

// decide applies a moderation action. Returns a human sentence for the page.
func (a *App) decide(kind, id, action string) (string, error) {
	switch kind + ":" + action {
	case "event:approve", "event:reject":
		status := "approved"
		if action == "reject" {
			status = "rejected"
		}
		res, err := a.db.Exec(`UPDATE events SET status=?, decided_at=? WHERE id=? AND status='pending_review'`, status, now(), id)
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return "That event was already decided or does not exist.", nil
		}
		if status == "approved" {
			a.fbQueueEvent(id)
		} else {
			a.fbCancelRef("event", id)
		}
		a.notifyMemberDecision(id, status)
		return fmt.Sprintf("Event %q is now %s.", id, status), nil
	case "post:approve", "post:reject":
		status := "approved"
		if action == "reject" {
			status = "rejected"
		}
		res, err := a.db.Exec(`UPDATE posts SET status=?, decided_at=? WHERE id=? AND status='pending_review'`, status, now(), id)
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return "That post was already decided or does not exist.", nil
		}
		a.notifyPostDecision(id, status)
		return fmt.Sprintf("Post %q is now %s.", id, status), nil
	case "listing:accept", "listing:reject":
		status := "accepted"
		if action == "reject" {
			status = "rejected"
		}
		res, err := a.db.Exec(`UPDATE listing_submissions SET status=?, decided_at=? WHERE id=? AND status='pending_review'`, status, now(), id)
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return "That submission was already decided or does not exist.", nil
		}
		return fmt.Sprintf("Listing submission #%s marked %s.", id, status), nil
	}
	return "", fmt.Errorf("unknown action")
}

// page is the plain HTML used for link landings and refusals outside the console.
func (a *App) page(w http.ResponseWriter, status int, title, body, queue string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = a.tmpl.ExecuteTemplate(w, "page", map[string]any{"Title": title, "Body": body, "Queue": queue, "Site": a.cfg.SiteURL})
}

// legacyAdminLink answers links from emails sent before the console existed.
func (a *App) legacyAdminLink(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/login?msg="+url.QueryEscape("That link is from the old moderation flow. Sign in and use the queue instead."), http.StatusSeeOther)
}
