package main

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Promoters: marketers, event organisers, venues, media and influencers who
// post on their own behalf. A member applies from /account/promoter, the
// admin approves in the console, and from then on the member has:
//
//   - events with a show-from date (schedule), hide/show and remove;
//   - posts: short notices for the site's noticeboard, with a run of dates;
//   - import: an .ics or .csv file, previewed then confirmed, and up to three
//     connected calendars (ICS URLs) the watcher checks on its usual cycle;
//   - listing submissions (groups, activities, places) without the anonymous
//     form's email round trip.
//
// Everything still goes through the moderation queue unless the admin marks
// the promoter "trusted", in which case items publish at once (and show up
// in the console's Events/Posts pages marked promoted, where the admin can
// still pull them). Auto-published items are not queued for the Facebook
// page; only an admin decision does that.

const (
	promoterEventsDay   = 60 // new events an approved promoter may add per day (imports count)
	promoterPostsDay    = 20
	promoterCalendars   = 3
	promoterImportMax   = 200     // rows per import
	promoterImportBytes = 2 << 20 // upload size
	postMaxRun          = 90      // days a post may run
	postBodyMax         = 600     // characters
	postsWindowDays     = 30      // the public feed looks this far ahead for posts that start later
	importCookieMax     = 30 * time.Minute
)

var promoterKinds = []string{"organiser", "venue", "business", "marketer", "influencer", "media", "nonprofit", "other"}
var promoterKindNames = map[string]string{"organiser": "Event organiser", "venue": "Venue or club", "business": "Business", "marketer": "Marketing agency", "influencer": "Influencer / creator", "media": "Media", "nonprofit": "Non-profit / school / church", "other": "Other"}

type Promoter struct {
	MemberID                   int64
	Org, Kind, Website         string
	Facebook, Instagram, Blurb string
	Towns                      []string
	Status                     string // pending | approved | declined
	AppliedAt, DecidedAt, Note string
	// filled for console lists
	Name, Email              string
	Trusted                  bool
	Events, Posts, Calendars int
}

func (p Promoter) KindName() string { return promoterKindNames[p.Kind] }

func (a *App) promoterByMember(id int64) *Promoter {
	p, err := scanPromoter(a.db.QueryRow(`SELECT p.member_id, p.org, p.kind, p.website, p.facebook, p.instagram, p.towns, p.blurb, p.status, p.applied_at, COALESCE(p.decided_at,''), p.note, m.name, m.email, m.trusted FROM promoters p JOIN members m ON m.id = p.member_id WHERE p.member_id = ?`, id))
	if err != nil {
		return nil
	}
	return p
}

func scanPromoter(row interface{ Scan(...any) error }) (*Promoter, error) {
	var p Promoter
	var towns string
	var trusted int
	if err := row.Scan(&p.MemberID, &p.Org, &p.Kind, &p.Website, &p.Facebook, &p.Instagram, &towns, &p.Blurb, &p.Status, &p.AppliedAt, &p.DecidedAt, &p.Note, &p.Name, &p.Email, &trusted); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(towns), &p.Towns)
	p.Trusted = trusted == 1
	return &p, nil
}

func (a *App) promoters(where string, args ...any) []Promoter {
	rows, err := a.db.Query(`SELECT p.member_id, p.org, p.kind, p.website, p.facebook, p.instagram, p.towns, p.blurb, p.status, p.applied_at, COALESCE(p.decided_at,''), p.note, m.name, m.email, m.trusted FROM promoters p JOIN members m ON m.id = p.member_id WHERE `+where+` ORDER BY p.applied_at DESC`, args...)
	if err != nil {
		return nil
	}
	var out []Promoter
	for rows.Next() {
		if p, err := scanPromoter(rows); err == nil {
			out = append(out, *p)
		}
	}
	rows.Close() // before the counts: the pool has one connection
	for i := range out {
		out[i].Events = a.count(`SELECT COUNT(*) FROM events WHERE member_id = ?`, out[i].MemberID)
		out[i].Posts = a.count(`SELECT COUNT(*) FROM posts WHERE member_id = ?`, out[i].MemberID)
		out[i].Calendars = a.count(`SELECT COUNT(*) FROM sources WHERE member_id = ?`, out[i].MemberID)
	}
	return out
}

// orgOf is the promoter's organisation name for a member, or "".
func (a *App) orgOf(memberID int64) string {
	var org string
	_ = a.db.QueryRow(`SELECT org FROM promoters WHERE member_id = ? AND status = 'approved'`, memberID).Scan(&org)
	return org
}

/* ---------- routes ---------- */

func (a *App) promoterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /account/promoter", a.requireMember(a.promoterPage))
	mux.HandleFunc("POST /account/promoter/apply", a.requireMember(a.promoterApply))
	mux.HandleFunc("GET /account/promoter/posts/new", a.requirePromoter(a.postForm))
	mux.HandleFunc("GET /account/promoter/posts/edit", a.requirePromoter(a.postForm))
	mux.HandleFunc("POST /account/promoter/posts/save", a.requirePromoter(a.postSave))
	mux.HandleFunc("POST /account/promoter/posts/toggle", a.requirePromoter(a.postToggle))
	mux.HandleFunc("POST /account/promoter/posts/delete", a.requirePromoter(a.postDelete))
	mux.HandleFunc("POST /account/promoter/events/toggle", a.requirePromoter(a.memberEventToggle))
	mux.HandleFunc("GET /account/promoter/import", a.requirePromoter(a.importPage))
	mux.HandleFunc("POST /account/promoter/import", a.requirePromoter(a.importPreview))
	mux.HandleFunc("POST /account/promoter/import/confirm", a.requirePromoter(a.importConfirm))
	mux.HandleFunc("POST /account/promoter/calendar", a.requirePromoter(a.calendarAdd))
	mux.HandleFunc("POST /account/promoter/calendar/remove", a.requirePromoter(a.calendarRemove))
	mux.HandleFunc("POST /account/promoter/calendar/check", a.requirePromoter(a.calendarCheck))
	mux.HandleFunc("GET /account/promoter/listing", a.requirePromoter(a.promoterListingForm))
	mux.HandleFunc("POST /account/promoter/listing", a.requirePromoter(a.promoterListingSave))
}

// requirePromoter is requireMember plus an approved application.
func (a *App) requirePromoter(next http.HandlerFunc) http.HandlerFunc {
	return a.requireMember(func(w http.ResponseWriter, r *http.Request) {
		if !memberOf(r).Member.IsPromoter() {
			a.accountBack(w, r, "/account/promoter", "That page is for approved promoters.", true)
			return
		}
		next(w, r)
	})
}

