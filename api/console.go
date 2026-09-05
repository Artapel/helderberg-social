package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The console: every page behind requireAdmin, every state change a POST to
// /admin/do carrying the session's CSRF token, every change audited.

const pageSize = 50

type navItem struct{ Path, Label, Icon string }

var consoleNav = []navItem{
	{"/admin", "Dashboard", "◧"}, {"/admin/queue", "Queue", "☐"}, {"/admin/events", "Events", "▤"},
	{"/admin/listings", "Listings", "▥"}, {"/admin/members", "Members", "☺"}, {"/admin/subscribers", "Subscribers", "✉"}, {"/admin/digests", "Digests", "⏰"},
	{"/admin/facebook", "Facebook", "f"}, {"/admin/sources", "Sources", "⇅"}, {"/admin/analytics", "Analytics", "▲"}, {"/admin/logs", "Logs", "≡"},
	{"/admin/security", "Security", "⚿"}, {"/admin/settings", "Settings", "⚙"}, {"/admin/system", "System", "▣"},
}

type view struct {
	Title   string
	Active  string
	Nav     []navItem
	CSRF    string
	Msg     string
	Err     bool
	Version string
	Pending int
	Body    template.HTML
	Data    any
}

func (a *App) registerConsole(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/login", a.loginPage)
	mux.HandleFunc("POST /admin/login", a.loginPost)
	mux.HandleFunc("GET /admin/auth", a.authLink)
	mux.HandleFunc("GET /admin/enrol", a.enrolPage)
	mux.HandleFunc("POST /admin/enrol", a.enrolPost)
	mux.HandleFunc("GET /admin/2fa", a.twofaPage)
	mux.HandleFunc("POST /admin/2fa", a.twofaPost)
	mux.HandleFunc("POST /admin/logout", a.requireAdmin(a.logout))
	mux.HandleFunc("GET /admin", a.requireAdmin(a.dashboard))
	mux.HandleFunc("GET /admin/{$}", a.requireAdmin(a.dashboard))
	mux.HandleFunc("GET /admin/queue", a.requireAdmin(a.queuePage))
	mux.HandleFunc("GET /admin/moderate", a.requireAdmin(a.moderateFromMail))
	mux.HandleFunc("GET /admin/events", a.requireAdmin(a.eventsPage))
	mux.HandleFunc("GET /admin/events/edit", a.requireAdmin(a.eventEditPage))
	mux.HandleFunc("GET /admin/listings", a.requireAdmin(a.listingsPage))
	mux.HandleFunc("GET /admin/members", a.requireAdmin(a.membersPage))
	mux.HandleFunc("GET /admin/members/view", a.requireAdmin(a.memberViewPage))
	mux.HandleFunc("GET /admin/subscribers", a.requireAdmin(a.subscribersPage))
	mux.HandleFunc("GET /admin/subscribers/edit", a.requireAdmin(a.subscriberEditPage))
	mux.HandleFunc("GET /admin/digests", a.requireAdmin(a.digestsPage))
	mux.HandleFunc("GET /admin/facebook", a.requireAdmin(a.facebookPage))
	mux.HandleFunc("GET /admin/facebook/groups", a.requireAdmin(a.groupsPage))
	mux.HandleFunc("GET /admin/sources", a.requireAdmin(a.sourcesPage))
	mux.HandleFunc("GET /admin/analytics", a.requireAdmin(a.analyticsPage))
	mux.HandleFunc("GET /admin/logs", a.requireAdmin(a.logsPage))
	mux.HandleFunc("GET /admin/security", a.requireAdmin(a.securityPage))
	mux.HandleFunc("GET /admin/settings", a.requireAdmin(a.settingsPage))
	mux.HandleFunc("GET /admin/system", a.requireAdmin(a.systemPage))
	mux.HandleFunc("GET /admin/export/subscribers.csv", a.requireAdmin(a.exportSubscribers))
	mux.HandleFunc("GET /admin/export/all.json", a.requireAdmin(a.exportAll))
	mux.HandleFunc("GET /admin/backups/{name}", a.requireAdmin(a.downloadBackup))
	mux.HandleFunc("POST /admin/do", a.requireAdmin(a.consoleAction))
}

/* ---------- rendering ---------- */

func (a *App) renderConsole(w http.ResponseWriter, r *http.Request, name, title string, data any) {
	s := sessionOf(r)
	v := view{Title: title, Active: r.URL.Path, Nav: consoleNav, Version: a.version, Data: data}
	if s != nil {
		v.CSRF = a.csrfToken(s)
	}
	if strings.HasPrefix(v.Active, "/admin/events/") {
		v.Active = "/admin/events"
	}
	if strings.HasPrefix(v.Active, "/admin/members/") {
		v.Active = "/admin/members"
	}
	if strings.HasPrefix(v.Active, "/admin/subscribers/") {
		v.Active = "/admin/subscribers"
	}
	v.Msg = clean(r.URL.Query().Get("msg"), 300)
	v.Err = r.URL.Query().Get("err") == "1"
	v.Pending = a.count(`SELECT COUNT(*) FROM events WHERE status='pending_review'`) + a.count(`SELECT COUNT(*) FROM listing_submissions WHERE status='pending_review'`)
	var buf bytes.Buffer
	if err := a.ctmpl.ExecuteTemplate(&buf, name, map[string]any{"D": data, "CSRF": v.CSRF, "V": v}); err != nil {
		a.logf("console template %s: %v", name, err)
		http.Error(w, "template error", 500)
		return
	}
	v.Body = template.HTML(buf.String())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.ctmpl.ExecuteTemplate(w, "layout", v); err != nil {
		a.logf("console layout: %v", err)
	}
}

// renderAuth draws the sign-in stages, which have no sidebar and no session.
func (a *App) renderAuth(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := a.ctmpl.ExecuteTemplate(w, "auth_"+name, map[string]any{"D": data, "Version": a.version}); err != nil {
		a.logf("auth template %s: %v", name, err)
	}
}

func (a *App) back(w http.ResponseWriter, r *http.Request, to, msg string, isErr bool) {
	u := to
	if msg != "" {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + "msg=" + url.QueryEscape(msg)
		if isErr {
			u += "&err=1"
		}
	}
	http.Redirect(w, r, safeNext(u), http.StatusSeeOther)
}

func pageOf(r *http.Request) (int, int) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if p < 1 {
		p = 1
	}
	return p, (p - 1) * pageSize
}

/* ---------- dashboard ---------- */

type dashData struct {
	PendingEvents, PendingListings, Upcoming, Subs, SubsPending, SourcesOn, Sources int
	Members, MembersPending                                                         int
	MailDay, MailFail, ReqToday, ErrToday, PVToday, UniqToday, Backup, Sessions     int
	Uptime, LastDaily, LastWeekly, LastWatch, Enrolled                              string
	PV                                                                              []dayCount
	Audit                                                                           []auditRow
	Maintenance, Announcement, FBOn                                                 bool
	FBQueued, FBFailed                                                              int
}

type auditRow struct{ At, Action, Target, Detail, IP string }

