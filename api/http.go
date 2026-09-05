package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const maxBody = 32 * 1024

type ctxKey int

const ipKey ctxKey = 1

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.health)
	mux.HandleFunc("GET /api/mail-dns", a.mailDNS)
	mux.HandleFunc("GET /api/events", a.getEvents)
	mux.HandleFunc("GET /api/posts", a.getPosts)
	mux.HandleFunc("POST /api/subscribe", a.subscribe)
	mux.HandleFunc("GET /api/confirm", a.confirm)
	mux.HandleFunc("GET /api/unsubscribe", a.unsubscribe)
	mux.HandleFunc("GET /api/digest", a.digestView)
	mux.HandleFunc("GET /api/wa/webhook", a.waWebhookVerify)
	mux.HandleFunc("POST /api/wa/webhook", a.waWebhook)
	mux.HandleFunc("POST /api/unsubscribe", a.unsubscribe)
	mux.HandleFunc("POST /api/submit/event", a.submitEvent)
	mux.HandleFunc("POST /api/submit/listing", a.submitListing)
	mux.HandleFunc("GET /api/verify", a.verifySubmission)
	mux.HandleFunc("POST /api/ping", a.ping)
	mux.HandleFunc("POST /api/admin/login", a.adminLogin)
	mux.HandleFunc("GET /api/moderate", a.legacyAdminLink)
	mux.HandleFunc("GET /api/admin", a.legacyAdminLink)
	a.registerConsole(mux)
	a.accountRoutes(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { a.fail(w, 404, "not found") })
	return a.middleware(mux)
}