/* ---------- application and dashboard ---------- */

type promoterPageView struct {
	P             *Promoter
	Kinds         []string
	KindNames     map[string]string
	Towns         []string
	Events        []memberEventRow
	Posts         []postRow
	Calendars     []sourceRow
	Live, Waiting int
	CalendarsLeft int
	Site          string
}

func (a *App) promoterPage(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	v := promoterPageView{P: a.promoterByMember(s.MemberID), Kinds: promoterKinds, KindNames: promoterKindNames, Towns: sortedKeys(towns), Site: a.cfg.SiteURL}
	if v.P == nil || !s.Member.IsPromoter() {
		title := "Promote with us"
		if v.P != nil {
			title = "Your promoter application"
		}
		a.renderAccount(w, r, 200, "acc_promoter_apply", title, v)
		return
	}
	v.Events = a.memberEventRows(s.MemberID)
	v.Posts = a.postRows(`member_id = ?`, s.MemberID)
	v.Calendars = a.memberSources(s.MemberID)
	v.CalendarsLeft = promoterCalendars - len(v.Calendars)
	today := a.localDay(time.Now())
	v.Live = a.count(`SELECT COUNT(*) FROM events WHERE member_id = ? AND status = 'approved' AND `+liveEventsWhere+` AND (CASE WHEN end_date = '' THEN date ELSE end_date END) >= ?`, s.MemberID, today, today) +
		a.count(`SELECT COUNT(*) FROM posts WHERE member_id = ? AND status = 'approved' AND hidden = 0 AND starts <= ? AND ends >= ?`, s.MemberID, today, today)
	v.Waiting = a.count(`SELECT COUNT(*) FROM events WHERE member_id = ? AND status = 'pending_review'`, s.MemberID) + a.count(`SELECT COUNT(*) FROM posts WHERE member_id = ? AND status = 'pending_review'`, s.MemberID)
	a.renderAccount(w, r, 200, "acc_promoter", v.P.Org, v)
}

func (a *App) promoterApply(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	f := r.PostForm
	if s.Member.IsPromoter() {
		a.accountBack(w, r, "/account/promoter", "You are already an approved promoter.", true)
		return
	}
	if p := a.promoterByMember(s.MemberID); p != nil && p.Status == "pending" {
		a.accountBack(w, r, "/account/promoter", "Your application is already waiting for a look.", true)
		return
	}
	org := clean(f.Get("org"), 80)
	kind := f.Get("kind")
	website, urlOK := validURL(f.Get("website"))
	fb := cleanHandle(f.Get("facebook"))
	ig := cleanHandle(f.Get("instagram"))
	blurb := cleanMulti(f.Get("blurb"), 600)
	tw, twOK := filterSet(f["towns"], towns, 4)
	var msg string
	switch {
	case len(org) < 2:
		msg = "Tell us the name you promote under (organisation, venue, brand or your own)."
	case promoterKindNames[kind] == "":
		msg = "Choose what best describes you."
	case !urlOK:
		msg = "The website must be a full http(s) address."
	case website == "" && fb == "" && ig == "":
		msg = "Give at least one of a website, a Facebook page or an Instagram account so we can see who you are."
	case len(blurb) < 30:
		msg = "Tell us in a few sentences what you would post (30+ characters)."
	case !twOK || len(tw) == 0:
		msg = "Tick at least one town."
	}
	if msg != "" {
		a.accountBack(w, r, "/account/promoter", msg, true)
		return
	}
	_, err := a.db.Exec(`INSERT INTO promoters(member_id, org, kind, website, facebook, instagram, towns, blurb, status, applied_at, decided_at, note) VALUES(?,?,?,?,?,?,?,?,'pending',?,NULL,'')
		ON CONFLICT(member_id) DO UPDATE SET org=excluded.org, kind=excluded.kind, website=excluded.website, facebook=excluded.facebook, instagram=excluded.instagram, towns=excluded.towns, blurb=excluded.blurb, status='pending', applied_at=excluded.applied_at, decided_at=NULL`,
		s.MemberID, org, kind, website, fb, ig, jsonList(tw), blurb, now())
	if err != nil {
		a.logf("promoter apply: %v", err)
		a.accountBack(w, r, "/account/promoter", "Could not save the application. Please try again.", true)
		return
	}
	a.audit(r, "promoter.apply", fmt.Sprint(s.MemberID), org)
	a.notify("admin-promoter", "[HS] Promoter application: "+org,
		fmt.Sprintf("%s (%s) applied to post as a promoter.\n\nOrganisation: %s\nType: %s\nWebsite: %s\nFacebook: %s\nInstagram: %s\nTowns: %s\n\n%s\n\nDecide: %s/admin/members/view?id=%d\n", s.Member.Name, s.Member.Email, org, promoterKindNames[kind], website, fb, ig, strings.Join(tw, ", "), blurb, a.cfg.APIURL, s.MemberID))
	a.accountBack(w, r, "/account/promoter", "Thanks, your application is in. A person looks at every one, usually within a day or two, and you get an email either way.", false)
}

// cleanHandle keeps a social handle or page URL as typed, minus junk.
func cleanHandle(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "@"))
	return clean(s, 120)
}

/* ---------- console side ---------- */