func (a *App) auditRows(where string, limit int, args ...any) []auditRow {
	args = append(args, limit)
	rows, err := a.db.Query(`SELECT at, action, target, detail, ip_hash FROM audit_log WHERE `+where+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var x auditRow
		if rows.Scan(&x.At, &x.Action, &x.Target, &x.Detail, &x.IP) == nil {
			out = append(out, x)
		}
	}
	return out
}

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	a.flushStats()
	today := a.localDay(time.Now())
	d := dashData{
		PendingEvents:   a.count(`SELECT COUNT(*) FROM events WHERE status='pending_review'`),
		PendingListings: a.count(`SELECT COUNT(*) FROM listing_submissions WHERE status='pending_review'`),
		Upcoming:        a.count(`SELECT COUNT(*) FROM events WHERE status='approved' AND (CASE WHEN end_date='' THEN date ELSE end_date END) >= ?`, today),
		Subs:            a.count(`SELECT COUNT(*) FROM subscribers WHERE confirmed_at IS NOT NULL`),
		SubsPending:     a.count(`SELECT COUNT(*) FROM subscribers WHERE confirmed_at IS NULL`),
		Members:         a.count(`SELECT COUNT(*) FROM members WHERE verified_at IS NOT NULL`),
		MembersPending:  a.count(`SELECT COUNT(*) FROM members WHERE verified_at IS NULL`),
		SourcesOn:       a.count(`SELECT COUNT(*) FROM sources WHERE enabled=1`),
		Sources:         a.count(`SELECT COUNT(*) FROM sources`),
		MailDay:         a.count(`SELECT COUNT(*) FROM mail_log WHERE sent_at > ?`, time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339)),
		MailFail:        a.count(`SELECT COUNT(*) FROM mail_log WHERE ok=0 AND sent_at > ?`, time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339)),
		ReqToday:        a.count(`SELECT COALESCE(SUM(n),0) FROM req_stats WHERE day=?`, today),
		ErrToday:        a.count(`SELECT COALESCE(SUM(n),0) FROM req_stats WHERE day=? AND status>=400`, today),
		PVToday:         a.count(`SELECT COALESCE(SUM(views),0) FROM pageviews WHERE day=?`, today),
		UniqToday:       a.count(`SELECT COUNT(DISTINCT iph) FROM pv_uniques WHERE day=?`, today),
		Backup:          a.backupCodesLeft(),
		Sessions:        len(a.sessions("")),
		Uptime:          fmtDuration(time.Since(a.stats.start)),
		LastDaily:       a.metaGet("last:digest:daily"),
		LastWeekly:      a.metaGet("last:digest:weekly"),
		LastWatch:       a.metaGet("last:watch"),
		Enrolled:        a.metaGet("totp:enrolled_at"),
		PV:              a.pageviewSeries(14),
		Audit:           a.auditRows("1=1", 10),
		Maintenance:     a.settingBool("maintenance"),
		Announcement:    a.settingBool("announcement_on"),
		FBOn:            a.fbEnabled(),
		FBQueued:        a.count(`SELECT COUNT(*) FROM fb_posts WHERE status='queued'`),
		FBFailed:        a.count(`SELECT COUNT(*) FROM fb_posts WHERE status='failed'`),
	}
	a.renderConsole(w, r, "p_dashboard", "Dashboard", d)
}

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, h, m)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

/* ---------- queue ---------- */

type queueData struct {
	Events   []Event
	Listings []listingSub
}

func (a *App) queuePage(w http.ResponseWriter, r *http.Request) {
	var d queueData
	d.Events, _ = a.queryEvents(`status = 'pending_review'`)
	d.Listings, _ = a.listingSubs(`status = 'pending_review'`)
	a.renderConsole(w, r, "p_queue", "Moderation queue", d)
}

// moderateFromMail is the approve/reject link in the notification email.
// It needs a session, so a forwarded email cannot approve anything.
func (a *App) moderateFromMail(w http.ResponseWriter, r *http.Request) {
	p, err := a.consume(r.URL.Query().Get("t"), "moderate")
	if err != nil {
		a.back(w, r, "/admin/queue", "That moderation link is invalid, expired or already used.", true)
		return
	}
	parts := strings.SplitN(p.Subject, "|", 3)
	if len(parts) != 3 {
		a.back(w, r, "/admin/queue", "Malformed link.", true)
		return
	}
	msg, err := a.decide(parts[0], parts[1], parts[2])
	if err != nil {
		a.back(w, r, "/admin/queue", "Error: "+err.Error(), true)
		return
	}
	a.audit(r, "moderate."+parts[2], parts[0]+":"+parts[1], "via email link")
	a.back(w, r, "/admin/queue", msg, false)
}

/* ---------- events ---------- */

type eventsData struct {
	Events                  []Event
	Status, Origin, Town, Q string
	Page, Total, Pages      int
	Statuses, Origins       []string
	Towns                   map[string]string
}

func (a *App) eventsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d := eventsData{Status: q.Get("status"), Origin: q.Get("origin"), Town: q.Get("town"), Q: clean(q.Get("q"), 80),
		Statuses: []string{"pending_review", "approved", "rejected", "pending_email"}, Origins: []string{"user", "auto", "admin", "seed"}, Towns: townNames}
	where, args := "1=1", []any{}
	if d.Status != "" {
		where += " AND status = ?"
		args = append(args, d.Status)
	}
	if d.Origin != "" {
		where += " AND origin = ?"
		args = append(args, d.Origin)
	}
	if d.Town != "" {
		where += " AND town = ?"
		args = append(args, d.Town)
	}
	if d.Q != "" {
		where += " AND (title LIKE ? OR id LIKE ? OR summary LIKE ?)"
		like := "%" + d.Q + "%"
		args = append(args, like, like, like)
	}
	d.Total = a.count(`SELECT COUNT(*) FROM events WHERE `+where, args...)
	var off int
	d.Page, off = pageOf(r)
	d.Pages = (d.Total + pageSize - 1) / pageSize
	rows, err := a.db.Query(`SELECT `+eventCols+` FROM events WHERE `+where+` ORDER BY date DESC, time, title LIMIT ? OFFSET ?`, append(args, pageSize, off)...)
	if err == nil {
		for rows.Next() {
			if e, err := scanEvent(rows); err == nil {
				d.Events = append(d.Events, e)
			}
		}
		rows.Close()
	}
	a.renderConsole(w, r, "p_events", "Events", d)
}

type eventForm struct {
	E                      Event
	New                    bool
	Towns, Cats, Costs     []string
	SubmitterEmail, IPHash string
	Listings               []string
}