func (a *App) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			if rec := recover(); rec != nil {
				a.logf("panic %s %s: %v", r.Method, r.URL.Path, rec)
				a.fail(w, 500, "internal error")
			}
		}()
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		// same-origin, not no-referrer: under no-referrer Chrome sends
		// "Origin: null" on same-origin form posts, which the origin check
		// below must refuse, and every console/account form breaks.
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		csp := "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
		if strings.HasPrefix(r.URL.Path, "/admin") {
			csp = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
		}
		h.Set("Content-Security-Policy", csp)
		h.Set("Strict-Transport-Security", "max-age=31536000")
		h.Set("Cache-Control", "no-store")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

		ip := a.clientIP(r)
		r = r.WithContext(context.WithValue(r.Context(), ipKey, ip))
		if a.isBlocked("ip", ipTag(ip)) {
			a.fail(w, 403, "forbidden")
			return
		}

		// CORS: only the site itself may call from a browser. Browsers also
		// send Origin on same-origin form POSTs (the console and account
		// pages), so this host's own origin passes without CORS headers.
		if origin := r.Header.Get("Origin"); origin != "" && !strings.EqualFold(strings.TrimSuffix(origin, "/"), strings.TrimSuffix(a.cfg.APIURL, "/")) {
			h.Add("Vary", "Origin")
			if origin == a.cfg.SiteURL {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type")
				h.Set("Access-Control-Max-Age", "600")
			} else if r.Method != http.MethodGet {
				a.fail(w, 403, "origin not allowed")
				return
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		// Three buckets per client: public writes are tight (and so are the
		// sign-in POSTs, against brute force), the signed-in console is roomy,
		// everything else is the read bucket.
		lim := a.limGet
		p := r.URL.Path
		switch {
		case r.Method == http.MethodPost && (p == "/admin/login" || p == "/admin/enrol" || p == "/admin/2fa" || accountAnonPost(p) || (strings.HasPrefix(p, "/api/") && p != "/api/ping")):
			lim = a.limPost
		case strings.HasPrefix(p, "/admin") || (r.Method == http.MethodPost && strings.HasPrefix(p, "/account")):
			lim = a.limAdmin
		}
		if !lim.allow(ip) {
			h.Set("Retry-After", "60")
			a.fail(w, 429, "too many requests, slow down")
			return
		}
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/ping" && a.settingBool("maintenance") {
			h.Set("Retry-After", "600")
			a.fail(w, 503, a.setting("maintenance_text"))
			return
		}
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/account") && r.URL.Path != "/account/logout" && a.settingBool("maintenance") {
			h.Set("Retry-After", "600")
			a.page(w, 503, "Back in a few minutes", a.setting("maintenance_text"), "")
			return
		}
		limit := int64(maxBody)
		if r.URL.Path == "/account/promoter/import" {
			limit = promoterImportBytes + 4096 // the file plus the form around it
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		ms := time.Since(start).Milliseconds()
		route := r.Pattern
		if route == "" || route == "/" {
			route = "other"
		}
		if r.URL.Path != "/api/health" {
			a.stats.request(a.localDay(start), route, sw.status, reqEntry{At: start, Method: r.Method, Path: clean(r.URL.Path, 80), Status: sw.status, Ms: ms, IP: ipTag(ip)})
			a.logf("%s %s %d %dms %s", r.Method, r.URL.Path, sw.status, ms, ipTag(ip))
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }

// clientIP trusts the proxy's headers only when configured to. NPM appends
// the real address to X-Forwarded-For, so the last entry is the trustworthy
// one; X-Real-IP is overwritten by the proxy and preferred when present.
func (a *App) clientIP(r *http.Request) string {
	if a.cfg.TrustProxy {
		if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" && net.ParseIP(v) != nil {
			return v
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if v := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(v) != nil {
				return v
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// accountAnonPost is true for the account-area forms anyone can submit
// without being signed in (register, sign in, reset). They share the tight
// anti-brute-force bucket; the signed-in forms behind a session + CSRF token
// get the roomy one, so editing a few events in a row never trips 429.
func accountAnonPost(p string) bool {
	switch p {
	case "/account/register", "/account/login", "/account/forgot", "/account/reset", "/account/resend":
		return true
	}
	return false
}

func ipOf(r *http.Request) string {
	if v, ok := r.Context().Value(ipKey).(string); ok {
		return v
	}
	return ""
}

// ipTag is what the log shows: a short hash, never the address itself.
func ipTag(ip string) string {
	s := sha256.Sum256([]byte(ip))
	return "ip:" + hex.EncodeToString(s[:4])
}

func (a *App) json(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *App) fail(w http.ResponseWriter, status int, msg string) {
	a.json(w, status, map[string]any{"ok": false, "error": msg})
}

func (a *App) ok(w http.ResponseWriter, msg string) {
	a.json(w, 200, map[string]any{"ok": true, "message": msg})
}

func readJSON(r *http.Request, v any) error {
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		return errors.New("expected application/json")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	if err := dec.Decode(v); err != nil {
		return errors.New("could not read the request")
	}
	return nil
}

func (a *App) redirectSite(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, a.cfg.SiteURL+"/thanks.html?m="+code, http.StatusSeeOther)
}

/* ---------- public ---------- */

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	var n int
	err := a.db.QueryRow(`SELECT COUNT(*) FROM meta`).Scan(&n)
	if err != nil {
		a.fail(w, 503, "database unavailable")
		return
	}
	a.json(w, 200, map[string]any{"ok": true, "version": a.version, "time": now(), "whatsapp": a.waEnabled(), "facebook": a.fbEnabled(), "logins": a.loginKeys()})
}

func (a *App) getEvents(w http.ResponseWriter, r *http.Request) {
	today := time.Now().In(a.cfg.TZ)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, a.cfg.TZ)
	evs, err := a.approvedEvents(today, a.settingInt("events_window_days"))
	if err != nil {
		a.logf("events: %v", err)
		a.fail(w, 500, "internal error")
		return
	}
	if evs == nil {
		evs = []Event{}
	}
	var orgs map[int64]string
	for i := range evs {
		if evs[i].Promoted && evs[i].MemberID != 0 {
			if orgs == nil {
				orgs = a.promoterOrgs()
			}
			evs[i].By = orgs[evs[i].MemberID]
		}
	}
	body, _ := json.Marshal(map[string]any{"ok": true, "events": evs, "generated": now(), "site": a.siteInfo()})
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=300")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(304)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(body)
}

type subscribeReq struct {
	Email      string   `json:"email"`
	Phone      string   `json:"phone"`
	Channel    string   `json:"channel"` // "email" (default) or "whatsapp"
	Frequency  string   `json:"frequency"`
	Horizon    int      `json:"horizon"`
	Towns      []string `json:"towns"`
	Categories []string `json:"categories"`
	Honeypot   string   `json:"website"`
}

func (a *App) subscribe(w http.ResponseWriter, r *http.Request) {
	var q subscribeReq
	if err := readJSON(r, &q); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	if q.Honeypot != "" { // bots fill every field; humans never see this one
		a.ok(w, "Check your inbox for a confirmation email.")
		return
	}
	if !a.settingBool("subscriptions_on") {
		a.fail(w, 503, "New subscriptions are paused for the moment. Please try again later.")
		return
	}
	channel := "email"
	if q.Channel == "whatsapp" {
		if !a.waEnabled() {
			a.fail(w, 400, "WhatsApp updates are not available at the moment. Choose email instead.")
			return
		}
		channel = "whatsapp"
	}
	email, phone := "", ""
	if channel == "email" {
		email = normEmail(q.Email)
		if !validEmail(email) {
			a.fail(w, 400, "That email address does not look right.")
			return
		}
		if a.isBlocked("email", emailHash(email)) {
			a.ok(w, "Check your inbox for a confirmation email.")
			return
		}
	} else {
		var ok bool
		if phone, ok = normPhone(q.Phone); !ok {
			a.fail(w, 400, "That phone number does not look right. Use the number your WhatsApp is on, e.g. 082 123 4567.")
			return
		}
		if a.isBlocked("email", emailHash("tel:"+phone)) {
			a.ok(w, "Check WhatsApp for a confirmation message.")
			return
		}
	}
	done := "Check your inbox for a confirmation email."
	if channel == "whatsapp" {
		done = "Check WhatsApp for a confirmation message."
	}
	if q.Frequency != "daily" && q.Frequency != "weekly" {
		a.fail(w, 400, "Choose daily or weekly.")
		return
	}
	if q.Horizon != 7 && q.Horizon != 14 && q.Horizon != 30 {
		a.fail(w, 400, "Choose 7, 14 or 30 days.")
		return
	}
	tw, ok1 := filterSet(q.Towns, towns, 4)
	ct, ok2 := filterSet(q.Categories, categories, len(categories))
	if !ok1 || !ok2 {
		a.fail(w, 400, "Unknown town or category.")
		return
	}
	ipHash := ipTag(ipOf(r))

	var subs []Subscriber
	var err error
	if channel == "email" {
		subs, err = a.subscribers(`email = ?`, email)
	} else {
		subs, err = a.subscribers(`phone = ?`, phone)
	}
	if err != nil {
		a.logf("subscribe: %v", err)
		a.fail(w, 500, "internal error")
		return
	}
	if len(subs) == 1 && subs[0].Confirmed {
		// Never let a stranger change someone's preferences: tell the owner.
		// (On WhatsApp there is no template for this, so the owner is simply
		// not disturbed; the reply to the form is the same either way.)
		if channel == "email" {
			_ = a.send(Message{To: email, Kind: "already", Subject: "You are already subscribed to Helderberg Social updates",
				Text: a.textAlready(subs[0])})
		}
		a.ok(w, done)
		return
	}
	var id int64
	if len(subs) == 1 {
		id = subs[0].ID
		_, err = a.db.Exec(`UPDATE subscribers SET frequency=?, horizon=?, towns=?, categories=?, created_at=?, ip_hash=? WHERE id=?`,
			q.Frequency, q.Horizon, jsonList(tw), jsonList(ct), now(), ipHash, id)
	} else {
		var res interface{ LastInsertId() (int64, error) }
		res, err = a.db.Exec(`INSERT INTO subscribers(email, phone, channel, frequency, horizon, towns, categories, created_at, ip_hash) VALUES(?,?,?,?,?,?,?,?,?)`,
			nullIfEmpty(email), nullIfEmpty(phone), channel, q.Frequency, q.Horizon, jsonList(tw), jsonList(ct), now(), ipHash)
		if err == nil {
			id, _ = res.LastInsertId()
		}
	}
	if err != nil {
		a.logf("subscribe: %v", err)
		a.fail(w, 500, "internal error")
		return
	}
	if channel == "whatsapp" {
		_ = a.waConfirm(phone, q.Frequency, q.Horizon)
	} else {
		link := a.cfg.APIURL + "/api/confirm?t=" + a.sign("confirm", fmt.Sprint(id), 72*time.Hour)
		_ = a.send(Message{To: email, Kind: "confirm", Subject: "Confirm your Helderberg Social updates",
			Text: a.textConfirm(q.Frequency, q.Horizon, link), HTML: a.htmlConfirm(q.Frequency, q.Horizon, link)})
	}
	a.ok(w, done)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (a *App) confirm(w http.ResponseWriter, r *http.Request) {
	p, err := a.consume(r.URL.Query().Get("t"), "confirm")
	if err != nil {
		a.redirectSite(w, r, "invalid")
		return
	}
	res, err := a.db.Exec(`UPDATE subscribers SET confirmed_at = COALESCE(confirmed_at, ?) WHERE id = ?`, now(), p.Subject)
	if n, _ := res.RowsAffected(); err != nil || n == 0 {
		a.redirectSite(w, r, "invalid")
		return
	}
	a.redirectSite(w, r, "subscribed")
}

// unsubscribe deletes the row outright: nothing is kept once someone leaves.
func (a *App) unsubscribe(w http.ResponseWriter, r *http.Request) {
	t := r.URL.Query().Get("t")
	if t == "" && r.Method == http.MethodPost {
		_ = r.ParseForm()
		t = r.PostForm.Get("t")
	}
	p, err := a.verify(t, "unsub")
	if err != nil {
		if r.Method == http.MethodPost {
			a.fail(w, 400, "invalid link")
			return
		}
		a.redirectSite(w, r, "invalid")
		return
	}
	_, _ = a.db.Exec(`DELETE FROM subscribers WHERE id = ?`, p.Subject)
	if r.Method == http.MethodPost {
		a.ok(w, "unsubscribed")
		return
	}
	a.redirectSite(w, r, "unsubscribed")
}

type eventReq struct {
	Title    string `json:"title"`
	Date     string `json:"date"`
	EndDate  string `json:"endDate"`
	Time     string `json:"time"`
	EndTime  string `json:"endTime"`
	Town     string `json:"town"`
	Category string `json:"category"`
	Listing  string `json:"listing"`
	Summary  string `json:"summary"`
	Cost     string `json:"cost"`
	Website  string `json:"website"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Honeypot string `json:"company"`
}

func (a *App) submitEvent(w http.ResponseWriter, r *http.Request) {
	var q eventReq
	if err := readJSON(r, &q); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	if q.Honeypot != "" {
		a.ok(w, "Check your inbox to confirm your submission.")
		return
	}
	if !a.settingBool("submissions_on") {
		a.fail(w, 503, "Submissions are paused for the moment. Please try again later.")
		return
	}
	if a.isBlocked("email", emailHash(normEmail(q.Email))) {
		a.ok(w, "Check your inbox to confirm your submission.")
		return
	}
	e := Event{
		Title:    clean(q.Title, 120),
		Date:     strings.TrimSpace(q.Date),
		EndDate:  strings.TrimSpace(q.EndDate),
		Time:     strings.TrimSpace(q.Time),
		EndTime:  strings.TrimSpace(q.EndTime),
		Town:     q.Town,
		Category: q.Category,
		Listing:  strings.TrimSpace(q.Listing),
		Summary:  cleanMulti(q.Summary, 800),
		Cost:     q.Cost,
	}
	email := normEmail(q.Email)
	name := clean(q.Name, 80)
	today := time.Now().In(a.cfg.TZ).Truncate(24 * time.Hour)
	var msg string
	switch {
	case len(e.Title) < 4:
		msg = "Give the event a title."
	case !validEmail(email):
		msg = "That email address does not look right."
	case name == "":
		msg = "We need your name."
	case !towns[e.Town]:
		msg = "Choose a town."
	case !categories[e.Category]:
		msg = "Choose a category."
	case !costs[e.Cost]:
		msg = "Choose a cost."
	case len(e.Summary) < 20:
		msg = "Describe the event in a sentence or two (20+ characters)."
	case e.Listing != "" && !slugRe.MatchString(e.Listing):
		msg = "Bad listing reference."
	case e.Time != "" && !timeRe.MatchString(e.Time), e.EndTime != "" && !timeRe.MatchString(e.EndTime):
		msg = "Times must look like 18:30."
	}
	if msg == "" {
		d, ok := validDate(e.Date)
		if !ok || d.Before(today.AddDate(0, 0, -1)) || d.After(today.AddDate(0, 0, 400)) {
			msg = "The date must be a real day within the next year."
		} else if e.EndDate != "" {
			if ed, ok := validDate(e.EndDate); !ok || ed.Before(d) || ed.After(d.AddDate(0, 0, 60)) {
				msg = "The end date must be on or after the start, and within 60 days of it."
			}
		}
	}
	if msg == "" {
		var ok bool
		if e.Website, ok = validURL(q.Website); !ok {
			msg = "The website must be a full http(s) address."
		}
	}
	if msg != "" {
		a.fail(w, 400, msg)
		return
	}
	e.Source = e.Website
	e.Status, e.Origin, e.SubmitterName = "pending_email", "user", name
	e.ID = a.uniqueEventID(slugify(e.Title) + "-" + e.Date)
	if err := a.insertEvent(e, ipTag(ipOf(r)), email, nil); err != nil {
		a.logf("submitEvent: %v", err)
		a.fail(w, 500, "internal error")
		return
	}
	link := a.cfg.APIURL + "/api/verify?t=" + a.sign("verify-event", e.ID, 72*time.Hour)
	_ = a.send(Message{To: email, Kind: "verify-event", Subject: "Confirm your event: " + e.Title,
		Text: a.textVerify("event", e.Title, link), HTML: a.htmlVerify("event", e.Title, link)})
	a.ok(w, "Check your inbox to confirm your submission.")
}

type listingReq struct {
	Kind     string   `json:"kind"`
	Existing string   `json:"existing"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Town     string   `json:"town"`
	Schedule string   `json:"schedule"`
	Summary  string   `json:"summary"`
	Cost     string   `json:"cost"`
	Website  string   `json:"website"`
	Audience []string `json:"audience"`
	YourName string   `json:"yourName"`
	Email    string   `json:"email"`
	Honeypot string   `json:"company"`
}

func (a *App) submitListing(w http.ResponseWriter, r *http.Request) {
	var q listingReq
	if err := readJSON(r, &q); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	if q.Honeypot != "" {
		a.ok(w, "Check your inbox to confirm your submission.")
		return
	}
	if !a.settingBool("submissions_on") {
		a.fail(w, 503, "Submissions are paused for the moment. Please try again later.")
		return
	}
	if a.isBlocked("email", emailHash(normEmail(q.Email))) {
		a.ok(w, "Check your inbox to confirm your submission.")
		return
	}
	name := clean(q.Name, 120)
	summary := cleanMulti(q.Summary, 800)
	schedule := clean(q.Schedule, 160)
	email := normEmail(q.Email)
	yourName := clean(q.YourName, 80)
	existing := strings.TrimSpace(q.Existing)
	aud, audOK := filterSet(q.Audience, audiences, 6)
	website, urlOK := validURL(q.Website)
	var msg string
	switch {
	case !kinds[q.Kind]:
		msg = "Choose what you are telling us about."
	case q.Kind == "update" && !slugRe.MatchString(existing):
		msg = "Choose which listing this is about."
	case len(name) < 2:
		msg = "Give it a name."
	case !categories[q.Category]:
		msg = "Choose a category."
	case !towns[q.Town]:
		msg = "Choose a town."
	case !costs[q.Cost]:
		msg = "Choose a cost."
	case len(summary) < 20:
		msg = "A sentence or two, please (20+ characters)."
	case !audOK:
		msg = "Unknown audience."
	case !urlOK:
		msg = "The website must be a full http(s) address."
	case yourName == "":
		msg = "We need your name."
	case !validEmail(email):
		msg = "That email address does not look right."
	}
	if msg != "" {
		a.fail(w, 400, msg)
		return
	}
	if q.Kind != "update" {
		existing = ""
	}
	res, err := a.db.Exec(`INSERT INTO listing_submissions(kind, existing_id, name, category, town, schedule, summary, cost, website, audience, submitter_name, submitter_email, status, created_at, ip_hash)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'pending_email',?,?)`,
		q.Kind, existing, name, q.Category, q.Town, schedule, summary, q.Cost, website, jsonList(aud), yourName, email, now(), ipTag(ipOf(r)))
	if err != nil {
		a.logf("submitListing: %v", err)
		a.fail(w, 500, "internal error")
		return
	}
	id, _ := res.LastInsertId()
	link := a.cfg.APIURL + "/api/verify?t=" + a.sign("verify-listing", fmt.Sprint(id), 72*time.Hour)
	_ = a.send(Message{To: email, Kind: "verify-listing", Subject: "Confirm your listing: " + name,
		Text: a.textVerify("listing", name, link), HTML: a.htmlVerify("listing", name, link)})
	a.ok(w, "Check your inbox to confirm your submission.")
}

// verifySubmission is the link in the submitter's email. It proves the
// address is real, then hands the item to the moderator.
func (a *App) verifySubmission(w http.ResponseWriter, r *http.Request) {
	t := r.URL.Query().Get("t")
	if p, err := a.consume(t, "verify-event"); err == nil {
		res, _ := a.db.Exec(`UPDATE events SET status='pending_review', verified_at=? WHERE id=? AND status='pending_email'`, now(), p.Subject)
		if n, _ := res.RowsAffected(); n == 1 {
			a.notifyAdminEvent(p.Subject)
			a.redirectSite(w, r, "verified")
			return
		}
	} else if p, err := a.consume(t, "verify-listing"); err == nil {
		res, _ := a.db.Exec(`UPDATE listing_submissions SET status='pending_review', verified_at=? WHERE id=? AND status='pending_email'`, now(), p.Subject)
		if n, _ := res.RowsAffected(); n == 1 {
			a.notifyAdminListing(p.Subject)
			a.redirectSite(w, r, "verified")
			return
		}
	}
	a.redirectSite(w, r, "invalid")
}
