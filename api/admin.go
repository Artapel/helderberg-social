package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"
)

// Moderation. Nothing a member of the public submits is ever shown on the
// site until the admin approves it. The admin authenticates with a signed
// link sent to the configured address; there is no password anywhere.

func (a *App) moderateURL(kind, id, action string) string {
	return a.cfg.APIURL + "/api/moderate?t=" + a.sign("moderate", kind+"|"+id+"|"+action, 30*24*time.Hour)
}

func (a *App) adminURL() string {
	return a.cfg.APIURL + "/api/admin?t=" + a.sign("admin", "session", 12*time.Hour)
}

func (a *App) notifyAdminEvent(id string) {
	evs, err := a.queryEvents(`id = ?`, id)
	if err != nil || len(evs) != 1 {
		return
	}
	e := evs[0]
	body := a.textEvent(e) + "\n\nApprove: " + a.moderateURL("event", e.ID, "approve") +
		"\nReject:  " + a.moderateURL("event", e.ID, "reject") + "\n\nQueue: " + a.adminURL() + "\n"
	_ = a.send(Message{To: a.cfg.AdminEmail, Kind: "admin-event", Subject: "[HS] Event to review: " + e.Title, Text: body})
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
	_ = a.send(Message{To: a.cfg.AdminEmail, Kind: "admin-listing", Subject: "[HS] Listing to review: " + s.Name, Text: body})
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
		return fmt.Sprintf("Event %q is now %s.", id, status), nil
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

func (a *App) moderateLink(w http.ResponseWriter, r *http.Request) {
	p, err := a.consume(r.URL.Query().Get("t"), "moderate")
	if err != nil {
		a.page(w, 400, "Link invalid", "This moderation link is invalid, expired or already used.", "")
		return
	}
	parts := strings.SplitN(p.Subject, "|", 3)
	if len(parts) != 3 {
		a.page(w, 400, "Link invalid", "Malformed link.", "")
		return
	}
	msg, err := a.decide(parts[0], parts[1], parts[2])
	if err != nil {
		a.logf("moderate: %v", err)
		a.page(w, 500, "Error", "Something went wrong applying that decision.", "")
		return
	}
	a.page(w, 200, "Done", msg, a.adminURL())
}

func (a *App) adminLogin(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Email string `json:"email"`
	}
	if err := readJSON(r, &q); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	// Same answer whether or not the address matches, so nothing is learnt.
	if normEmail(q.Email) == a.cfg.AdminEmail {
		_ = a.send(Message{To: a.cfg.AdminEmail, Kind: "admin-login", Subject: "[HS] Your moderation link",
			Text: "Open the moderation queue (valid 12 hours):\n\n" + a.adminURL() + "\n\nIf you did not request this, ignore it.\n"})
	}
	a.ok(w, "If that is the admin address, a link is on its way.")
}

type queueView struct {
	Token           string
	Events          []Event
	Listings        []listingSub
	Approved        int
	SubsConfirmed   int
	SubsPending     int
	Sources         []sourceRow
	MailFailures    []mailRow
	Message         string
	Version         string
	LastDigestDaily string
	LastDigestWeek  string
	LastWatch       string
}

type mailRow struct{ Kind, SentAt, Err string }

func (a *App) adminQueue(w http.ResponseWriter, r *http.Request) {
	t := r.URL.Query().Get("t")
	if _, err := a.verify(t, "admin"); err != nil {
		a.page(w, 401, "Sign in", "This link is invalid or has expired. Request a new one from the site's moderation page.", "")
		return
	}
	a.renderQueue(w, t, r.URL.Query().Get("msg"))
}

func (a *App) renderQueue(w http.ResponseWriter, t, msg string) {
	v := queueView{Token: t, Message: clean(msg, 200), Version: a.version}
	v.Events, _ = a.queryEvents(`status = 'pending_review'`)
	v.Listings, _ = a.listingSubs(`status = 'pending_review'`)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM events WHERE status='approved'`).Scan(&v.Approved)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM subscribers WHERE confirmed_at IS NOT NULL`).Scan(&v.SubsConfirmed)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM subscribers WHERE confirmed_at IS NULL`).Scan(&v.SubsPending)
	v.Sources, _ = a.sourceRows()
	rows, err := a.db.Query(`SELECT kind, sent_at, err FROM mail_log WHERE ok = 0 ORDER BY sent_at DESC LIMIT 10`)
	if err == nil {
		for rows.Next() {
			var m mailRow
			_ = rows.Scan(&m.Kind, &m.SentAt, &m.Err)
			v.MailFailures = append(v.MailFailures, m)
		}
		rows.Close()
	}
	v.LastDigestDaily = a.metaGet("last:digest:daily")
	v.LastDigestWeek = a.metaGet("last:digest:weekly")
	v.LastWatch = a.metaGet("last:watch")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.ExecuteTemplate(w, "queue", v); err != nil {
		a.logf("queue template: %v", err)
	}
}

func (a *App) adminAct(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.fail(w, 400, "bad form")
		return
	}
	t := r.PostForm.Get("t")
	if _, err := a.verify(t, "admin"); err != nil {
		a.page(w, 401, "Sign in", "This link is invalid or has expired.", "")
		return
	}
	action := r.PostForm.Get("action")
	var msg string
	var err error
	switch action {
	case "approve", "reject":
		msg, err = a.decide("event", r.PostForm.Get("id"), action)
	case "accept", "reject-listing":
		msg, err = a.decide("listing", r.PostForm.Get("id"), strings.TrimSuffix(action, "-listing"))
	case "watch":
		msg = a.runWatch("manual")
	case "digest-preview":
		n, sendErr := a.runDigest("weekly", true)
		msg = fmt.Sprintf("Preview digest sent to the admin address (%d events).", n)
		if sendErr != nil {
			msg = "Preview failed: " + sendErr.Error()
		}
	default:
		err = fmt.Errorf("unknown action")
	}
	if err != nil {
		a.logf("adminAct %s: %v", action, err)
		msg = "Error: " + err.Error()
	}
	http.Redirect(w, r, a.cfg.APIURL+"/api/admin?t="+t+"&msg="+template.URLQueryEscaper(msg), http.StatusSeeOther)
}

// page is the plain HTML used for link landings that are not on the site.
func (a *App) page(w http.ResponseWriter, status int, title, body, queue string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = a.tmpl.ExecuteTemplate(w, "page", map[string]any{"Title": title, "Body": body, "Queue": queue, "Site": a.cfg.SiteURL})
}