func (a *App) eventEditPage(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	f := eventForm{New: id == "", Towns: sortedKeys(towns), Cats: sortedKeys(categories), Costs: sortedKeys(costs)}
	if id != "" {
		evs, _ := a.queryEvents(`id = ?`, id)
		if len(evs) != 1 {
			a.back(w, r, "/admin/events", "No such event.", true)
			return
		}
		f.E = evs[0]
		_ = a.db.QueryRow(`SELECT submitter_email, ip_hash FROM events WHERE id = ?`, id).Scan(&f.SubmitterEmail, &f.IPHash)
	} else {
		f.E = Event{Town: "somerset-west", Category: "community", Cost: "free", Date: a.localDay(time.Now().AddDate(0, 0, 7))}
	}
	a.renderConsole(w, r, "p_event_edit", "Edit event", f)
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// saveEvent applies the same rules as public submission, minus the date
// floor: the admin may back-date to fix a record.
func (a *App) saveEvent(r *http.Request) (string, error) {
	f := r.PostForm
	e := Event{
		ID: strings.TrimSpace(f.Get("id")), Title: clean(f.Get("title"), 120), Date: strings.TrimSpace(f.Get("date")), EndDate: strings.TrimSpace(f.Get("end_date")),
		Time: strings.TrimSpace(f.Get("time")), EndTime: strings.TrimSpace(f.Get("end_time")), Town: f.Get("town"), Category: f.Get("category"),
		Listing: strings.TrimSpace(f.Get("listing")), Summary: cleanMulti(f.Get("summary"), 800), Cost: f.Get("cost"), Status: f.Get("status"),
	}
	website, okURL := validURL(f.Get("website"))
	source, okSrc := validURL(f.Get("source"))
	e.Website, e.Source = website, source
	var ok bool
	switch {
	case len(e.Title) < 4:
		return "", fmt.Errorf("give the event a title")
	case !towns[e.Town], !categories[e.Category], !costs[e.Cost]:
		return "", fmt.Errorf("choose a valid town, category and cost")
	case e.Listing != "" && !slugRe.MatchString(e.Listing):
		return "", fmt.Errorf("bad listing reference")
	case e.Time != "" && !timeRe.MatchString(e.Time), e.EndTime != "" && !timeRe.MatchString(e.EndTime):
		return "", fmt.Errorf("times must look like 18:30")
	case !okURL, !okSrc:
		return "", fmt.Errorf("website and source must be full http(s) addresses")
	case e.Status != "approved" && e.Status != "rejected" && e.Status != "pending_review":
		return "", fmt.Errorf("bad status")
	}
	if _, ok = validDate(e.Date); !ok {
		return "", fmt.Errorf("the date is not a real day")
	}
	if e.EndDate != "" {
		if _, ok = validDate(e.EndDate); !ok || e.EndDate < e.Date {
			return "", fmt.Errorf("the end date must be a real day on or after the start")
		}
	}
	if e.ID == "" {
		e.ID = a.uniqueEventID(slugify(e.Title) + "-" + e.Date)
		e.Origin = "admin"
		if err := a.insertEvent(e, "", "", nil); err != nil {
			return "", err
		}
		_, _ = a.db.Exec(`UPDATE events SET decided_at = ? WHERE id = ?`, now(), e.ID)
		return "Event " + e.ID + " created.", nil
	}
	res, err := a.db.Exec(`UPDATE events SET title=?, date=?, end_date=?, time=?, end_time=?, town=?, category=?, listing=?, summary=?, cost=?, website=?, source=?, status=?, decided_at=COALESCE(decided_at, ?) WHERE id=?`,
		e.Title, e.Date, e.EndDate, e.Time, e.EndTime, e.Town, e.Category, e.Listing, e.Summary, e.Cost, e.Website, e.Source, e.Status, now(), e.ID)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", fmt.Errorf("no such event")
	}
	return "Event " + e.ID + " saved.", nil
}

/* ---------- listings ---------- */

type listingsData struct {
	Listings    []listingSub
	Status      string
	Page, Total int
	Pages       int
}

func (a *App) listingsPage(w http.ResponseWriter, r *http.Request) {
	d := listingsData{Status: r.URL.Query().Get("status")}
	where, args := "1=1", []any{}
	if d.Status != "" {
		where, args = "status = ?", []any{d.Status}
	}
	d.Total = a.count(`SELECT COUNT(*) FROM listing_submissions WHERE `+where, args...)
	var off int
	d.Page, off = pageOf(r)
	d.Pages = (d.Total + pageSize - 1) / pageSize
	d.Listings, _ = a.listingSubs(where+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, pageSize, off)...)
	a.renderConsole(w, r, "p_listings", "Listing submissions", d)
}

/* ---------- subscribers ---------- */

type subRow struct {
	PhonePretty string
	Subscriber
	CreatedAt, ConfirmedAt, LastSent string
}

type subsData struct {
	Subs               []subRow
	Filter, Q          string
	Page, Total, Pages int
	Confirmed, Pending int
	Daily, Weekly      int
}

func (a *App) subscriberRows(where string, args ...any) []subRow {
	rows, err := a.db.Query(`SELECT id, COALESCE(email,''), COALESCE(phone,''), channel, frequency, horizon, towns, categories, confirmed_at IS NOT NULL, created_at, COALESCE(confirmed_at,''), COALESCE(last_sent_at,'') FROM subscribers WHERE `+where, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []subRow
	for rows.Next() {
		var s subRow
		var t, c string
		if rows.Scan(&s.ID, &s.Email, &s.Phone, &s.Channel, &s.Frequency, &s.Horizon, &t, &c, &s.Confirmed, &s.CreatedAt, &s.ConfirmedAt, &s.LastSent) == nil {
			if s.Phone != "" {
				s.PhonePretty = prettyPhone(s.Phone)
			}
			_ = json.Unmarshal([]byte(t), &s.Towns)
			_ = json.Unmarshal([]byte(c), &s.Categories)
			out = append(out, s)
		}
	}
	return out
}

func (a *App) subscribersPage(w http.ResponseWriter, r *http.Request) {
	d := subsData{Filter: r.URL.Query().Get("f"), Q: clean(r.URL.Query().Get("q"), 80)}
	where, args := "1=1", []any{}
	switch d.Filter {
	case "confirmed":
		where = "confirmed_at IS NOT NULL"
	case "pending":
		where = "confirmed_at IS NULL"
	case "daily", "weekly":
		where = "frequency = ?"
		args = append(args, d.Filter)
	case "email", "whatsapp":
		where = "channel = ?"
		args = append(args, d.Filter)
	}
	if d.Q != "" {
		where += " AND (COALESCE(email,'') LIKE ? OR COALESCE(phone,'') LIKE ?)"
		args = append(args, "%"+d.Q+"%", "%"+strings.TrimPrefix(d.Q, "+")+"%")
	}
	d.Total = a.count(`SELECT COUNT(*) FROM subscribers WHERE `+where, args...)
	var off int
	d.Page, off = pageOf(r)
	d.Pages = (d.Total + pageSize - 1) / pageSize
	d.Subs = a.subscriberRows(where+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, pageSize, off)...)
	d.Confirmed = a.count(`SELECT COUNT(*) FROM subscribers WHERE confirmed_at IS NOT NULL`)
	d.Pending = a.count(`SELECT COUNT(*) FROM subscribers WHERE confirmed_at IS NULL`)
	d.Daily = a.count(`SELECT COUNT(*) FROM subscribers WHERE confirmed_at IS NOT NULL AND frequency='daily'`)
	d.Weekly = a.count(`SELECT COUNT(*) FROM subscribers WHERE confirmed_at IS NOT NULL AND frequency='weekly'`)
	a.renderConsole(w, r, "p_subscribers", "Subscribers", d)
}

type subForm struct {
	S           subRow
	Towns, Cats []string
	TownSet     map[string]bool
	CatSet      map[string]bool
}

