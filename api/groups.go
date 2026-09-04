package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Facebook groups. The page has joined the Helderberg's community, what's-on
// and buy-and-sell groups, and the aim is one post in each of them about
// once a month. The Graph API cannot post to groups (Meta withdrew the
// Groups API in 2024) and a script driving the page's browser session in
// dozens of groups is exactly what Facebook's automation rules forbid, so
// the posting itself is done by a person in the browser. What the console
// does is everything around that: it keeps the list, knows which groups are
// due, writes the post for each one from the approved events (with a
// different lead for a parents' group than for a buy-and-sell group), spreads
// the work out to a few groups a day so the page never looks like spam, and
// emails the admin the day's batch with the text ready to paste. "Mark as
// posted" records it and sets the next date.

//go:embed fb-groups.json
var fbGroupsJSON []byte

type fbGroupDef struct {
	FBID string `json:"fb_id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Town string `json:"town"`
	Note string `json:"note"`
	Skip string `json:"skip"`
}

// groupKinds decide which lead paragraph a group gets. "skip" is a group the
// page is in but should not post to (wrong area, off-topic, request pending).
var groupKinds = map[string]string{
	"community": "Community and local news",
	"whatson":   "What's on, events, dining",
	"market":    "Buy, sell and swap",
	"business":  "Advertising and business",
	"jobs":      "Jobs",
	"interest":  "Interest group (parents, hikers, music…)",
	"skip":      "Do not post",
}

var fbIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{2,80}$`)

const (
	groupCadenceMin = 7
	groupCadenceMax = 120
)

// seedGroups upserts the shipped list by Facebook id. Names, kinds and notes
// follow the file; the cadence, enabled flag and posting history are the
// admin's and are left alone. A group marked skip in the file is disabled
// with that reason unless the admin has since enabled it by hand.
func (a *App) seedGroups() error {
	var defs []fbGroupDef
	if err := json.Unmarshal(fbGroupsJSON, &defs); err != nil {
		return fmt.Errorf("fb-groups.json: %w", err)
	}
	for _, d := range defs {
		if !fbIDRe.MatchString(d.FBID) || strings.TrimSpace(d.Name) == "" {
			return fmt.Errorf("fb-groups.json: bad entry %q", d.FBID)
		}
		if groupKinds[d.Kind] == "" {
			return fmt.Errorf("fb-groups.json: %s has kind %q", d.FBID, d.Kind)
		}
		if d.Town != "" && !towns[d.Town] {
			return fmt.Errorf("fb-groups.json: %s has town %q", d.FBID, d.Town)
		}
		enabled, skip := 1, ""
		if d.Kind == "skip" {
			enabled, skip = 0, d.Skip
			if skip == "" {
				skip = "marked do-not-post in fb-groups.json"
			}
		}
		if _, err := a.db.Exec(`INSERT INTO fb_groups(fb_id, name, kind, town, note, enabled, skip_reason, next_due, created_at) VALUES(?,?,?,?,?,?,?,?,?)
			ON CONFLICT(fb_id) DO UPDATE SET name=excluded.name, kind=excluded.kind, town=excluded.town, note=excluded.note,
			skip_reason=CASE WHEN excluded.kind='skip' AND fb_groups.enabled=0 THEN excluded.skip_reason ELSE fb_groups.skip_reason END`,
			d.FBID, d.Name, d.Kind, d.Town, d.Note, enabled, skip, a.localDay(time.Now()), now()); err != nil {
			return err
		}
	}
	return nil
}

type fbGroup struct {
	ID           int64
	FBID         string
	Name         string
	Kind         string
	Town         string
	Note         string
	Cadence      int
	Enabled      bool
	SkipReason   string
	Posts        int
	LastPostedAt string
	NextDue      string // local date, "" = never posted, due now
}

func (g fbGroup) URL() string { return "https://www.facebook.com/groups/" + g.FBID + "/" }

// State is what the list shows: due, overdue, scheduled, or off.
func (g fbGroup) State(today string) string {
	switch {
	case !g.Enabled:
		return "off"
	case g.NextDue == "" || g.NextDue < today:
		if g.Posts == 0 {
			return "never posted"
		}
		return "overdue"
	case g.NextDue == today:
		return "due today"
	}
	return "scheduled"
}