// promoterDecide is the admin's approve / decline / revoke / trust switch.
func (a *App) promoterDecide(r *http.Request, memberID int64, action string) (string, error) {
	p := a.promoterByMember(memberID)
	if p == nil {
		return "", fmt.Errorf("that member has not applied")
	}
	switch action {
	case "promoter-approve":
		if _, err := a.db.Exec(`UPDATE promoters SET status = 'approved', decided_at = ? WHERE member_id = ?`, now(), memberID); err != nil {
			return "", err
		}
		_, _ = a.db.Exec(`UPDATE members SET role = 'promoter' WHERE id = ?`, memberID)
		a.audit(r, "promoter.approve", fmt.Sprint(memberID), p.Org)
		a.mailPromoterDecision(p, true)
		return fmt.Sprintf("%s is now an approved promoter.", p.Org), nil
	case "promoter-decline", "promoter-revoke":
		note := clean(r.PostForm.Get("note"), 300)
		status := "declined"
		if _, err := a.db.Exec(`UPDATE promoters SET status = ?, decided_at = ?, note = ? WHERE member_id = ?`, status, now(), note, memberID); err != nil {
			return "", err
		}
		_, _ = a.db.Exec(`UPDATE members SET role = 'member', trusted = 0 WHERE id = ?`, memberID)
		// Their live items come down; the admin can re-approve them one by one.
		if action == "promoter-revoke" {
			_, _ = a.db.Exec(`UPDATE events SET status = 'pending_review', decided_at = NULL WHERE member_id = ? AND status = 'approved'`, memberID)
			_, _ = a.db.Exec(`UPDATE posts SET status = 'pending_review', decided_at = NULL WHERE member_id = ? AND status = 'approved'`, memberID)
			_, _ = a.db.Exec(`UPDATE sources SET enabled = 0 WHERE member_id = ?`, memberID)
		}
		a.audit(r, "promoter."+strings.TrimPrefix(action, "promoter-"), fmt.Sprint(memberID), note)
		p.Note = note
		if action == "promoter-decline" {
			a.mailPromoterDecision(p, false)
			return fmt.Sprintf("%s declined.", p.Org), nil
		}
		return fmt.Sprintf("%s is no longer a promoter; their published items are back in the queue and their calendars are off.", p.Org), nil
	case "promoter-trust", "promoter-untrust":
		t := 0
		if action == "promoter-trust" {
			t = 1
		}
		if _, err := a.db.Exec(`UPDATE members SET trusted = ? WHERE id = ? AND role = 'promoter'`, t, memberID); err != nil {
			return "", err
		}
		a.audit(r, "promoter."+strings.TrimPrefix(action, "promoter-"), fmt.Sprint(memberID), "")
		if t == 1 {
			return fmt.Sprintf("%s is trusted: their events and posts publish without a check.", p.Org), nil
		}
		return fmt.Sprintf("%s goes through the queue again.", p.Org), nil
	}
	return "", fmt.Errorf("unknown action")
}

func (a *App) mailPromoterDecision(p *Promoter, approved bool) {
	if approved {
		_ = a.send(Message{To: p.Email, Kind: "promoter-approved", Subject: "You can now post on Helderberg Social as " + p.Org,
			Text: fmt.Sprintf("Hi %s,\n\nYour promoter application for %s is approved. Sign in and open Promote:\n%s/account/promoter\n\nThere you can add events and posts, schedule them, hide or remove them, import an .ics or .csv file, and connect a calendar we check for you. Everything still gets a quick look before it shows on the site, unless we tell you otherwise.\n\nHelderberg Social\n%s\n", p.Name, p.Org, a.cfg.APIURL, a.cfg.SiteURL),
			HTML: a.htmlMemberMail("Your promoter application is approved", fmt.Sprintf("Hi %s, you can now post on Helderberg Social as %s: events and posts, scheduled or straight away, imported from a file or a connected calendar. Everything still gets a quick look before it shows on the site.", p.Name, p.Org), "Open Promote", a.cfg.APIURL+"/account/promoter")})
		return
	}
	_ = a.send(Message{To: p.Email, Kind: "promoter-declined", Subject: "About your promoter application",
		Text: fmt.Sprintf("Hi %s,\n\nWe looked at the promoter application for %s and are not able to approve it at the moment.%s\n\nYou can still post individual community events from your account as before:\n%s/account\n\nIf you think we got it wrong, reply to this email.\n\nHelderberg Social\n%s\n", p.Name, p.Org, noteLine(p.Note), a.cfg.APIURL, a.cfg.SiteURL)})
}

func noteLine(note string) string {
	if note == "" {
		return ""
	}
	return " The note we left: " + note
}

/* ---------- events: promoter controls ---------- */

func (a *App) memberEventRows(memberID int64) []memberEventRow {
	evs, _ := a.queryEvents(`member_id = ?`, memberID)
	var rows []memberEventRow
	today := a.localDay(time.Now())
	for i := len(evs) - 1; i >= 0; i-- { // newest date first
		e := evs[i]
		row := memberEventRow{Event: e, StatusText: memberStatusText(e.Status), Editable: true}
		if e.Status == "approved" && !e.Hidden && (e.VisibleFrom == "" || e.VisibleFrom <= today) {
			row.Live = a.cfg.SiteURL + "/events.html?ev=" + url.QueryEscape(e.ID)
		}
		if e.Hidden {
			row.StatusText += " · hidden"
		} else if e.VisibleFrom > today {
			row.StatusText += " · shows from " + fmtDate(e.VisibleFrom)
		}
		if end := e.EndDate; (end != "" && end < today) || (end == "" && e.Date < today) {
			row.StatusText += " · past"
			row.Editable = false
		}
		rows = append(rows, row)
	}
	return rows
}

// memberEventToggle hides or shows one of the promoter's events without
// touching its approval.
func (a *App) memberEventToggle(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	id := strings.TrimSpace(r.PostForm.Get("id"))
	back := accountNext(r.PostForm.Get("return"))
	hide := r.PostForm.Get("to") == "hide"
	h := 0
	if hide {
		h = 1
	}
	res, err := a.db.Exec(`UPDATE events SET hidden = ? WHERE id = ? AND member_id = ?`, h, id, s.MemberID)
	if err != nil {
		a.accountBack(w, r, back, "Could not change the event.", true)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		a.accountBack(w, r, back, "That event is not one of yours.", true)
		return
	}
	if hide {
		a.fbCancelRef("event", id)
	}
	a.audit(r, "member.event_toggle", id, r.PostForm.Get("to"))
	if hide {
		a.accountBack(w, r, back, "The event is hidden from the site. Show it again whenever you like.", false)
		return
	}
	a.accountBack(w, r, back, "The event shows on the site again (once it is published and past its show-from date).", false)
}

/* ---------- posts ---------- */

type Post struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Link     string `json:"link,omitempty"`
	Town     string `json:"town"`
	Category string `json:"category"`
	Starts   string `json:"starts"`
	Ends     string `json:"ends"`
	By       string `json:"by,omitempty"`
	// internal
	MemberID  int64  `json:"-"`
	Status    string `json:"-"`
	Hidden    bool   `json:"-"`
	CreatedAt string `json:"-"`
	UpdatedAt string `json:"-"`
}

func (p Post) TownName() string { return townName(p.Town) }
func (p Post) CatName() string  { return catName(p.Category) }
func (p Post) When() string {
	if p.Starts == p.Ends {
		return fmtDate(p.Starts)
	}
	return fmtDate(p.Starts) + " – " + fmtDate(p.Ends)
}

type postRow struct {
	Post
	StatusText string
	Editable   bool
	Live       string
	Org        string
}

const postCols = `id, member_id, title, body, link, town, category, starts, ends, status, hidden, created_at, updated_at`