func (a *App) subscriberEditPage(w http.ResponseWriter, r *http.Request) {
	rows := a.subscriberRows(`id = ?`, r.URL.Query().Get("id"))
	if len(rows) != 1 {
		a.back(w, r, "/admin/subscribers", "No such subscriber.", true)
		return
	}
	f := subForm{S: rows[0], Towns: sortedKeys(towns), Cats: sortedKeys(categories), TownSet: set(rows[0].Towns...), CatSet: set(rows[0].Categories...)}
	a.renderConsole(w, r, "p_subscriber_edit", "Edit subscriber", f)
}

func (a *App) exportSubscribers(w http.ResponseWriter, r *http.Request) {
	a.audit(r, "export.subscribers", "", "")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="subscribers-`+a.localDay(time.Now())+`.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"email", "phone", "channel", "frequency", "horizon", "towns", "categories", "confirmed_at", "created_at", "last_sent_at"})
	for _, s := range a.subscriberRows(`1=1 ORDER BY id`) {
		_ = cw.Write([]string{csvSafe(s.Email), csvSafe(s.PhonePretty), s.Channel, s.Frequency, fmt.Sprint(s.Horizon), strings.Join(s.Towns, " "), strings.Join(s.Categories, " "), s.ConfirmedAt, s.CreatedAt, s.LastSent})
	}
	cw.Flush()
}

// csvSafe defuses spreadsheet formula injection in exported cells.
func csvSafe(s string) string {
	if s != "" && strings.ContainsAny(s[:1], "=+-@\t\r") {
		return "'" + s
	}
	return s
}

/* ---------- digests ---------- */

type digestData struct {
	LastDaily, LastWeekly string
	Hour                  int
	Day                   string
	On                    bool
	Daily, Weekly         int
	NextDaily, NextWeekly string
	History               []mailDayRow
	Upcoming7, Upcoming30 int
}

type mailDayRow struct {
	Day           string
	Daily, Weekly int
	Failed        int
}

func (a *App) digestsPage(w http.ResponseWriter, r *http.Request) {
	today := time.Now().In(a.cfg.TZ)
	d := digestData{LastDaily: a.metaGet("last:digest:daily"), LastWeekly: a.metaGet("last:digest:weekly"), Hour: a.digestHour(), Day: a.weeklyDay().String(), On: a.settingBool("digests_on"),
		Daily:      a.count(`SELECT COUNT(*) FROM subscribers WHERE confirmed_at IS NOT NULL AND frequency='daily'`),
		Weekly:     a.count(`SELECT COUNT(*) FROM subscribers WHERE confirmed_at IS NOT NULL AND frequency='weekly'`),
		Upcoming7:  a.count(`SELECT COUNT(*) FROM events WHERE status='approved' AND date >= ? AND date <= ?`, a.localDay(today), a.localDay(today.AddDate(0, 0, 7))),
		Upcoming30: a.count(`SELECT COUNT(*) FROM events WHERE status='approved' AND date >= ? AND date <= ?`, a.localDay(today), a.localDay(today.AddDate(0, 0, 30))),
	}
	next := time.Date(today.Year(), today.Month(), today.Day(), d.Hour, 0, 0, 0, a.cfg.TZ)
	if !next.After(today) || a.metaGet("last:digest:daily") == a.localDay(today) {
		next = next.AddDate(0, 0, 1)
	}
	d.NextDaily = next.Format("Mon 02 Jan 15:04")
	nw := next
	for nw.Weekday() != a.weeklyDay() {
		nw = nw.AddDate(0, 0, 1)
	}
	d.NextWeekly = nw.Format("Mon 02 Jan 15:04")
	rows, err := a.db.Query(`SELECT substr(sent_at,1,10) AS d, SUM(kind='digest-daily' AND ok=1), SUM(kind='digest-weekly' AND ok=1), SUM(kind LIKE 'digest-%' AND ok=0) FROM mail_log WHERE kind LIKE 'digest-%' GROUP BY d ORDER BY d DESC LIMIT 30`)
	if err == nil {
		for rows.Next() {
			var m mailDayRow
			if rows.Scan(&m.Day, &m.Daily, &m.Weekly, &m.Failed) == nil {
				d.History = append(d.History, m)
			}
		}
		rows.Close()
	}
	a.renderConsole(w, r, "p_digests", "Digests", d)
}

/* ---------- sources ---------- */

type sourceFull struct {
	ID                                               int64
	URL, Kind, Label, Listing, Category, Town, Match string
	Enabled                                          bool
	Checked, Hash, Status, Changed                   string
	Events                                           int
}

type sourcesData struct {
	Sources     []sourceFull
	Kinds       []string
	Towns, Cats []string
	LastWatch   string
	On          bool
	Interval    int
}

func (a *App) sourcesPage(w http.ResponseWriter, r *http.Request) {
	d := sourcesData{Kinds: []string{"ics", "html", "list"}, Towns: sortedKeys(towns), Cats: sortedKeys(categories), LastWatch: a.metaGet("last:watch"), On: a.settingBool("watch_on"), Interval: a.settingInt("watch_minutes")}
	rows, err := a.db.Query(`SELECT s.id, s.url, s.kind, s.label, s.listing, s.category, s.town, s.match, s.enabled, COALESCE(s.last_checked_at,''), s.last_hash, s.last_status, COALESCE(s.last_changed_at,''), (SELECT COUNT(*) FROM events e WHERE e.source_id = s.id) FROM sources s ORDER BY s.enabled DESC, s.label`)
	if err == nil {
		for rows.Next() {
			var s sourceFull
			var en int
			if rows.Scan(&s.ID, &s.URL, &s.Kind, &s.Label, &s.Listing, &s.Category, &s.Town, &s.Match, &en, &s.Checked, &s.Hash, &s.Status, &s.Changed, &s.Events) == nil {
				s.Enabled = en == 1
				if len(s.Hash) > 10 {
					s.Hash = s.Hash[:10]
				}
				d.Sources = append(d.Sources, s)
			}
		}
		rows.Close()
	}
	a.renderConsole(w, r, "p_sources", "Watched sources", d)
}

func (a *App) saveSource(f url.Values) (string, error) {
	u, ok := validURL(f.Get("url"))
	label := clean(f.Get("label"), 80)
	listing := strings.TrimSpace(f.Get("listing"))
	kind, cat, town := f.Get("kind"), f.Get("category"), f.Get("town")
	match := strings.TrimSpace(f.Get("match"))
	_, matchErr := compileMatch(match)
	switch {
	case !ok || u == "":
		return "", fmt.Errorf("the source needs a full http(s) address")
	case matchErr != nil:
		return "", fmt.Errorf("the filter is not a valid pattern: %v", matchErr)
	case !sourceKinds[kind]:
		return "", fmt.Errorf("kind must be ics, html or list")
	case len(label) < 2:
		return "", fmt.Errorf("give the source a label")
	case !categories[cat], !towns[town]:
		return "", fmt.Errorf("choose a valid category and town")
	case listing != "" && !slugRe.MatchString(listing):
		return "", fmt.Errorf("bad listing reference")
	}
	if id := f.Get("id"); id != "" {
		_, err := a.db.Exec(`UPDATE sources SET url=?, kind=?, label=?, listing=?, category=?, town=?, match=? WHERE id=?`, u, kind, label, listing, cat, town, match, id)
		return "Source saved.", err
	}
	_, err := a.db.Exec(`INSERT INTO sources(url, kind, label, listing, category, town, match) VALUES(?,?,?,?,?,?,?)`, u, kind, label, listing, cat, town, match)
	return "Source added. It will be checked on the next run.", err
}