const groupCols = `id, fb_id, name, kind, town, note, cadence_days, enabled, skip_reason, posts, last_posted_at, next_due`

func (a *App) groups(where string, args ...any) []fbGroup {
	rows, err := a.db.Query(`SELECT `+groupCols+` FROM fb_groups WHERE `+where+` ORDER BY enabled DESC, next_due, posts, name`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []fbGroup
	for rows.Next() {
		var g fbGroup
		var en int
		if rows.Scan(&g.ID, &g.FBID, &g.Name, &g.Kind, &g.Town, &g.Note, &g.Cadence, &en, &g.SkipReason, &g.Posts, &g.LastPostedAt, &g.NextDue) == nil {
			g.Enabled = en == 1
			out = append(out, g)
		}
	}
	return out
}

// dueGroups are the enabled groups whose next date is today or earlier,
// never-posted ones first, then the longest overdue, capped at the day's
// batch size so the admin gets a manageable list and the page a natural pace.
func (a *App) dueGroups(today string, limit int) []fbGroup {
	all := a.groups(`enabled = 1 AND (next_due = '' OR next_due <= ?)`, today)
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
}

// markPosted records a post by hand and books the next one a cadence away.
func (a *App) markPosted(id string, when time.Time) (fbGroup, error) {
	gs := a.groups(`id = ?`, id)
	if len(gs) != 1 {
		return fbGroup{}, fmt.Errorf("no such group")
	}
	g := gs[0]
	next := when.In(a.cfg.TZ).AddDate(0, 0, g.Cadence)
	_, err := a.db.Exec(`UPDATE fb_groups SET posts = posts + 1, last_posted_at = ?, next_due = ? WHERE id = ?`,
		when.UTC().Format(time.RFC3339), next.Format("2006-01-02"), g.ID)
	g.Posts++
	g.NextDue = next.Format("2006-01-02")
	return g, err
}

// saveGroup adds or edits a group from the console form.
func (a *App) saveGroup(f url.Values) (string, error) {
	fbid := strings.TrimSpace(f.Get("fb_id"))
	// Accept a pasted group URL as well as a bare id.
	if m := regexp.MustCompile(`facebook\.com/groups/([A-Za-z0-9._-]+)`).FindStringSubmatch(fbid); m != nil {
		fbid = m[1]
	}
	name := clean(f.Get("name"), 120)
	kind, town, note := f.Get("kind"), f.Get("town"), clean(f.Get("note"), 200)
	cadence, _ := strconv.Atoi(strings.TrimSpace(f.Get("cadence")))
	if strings.TrimSpace(f.Get("cadence")) == "" {
		cadence = 30
	}
	switch {
	case !fbIDRe.MatchString(fbid):
		return "", fmt.Errorf("the group needs its Facebook id or address (facebook.com/groups/…)")
	case len(name) < 2:
		return "", fmt.Errorf("give the group its name")
	case groupKinds[kind] == "":
		return "", fmt.Errorf("choose a kind")
	case town != "" && !towns[town]:
		return "", fmt.Errorf("choose a valid town")
	case cadence < groupCadenceMin || cadence > groupCadenceMax:
		return "", fmt.Errorf("post every %d-%d days", groupCadenceMin, groupCadenceMax)
	}
	enabled := 1
	if kind == "skip" {
		enabled = 0
	}
	if id := f.Get("id"); id != "" {
		_, err := a.db.Exec(`UPDATE fb_groups SET fb_id=?, name=?, kind=?, town=?, note=?, cadence_days=?, enabled=CASE WHEN ?=0 THEN 0 ELSE enabled END WHERE id=?`,
			fbid, name, kind, town, note, cadence, enabled, id)
		return "Group saved.", err
	}
	_, err := a.db.Exec(`INSERT INTO fb_groups(fb_id, name, kind, town, note, cadence_days, enabled, next_due, created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		fbid, name, kind, town, note, cadence, enabled, a.localDay(time.Now()), now())
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return "", fmt.Errorf("that group is already on the list")
	}
	return "Group added; it is due now.", err
}

/* ---------- the post ---------- */

// groupLead is the opening paragraph by kind of group. It says who we are and
// why this post belongs here, because that is what a group admin checks.
func groupLead(kind, name string) string {
	switch kind {
	case "market", "business":
		return "Not a sale, a free local resource: Helderberg Social lists what's on across Somerset West, Strand and Gordon's Bay, and clubs, markets and venues can add their own events at no charge."
	case "jobs":
		return "Not a job ad, but useful if you are new to the area or between things: Helderberg Social is a free guide to the clubs, markets and events across Somerset West, Strand and Gordon's Bay."
	case "interest":
		return "For everyone in " + name + ": Helderberg Social is a free, volunteer-run guide to what's on across Somerset West, Strand and Gordon's Bay."
	case "whatson":
		return "A hand for the admins here: Helderberg Social keeps a free, checked list of what's on across Somerset West, Strand and Gordon's Bay, updated every week."
	}
	return "Helderberg Social is a free, volunteer-run guide to what's on across Somerset West, Strand and Gordon's Bay: markets, trails, clubs, live music, wine estates, church and community events, all checked before they go up."
}

// groupPick chooses which categories to list first for a group, from its
// kind and its note ("lead with family and kids events").
func groupPick(g fbGroup) []string {
	n := strings.ToLower(g.Note + " " + g.Name)
	var pick []string
	for _, kw := range []struct{ word, cat string }{
		{"family", "family"}, {"kids", "family"}, {"mamma", "family"}, {"mom", "family"},
		{"hik", "hiking"}, {"nature", "nature"}, {"walk", "hiking"},
		{"music", "music"}, {"musiek", "music"}, {"entertain", "music"},
		{"market", "markets"}, {"crafter", "markets"}, {"produce", "markets"},
		{"wine", "wine"}, {"dining", "wine"}, {"food", "wine"}, {"restaurant", "wine"},
		{"social", "community"}, {"single", "community"},
	} {
		if strings.Contains(n, kw.word) {
			pick = append(pick, kw.cat)
		}
	}
	return pick
}

// groupText writes the post for one group: lead, up to eight upcoming
// approved events (the group's own town and favourite categories first),
// and the site links. The text is meant to be pasted as-is.
func (a *App) groupText(g fbGroup, n time.Time) string {
	var b strings.Builder
	b.WriteString(groupLead(g.Kind, g.Name))
	from := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, a.cfg.TZ)
	evs, _ := a.approvedEvents(from, 30)
	if len(evs) > 0 {
		pick := groupPick(g)
		score := func(e Event) int {
			s := 0
			if g.Town != "" && e.Town == g.Town {
				s += 2
			}
			for _, c := range pick {
				if e.Category == c {
					s += 3
				}
			}
			return s
		}
		// Stable partial sort: best matches first, then by date as approvedEvents gave them.
		sorted := append([]Event(nil), evs...)
		for i := 1; i < len(sorted); i++ {
			for j := i; j > 0 && score(sorted[j]) > score(sorted[j-1]); j-- {
				sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
			}
		}
		const max = 8
		fmt.Fprintf(&b, "\n\nComing up in the next month:\n")
		for i, e := range sorted {
			if i == max {
				fmt.Fprintf(&b, "…and %d more on the site.\n", len(sorted)-max)
				break
			}
			line := "• " + fmtDate(e.Date) + ": " + e.Title + " (" + townName(e.Town) + ")"
			if e.Time != "" {
				line += " " + e.Time
			}
			if e.Cost != "" && e.Cost != "varies" {
				line += " · " + e.Cost
			}
			b.WriteString(line + "\n")
		}
	} else {
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\nEverything, with details and a map: %s/events.html\nRun a club, market or venue? Add your event free: %s/submit.html?kind=event\nWeekly what's-on email: %s/subscribe.html", a.cfg.SiteURL, a.cfg.SiteURL, a.cfg.SiteURL)
	return b.String()
}

/* ---------- reminder ---------- */

// groupsTick runs from the scheduler: at the reminder hour, when something
// is due, it emails the admin the day's batch once. Nothing is posted.
func (a *App) groupsTick(n time.Time) {
	if !a.settingBool("fb_groups_remind") || n.Hour() != a.settingInt("fb_groups_hour") {
		return
	}
	day := n.Format("2006-01-02")
	if a.metaGet("last:fb:groups:remind") == day {
		return
	}
	_ = a.metaSet("last:fb:groups:remind", day)
	if n.Weekday() == time.Sunday {
		return // nobody wants the Sunday batch
	}
	if sent, err := a.sendGroupsReminder(n); err != nil {
		a.logf("facebook groups: reminder: %v", err)
	} else if sent > 0 {
		a.logf("facebook groups: reminder for %d groups sent", sent)
	}
}

func (a *App) sendGroupsReminder(n time.Time) (int, error) {
	due := a.dueGroups(n.Format("2006-01-02"), a.settingInt("fb_groups_per_day"))
	if len(due) == 0 {
		return 0, nil
	}
	var b strings.Builder
	total := a.count(`SELECT COUNT(*) FROM fb_groups WHERE enabled = 1 AND (next_due = '' OR next_due <= ?)`, n.Format("2006-01-02"))
	fmt.Fprintf(&b, "%d of %d due groups for today. Open each link, post as the page, then press \"Mark posted\" in the console:\n%s/admin/facebook/groups\n\n", len(due), total, a.cfg.APIURL)
	for i, g := range due {
		fmt.Fprintf(&b, "==== %d. %s (%s) ====\n%s\n", i+1, g.Name, groupKinds[g.Kind], g.URL())
		if g.Note != "" {
			fmt.Fprintf(&b, "Note: %s\n", g.Note)
		}
		fmt.Fprintf(&b, "\n%s\n\n", a.groupText(g, n))
	}
	b.WriteString("Post as the page (switch the profile in the composer). Read the group's rules first; a group that forbids promotion gets a shorter, friendlier version or nothing.\n")
	for _, to := range a.notifyList() {
		if err := a.send(Message{To: to, Kind: "fbgroups", Subject: fmt.Sprintf("[HS] Facebook groups: %d to post today", len(due)), Text: b.String()}); err != nil {
			return 0, err
		}
	}
	return len(due), nil
}

/* ---------- console ---------- */

type groupsPageData struct {
	Today                      string
	Due, Rest                  []fbGroup
	DueTotal, Enabled, Skipped int
	Posted30, Posted           int
	PerDay, Hour               int
	RemindOn                   bool
	LastRemind                 string
	Kinds                      map[string]string
	KindOrder, Towns           []string
	Preview                    string
	PreviewFor                 fbGroup
	Texts                      map[int64]string
	CadenceMin, CadenceMax     int
}

func (a *App) groupsPage(w http.ResponseWriter, r *http.Request) {
	n := time.Now().In(a.cfg.TZ)
	today := n.Format("2006-01-02")
	d := groupsPageData{Today: today, Kinds: groupKinds, Towns: sortedKeys(towns), PerDay: a.settingInt("fb_groups_per_day"), Hour: a.settingInt("fb_groups_hour"),
		RemindOn: a.settingBool("fb_groups_remind"), LastRemind: a.metaGet("last:fb:groups:remind"), Texts: map[int64]string{},
		CadenceMin: groupCadenceMin, CadenceMax: groupCadenceMax}
	d.KindOrder = []string{"community", "whatson", "market", "business", "jobs", "interest", "skip"}
	d.Due = a.dueGroups(today, d.PerDay)
	d.DueTotal = a.count(`SELECT COUNT(*) FROM fb_groups WHERE enabled = 1 AND (next_due = '' OR next_due <= ?)`, today)
	d.Enabled = a.count(`SELECT COUNT(*) FROM fb_groups WHERE enabled = 1`)
	d.Skipped = a.count(`SELECT COUNT(*) FROM fb_groups WHERE enabled = 0`)
	d.Posted = a.count(`SELECT COALESCE(SUM(posts),0) FROM fb_groups`)
	d.Posted30 = a.count(`SELECT COUNT(*) FROM fb_groups WHERE last_posted_at >= ?`, n.AddDate(0, 0, -30).UTC().Format(time.RFC3339))
	dueIDs := map[int64]bool{}
	for _, g := range d.Due {
		dueIDs[g.ID] = true
		d.Texts[g.ID] = a.groupText(g, n)
	}
	for _, g := range a.groups(`1 = 1`) {
		if !dueIDs[g.ID] {
			d.Rest = append(d.Rest, g)
		}
	}
	if id := r.URL.Query().Get("preview"); id != "" {
		if gs := a.groups(`id = ?`, id); len(gs) == 1 {
			d.PreviewFor = gs[0]
			d.Preview = a.groupText(gs[0], n)
		}
	}
	a.renderConsole(w, r, "p_fb_groups", "Facebook groups", d)
}

// groupsAction handles the page's POSTs; the caller audits them.
func (a *App) groupsAction(r *http.Request, action, id string) (string, error) {
	f := r.PostForm
	switch action {
	case "grp-posted":
		when := time.Now()
		if w := strings.TrimSpace(f.Get("when")); w != "" {
			t, err := time.ParseInLocation("2006-01-02", w, a.cfg.TZ)
			if err != nil || t.After(time.Now()) {
				return "", fmt.Errorf("the date must be today or earlier, like 2026-09-04")
			}
			when = t.Add(12 * time.Hour)
		}
		g, err := a.markPosted(id, when)
		if err != nil {
			return "", err
		}
		a.audit(r, "fbgroup.posted", g.FBID, g.Name)
		return fmt.Sprintf("Marked %q as posted; next due %s.", g.Name, fmtDate(g.NextDue)), nil
	case "grp-save":
		msg, err := a.saveGroup(f)
		a.audit(r, "fbgroup.save", f.Get("fb_id"), msg)
		return msg, err
	case "grp-skip":
		reason := clean(f.Get("reason"), 200)
		if reason == "" {
			reason = "switched off in the console"
		}
		if _, err := a.db.Exec(`UPDATE fb_groups SET enabled = 0, skip_reason = ? WHERE id = ?`, reason, id); err != nil {
			return "", err
		}
		a.audit(r, "fbgroup.skip", id, reason)
		return "Group switched off; it will not come up as due.", nil
	case "grp-enable":
		if _, err := a.db.Exec(`UPDATE fb_groups SET enabled = 1, skip_reason = '', kind = CASE WHEN kind = 'skip' THEN 'community' ELSE kind END, next_due = CASE WHEN next_due = '' THEN ? ELSE next_due END WHERE id = ?`, a.localDay(time.Now()), id); err != nil {
			return "", err
		}
		a.audit(r, "fbgroup.enable", id, "")
		return "Group switched on.", nil
	case "grp-defer":
		days, _ := strconv.Atoi(f.Get("days"))
		if days < 1 || days > groupCadenceMax {
			days = 7
		}
		next := time.Now().In(a.cfg.TZ).AddDate(0, 0, days).Format("2006-01-02")
		if _, err := a.db.Exec(`UPDATE fb_groups SET next_due = ? WHERE id = ?`, next, id); err != nil {
			return "", err
		}
		a.audit(r, "fbgroup.defer", id, next)
		return "Pushed out to " + fmtDate(next) + ".", nil
	case "grp-delete":
		if _, err := a.db.Exec(`DELETE FROM fb_groups WHERE id = ?`, id); err != nil {
			return "", err
		}
		a.audit(r, "fbgroup.delete", id, "")
		return "Group removed from the list (the page is still a member on Facebook).", nil
	case "grp-remind":
		sent, err := a.sendGroupsReminder(time.Now().In(a.cfg.TZ))
		if err != nil {
			return "", err
		}
		a.audit(r, "fbgroup.remind", "", fmt.Sprint(sent))
		if sent == 0 {
			return "Nothing is due, so no reminder was sent.", nil
		}
		return fmt.Sprintf("Reminder with %d groups sent to the admin address.", sent), nil
	}
	return "", fmt.Errorf("unknown action")
}