func scanPost(row interface{ Scan(...any) error }) (Post, error) {
	var p Post
	var hidden int
	err := row.Scan(&p.ID, &p.MemberID, &p.Title, &p.Body, &p.Link, &p.Town, &p.Category, &p.Starts, &p.Ends, &p.Status, &hidden, &p.CreatedAt, &p.UpdatedAt)
	p.Hidden = hidden == 1
	return p, err
}

func (a *App) queryPosts(where string, args ...any) []Post {
	rows, err := a.db.Query(`SELECT `+postCols+` FROM posts WHERE `+where+` ORDER BY starts DESC, created_at DESC`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		if p, err := scanPost(rows); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func (a *App) postByID(id string) *Post {
	p, err := scanPost(a.db.QueryRow(`SELECT `+postCols+` FROM posts WHERE id = ?`, id))
	if err != nil {
		return nil
	}
	return &p
}

func (a *App) postRows(where string, args ...any) []postRow {
	today := a.localDay(time.Now())
	orgs := map[int64]string{}
	var rows []postRow
	for _, p := range a.queryPosts(where, args...) {
		row := postRow{Post: p, StatusText: memberStatusText(p.Status), Editable: p.Ends >= today}
		if _, ok := orgs[p.MemberID]; !ok {
			orgs[p.MemberID] = a.orgOf(p.MemberID)
		}
		row.Org = orgs[p.MemberID]
		if p.Status == "approved" && !p.Hidden && p.Starts <= today && p.Ends >= today {
			row.Live = a.cfg.SiteURL + "/notices.html?post=" + url.QueryEscape(p.ID)
		}
		if p.Hidden {
			row.StatusText += " · hidden"
		} else if p.Status == "approved" && p.Starts > today {
			row.StatusText += " · shows from " + fmtDate(p.Starts)
		}
		if p.Ends < today {
			row.StatusText += " · ended"
		}
		rows = append(rows, row)
	}
	return rows
}

// livePosts is the public noticeboard: approved, not hidden, running now or
// starting within the window.
func (a *App) livePosts(today string) []Post {
	to := mustDate(today).AddDate(0, 0, postsWindowDays).Format("2006-01-02")
	posts := a.queryPosts(`status = 'approved' AND hidden = 0 AND starts <= ? AND ends >= ?`, to, today)
	sort.SliceStable(posts, func(i, j int) bool {
		if posts[i].Starts != posts[j].Starts {
			return posts[i].Starts < posts[j].Starts
		}
		return posts[i].Title < posts[j].Title
	})
	orgs := map[int64]string{}
	for i := range posts {
		if _, ok := orgs[posts[i].MemberID]; !ok {
			orgs[posts[i].MemberID] = a.orgOf(posts[i].MemberID)
		}
		posts[i].By = orgs[posts[i].MemberID]
	}
	return posts
}

func mustDate(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

type postFormView struct {
	P           Post
	New         bool
	Towns, Cats []string
	Trusted     bool
}

func (a *App) postForm(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	f := postFormView{New: true, Towns: sortedKeys(towns), Cats: sortedKeys(categories), Trusted: s.Member.Trusted}
	if id := r.URL.Query().Get("id"); id != "" {
		p := a.postByID(id)
		if p == nil || p.MemberID != s.MemberID {
			a.accountBack(w, r, "/account/promoter", "That post is not one of yours.", true)
			return
		}
		f.P, f.New = *p, false
	} else {
		today := a.localDay(time.Now())
		f.P = Post{Town: "somerset-west", Category: "community", Starts: today, Ends: mustDate(today).AddDate(0, 0, 14).Format("2006-01-02")}
	}
	title := "New post"
	if !f.New {
		title = "Edit post"
	}
	a.renderAccount(w, r, 200, "acc_post_form", title, f)
}

// postProblem validates a post and fills the link; "" when fine.
func (a *App) postProblem(p *Post, link string) string {
	today := time.Now().In(a.cfg.TZ).Truncate(24 * time.Hour)
	switch {
	case len(p.Title) < 4:
		return "Give the post a title."
	case !towns[p.Town]:
		return "Choose a town."
	case !categories[p.Category]:
		return "Choose a category."
	case len(p.Body) < 20:
		return "Say what it is about in a sentence or two (20+ characters)."
	}
	sd, ok := validDate(p.Starts)
	if !ok || sd.Before(today.AddDate(0, 0, -1)) || sd.After(today.AddDate(0, 0, 400)) {
		return "The start date must be a real day within the next year."
	}
	ed, ok := validDate(p.Ends)
	if !ok || ed.Before(sd) || ed.After(sd.AddDate(0, 0, postMaxRun)) {
		return fmt.Sprintf("The end date must be on or after the start, and within %d days of it.", postMaxRun)
	}
	var good bool
	if p.Link, good = validURL(link); !good {
		return "The link must be a full http(s) address."
	}
	return ""
}

func (a *App) postSave(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	f := r.PostForm
	id := strings.TrimSpace(f.Get("id"))
	if !a.settingBool("submissions_on") {
		a.accountBack(w, r, "/account/promoter", "Submissions are paused for the moment. Please try again later.", true)
		return
	}
	p := Post{Title: clean(f.Get("title"), 120), Body: cleanMulti(f.Get("body"), postBodyMax), Town: f.Get("town"), Category: f.Get("category"), Starts: strings.TrimSpace(f.Get("starts")), Ends: strings.TrimSpace(f.Get("ends"))}
	back := "/account/promoter/posts/new"
	if id != "" {
		back = "/account/promoter/posts/edit?id=" + url.QueryEscape(id)
	}
	if msg := a.postProblem(&p, f.Get("link")); msg != "" {
		a.accountBack(w, r, back, msg, true)
		return
	}
	status := "pending_review"
	if s.Member.Trusted {
		status = "approved"
	}
	if id == "" {
		if a.count(`SELECT COUNT(*) FROM posts WHERE member_id = ? AND created_at > ?`, s.MemberID, time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339)) >= promoterPostsDay {
			a.accountBack(w, r, "/account/promoter", fmt.Sprintf("You have added %d posts in the last day, which is the limit. Try again tomorrow.", promoterPostsDay), true)
			return
		}
		p.ID = a.uniquePostID(slugify(p.Title))
		var decided any
		if status == "approved" {
			decided = now()
		}
		if _, err := a.db.Exec(`INSERT INTO posts(id, member_id, title, body, link, town, category, starts, ends, status, hidden, created_at, updated_at, decided_at) VALUES(?,?,?,?,?,?,?,?,?,?,0,?,?,?)`,
			p.ID, s.MemberID, p.Title, p.Body, p.Link, p.Town, p.Category, p.Starts, p.Ends, status, now(), now(), decided); err != nil {
			a.logf("post insert: %v", err)
			a.accountBack(w, r, back, "Could not save the post. Please try again.", true)
			return
		}
		a.audit(r, "promoter.post_new", p.ID, fmt.Sprint(s.MemberID))
		if status == "approved" {
			a.accountBack(w, r, "/account/promoter", "Published. The post is on the noticeboard from its start date.", false)
			return
		}
		a.notifyAdminPost(p.ID)
		a.accountBack(w, r, "/account/promoter", "Thanks! The post is in the queue for a quick check, usually within a day.", false)
		return
	}
	var decided any
	if status == "approved" {
		decided = now()
	}
	res, err := a.db.Exec(`UPDATE posts SET title=?, body=?, link=?, town=?, category=?, starts=?, ends=?, status=?, decided_at=?, updated_at=? WHERE id=? AND member_id=?`,
		p.Title, p.Body, p.Link, p.Town, p.Category, p.Starts, p.Ends, status, decided, now(), id, s.MemberID)
	if err != nil {
		a.accountBack(w, r, back, "Could not save the changes. Please try again.", true)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		a.accountBack(w, r, "/account/promoter", "That post is not one of yours.", true)
		return
	}
	a.audit(r, "promoter.post_edit", id, fmt.Sprint(s.MemberID))
	if status == "approved" {
		a.accountBack(w, r, "/account/promoter", "Saved and live.", false)
		return
	}
	a.notifyAdminPost(id)
	a.accountBack(w, r, "/account/promoter", "Saved. The updated post goes back in the queue for a quick check before it shows again.", false)
}

func (a *App) uniquePostID(base string) string {
	if base == "" {
		base = "post"
	}
	for {
		id := base + "-" + randomID(2)
		if a.count(`SELECT COUNT(*) FROM posts WHERE id = ?`, id) == 0 {
			return id
		}
	}
}

func (a *App) postToggle(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	id := strings.TrimSpace(r.PostForm.Get("id"))
	h := 0
	if r.PostForm.Get("to") == "hide" {
		h = 1
	}
	res, err := a.db.Exec(`UPDATE posts SET hidden = ?, updated_at = ? WHERE id = ? AND member_id = ?`, h, now(), id, s.MemberID)
	if n, _ := res.RowsAffected(); err != nil || n == 0 {
		a.accountBack(w, r, "/account/promoter", "That post is not one of yours.", true)
		return
	}
	a.audit(r, "promoter.post_toggle", id, r.PostForm.Get("to"))
	if h == 1 {
		a.accountBack(w, r, "/account/promoter", "The post is hidden. Show it again whenever you like.", false)
		return
	}
	a.accountBack(w, r, "/account/promoter", "The post shows again (once published and within its dates).", false)
}

func (a *App) postDelete(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	id := strings.TrimSpace(r.PostForm.Get("id"))
	if r.PostForm.Get("confirm") != "yes" {
		a.accountBack(w, r, "/account/promoter", "Tick the box to confirm you want to delete the post.", true)
		return
	}
	res, err := a.db.Exec(`DELETE FROM posts WHERE id = ? AND member_id = ?`, id, s.MemberID)
	if n, _ := res.RowsAffected(); err != nil || n == 0 {
		a.accountBack(w, r, "/account/promoter", "That post is not one of yours.", true)
		return
	}
	a.audit(r, "promoter.post_delete", id, fmt.Sprint(s.MemberID))
	a.accountBack(w, r, "/account/promoter", "The post is deleted.", false)
}

func (a *App) notifyAdminPost(id string) {
	p := a.postByID(id)
	if p == nil {
		return
	}
	body := fmt.Sprintf("%s\n%s · %s · %s\nby %s\n\n%s\n\n%s\n\nApprove: %s\nReject:  %s\n\nQueue: %s\n", p.Title, p.When(), townName(p.Town), catName(p.Category), a.orgOf(p.MemberID), p.Body, p.Link,
		a.moderateURL("post", p.ID, "approve"), a.moderateURL("post", p.ID, "reject"), a.adminURL())
	a.notify("admin-post", "[HS] Post to review: "+p.Title, body)
}

// notifyPostDecision tells the promoter what happened to a post.
func (a *App) notifyPostDecision(id, status string) {
	var email, name, title string
	err := a.db.QueryRow(`SELECT m.email, m.name, p.title FROM posts p JOIN members m ON m.id = p.member_id WHERE p.id = ? AND m.status = 'active'`, id).Scan(&email, &name, &title)
	if err != nil {
		return
	}
	if status == "approved" {
		link := a.cfg.SiteURL + "/notices.html?post=" + url.QueryEscape(id)
		_ = a.send(Message{To: email, Kind: "member-approved", Subject: "Your post is live: " + title,
			Text: fmt.Sprintf("Hi %s,\n\n\"%s\" has been checked and is on the Helderberg Social noticeboard for its dates:\n\n%s\n\nTo change, hide or remove it, open Promote:\n%s/account/promoter\n\nHelderberg Social\n%s\n", name, title, link, a.cfg.APIURL, a.cfg.SiteURL),
			HTML: a.htmlMemberMail("Your post is live", fmt.Sprintf("Hi %s, \"%s\" has been checked and is on the noticeboard for its dates. To change, hide or remove it, open Promote in your account.", name, title), "See it on the site", link)})
		return
	}
	_ = a.send(Message{To: email, Kind: "member-rejected", Subject: "About your post: " + title,
		Text: fmt.Sprintf("Hi %s,\n\nWe looked at \"%s\" and did not publish it. Usually that means it is outside the Helderberg, is not something residents can go to or use, or reads as an advert with nothing in it for the community.\n\nYou can edit and resubmit it from Promote:\n%s/account/promoter\n\nIf you think we got it wrong, reply to this email.\n\nHelderberg Social\n%s\n", name, title, a.cfg.APIURL, a.cfg.SiteURL)})
}

/* ---------- import: a file, previewed then confirmed ---------- */

type importRow struct {
	E       Event
	Problem string
	Dup     bool
}

type importView struct {
	Rows            []importRow
	Good, Bad, Dups int
	Token           string
	FileName        string
	Towns, Cats     []string
	Trusted         bool
	Calendars       []sourceRow
	CalendarsLeft   int
}

func (a *App) importPage(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	v := importView{Towns: sortedKeys(towns), Cats: sortedKeys(categories), Trusted: s.Member.Trusted, Calendars: a.memberSources(s.MemberID)}
	v.CalendarsLeft = promoterCalendars - len(v.Calendars)
	a.renderAccount(w, r, 200, "acc_import", "Import events", v)
}

// importPreview reads the uploaded .ics or .csv, validates every row and
// shows what would be added. Nothing is written yet; the parsed rows travel
// to the confirm step in a signed token so a plain form can carry them.
func (a *App) importPreview(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	if err := r.ParseMultipartForm(promoterImportBytes); err != nil {
		a.accountBack(w, r, "/account/promoter/import", "The file is too big (2 MB at most) or the form was not sent as a file upload.", true)
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		a.accountBack(w, r, "/account/promoter/import", "Choose an .ics or .csv file first.", true)
		return
	}
	defer file.Close()
	if hdr.Size > promoterImportBytes {
		a.accountBack(w, r, "/account/promoter/import", "The file is too big (2 MB at most).", true)
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(file, promoterImportBytes))
	town, cat := r.FormValue("town"), r.FormValue("category")
	if !towns[town] {
		town = "somerset-west"
	}
	if !categories[cat] {
		cat = "community"
	}
	var events []Event
	var parseErr string
	text := string(raw)
	switch {
	case strings.Contains(strings.ToUpper(text[:min(len(text), 512)]), "BEGIN:VCALENDAR"):
		events = a.eventsFromICS(text, town, cat, "")
		if len(events) == 0 {
			parseErr = "No upcoming events were found in that calendar file."
		}
	default:
		events, parseErr = a.eventsFromCSV(text, town, cat)
	}
	if parseErr != "" {
		a.accountBack(w, r, "/account/promoter/import", parseErr, true)
		return
	}
	if len(events) > promoterImportMax {
		events = events[:promoterImportMax]
	}
	v := importView{FileName: clean(hdr.Filename, 80), Towns: sortedKeys(towns), Cats: sortedKeys(categories), Trusted: s.Member.Trusted}
	var good []Event
	for _, e := range events {
		row := importRow{E: e}
		e2 := e
		row.Problem = a.eventProblem(&e2, e.Website)
		row.E = e2
		if row.Problem == "" && a.count(`SELECT COUNT(*) FROM events WHERE member_id = ? AND lower(title) = lower(?) AND date = ?`, s.MemberID, e2.Title, e2.Date) > 0 {
			row.Dup = true
		}
		switch {
		case row.Problem != "":
			v.Bad++
		case row.Dup:
			v.Dups++
		default:
			v.Good++
			good = append(good, e2)
		}
		v.Rows = append(v.Rows, row)
	}
	if len(good) > 0 {
		payload, _ := json.Marshal(good)
		v.Token = a.sign("import:"+fmt.Sprint(s.MemberID), base64.RawURLEncoding.EncodeToString(payload), importCookieMax)
	}
	a.renderAccount(w, r, 200, "acc_import_preview", "Import: check before adding", v)
}

func (a *App) importConfirm(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	tok, err := a.consume(r.PostForm.Get("token"), "import:"+fmt.Sprint(s.MemberID))
	if err != nil {
		a.accountBack(w, r, "/account/promoter/import", "That preview has expired or was already added. Upload the file again.", true)
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(tok.Subject)
	var events []Event
	if err != nil || json.Unmarshal(raw, &events) != nil {
		a.accountBack(w, r, "/account/promoter/import", "Could not read the preview. Upload the file again.", true)
		return
	}
	if !a.settingBool("submissions_on") {
		a.accountBack(w, r, "/account/promoter", "Submissions are paused for the moment. Please try again later.", true)
		return
	}
	left := promoterEventsDay - a.count(`SELECT COUNT(*) FROM events WHERE member_id = ? AND created_at > ?`, s.MemberID, time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339))
	added, skipped := 0, 0
	for _, e := range events {
		if added >= left {
			skipped++
			continue
		}
		e2 := e
		if a.eventProblem(&e2, e.Website) != "" {
			skipped++
			continue
		}
		if a.count(`SELECT COUNT(*) FROM events WHERE member_id = ? AND lower(title) = lower(?) AND date = ?`, s.MemberID, e2.Title, e2.Date) > 0 {
			skipped++
			continue
		}
		e2.Source = e2.Website
		e2.ID = a.uniqueEventID(slugify(e2.Title) + "-" + e2.Date)
		a.stampPromoterEvent(&e2, s.Member)
		if err := a.insertEvent(e2, ipTag(ipOf(r)), s.Member.Email, nil); err != nil {
			skipped++
			continue
		}
		_, _ = a.db.Exec(`UPDATE events SET verified_at = ? WHERE id = ?`, now(), e2.ID)
		added++
	}
	a.audit(r, "promoter.import", fmt.Sprint(s.MemberID), fmt.Sprintf("added %d skipped %d", added, skipped))
	if added > 0 && !s.Member.Trusted {
		a.notify("admin-event", fmt.Sprintf("[HS] %d imported events to review from %s", added, a.orgOf(s.MemberID)), fmt.Sprintf("%s imported %d events; they are in the queue.\n\n%s\n", a.orgOf(s.MemberID), added, a.adminURL()))
	}
	msg := fmt.Sprintf("Added %d event%s.", added, plural(added))
	if skipped > 0 {
		msg += fmt.Sprintf(" Skipped %d (already there, invalid, or over today's limit of %d).", skipped, promoterEventsDay)
	}
	if added > 0 && !s.Member.Trusted {
		msg += " They are in the queue for a quick check."
	}
	a.accountBack(w, r, "/account", msg, false)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// stampPromoterEvent sets who it is from and whether it needs a check.
func (a *App) stampPromoterEvent(e *Event, m *Member) {
	e.Origin, e.SubmitterName, e.MemberID, e.Promoted = "user", m.Name, m.ID, m.IsPromoter()
	if org := a.orgOf(m.ID); org != "" {
		e.SubmitterName = org
	}
	e.Status = "pending_review"
	if m.IsPromoter() && m.Trusted {
		e.Status = "approved"
	}
}

// eventsFromICS turns a calendar into event rows the way the watcher does:
// first upcoming occurrence of each entry, series described in the summary.
func (a *App) eventsFromICS(text, town, cat, source string) []Event {
	today := time.Now().In(a.cfg.TZ).Truncate(24 * time.Hour)
	var out []Event
	for _, ev := range parseICS(text, a.cfg.TZ) {
		if ev.RecurrenceID {
			continue
		}
		occ := ev.occurrences(today.AddDate(0, 0, -1), today.AddDate(1, 0, 0), 1)
		if len(occ) == 0 {
			continue
		}
		start := occ[0]
		summary := ev.Description
		if rt := repeatText(ev.RRule, a.cfg.TZ); rt != "" {
			summary = strings.TrimSpace(rt + "\n\n" + summary)
		}
		if ev.Location != "" && !strings.Contains(summary, ev.Location) {
			summary = strings.TrimSpace(summary + "\n\nVenue: " + ev.Location)
		}
		e := Event{Title: clean(ev.Summary, 120), Date: start.Format("2006-01-02"), Town: town, Category: cat, Summary: cleanMulti(summary, 800), Cost: "varies", Source: source}
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
			end := start.Add(ev.End.Sub(ev.Start))
			if ev.AllDay {
				end = end.AddDate(0, 0, -1)
			}
			if ed := end.Format("2006-01-02"); ed > e.Date {
				e.EndDate = ed
			}
		}
		out = append(out, e)
		if len(out) >= promoterImportMax {
			break
		}
	}
	return out
}

// eventsFromCSV reads a header row then one event per line. Column names are
// matched case-insensitively; only title, date and summary are required.
func (a *App) eventsFromCSV(text, town, cat string) ([]Event, string) {
	rd := csv.NewReader(strings.NewReader(strings.TrimPrefix(text, string(rune(0xFEFF)))))
	rd.FieldsPerRecord = -1
	rd.LazyQuotes = true
	rd.TrimLeadingSpace = true
	recs, err := rd.ReadAll()
	if err != nil {
		return nil, "That does not read as a CSV file: " + clean(err.Error(), 120)
	}
	if len(recs) < 2 {
		return nil, "The CSV needs a header row and at least one event."
	}
	idx := map[string]int{}
	for i, h := range recs[0] {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	get := func(rec []string, names ...string) string {
		for _, n := range names {
			if i, ok := idx[n]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
		}
		return ""
	}
	if _, ok := idx["title"]; !ok {
		return nil, "The CSV needs at least a 'title' column (plus date and summary). Download the template on the import page."
	}
	var out []Event
	for _, rec := range recs[1:] {
		if len(rec) == 0 || strings.TrimSpace(strings.Join(rec, "")) == "" {
			continue
		}
		e := Event{
			Title:    clean(get(rec, "title", "name", "event"), 120),
			Date:     normDate(get(rec, "date", "start date", "start_date", "start")),
			EndDate:  normDate(get(rec, "end_date", "end date", "end")),
			Time:     normTime(get(rec, "time", "start time", "start_time")),
			EndTime:  normTime(get(rec, "end_time", "end time")),
			Town:     strings.ToLower(get(rec, "town")),
			Category: strings.ToLower(get(rec, "category")),
			Cost:     strings.ToLower(get(rec, "cost")),
			Website:  get(rec, "website", "url", "link"),
			Summary:  cleanMulti(get(rec, "summary", "description", "details"), 800),
		}
		if e.Town == "" || !towns[e.Town] {
			e.Town = townKey(e.Town, town)
		}
		if e.Category == "" || !categories[e.Category] {
			e.Category = cat
		}
		if e.Cost == "" || !costs[e.Cost] {
			e.Cost = "varies"
		}
		out = append(out, e)
		if len(out) >= promoterImportMax {
			break
		}
	}
	if len(out) == 0 {
		return nil, "No event rows were found under the header."
	}
	return out, ""
}

// townKey maps a typed town name ("Gordon's Bay") to its key, else the default.
func townKey(typed, def string) string {
	t := strings.ToLower(strings.TrimSpace(typed))
	for k := range towns {
		if k == t || strings.ToLower(townName(k)) == t || strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(townName(k)), "'", ""), " ", "-") == strings.ReplaceAll(t, " ", "-") {
			return k
		}
	}
	return def
}

// normDate accepts YYYY-MM-DD, DD/MM/YYYY and DD-MM-YYYY.
func normDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range []string{"2006-01-02", "02/01/2006", "02-01-2006", "2/1/2006", "2 Jan 2006", "2 January 2006", "Jan 2, 2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return s
}

// normTime accepts 18:30, 18h30, 6:30pm, 1830.
func normTime(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "h", ":")
	s = strings.ReplaceAll(s, " ", "")
	for _, layout := range []string{"15:04", "3:04pm", "3pm", "1504", "15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("15:04")
		}
	}
	return s
}

