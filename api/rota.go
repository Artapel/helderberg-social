package main

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The groups rota for a runner. `scripts/fb-groups/post.mjs` on the admin's
// own machine asks for the day's due groups with their post text, drives
// the page's browser session through the groups, and reports what happened
// to each one. The API stays the planner and the record; the browser side
// is the runner's. Both doors need the bearer token from HS_FB_ROTA_TOKEN
// and are absent when it is unset.
//
// Outcomes a runner may report:
//   posted   the post went up (or is awaiting the group's admins): counts as
//            a post and books the next one a cadence away.
//   retry    the page could not post today but should try again soon: the
//            group's "request to participate is pending approval", the
//            composer never appeared. Booked again in rotaRetryDays.
//   blocked  the page is not a member, was removed, or the group is gone:
//            the group is switched off with the reason, for the admin to
//            look at on the console.
//   failed   something broke in the runner (a timeout, a changed page);
//            the group moves one day so a single stuck group cannot hog the
//            batch, and the audit log keeps the note.

const (
	rotaRetryDays = 3
	rotaMaxLimit  = 100
)

var rotaOutcomes = map[string]bool{"posted": true, "retry": true, "blocked": true, "failed": true}

func (a *App) rotaEnabled() bool { return a.cfg.RotaToken != "" }

// rotaAuth accepts "Authorization: Bearer <token>" and nothing else; a
// wrong or missing token is a 404 so the door is invisible to a scanner.
func (a *App) rotaAuth(w http.ResponseWriter, r *http.Request) bool {
	if !a.rotaEnabled() {
		http.NotFound(w, r)
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(a.cfg.RotaToken)) != 1 {
		http.NotFound(w, r)
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	return true
}

type rotaGroup struct {
	ID      int64  `json:"id"`
	FBID    string `json:"fb_id"`
	URL     string `json:"url"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Town    string `json:"town"`
	Note    string `json:"note"`
	Posts   int    `json:"posts"`
	NextDue string `json:"next_due"`
	Text    string `json:"text"`
}

// GET /api/fb/rota?limit=N — the due groups with their text. The limit
// defaults to the console's groups-per-day setting; "all" lifts it (capped
// at rotaMaxLimit) for a catch-up run.
func (a *App) rotaGet(w http.ResponseWriter, r *http.Request) {
	if !a.rotaAuth(w, r) {
		return
	}
	n := time.Now().In(a.cfg.TZ)
	today := n.Format("2006-01-02")
	limit := a.settingInt("fb_groups_per_day")
	if v := r.URL.Query().Get("limit"); v == "all" {
		limit = rotaMaxLimit
	} else if v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			limit = min(i, rotaMaxLimit)
		}
	}
	total := a.count(`SELECT COUNT(*) FROM fb_groups WHERE enabled = 1 AND (next_due = '' OR next_due <= ?)`, today)
	out := []rotaGroup{}
	for _, g := range a.dueGroups(today, limit) {
		out = append(out, rotaGroup{ID: g.ID, FBID: g.FBID, URL: g.URL(), Name: g.Name, Kind: g.Kind, Town: g.Town, Note: g.Note,
			Posts: g.Posts, NextDue: g.NextDue, Text: a.groupText(g, n)})
	}
	a.json(w, 200, map[string]any{"ok": true, "today": today, "per_day": a.settingInt("fb_groups_per_day"), "due_total": total, "groups": out})
}

// POST /api/fb/rota/result {"id":12,"outcome":"posted","note":"…"} — one
// group's result, recorded as if the admin had pressed the console button.
func (a *App) rotaResult(w http.ResponseWriter, r *http.Request) {
	if !a.rotaAuth(w, r) {
		return
	}
	var in struct {
		ID      int64  `json:"id"`
		Outcome string `json:"outcome"`
		Note    string `json:"note"`
	}
	if err := readJSON(r, &in); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	if !rotaOutcomes[in.Outcome] {
		a.fail(w, 400, "outcome must be posted, retry, blocked or failed")
		return
	}
	id := strconv.FormatInt(in.ID, 10)
	gs := a.groups(`id = ?`, id)
	if len(gs) != 1 {
		a.fail(w, 404, "no such group")
		return
	}
	g := gs[0]
	n := time.Now().In(a.cfg.TZ)
	note := clean(in.Note, 200)
	var err error
	switch in.Outcome {
	case "posted":
		g, err = a.markPosted(id, n)
		a.audit(r, "fb.group_posted", g.Name, "by the runner "+note)
	case "retry":
		next := n.AddDate(0, 0, rotaRetryDays).Format("2006-01-02")
		_, err = a.db.Exec(`UPDATE fb_groups SET next_due = ? WHERE id = ?`, next, g.ID)
		g.NextDue = next
		a.audit(r, "fb.group_retry", g.Name, note)
	case "blocked":
		if note == "" {
			note = "runner: the page cannot post here"
		}
		_, err = a.db.Exec(`UPDATE fb_groups SET enabled = 0, skip_reason = ? WHERE id = ?`, note, g.ID)
		g.Enabled, g.SkipReason = false, note
		a.audit(r, "fb.group_blocked", g.Name, note)
	case "failed":
		next := n.AddDate(0, 0, 1).Format("2006-01-02")
		_, err = a.db.Exec(`UPDATE fb_groups SET next_due = ? WHERE id = ?`, next, g.ID)
		g.NextDue = next
		a.audit(r, "fb.group_failed", g.Name, note)
	}
	if err != nil {
		a.fail(w, 500, "could not record that")
		return
	}
	a.json(w, 200, map[string]any{"ok": true, "id": g.ID, "name": g.Name, "posts": g.Posts, "next_due": g.NextDue, "enabled": g.Enabled})
}