/* ---------- analytics ---------- */

type analyticsData struct {
	Days               int
	PV                 []dayCount
	Req                []dayCount
	Top                []kv
	Routes             []kv
	SubGrowth          []dayCount
	Towns, Cats        []kv
	TotalPV, TotalUniq int
	TotalReq, TotalErr int
	MaxPV, MaxReq      int
}

func (a *App) analyticsPage(w http.ResponseWriter, r *http.Request) {
	a.flushStats()
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days != 7 && days != 30 && days != 90 {
		days = 30
	}
	d := analyticsData{Days: days, PV: a.pageviewSeries(days), Req: a.requestSeries(days), Top: a.topPages(days, 25), Routes: a.routeStats(days)}
	for _, x := range d.PV {
		d.TotalPV += x.N
		d.TotalUniq += x.N2
		if x.N > d.MaxPV {
			d.MaxPV = x.N
		}
	}
	for _, x := range d.Req {
		d.TotalReq += x.N
		d.TotalErr += x.N2
		if x.N > d.MaxReq {
			d.MaxReq = x.N
		}
	}
	from := time.Now().UTC().AddDate(0, 0, -days+1).Format("2006-01-02")
	rows, err := a.db.Query(`SELECT substr(confirmed_at,1,10) AS d, COUNT(*) FROM subscribers WHERE confirmed_at >= ? GROUP BY d ORDER BY d`, from)
	if err == nil {
		for rows.Next() {
			var x dayCount
			if rows.Scan(&x.Day, &x.N) == nil {
				d.SubGrowth = append(d.SubGrowth, x)
			}
		}
		rows.Close()
	}
	d.Towns = a.groupCount(`SELECT town, COUNT(*) FROM events WHERE status='approved' GROUP BY town ORDER BY 2 DESC`)
	d.Cats = a.groupCount(`SELECT category, COUNT(*) FROM events WHERE status='approved' GROUP BY category ORDER BY 2 DESC`)
	a.renderConsole(w, r, "p_analytics", "Analytics", d)
}

func (a *App) groupCount(q string) []kv {
	rows, err := a.db.Query(q)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []kv
	for rows.Next() {
		var k kv
		if rows.Scan(&k.K, &k.N) == nil {
			out = append(out, k)
		}
	}
	return out
}

/* ---------- logs ---------- */

type mailLogRow struct {
	ID               int64
	ToHash, Kind, At string
	OK               bool
	Err              string
}

type logsData struct {
	Tab      string
	Reqs     []reqEntry
	Lines    []string
	Mail     []mailLogRow
	Audit    []auditRow
	OnlyFail bool
}

func (a *App) logsPage(w http.ResponseWriter, r *http.Request) {
	d := logsData{Tab: r.URL.Query().Get("tab"), OnlyFail: r.URL.Query().Get("fail") == "1"}
	switch d.Tab {
	case "mail":
		where := "1=1"
		if d.OnlyFail {
			where = "ok = 0"
		}
		rows, err := a.db.Query(`SELECT id, to_hash, kind, sent_at, ok, err FROM mail_log WHERE ` + where + ` ORDER BY id DESC LIMIT 200`)
		if err == nil {
			for rows.Next() {
				var m mailLogRow
				var ok int
				if rows.Scan(&m.ID, &m.ToHash, &m.Kind, &m.At, &ok, &m.Err) == nil {
					m.OK = ok == 1
					if len(m.ToHash) > 10 {
						m.ToHash = m.ToHash[:10]
					}
					d.Mail = append(d.Mail, m)
				}
			}
			rows.Close()
		}
	case "audit":
		d.Audit = a.auditRows("1=1", 200)
	case "app":
		_, d.Lines = a.stats.recent()
		for i, j := 0, len(d.Lines)-1; i < j; i, j = i+1, j-1 {
			d.Lines[i], d.Lines[j] = d.Lines[j], d.Lines[i]
		}
	default:
		d.Tab = "requests"
		d.Reqs, _ = a.stats.recent()
		for i, j := 0, len(d.Reqs)-1; i < j; i, j = i+1, j-1 {
			d.Reqs[i], d.Reqs[j] = d.Reqs[j], d.Reqs[i]
		}
	}
	a.renderConsole(w, r, "p_logs", "Logs", d)
}

/* ---------- security ---------- */

type blockRow struct {
	ID                           int64
	Kind, Value, Note, CreatedAt string
}

type securityData struct {
	Enrolled, EnrolledAt string
	BackupLeft           int
	Sessions             []session
	Logins               []auditRow
	Blocks               []blockRow
	NewCodes             []string
	Secure               bool
	AdminEmail           string
}

func (a *App) securityPage(w http.ResponseWriter, r *http.Request) {
	s := sessionOf(r)
	d := securityData{EnrolledAt: a.metaGet("totp:enrolled_at"), BackupLeft: a.backupCodesLeft(), Sessions: a.sessions(s.Hash),
		Logins: a.auditRows(`action LIKE 'login.%' OR action LIKE 'enrol.%' OR action = 'logout' OR action = 'csrf.reject'`, 40), Secure: a.secureCookies(), AdminEmail: a.cfg.AdminEmail}
	if a.totpEnrolled() {
		d.Enrolled = "yes"
	}
	rows, err := a.db.Query(`SELECT id, kind, value, note, created_at FROM blocklist ORDER BY id DESC`)
	if err == nil {
		for rows.Next() {
			var b blockRow
			if rows.Scan(&b.ID, &b.Kind, &b.Value, &b.Note, &b.CreatedAt) == nil {
				d.Blocks = append(d.Blocks, b)
			}
		}
		rows.Close()
	}
	// Freshly generated backup codes are handed over exactly once via a
	// short-lived sealed cookie, never via the URL.
	if c := cookieVal(r, "hs_codes"); c != "" {
		if b, err := a.open("codes", c); err == nil {
			_ = json.Unmarshal(b, &d.NewCodes)
		}
		a.setCookie(w, "hs_codes", "", 0)
	}
	a.renderConsole(w, r, "p_security", "Security", d)
}

/* ---------- settings ---------- */

type settingRow struct {
	settingDef
	Value, Default string
}

type settingsData struct {
	Rows []settingRow
	Env  [][2]string
	Days []string
}

func (a *App) settingsPage(w http.ResponseWriter, r *http.Request) {
	d := settingsData{Days: []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}}
	for _, def := range settingDefs {
		d.Rows = append(d.Rows, settingRow{settingDef: def, Value: a.setting(def.Key), Default: a.settingDefault(def.Key)})
	}
	c := a.cfg
	d.Env = [][2]string{{"HS_LISTEN", c.Listen}, {"HS_DATA_DIR", c.DataDir}, {"HS_SITE_URL", c.SiteURL}, {"HS_API_URL", c.APIURL}, {"HS_ADMIN_EMAIL", c.AdminEmail},
		{"HS_MAIL_FROM", c.MailFrom}, {"HS_SMTP_HOST", c.SMTPHost}, {"HS_SMTP_PORT", fmt.Sprint(c.SMTPPort)}, {"HS_SMTP_USER", c.SMTPUser}, {"HS_SMTP_PASS", mask(c.SMTPPass)},
		{"HS_SECRET", mask(string(c.Secret))}, {"HS_FB_PAGE_ID", c.FBPageID}, {"HS_FB_PAGE_TOKEN", mask(c.FBToken)}, {"HS_DEV_MAIL_DIR", c.DevMailDir}, {"HS_TZ", c.TZ.String()}, {"HS_TRUST_PROXY", fmt.Sprint(c.TrustProxy)},
		{"HS_DIGEST_HOUR (default)", fmt.Sprint(c.DigestHour)}, {"HS_WEEKLY_DAY (default)", c.WeeklyDay.String()}, {"HS_WATCH_INTERVAL (default)", c.WatchInterval.String()}}
	a.renderConsole(w, r, "p_settings", "Settings", d)
}