/* ---------- connected calendars ---------- */

func (a *App) memberSources(memberID int64) []sourceRow {
	rows, _ := a.sourceRowsWhere(`s.member_id = ?`, memberID)
	return rows
}

func (a *App) calendarAdd(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	f := r.PostForm
	u, ok := validURL(f.Get("url"))
	if !ok || u == "" {
		a.accountBack(w, r, "/account/promoter/import", "Paste the calendar's public .ics address (a full http(s) link).", true)
		return
	}
	u = strings.Replace(u, "webcal://", "https://", 1)
	if msg := publicCalendarURL(u); msg != "" {
		a.accountBack(w, r, "/account/promoter/import", msg, true)
		return
	}
	if len(a.memberSources(s.MemberID)) >= promoterCalendars {
		a.accountBack(w, r, "/account/promoter/import", fmt.Sprintf("You can connect up to %d calendars. Remove one first.", promoterCalendars), true)
		return
	}
	if a.count(`SELECT COUNT(*) FROM sources WHERE url = ?`, u) > 0 {
		a.accountBack(w, r, "/account/promoter/import", "That calendar is already being watched.", true)
		return
	}
	town, cat := f.Get("town"), f.Get("category")
	if !towns[town] || !categories[cat] {
		a.accountBack(w, r, "/account/promoter/import", "Choose a town and a category for events from this calendar.", true)
		return
	}
	body, err := a.fetch(u)
	if err != nil {
		a.accountBack(w, r, "/account/promoter/import", "Could not fetch that address: "+clean(err.Error(), 100), true)
		return
	}
	if !strings.Contains(strings.ToUpper(string(body[:min(len(body), 512)])), "BEGIN:VCALENDAR") {
		a.accountBack(w, r, "/account/promoter/import", "That address does not return a calendar (.ics) file. In Google Calendar it is under Settings → your calendar → 'Public address in iCal format'.", true)
		return
	}
	label := clean(f.Get("label"), 80)
	if label == "" {
		label = a.orgOf(s.MemberID) + " calendar"
	}
	res, err := a.db.Exec(`INSERT INTO sources(url, kind, label, listing, category, town, match, enabled, member_id) VALUES(?,'ics',?,'',?,?,'',1,?)`, u, label, cat, town, s.MemberID)
	if err != nil {
		a.accountBack(w, r, "/account/promoter/import", "Could not save the calendar.", true)
		return
	}
	id, _ := res.LastInsertId()
	a.audit(r, "promoter.calendar_add", fmt.Sprint(id), fmt.Sprint(s.MemberID))
	report := a.runWatchOne(fmt.Sprint(id))
	a.accountBack(w, r, "/account/promoter/import", "Calendar connected and checked: "+report+" We check it again every few hours.", false)
}