func mask(s string) string {
	if s == "" {
		return "(not set)"
	}
	return fmt.Sprintf("•••• (%d chars)", len(s))
}

/* ---------- system ---------- */

type backupFile struct {
	Name, Size, At string
}

type systemData struct {
	Version, Go, Uptime, Started, DBPath, DBSize, WALSize, Schema string
	Tables                                                        []kv
	Backups                                                       []backupFile
	Integrity                                                     string
	LastHousekeeping                                              string
	Goroutines                                                    int
	Mem                                                           string
	MailMode, MailFrom, MailHelo                                  string
	MailRecords                                                   []mailRecord
	MailOK                                                        bool
	WAOn                                                          bool
	WAPhoneID, WAVersion, WALang, WAWebhook, WATemplateNote       string
	WATemplates                                                   map[string]string
	WAAdminPhone                                                  string
}

var backupNameRe = regexp.MustCompile(`^helderberg-\d{8}-\d{6}\.sqlite$`)

func (a *App) backupDir() string { return filepath.Join(a.cfg.DataDir, "backups") }

func (a *App) systemPage(w http.ResponseWriter, r *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	d := systemData{Version: a.version, Go: runtime.Version(), Uptime: fmtDuration(time.Since(a.stats.start)), Started: a.stats.start.UTC().Format(time.RFC3339),
		DBPath: filepath.Join(a.cfg.DataDir, "helderberg.sqlite"), Schema: a.metaGet("schema_version"), LastHousekeeping: a.metaGet("last:housekeeping"),
		Goroutines: runtime.NumGoroutine(), Mem: fmtBytes(int64(ms.Alloc)), Integrity: clean(r.URL.Query().Get("integrity"), 200)}
	if st, err := os.Stat(d.DBPath); err == nil {
		d.DBSize = fmtBytes(st.Size())
	}
	d.MailMode, d.MailFrom, d.MailHelo = mailMode(a.cfg), a.cfg.MailFrom, a.cfg.MailHelo
	d.WAOn = a.waEnabled()
	if d.WAOn {
		d.WAPhoneID, d.WAVersion, d.WALang = a.wa.phoneID, a.wa.version, a.wa.lang
		d.WAWebhook = a.cfg.APIURL + "/api/wa/webhook"
		d.WAAdminPhone = a.cfg.AdminPhone
		if st, err := a.wa.templateStatus(); err != nil {
			d.WATemplateNote = "template status not checked: " + err.Error()
			d.WATemplates = map[string]string{a.wa.tConfirm: "?", a.wa.tDigest: "?"}
		} else {
			d.WATemplates = map[string]string{a.wa.tConfirm: st[a.wa.tConfirm], a.wa.tDigest: st[a.wa.tDigest]}
			for k, v := range d.WATemplates {
				if v == "" {
					d.WATemplates[k] = "MISSING: create it in Meta Business Suite"
				}
			}
		}
	}
	d.MailRecords = a.mailRecords()
	d.MailOK = true
	for _, m := range d.MailRecords {
		if !m.OK {
			d.MailOK = false
		}
	}
	if st, err := os.Stat(d.DBPath + "-wal"); err == nil {
		d.WALSize = fmtBytes(st.Size())
	}
	for _, t := range []string{"events", "subscribers", "listing_submissions", "sources", "seen_uids", "tokens_used", "mail_log", "sessions", "audit_log", "req_stats", "pageviews", "pv_uniques", "blocklist", "meta"} {
		d.Tables = append(d.Tables, kv{K: t, N: a.count(`SELECT COUNT(*) FROM ` + t)})
	}
	if entries, err := os.ReadDir(a.backupDir()); err == nil {
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			if info, err := e.Info(); err == nil && backupNameRe.MatchString(e.Name()) {
				d.Backups = append(d.Backups, backupFile{Name: e.Name(), Size: fmtBytes(info.Size()), At: info.ModTime().UTC().Format(time.RFC3339)})
			}
		}
	}
	a.renderConsole(w, r, "p_system", "System", d)
}

func fmtBytes(n int64) string {
	switch {
	case n > 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n > 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// backupNow snapshots the database with VACUUM INTO, which is consistent
// even while the service is writing.
func (a *App) backupNow() (string, error) {
	if err := os.MkdirAll(a.backupDir(), 0o750); err != nil {
		return "", err
	}
	name := "helderberg-" + time.Now().UTC().Format("20060102-150405") + ".sqlite"
	path := filepath.Join(a.backupDir(), name)
	if _, err := a.db.Exec(`VACUUM INTO ?`, path); err != nil {
		return "", err
	}
	// Keep the newest 14.
	if entries, err := os.ReadDir(a.backupDir()); err == nil {
		var names []string
		for _, e := range entries {
			if backupNameRe.MatchString(e.Name()) {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for len(names) > 14 {
			_ = os.Remove(filepath.Join(a.backupDir(), names[0]))
			names = names[1:]
		}
	}
	return name, nil
}

func (a *App) downloadBackup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !backupNameRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	a.audit(r, "backup.download", name, "")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, filepath.Join(a.backupDir(), name))
}

func (a *App) exportAll(w http.ResponseWriter, r *http.Request) {
	a.audit(r, "export.all", "", "")
	evs, _ := a.queryEvents(`1=1`)
	subs := a.subscriberRows(`1=1 ORDER BY id`)
	lst, _ := a.listingSubs(`1=1`)
	settings := map[string]string{}
	for _, d := range settingDefs {
		settings[d.Key] = a.setting(d.Key)
	}
	type evFull struct {
		Event
		Status, Origin, SubmitterName, CreatedAt string
	}
	full := make([]evFull, len(evs))
	for i, e := range evs {
		full[i] = evFull{e, e.Status, e.Origin, e.SubmitterName, e.CreatedAt}
	}
	var srcs []map[string]any
	rows, err := a.db.Query(`SELECT id, url, kind, label, listing, category, town, match, enabled FROM sources`)
	if err == nil {
		for rows.Next() {
			var id int64
			var u, k, l, li, c, t, m string
			var en int
			if rows.Scan(&id, &u, &k, &l, &li, &c, &t, &m, &en) == nil {
				srcs = append(srcs, map[string]any{"id": id, "url": u, "kind": k, "label": l, "listing": li, "category": c, "town": t, "match": m, "enabled": en == 1})
			}
		}
		rows.Close()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="helderberg-export-`+a.localDay(time.Now())+`.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", " ")
	_ = enc.Encode(map[string]any{"exported": now(), "version": a.version, "events": full, "subscribers": subs, "listingSubmissions": lst, "sources": srcs, "settings": settings})
}

/* ---------- actions ---------- */

// consoleAction is the single POST endpoint. requireAdmin has already
// verified the session and the CSRF token by the time we get here.
func (a *App) consoleAction(w http.ResponseWriter, r *http.Request) {
	f := r.PostForm
	action := f.Get("action")
	id := strings.TrimSpace(f.Get("id"))
	ret := safeNext(f.Get("return"))
	s := sessionOf(r)
	var msg string
	var err error
	switch action {
	// moderation
	case "approve", "reject":
		msg, err = a.decide("event", id, action)
		a.audit(r, "event."+action, id, "")
	case "accept", "reject-listing":
		msg, err = a.decide("listing", id, strings.TrimSuffix(action, "-listing"))
		a.audit(r, "listing."+strings.TrimSuffix(action, "-listing"), id, "")
	// events
	case "event-save":
		msg, err = a.saveEvent(r)
		a.audit(r, "event.save", f.Get("id"), msg)
		if err == nil && f.Get("id") == "" {
			ret = "/admin/events?status=approved"
		}
	case "event-delete":
		_, err = a.db.Exec(`DELETE FROM events WHERE id = ?`, id)
		a.fbCancelRef("event", id)
		msg = "Event deleted."
		a.audit(r, "event.delete", id, "")
		ret = "/admin/events"
	case "event-unapprove":
		_, err = a.db.Exec(`UPDATE events SET status='pending_review' WHERE id = ?`, id)
		a.fbCancelRef("event", id)
		msg = "Event sent back to the queue."
		a.audit(r, "event.unapprove", id, "")
	case "listing-delete":
		_, err = a.db.Exec(`DELETE FROM listing_submissions WHERE id = ?`, id)
		msg = "Submission deleted."
		a.audit(r, "listing.delete", id, "")
	// subscribers
	case "sub-confirm":
		_, err = a.db.Exec(`UPDATE subscribers SET confirmed_at = COALESCE(confirmed_at, ?) WHERE id = ?`, now(), id)
		msg = "Subscriber confirmed."
		a.audit(r, "subscriber.confirm", id, "")
	case "sub-resend":
		subs := a.subscriberRows(`id = ?`, id)
		if len(subs) == 1 && subs[0].Channel == "whatsapp" {
			if a.waEnabled() {
				err = a.waConfirm(subs[0].Phone, subs[0].Frequency, subs[0].Horizon)
				msg = "Confirmation message re-sent on WhatsApp."
			} else {
				err = fmt.Errorf("WhatsApp is not configured")
			}
		} else if len(subs) == 1 {
			link := a.cfg.APIURL + "/api/confirm?t=" + a.sign("confirm", fmt.Sprint(subs[0].ID), 72*time.Hour)
			err = a.send(Message{To: subs[0].Email, Kind: "confirm", Subject: "Confirm your Helderberg Social updates",
				Text: a.textConfirm(subs[0].Frequency, subs[0].Horizon, link), HTML: a.htmlConfirm(subs[0].Frequency, subs[0].Horizon, link)})
			msg = "Confirmation email re-sent."
		} else {
			err = fmt.Errorf("no such subscriber")
		}
		a.audit(r, "subscriber.resend", id, "")
	// members
	case "member-disable", "member-enable", "member-verify", "member-delete", "member-block", "member-resend", "member-signout":
		msg, err = a.memberAction(r, action, id)
		if action == "member-delete" || action == "member-block" {
			ret = "/admin/members"
		}
	case "sub-delete":
		_, err = a.db.Exec(`DELETE FROM subscribers WHERE id = ?`, id)
		msg = "Subscriber removed."
		a.audit(r, "subscriber.delete", id, "")
		ret = "/admin/subscribers"
	case "sub-block":
		var email, phone string
		_ = a.db.QueryRow(`SELECT COALESCE(email,''), COALESCE(phone,'') FROM subscribers WHERE id = ?`, id).Scan(&email, &phone)
		if email != "" {
			err = a.addBlock("email", emailHash(email), "blocked from subscriber "+id)
		} else if phone != "" {
			err = a.addBlock("email", emailHash("tel:"+phone), "blocked phone from subscriber "+id)
		}
		if email != "" || phone != "" {
			_, _ = a.db.Exec(`DELETE FROM subscribers WHERE id = ?`, id)
		}
		msg = "Address blocked and removed."
		a.audit(r, "subscriber.block", id, "")
		ret = "/admin/subscribers"
	case "sub-save":
		freq, hz := f.Get("frequency"), f.Get("horizon")
		tw, ok1 := filterSet(f["towns"], towns, 4)
		ct, ok2 := filterSet(f["categories"], categories, len(categories))
		if (freq != "daily" && freq != "weekly") || (hz != "7" && hz != "14" && hz != "30") || !ok1 || !ok2 {
			err = fmt.Errorf("bad preferences")
		} else {
			_, err = a.db.Exec(`UPDATE subscribers SET frequency=?, horizon=?, towns=?, categories=? WHERE id=?`, freq, hz, jsonList(tw), jsonList(ct), id)
			msg = "Preferences saved."
		}
		a.audit(r, "subscriber.save", id, "")
	case "sub-add":
		email := normEmail(f.Get("email"))
		if phone, ok := normPhone(f.Get("email")); ok && !strings.Contains(email, "@") {
			// A phone number in the box adds a WhatsApp subscriber instead.
			_, err = a.db.Exec(`INSERT INTO subscribers(phone, channel, frequency, horizon, towns, categories, created_at, confirmed_at, ip_hash) VALUES(?,'whatsapp',?,7,'[]','[]',?,?,'admin')`, phone, "weekly", now(), now())
			msg = "WhatsApp subscriber added as confirmed (weekly, 7 days). They can reply STOP to any digest."
			email = "tel:" + phone
		} else if !validEmail(email) {
			err = fmt.Errorf("that email address or phone number does not look right")
		} else {
			_, err = a.db.Exec(`INSERT INTO subscribers(email, frequency, horizon, towns, categories, created_at, confirmed_at, ip_hash) VALUES(?,?,7,'[]','[]',?,?,'admin')`, email, "weekly", now(), now())
			msg = "Subscriber added as confirmed (weekly, 7 days). They can unsubscribe from any digest."
		}
		a.audit(r, "subscriber.add", emailHash(email), "")
	// digests
	case "test-whatsapp":
		if !a.waEnabled() {
			err = fmt.Errorf("WhatsApp is not configured")
		} else if a.cfg.AdminPhone == "" {
			err = fmt.Errorf("set HS_ADMIN_PHONE to receive the test")
		} else {
			err = a.waConfirm(a.cfg.AdminPhone, "weekly", 7)
			msg = "Test sent: the confirm template went to " + prettyPhone(a.cfg.AdminPhone) + ". Tapping Confirm there does nothing unless that number has a pending subscription."
		}
		a.audit(r, "whatsapp.test", "", "")
	case "digest-preview":
		freq := f.Get("freq")
		if freq != "daily" && freq != "weekly" {
			freq = "weekly"
		}
		var n int
		n, err = a.runDigest(freq, true)
		msg = fmt.Sprintf("Preview %s digest sent to %s (%d events).", freq, a.cfg.AdminEmail, n)
		a.audit(r, "digest.preview", freq, "")
	case "digest-send":
		freq := f.Get("freq")
		if f.Get("confirm") != "yes" {
			err = fmt.Errorf("tick the confirmation box to send a real digest")
			break
		}
		if freq != "daily" && freq != "weekly" {
			err = fmt.Errorf("bad frequency")
			break
		}
		var n int
		n, err = a.runDigest(freq, false)
		_ = a.metaSet("last:digest:"+freq, a.localDay(time.Now()))
		msg = fmt.Sprintf("%s digest sent to %d subscribers.", strings.Title(freq), n)
		a.audit(r, "digest.send", freq, fmt.Sprint(n))
	// sources
	case "watch":
		msg = a.runWatch("manual")
		a.audit(r, "sources.check", "", msg)
	case "watch-one":
		msg = a.runWatchOne(id)
		a.audit(r, "sources.check", id, msg)
	case "source-save":
		msg, err = a.saveSource(f)
		a.audit(r, "source.save", f.Get("id"), f.Get("url"))
	case "source-toggle":
		_, err = a.db.Exec(`UPDATE sources SET enabled = 1 - enabled WHERE id = ?`, id)
		msg = "Source toggled."
		a.audit(r, "source.toggle", id, "")
	case "source-delete":
		_, err = a.db.Exec(`DELETE FROM sources WHERE id = ?`, id)
		_, _ = a.db.Exec(`DELETE FROM seen_uids WHERE source_id = ?`, id)
		msg = "Source deleted. If it came from sources.json it returns on the next restart; disable it instead."
		a.audit(r, "source.delete", id, "")
	case "source-forget":
		_, err = a.db.Exec(`UPDATE sources SET last_hash = '', last_status = '' WHERE id = ?`, id)
		_, _ = a.db.Exec(`DELETE FROM seen_uids WHERE source_id = ?`, id)
		msg = "Source memory cleared: the next check treats everything as new."
		a.audit(r, "source.forget", id, "")
	// security
	case "session-revoke":
		if id == s.Hash {
			err = fmt.Errorf("use Sign out for the current session")
			break
		}
		a.revokeSession(id)
		msg = "Session revoked."
		a.audit(r, "session.revoke", id[:8], "")
	case "session-revoke-others":
		_, err = a.db.Exec(`UPDATE sessions SET revoked = 1 WHERE id_hash <> ?`, s.Hash)
		msg = "Every other session is signed out."
		a.audit(r, "session.revoke_others", "", "")
	case "backup-codes":
		if _, ok := a.checkSecondFactor(f.Get("code")); !ok {
			err = fmt.Errorf("enter a current authenticator code to regenerate backup codes")
			a.audit(r, "backup_codes.refused", "", "")
			break
		}
		var codes []string
		codes, err = a.newBackupCodes()
		if err == nil {
			j, _ := json.Marshal(codes)
			sealed, _ := a.seal("codes", j)
			a.setCookie(w, "hs_codes", sealed, 2*time.Minute)
			msg = "New backup codes generated. Write them down now: they are shown once."
		}
		a.audit(r, "backup_codes.regenerate", "", "")
	case "totp-reset":
		if _, ok := a.checkSecondFactor(f.Get("code")); !ok {
			err = fmt.Errorf("enter a current authenticator code to reset the authenticator")
			a.audit(r, "totp.reset_refused", "", "")
			break
		}
		a.totpReset()
		_, _ = a.db.Exec(`UPDATE sessions SET revoked = 1`)
		a.audit(r, "totp.reset", "", "all sessions revoked")
		a.setCookie(w, cookieSession, "", 0)
		http.Redirect(w, r, "/admin/login?msg="+url.QueryEscape("Authenticator removed. Sign in again to enrol a new one."), http.StatusSeeOther)
		return
	case "block-add":
		kind, value := f.Get("kind"), strings.TrimSpace(f.Get("value"))
		switch kind {
		case "email":
			if !validEmail(normEmail(value)) {
				err = fmt.Errorf("that is not an email address")
			} else {
				err = a.addBlock("email", emailHash(normEmail(value)), f.Get("note"))
			}
		case "ip":
			if !strings.HasPrefix(value, "ip:") || len(value) != 11 {
				err = fmt.Errorf("enter the ip:xxxxxxxx tag exactly as shown in the logs")
			} else {
				err = a.addBlock("ip", value, f.Get("note"))
			}
		default:
			err = fmt.Errorf("bad kind")
		}
		msg = "Blocked."
		a.audit(r, "block.add", kind, f.Get("note"))
	case "block-remove":
		a.removeBlock(id)
		msg = "Unblocked."
		a.audit(r, "block.remove", id, "")
	// settings
	// facebook
	case "fb-compose", "fb-cancel", "fb-retry", "fb-now", "fb-event", "fb-weekend", "fb-check":
		msg, err = a.facebookAction(r, action, id)
		ret = "/admin/facebook"
	case "grp-posted", "grp-save", "grp-skip", "grp-enable", "grp-defer", "grp-delete", "grp-remind":
		msg, err = a.groupsAction(r, action, id)
		if ret == "" || ret == "/admin" {
			ret = "/admin/facebook/groups"
		}
	case "settings-save":
		form := map[string]string{}
		for _, d := range settingDefs {
			form[d.Key] = f.Get(d.Key)
		}
		msg, err = a.saveSettings(form)
		a.audit(r, "settings.save", "", "")
	case "settings-reset":
		for _, d := range settingDefs {
			_, _ = a.db.Exec(`DELETE FROM meta WHERE key = ?`, "set:"+d.Key)
		}
		msg = "Settings reset to the environment defaults."
		a.audit(r, "settings.reset", "", "")
	// system
	case "housekeeping":
		a.housekeeping()
		_ = a.metaSet("last:housekeeping", a.localDay(time.Now()))
		msg = "Housekeeping done."
		a.audit(r, "system.housekeeping", "", "")
	case "backup":
		var name string
		name, err = a.backupNow()
		msg = "Backup written: " + name
		a.audit(r, "backup.create", name, "")
	case "backup-delete":
		if backupNameRe.MatchString(id) {
			err = os.Remove(filepath.Join(a.backupDir(), id))
			msg = "Backup deleted."
			a.audit(r, "backup.delete", id, "")
		} else {
			err = fmt.Errorf("bad name")
		}
	case "integrity":
		var res string
		err = a.db.QueryRow(`PRAGMA integrity_check`).Scan(&res)
		msg = "Integrity check: " + res
		a.audit(r, "system.integrity", "", res)
	case "checkpoint":
		_, err = a.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
		msg = "WAL checkpointed."
	case "flush-stats":
		a.flushStats()
		msg = "Statistics flushed to the database."
	case "test-mail":
		err = a.send(Message{To: a.cfg.AdminEmail, Kind: "test", Subject: "[HS] Test message", Text: "This is a test from the Helderberg Social console at " + now() + ".\n"})
		msg = "Test email sent to " + a.cfg.AdminEmail
		a.audit(r, "system.test_mail", "", "")
	default:
		err = fmt.Errorf("unknown action %q", action)
	}
	if err != nil {
		a.logf("console %s: %v", action, err)
		a.back(w, r, ret, "Error: "+err.Error(), true)
		return
	}
	a.back(w, r, ret, msg, false)
}