// allowLocalFetch lets tests point a calendar at a loopback server.
var allowLocalFetch = false

// publicCalendarURL refuses addresses a member could use to make the server
// fetch something internal: only https on the default port, a real host
// name, and no name that resolves to a loopback, private, link-local or
// otherwise non-public address. "" when the URL is fine.
func publicCalendarURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "That is not a valid address."
	}
	if u.Scheme != "https" && !allowLocalFetch {
		return "Calendar addresses must start with https://."
	}
	if u.Hostname() == "" || (u.Port() != "" && !allowLocalFetch) {
		return "Use the calendar's plain https address, without a port."
	}
	return privateTarget(u)
}

// privateTarget is "" when the URL's host is a public one, else why not.
func privateTarget(u *url.URL) string {
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if allowLocalFetch && ip.IsLoopback() {
			return ""
		}
		return "Use the calendar's host name, not an IP address."
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return "That host name does not resolve. Check the address."
	}
	for _, ip := range ips {
		if allowLocalFetch && ip.IsLoopback() {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
			return "That address points at a private network, which we cannot fetch from."
		}
	}
	return ""
}

func (a *App) calendarRemove(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	id := strings.TrimSpace(r.PostForm.Get("id"))
	res, err := a.db.Exec(`DELETE FROM sources WHERE id = ? AND member_id = ?`, id, s.MemberID)
	if n, _ := res.RowsAffected(); err != nil || n == 0 {
		a.accountBack(w, r, "/account/promoter/import", "That calendar is not one of yours.", true)
		return
	}
	_, _ = a.db.Exec(`DELETE FROM seen_uids WHERE source_id = ?`, id)
	a.audit(r, "promoter.calendar_remove", id, fmt.Sprint(s.MemberID))
	a.accountBack(w, r, "/account/promoter/import", "Calendar removed. Events already added stay under My events.", false)
}

func (a *App) calendarCheck(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	id := strings.TrimSpace(r.PostForm.Get("id"))
	if a.count(`SELECT COUNT(*) FROM sources WHERE id = ? AND member_id = ?`, id, s.MemberID) == 0 {
		a.accountBack(w, r, "/account/promoter/import", "That calendar is not one of yours.", true)
		return
	}
	a.accountBack(w, r, "/account/promoter/import", a.runWatchOne(id), false)
}

/* ---------- listings from the portal ---------- */

type listingFormView struct {
	Kinds, Towns, Cats, Costs, Audiences []string
}

func (a *App) promoterListingForm(w http.ResponseWriter, r *http.Request) {
	a.renderAccount(w, r, 200, "acc_listing_form", "Add a listing", listingFormView{Kinds: []string{"group", "activity", "place"}, Towns: sortedKeys(towns), Cats: sortedKeys(categories), Costs: []string{"free", "paid", "membership", "donation", "varies"}, Audiences: sortedKeys(audiences)})
}

func (a *App) promoterListingSave(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	f := r.PostForm
	if !a.settingBool("submissions_on") {
		a.accountBack(w, r, "/account/promoter", "Submissions are paused for the moment. Please try again later.", true)
		return
	}
	name := clean(f.Get("name"), 120)
	summary := cleanMulti(f.Get("summary"), 800)
	schedule := clean(f.Get("schedule"), 160)
	kind := f.Get("kind")
	aud, audOK := filterSet(f["audience"], audiences, 6)
	website, urlOK := validURL(f.Get("website"))
	var msg string
	switch {
	case kind != "group" && kind != "activity" && kind != "place":
		msg = "Choose what you are adding."
	case len(name) < 2:
		msg = "Give it a name."
	case !categories[f.Get("category")]:
		msg = "Choose a category."
	case !towns[f.Get("town")]:
		msg = "Choose a town."
	case !costs[f.Get("cost")]:
		msg = "Choose a cost."
	case len(summary) < 20:
		msg = "A sentence or two, please (20+ characters)."
	case !audOK:
		msg = "Unknown audience."
	case !urlOK:
		msg = "The website must be a full http(s) address."
	}
	if msg != "" {
		a.accountBack(w, r, "/account/promoter/listing", msg, true)
		return
	}
	org := a.orgOf(s.MemberID)
	res, err := a.db.Exec(`INSERT INTO listing_submissions(kind, existing_id, name, category, town, schedule, summary, cost, website, audience, submitter_name, submitter_email, status, created_at, verified_at, ip_hash, member_id)
		VALUES(?,'',?,?,?,?,?,?,?,?,?,?,'pending_review',?,?,?,?)`,
		kind, name, f.Get("category"), f.Get("town"), schedule, summary, f.Get("cost"), website, jsonList(aud), org, s.Member.Email, now(), now(), ipTag(ipOf(r)), s.MemberID)
	if err != nil {
		a.logf("promoter listing: %v", err)
		a.accountBack(w, r, "/account/promoter/listing", "Could not save the listing. Please try again.", true)
		return
	}
	id, _ := res.LastInsertId()
	a.notifyAdminListing(fmt.Sprint(id))
	a.audit(r, "promoter.listing", fmt.Sprint(id), fmt.Sprint(s.MemberID))
	a.accountBack(w, r, "/account/promoter", "Thanks! The listing is in the queue. Listings are added to the directory by hand, so this can take a few days.", false)
}

/* ---------- public feed ---------- */

func (a *App) getPosts(w http.ResponseWriter, r *http.Request) {
	today := a.localDay(time.Now())
	posts := a.livePosts(today)
	if posts == nil {
		posts = []Post{}
	}
	body, _ := json.Marshal(map[string]any{"ok": true, "posts": posts, "generated": now()})
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(body)
}

// promoterOrgs maps member ids to organisation names for the public events
// feed, so a promoted event can say who it is from.
func (a *App) promoterOrgs() map[int64]string {
	rows, err := a.db.Query(`SELECT member_id, org FROM promoters WHERE status = 'approved'`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	m := map[int64]string{}
	for rows.Next() {
		var id int64
		var org string
		if rows.Scan(&id, &org) == nil {
			m[id] = org
		}
	}
	return m
}
