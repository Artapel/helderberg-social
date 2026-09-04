package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// Member accounts. Anyone may register with an email address and a
// password; after the address is confirmed they can post events, which land
// in the admin queue exactly like an anonymous submission did, and see what
// happened to them. The account area is server-rendered on the API host
// (/account/*), form-only like the console, so the same no-script CSP holds.
//
//   /account/register -> verify link mailed (24 h, single use)
//   /account/verify   -> address confirmed, signed in
//   /account/login    -> password -> session cookie (30 days, 7 days idle)
//   /account/forgot   -> reset link mailed (1 h, single use)
//   /account          -> my events; new / edit / withdraw
//   /account/settings -> name, password, delete account

const (
	cookieMember     = "hs_member"
	memberSessionMax = 30 * 24 * time.Hour
	memberIdle       = 7 * 24 * time.Hour
	memberVerifyMax  = 24 * time.Hour
	memberResetMax   = time.Hour
	memberMinPass    = 10
	memberMaxFails   = 8                // wrong passwords per email before a lock
	memberLock       = 15 * time.Minute // length of that lock
	memberEventsDay  = 10               // new events one account may post per day
)

type Member struct {
	ID          int64
	Email, Name string
	PwHash      string
	CreatedAt   string
	VerifiedAt  string
	LastLoginAt string
	Status      string // active | disabled
	IPHash      string
	Events      int // filled by the console list
}

type memberSession struct {
	Hash     string
	MemberID int64
	Member   *Member
}

const memberCtx sessKey = 3

func memberOf(r *http.Request) *memberSession {
	if s, ok := r.Context().Value(memberCtx).(*memberSession); ok {
		return s
	}
	return nil
}

/* ---------- passwords ---------- */

// Argon2id with the OWASP minimum (19 MiB, 2 passes). The encoded form
// carries its own parameters, so they can be raised later without
// invalidating anyone: old hashes verify with the old numbers and are
// re-hashed on the next successful sign-in.
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
)

func hashPassword(pw string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

// checkPassword returns whether pw matches, and whether the stored hash uses
// weaker parameters than the current ones (so it should be re-hashed).
func checkPassword(encoded, pw string) (ok bool, stale bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false, false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	ok = subtle.ConstantTimeCompare(got, want) == 1
	return ok, ok && (m < argonMemory || t < argonTime)
}

// passwordProblem says why a password is not acceptable, or "".
func passwordProblem(pw, email string) string {
	switch {
	case len(pw) < memberMinPass:
		return fmt.Sprintf("Use at least %d characters. A short sentence works well.", memberMinPass)
	case len(pw) > 200:
		return "That password is too long (200 characters is plenty)."
	case strings.EqualFold(strings.TrimSpace(pw), email):
		return "Your password cannot be your email address."
	case weakPasswords[strings.ToLower(pw)]:
		return "That password is on every attacker's list. Pick something less common."
	}
	return ""
}

var weakPasswords = map[string]bool{
	"password": true, "password1": true, "password12": true, "password123": true, "passwordpassword": true,
	"1234567890": true, "12345678910": true, "qwertyuiop": true, "qwerty1234": true, "iloveyou12": true,
	"helderberg": true, "helderberg1": true, "helderbergsocial": true, "somersetwest": true, "administrator": true,
}

/* ---------- lockout ---------- */

// lockout keeps a short-lived failure count per key (email hash or ip tag)
// in memory. A restart clears it, which is fine: the rate limiter still
// stands in front of the login form.
type lockout struct {
	mu sync.Mutex
	m  map[string]lockEntry
}

type lockEntry struct {
	fails int
	until time.Time
}

func (l *lockout) locked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.m[key]
	return ok && time.Now().Before(e.until)
}

func (l *lockout) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string]lockEntry{}
	}
	e := l.m[key]
	if time.Now().After(e.until) && e.fails >= memberMaxFails {
		e = lockEntry{}
	}
	e.fails++
	if e.fails >= memberMaxFails {
		e.until = time.Now().Add(memberLock)
	}
	l.m[key] = e
}

func (l *lockout) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, key)
}

/* ---------- storage ---------- */

const memberCols = `id, email, name, pw_hash, created_at, COALESCE(verified_at,''), COALESCE(last_login_at,''), status, ip_hash`

func scanMember(row interface{ Scan(...any) error }) (*Member, error) {
	var m Member
	err := row.Scan(&m.ID, &m.Email, &m.Name, &m.PwHash, &m.CreatedAt, &m.VerifiedAt, &m.LastLoginAt, &m.Status, &m.IPHash)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (a *App) memberByID(id int64) *Member {
	m, err := scanMember(a.db.QueryRow(`SELECT `+memberCols+` FROM members WHERE id = ?`, id))
	if err != nil {
		return nil
	}
	return m
}

func (a *App) memberByEmail(email string) *Member {
	m, err := scanMember(a.db.QueryRow(`SELECT `+memberCols+` FROM members WHERE email = ?`, normEmail(email)))
	if err != nil {
		return nil
	}
	return m
}

func (a *App) members(where string, limit, offset int, args ...any) []Member {
	rows, err := a.db.Query(`SELECT `+memberCols+`, (SELECT COUNT(*) FROM events e WHERE e.member_id = members.id) FROM members WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		if rows.Scan(&m.ID, &m.Email, &m.Name, &m.PwHash, &m.CreatedAt, &m.VerifiedAt, &m.LastLoginAt, &m.Status, &m.IPHash, &m.Events) == nil {
			out = append(out, m)
		}
	}
	return out
}

/* ---------- sessions ---------- */

func memberCookieHash(v string) string {
	s := sha256.Sum256([]byte("member-session:" + v))
	return hex.EncodeToString(s[:])
}

func (a *App) setMemberCookie(w http.ResponseWriter, value string, ttl time.Duration) {
	c := &http.Cookie{Name: cookieMember, Value: value, Path: "/account", HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteLaxMode}
	if ttl <= 0 {
		c.MaxAge = -1
	} else {
		c.MaxAge = int(ttl.Seconds())
	}
	http.SetCookie(w, c)
}

func (a *App) createMemberSession(w http.ResponseWriter, r *http.Request, m *Member) error {
	raw := randomID(32)
	exp := time.Now().UTC().Add(memberSessionMax).Format(time.RFC3339)
	_, err := a.db.Exec(`INSERT INTO member_sessions(id_hash, member_id, created_at, last_seen_at, expires_at, ip_hash, ua) VALUES(?,?,?,?,?,?,?)`,
		memberCookieHash(raw), m.ID, now(), now(), exp, ipTag(ipOf(r)), clean(r.UserAgent(), 160))
	if err != nil {
		return err
	}
	_, _ = a.db.Exec(`UPDATE members SET last_login_at = ? WHERE id = ?`, now(), m.ID)
	a.setMemberCookie(w, raw, memberSessionMax)
	return nil
}

// currentMember validates the cookie, refreshes the idle clock and loads the
// member. Disabled or deleted members have no session.
func (a *App) currentMember(r *http.Request) *memberSession {
	raw := cookieVal(r, cookieMember)
	if raw == "" {
		return nil
	}
	h := memberCookieHash(raw)
	var mid int64
	var lastSeen, expires string
	var revoked int
	err := a.db.QueryRow(`SELECT member_id, last_seen_at, expires_at, revoked FROM member_sessions WHERE id_hash = ?`, h).Scan(&mid, &lastSeen, &expires, &revoked)
	if err != nil || revoked != 0 {
		return nil
	}
	exp, _ := time.Parse(time.RFC3339, expires)
	seen, _ := time.Parse(time.RFC3339, lastSeen)
	if time.Now().After(exp) || time.Since(seen) > memberIdle {
		_, _ = a.db.Exec(`UPDATE member_sessions SET revoked = 1 WHERE id_hash = ?`, h)
		return nil
	}
	m := a.memberByID(mid)
	if m == nil || m.Status != "active" || m.VerifiedAt == "" {
		return nil
	}
	if time.Since(seen) > time.Minute {
		_, _ = a.db.Exec(`UPDATE member_sessions SET last_seen_at = ? WHERE id_hash = ?`, now(), h)
	}
	return &memberSession{Hash: h, MemberID: mid, Member: m}
}

func (a *App) revokeMemberSessions(memberID int64) {
	_, _ = a.db.Exec(`UPDATE member_sessions SET revoked = 1 WHERE member_id = ?`, memberID)
}

func (a *App) memberCSRF(s *memberSession) string {
	mac := hmac.New(sha256.New, a.cfg.Secret)
	mac.Write([]byte("member-csrf:" + s.Hash))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *App) memberCSRFOK(r *http.Request, s *memberSession) bool {
	if r.Method != http.MethodPost {
		return true
	}
	_ = r.ParseForm()
	return subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(a.memberCSRF(s))) == 1
}

// requireMember gates the signed-in part of the account area.
func (a *App) requireMember(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := a.currentMember(r)
		if s == nil {
			if r.Method == http.MethodPost {
				a.accountBack(w, r, "/account/login", "Please sign in again.", true)
				return
			}
			http.Redirect(w, r, "/account/login?next="+url.QueryEscape(accountNext(r.URL.RequestURI())), http.StatusSeeOther)
			return
		}
		if !a.memberCSRFOK(r, s) {
			a.accountBack(w, r, "/account", "That form had expired. Please try again.", true)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), memberCtx, s)))
	}
}

func accountNext(v string) string {
	if strings.HasPrefix(v, "/account") && !strings.Contains(v, "//") && len(v) < 400 {
		return v
	}
	return "/account"
}

/* ---------- rendering ---------- */

type accountView struct {
	Title   string
	Member  *Member
	CSRF    string
	Msg     string
	Err     bool
	Body    template.HTML
	Site    string
	Version string
	Active  string
	RegOn   bool
	Pending int
}

func (a *App) renderAccount(w http.ResponseWriter, r *http.Request, status int, name, title string, data any) {
	s := memberOf(r)
	v := accountView{Title: title, Site: a.cfg.SiteURL, Version: a.version, Active: r.URL.Path, RegOn: a.settingBool("registrations_on")}
	if s != nil {
		v.Member = s.Member
		v.CSRF = a.memberCSRF(s)
		v.Pending = a.count(`SELECT COUNT(*) FROM events WHERE member_id = ? AND status = 'pending_review'`, s.MemberID)
	}
	v.Msg = clean(r.URL.Query().Get("msg"), 300)
	v.Err = r.URL.Query().Get("err") == "1"
	if data == nil {
		data = map[string]any{}
	}
	var buf bytes.Buffer
	if err := a.atmpl.ExecuteTemplate(&buf, name, map[string]any{"D": data, "V": v, "CSRF": v.CSRF}); err != nil {
		a.logf("account template %s: %v", name, err)
		http.Error(w, "template error", 500)
		return
	}
	v.Body = template.HTML(buf.String())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := a.atmpl.ExecuteTemplate(w, "account_layout", v); err != nil {
		a.logf("account layout: %v", err)
	}
}

func (a *App) accountBack(w http.ResponseWriter, r *http.Request, to, msg string, isErr bool) {
	u := accountNext(to)
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
	http.Redirect(w, r, u, http.StatusSeeOther)
}

/* ---------- routes ---------- */

func (a *App) accountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /account/register", a.registerPage)
	mux.HandleFunc("POST /account/register", a.registerPost)
	mux.HandleFunc("GET /account/verify", a.verifyMember)
	mux.HandleFunc("GET /account/login", a.memberLoginPage)
	mux.HandleFunc("POST /account/login", a.memberLoginPost)
	mux.HandleFunc("GET /account/forgot", a.forgotPage)
	mux.HandleFunc("POST /account/forgot", a.forgotPost)
	mux.HandleFunc("GET /account/reset", a.resetPage)
	mux.HandleFunc("POST /account/reset", a.resetPost)
	mux.HandleFunc("POST /account/resend", a.resendVerify)
	mux.HandleFunc("GET /account", a.requireMember(a.myEventsPage))
	mux.HandleFunc("GET /account/{$}", a.requireMember(a.myEventsPage))
	mux.HandleFunc("GET /account/events/new", a.requireMember(a.memberEventForm))
	mux.HandleFunc("GET /account/events/edit", a.requireMember(a.memberEventForm))
	mux.HandleFunc("POST /account/events/save", a.requireMember(a.memberEventSave))
	mux.HandleFunc("POST /account/events/withdraw", a.requireMember(a.memberEventWithdraw))
	mux.HandleFunc("GET /account/settings", a.requireMember(a.memberSettingsPage))
	mux.HandleFunc("POST /account/settings", a.requireMember(a.memberSettingsPost))
	mux.HandleFunc("POST /account/logout", a.requireMember(a.memberLogout))
}

/* ---------- register / verify ---------- */

func (a *App) registerPage(w http.ResponseWriter, r *http.Request) {
	if a.currentMember(r) != nil {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	a.renderAccount(w, r, 200, "acc_register", "Create an account", map[string]any{"Next": accountNext(r.URL.Query().Get("next"))})
}

func (a *App) registerPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f := r.PostForm
	if f.Get("website_url") != "" { // honeypot
		a.renderAccount(w, r, 200, "acc_sent", "Check your email", map[string]any{"Kind": "verify"})
		return
	}
	if !a.settingBool("registrations_on") {
		a.accountBack(w, r, "/account/register", "New accounts are paused for the moment. Please try again later.", true)
		return
	}
	name := clean(f.Get("name"), 80)
	email := normEmail(f.Get("email"))
	pw := f.Get("password")
	next := accountNext(f.Get("next"))
	var msg string
	switch {
	case len(name) < 2:
		msg = "Tell us your name (it is never shown on the site)."
	case !validEmail(email):
		msg = "That email address does not look right."
	case pw != f.Get("password2"):
		msg = "The two passwords do not match."
	default:
		msg = passwordProblem(pw, email)
	}
	if msg != "" {
		a.accountBack(w, r, "/account/register?next="+url.QueryEscape(next), msg, true)
		return
	}
	if a.isBlocked("email", emailHash(email)) {
		a.renderAccount(w, r, 200, "acc_sent", "Check your email", map[string]any{"Kind": "verify"})
		return
	}
	if existing := a.memberByEmail(email); existing != nil {
		// Same reply as a new account, so the form cannot be used to find out
		// who has one. The owner learns what happened from the email itself.
		if existing.VerifiedAt == "" {
			a.sendMemberVerify(existing, next)
		} else {
			_ = a.send(Message{To: email, Kind: "member-exists", Subject: "You already have a Helderberg Social account",
				Text: fmt.Sprintf("Hi %s,\n\nSomeone (probably you) tried to create a Helderberg Social account with this address, but you already have one.\n\nSign in here:\n%s/account/login\n\nForgotten your password? %s/account/forgot\n\nIf this was not you, you can ignore this email.\n\nHelderberg Social\n%s\n", existing.Name, a.cfg.APIURL, a.cfg.APIURL, a.cfg.SiteURL)})
		}
		a.audit(r, "member.register_dup", emailHash(email), "")
		a.renderAccount(w, r, 200, "acc_sent", "Check your email", map[string]any{"Kind": "verify"})
		return
	}
	res, err := a.db.Exec(`INSERT INTO members(email, name, pw_hash, created_at, status, ip_hash) VALUES(?,?,?,?,'active',?)`, email, name, hashPassword(pw), now(), ipTag(ipOf(r)))
	if err != nil {
		a.logf("register: %v", err)
		a.accountBack(w, r, "/account/register", "Something went wrong on our side. Please try again.", true)
		return
	}
	id, _ := res.LastInsertId()
	m := a.memberByID(id)
	a.sendMemberVerify(m, next)
	a.audit(r, "member.register", fmt.Sprint(id), emailHash(email))
	a.renderAccount(w, r, 200, "acc_sent", "Check your email", map[string]any{"Kind": "verify"})
}

func (a *App) sendMemberVerify(m *Member, next string) {
	link := a.cfg.APIURL + "/account/verify?t=" + a.sign("member-verify", fmt.Sprintf("%d|%s", m.ID, accountNext(next)), memberVerifyMax)
	_ = a.send(Message{To: m.Email, Kind: "member-verify", Subject: "Confirm your Helderberg Social account",
		Text: fmt.Sprintf("Hi %s,\n\nWelcome. Confirm your email address by opening this link within 24 hours:\n\n%s\n\nAfter that you can post events; each one is checked by a person before it appears on the site.\n\nIf you did not create this account, ignore this email and it will be removed.\n\nHelderberg Social\n%s\n", m.Name, link, a.cfg.SiteURL),
		HTML: a.htmlMemberMail("Confirm your account", fmt.Sprintf("Hi %s, welcome to Helderberg Social. Confirm your email address to finish creating your account. The link works once, for 24 hours.", m.Name), "Confirm my email", link)})
}

func (a *App) verifyMember(w http.ResponseWriter, r *http.Request) {
	p, err := a.consume(r.URL.Query().Get("t"), "member-verify")
	if err != nil {
		a.accountBack(w, r, "/account/login", "That link is invalid, expired or already used. Sign in to get a new one.", true)
		return
	}
	id, next, _ := strings.Cut(p.Subject, "|")
	var mid int64
	fmt.Sscan(id, &mid)
	m := a.memberByID(mid)
	if m == nil || m.Status != "active" {
		a.accountBack(w, r, "/account/login", "That account no longer exists.", true)
		return
	}
	if m.VerifiedAt == "" {
		_, _ = a.db.Exec(`UPDATE members SET verified_at = ? WHERE id = ?`, now(), m.ID)
		m.VerifiedAt = now()
		a.audit(r, "member.verified", fmt.Sprint(m.ID), "")
	}
	if err := a.createMemberSession(w, r, m); err != nil {
		a.accountBack(w, r, "/account/login", "Your address is confirmed; please sign in.", false)
		return
	}
	a.accountBack(w, r, accountNext(next), "Your email is confirmed and you are signed in. Welcome!", false)
}

// resendVerify is the "I never got the email" button on the sign-in page.
func (a *App) resendVerify(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := normEmail(r.PostForm.Get("email"))
	if m := a.memberByEmail(email); m != nil && m.VerifiedAt == "" && m.Status == "active" {
		a.sendMemberVerify(m, "/account")
	}
	a.renderAccount(w, r, 200, "acc_sent", "Check your email", map[string]any{"Kind": "verify"})
}

/* ---------- login / logout ---------- */

func (a *App) memberLoginPage(w http.ResponseWriter, r *http.Request) {
	if a.currentMember(r) != nil {
		http.Redirect(w, r, accountNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	a.renderAccount(w, r, 200, "acc_login", "Sign in", map[string]any{"Next": accountNext(r.URL.Query().Get("next")), "Email": clean(r.URL.Query().Get("email"), 120)})
}

func (a *App) memberLoginPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f := r.PostForm
	email := normEmail(f.Get("email"))
	pw := f.Get("password")
	next := accountNext(f.Get("next"))
	back := "/account/login?next=" + url.QueryEscape(next) + "&email=" + url.QueryEscape(email)
	key := emailHash(email)
	if a.lock.locked(key) || a.lock.locked(ipTag(ipOf(r))) {
		a.audit(r, "member.login_locked", key, "")
		a.accountBack(w, r, back, "Too many wrong passwords. Wait 15 minutes, or reset your password.", true)
		return
	}
	m := a.memberByEmail(email)
	ok := false
	stale := false
	if m != nil {
		ok, stale = checkPassword(m.PwHash, pw)
	} else {
		checkPassword(dummyHash, pw) // same cost whether or not the address exists
	}
	if !ok {
		a.lock.fail(key)
		a.lock.fail(ipTag(ipOf(r)))
		a.audit(r, "member.login_fail", key, "")
		a.accountBack(w, r, back, "Wrong email address or password.", true)
		return
	}
	if m.Status != "active" {
		a.audit(r, "member.login_disabled", fmt.Sprint(m.ID), "")
		a.accountBack(w, r, back, "This account has been disabled. Email us if you think that is a mistake.", true)
		return
	}
	if m.VerifiedAt == "" {
		a.audit(r, "member.login_unverified", fmt.Sprint(m.ID), "")
		a.renderAccount(w, r, 200, "acc_unverified", "Confirm your email first", map[string]any{"Email": email})
		return
	}
	a.lock.clear(key)
	if stale {
		_, _ = a.db.Exec(`UPDATE members SET pw_hash = ? WHERE id = ?`, hashPassword(pw), m.ID)
	}
	if err := a.createMemberSession(w, r, m); err != nil {
		a.logf("member session: %v", err)
		a.accountBack(w, r, back, "Could not sign you in. Please try again.", true)
		return
	}
	a.audit(r, "member.login", fmt.Sprint(m.ID), "")
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// dummyHash is verified against when the address is unknown, so a wrong
// address costs the same time as a wrong password.
var dummyHash = hashPassword("not-a-real-password-" + randomID(4))

func (a *App) memberLogout(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	_, _ = a.db.Exec(`UPDATE member_sessions SET revoked = 1 WHERE id_hash = ?`, s.Hash)
	a.setMemberCookie(w, "", 0)
	a.accountBack(w, r, "/account/login", "You are signed out.", false)
}

/* ---------- forgot / reset ---------- */

func (a *App) forgotPage(w http.ResponseWriter, r *http.Request) {
	a.renderAccount(w, r, 200, "acc_forgot", "Reset your password", nil)
}

func (a *App) forgotPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := normEmail(r.PostForm.Get("email"))
	if m := a.memberByEmail(email); m != nil && m.Status == "active" {
		link := a.cfg.APIURL + "/account/reset?t=" + a.sign("member-reset", fmt.Sprint(m.ID), memberResetMax)
		_ = a.send(Message{To: m.Email, Kind: "member-reset", Subject: "Reset your Helderberg Social password",
			Text: fmt.Sprintf("Hi %s,\n\nOpen this link within an hour to choose a new password:\n\n%s\n\nIf you did not ask for this, ignore it; your password stays as it is.\n\nHelderberg Social\n%s\n", m.Name, link, a.cfg.SiteURL),
			HTML: a.htmlMemberMail("Reset your password", fmt.Sprintf("Hi %s, someone asked to reset the password for this account. If that was you, choose a new one with the button below. The link works once, for one hour.", m.Name), "Choose a new password", link)})
		a.audit(r, "member.reset_sent", fmt.Sprint(m.ID), "")
	} else {
		a.audit(r, "member.reset_unknown", emailHash(email), "")
	}
	a.renderAccount(w, r, 200, "acc_sent", "Check your email", map[string]any{"Kind": "reset"})
}

func (a *App) resetPage(w http.ResponseWriter, r *http.Request) {
	t := r.URL.Query().Get("t")
	if _, err := a.verify(t, "member-reset"); err != nil {
		a.accountBack(w, r, "/account/forgot", "That link is invalid, expired or already used. Ask for a new one.", true)
		return
	}
	a.renderAccount(w, r, 200, "acc_reset", "Choose a new password", map[string]any{"Token": t})
}

func (a *App) resetPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f := r.PostForm
	t := f.Get("t")
	p, err := a.verify(t, "member-reset")
	if err != nil {
		a.accountBack(w, r, "/account/forgot", "That link is invalid, expired or already used. Ask for a new one.", true)
		return
	}
	var mid int64
	fmt.Sscan(p.Subject, &mid)
	m := a.memberByID(mid)
	if m == nil || m.Status != "active" {
		a.accountBack(w, r, "/account/forgot", "That account no longer exists.", true)
		return
	}
	pw := f.Get("password")
	if pw != f.Get("password2") {
		a.accountBack(w, r, "/account/reset?t="+url.QueryEscape(t), "The two passwords do not match.", true)
		return
	}
	if msg := passwordProblem(pw, m.Email); msg != "" {
		a.accountBack(w, r, "/account/reset?t="+url.QueryEscape(t), msg, true)
		return
	}
	if _, err := a.consume(t, "member-reset"); err != nil {
		a.accountBack(w, r, "/account/forgot", "That link was already used. Ask for a new one.", true)
		return
	}
	_, _ = a.db.Exec(`UPDATE members SET pw_hash = ?, verified_at = COALESCE(verified_at, ?) WHERE id = ?`, hashPassword(pw), now(), m.ID)
	a.revokeMemberSessions(m.ID)
	a.lock.clear(emailHash(m.Email))
	a.audit(r, "member.reset_done", fmt.Sprint(m.ID), "")
	m = a.memberByID(m.ID)
	if err := a.createMemberSession(w, r, m); err != nil {
		a.accountBack(w, r, "/account/login", "Your password is changed; please sign in.", false)
		return
	}
	a.accountBack(w, r, "/account", "Your password is changed and you are signed in.", false)
}

/* ---------- my events ---------- */

type memberEventRow struct {
	Event
	StatusText string
	Editable   bool
	Live       string
}

func memberStatusText(s string) string {
	switch s {
	case "pending_review":
		return "Waiting for a check"
	case "approved":
		return "Published"
	case "rejected":
		return "Not published"
	}
	return s
}

func (a *App) myEventsPage(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	evs, _ := a.queryEvents(`member_id = ?`, s.MemberID)
	var rows []memberEventRow
	today := a.localDay(time.Now())
	for i := len(evs) - 1; i >= 0; i-- { // newest date first
		e := evs[i]
		row := memberEventRow{Event: e, StatusText: memberStatusText(e.Status), Editable: true}
		if e.Status == "approved" {
			row.Live = a.cfg.SiteURL + "/events.html?ev=" + url.QueryEscape(e.ID)
		}
		if end := e.EndDate; (end != "" && end < today) || (end == "" && e.Date < today) {
			row.StatusText += " · past"
			row.Editable = false
		}
		rows = append(rows, row)
	}
	a.renderAccount(w, r, 200, "acc_events", "My events", map[string]any{"Events": rows})
}

type memberEventFormView struct {
	E                  Event
	New                bool
	Towns, Cats, Costs []string
}

func (a *App) memberEventForm(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	f := memberEventFormView{New: true, Towns: sortedKeys(towns), Cats: sortedKeys(categories), Costs: []string{"free", "paid", "membership", "donation", "varies"}}
	if id := r.URL.Query().Get("id"); id != "" {
		evs, err := a.queryEvents(`id = ? AND member_id = ?`, id, s.MemberID)
		if err != nil || len(evs) != 1 {
			a.accountBack(w, r, "/account", "That event is not one of yours.", true)
			return
		}
		f.E, f.New = evs[0], false
	} else {
		f.E = Event{Town: "somerset-west", Category: "community", Cost: "free"}
	}
	title := "Post an event"
	if !f.New {
		title = "Edit event"
	}
	a.renderAccount(w, r, 200, "acc_event_form", title, f)
}

func (a *App) memberEventSave(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	f := r.PostForm
	id := strings.TrimSpace(f.Get("id"))
	if !a.settingBool("submissions_on") {
		a.accountBack(w, r, "/account", "Event submissions are paused for the moment. Please try again later.", true)
		return
	}
	e := Event{
		Title:    clean(f.Get("title"), 120),
		Date:     strings.TrimSpace(f.Get("date")),
		EndDate:  strings.TrimSpace(f.Get("end_date")),
		Time:     strings.TrimSpace(f.Get("time")),
		EndTime:  strings.TrimSpace(f.Get("end_time")),
		Town:     f.Get("town"),
		Category: f.Get("category"),
		Summary:  cleanMulti(f.Get("summary"), 800),
		Cost:     f.Get("cost"),
	}
	if e.Cost == "" {
		e.Cost = "varies"
	}
	back := "/account/events/new"
	if id != "" {
		back = "/account/events/edit?id=" + url.QueryEscape(id)
	}
	msg := a.eventProblem(&e, f.Get("website"))
	if msg != "" {
		a.accountBack(w, r, back, msg, true)
		return
	}
	e.Source = e.Website
	if id == "" {
		if a.count(`SELECT COUNT(*) FROM events WHERE member_id = ? AND created_at > ?`, s.MemberID, time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339)) >= memberEventsDay {
			a.accountBack(w, r, "/account", fmt.Sprintf("You have posted %d events in the last day, which is the limit. Try again tomorrow.", memberEventsDay), true)
			return
		}
		e.ID = a.uniqueEventID(slugify(e.Title) + "-" + e.Date)
		e.Status, e.Origin, e.SubmitterName, e.MemberID = "pending_review", "user", s.Member.Name, s.MemberID
		if err := a.insertEvent(e, ipTag(ipOf(r)), s.Member.Email, nil); err != nil {
			a.logf("member event insert: %v", err)
			a.accountBack(w, r, back, "Could not save the event. Please try again.", true)
			return
		}
		_, _ = a.db.Exec(`UPDATE events SET verified_at = ? WHERE id = ?`, now(), e.ID)
		a.notifyAdminEvent(e.ID)
		a.audit(r, "member.event_new", e.ID, fmt.Sprint(s.MemberID))
		a.accountBack(w, r, "/account", "Thanks! Your event is in the queue. A person checks every event before it goes on the site, usually within a day.", false)
		return
	}
	// Editing: whatever the state was, the new text needs a fresh look.
	res, err := a.db.Exec(`UPDATE events SET title=?, date=?, end_date=?, time=?, end_time=?, town=?, category=?, summary=?, cost=?, website=?, source=?, status='pending_review', decided_at=NULL WHERE id=? AND member_id=?`,
		e.Title, e.Date, e.EndDate, e.Time, e.EndTime, e.Town, e.Category, e.Summary, e.Cost, e.Website, e.Source, id, s.MemberID)
	if err != nil {
		a.accountBack(w, r, back, "Could not save the changes. Please try again.", true)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		a.accountBack(w, r, "/account", "That event is not one of yours.", true)
		return
	}
	a.fbCancelRef("event", id)
	a.notifyAdminEvent(id)
	a.audit(r, "member.event_edit", id, fmt.Sprint(s.MemberID))
	a.accountBack(w, r, "/account", "Saved. The updated event goes back in the queue for a quick check before it shows again.", false)
}

// eventProblem validates the shared event fields (also used by the console)
// and fills e.Website. Returns "" when everything is fine.
func (a *App) eventProblem(e *Event, website string) string {
	today := time.Now().In(a.cfg.TZ).Truncate(24 * time.Hour)
	switch {
	case len(e.Title) < 4:
		return "Give the event a title."
	case !towns[e.Town]:
		return "Choose a town."
	case !categories[e.Category]:
		return "Choose a category."
	case !costs[e.Cost]:
		return "Choose a cost."
	case len(e.Summary) < 20:
		return "Describe the event in a sentence or two (20+ characters)."
	case e.Time != "" && !timeRe.MatchString(e.Time), e.EndTime != "" && !timeRe.MatchString(e.EndTime):
		return "Times must look like 18:30."
	}
	d, ok := validDate(e.Date)
	if !ok || d.Before(today.AddDate(0, 0, -1)) || d.After(today.AddDate(0, 0, 400)) {
		return "The date must be a real day within the next year."
	}
	if e.EndDate != "" {
		if ed, ok := validDate(e.EndDate); !ok || ed.Before(d) || ed.After(d.AddDate(0, 0, 60)) {
			return "The end date must be on or after the start, and within 60 days of it."
		}
	}
	var good bool
	if e.Website, good = validURL(website); !good {
		return "The website must be a full http(s) address."
	}
	return ""
}

func (a *App) memberEventWithdraw(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	id := strings.TrimSpace(r.PostForm.Get("id"))
	if r.PostForm.Get("confirm") != "yes" {
		a.accountBack(w, r, "/account", "Tick the box to confirm you want to remove the event.", true)
		return
	}
	res, err := a.db.Exec(`DELETE FROM events WHERE id = ? AND member_id = ?`, id, s.MemberID)
	if err != nil {
		a.accountBack(w, r, "/account", "Could not remove the event.", true)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		a.accountBack(w, r, "/account", "That event is not one of yours.", true)
		return
	}
	a.fbCancelRef("event", id)
	a.audit(r, "member.event_withdraw", id, fmt.Sprint(s.MemberID))
	a.accountBack(w, r, "/account", "The event is removed.", false)
}

/* ---------- settings ---------- */

func (a *App) memberSettingsPage(w http.ResponseWriter, r *http.Request) {
	a.renderAccount(w, r, 200, "acc_settings", "Your account", nil)
}

func (a *App) memberSettingsPost(w http.ResponseWriter, r *http.Request) {
	s := memberOf(r)
	f := r.PostForm
	switch f.Get("action") {
	case "name":
		name := clean(f.Get("name"), 80)
		if len(name) < 2 {
			a.accountBack(w, r, "/account/settings", "Tell us your name.", true)
			return
		}
		_, _ = a.db.Exec(`UPDATE members SET name = ? WHERE id = ?`, name, s.MemberID)
		_, _ = a.db.Exec(`UPDATE events SET submitter_name = ? WHERE member_id = ?`, name, s.MemberID)
		a.accountBack(w, r, "/account/settings", "Name saved.", false)
	case "password":
		if ok, _ := checkPassword(s.Member.PwHash, f.Get("current")); !ok {
			a.accountBack(w, r, "/account/settings", "Your current password is wrong.", true)
			return
		}
		pw := f.Get("password")
		if pw != f.Get("password2") {
			a.accountBack(w, r, "/account/settings", "The two new passwords do not match.", true)
			return
		}
		if msg := passwordProblem(pw, s.Member.Email); msg != "" {
			a.accountBack(w, r, "/account/settings", msg, true)
			return
		}
		_, _ = a.db.Exec(`UPDATE members SET pw_hash = ? WHERE id = ?`, hashPassword(pw), s.MemberID)
		// every other device signs out; this one gets a fresh session
		a.revokeMemberSessions(s.MemberID)
		_ = a.createMemberSession(w, r, s.Member)
		a.audit(r, "member.password", fmt.Sprint(s.MemberID), "")
		a.accountBack(w, r, "/account/settings", "Password changed. Other devices have been signed out.", false)
	case "delete":
		if f.Get("confirm") != "yes" {
			a.accountBack(w, r, "/account/settings", "Tick the box to confirm.", true)
			return
		}
		if ok, _ := checkPassword(s.Member.PwHash, f.Get("current")); !ok {
			a.accountBack(w, r, "/account/settings", "Your password is wrong.", true)
			return
		}
		a.deleteMember(s.MemberID, r, "self")
		a.setMemberCookie(w, "", 0)
		a.accountBack(w, r, "/account/login", "Your account is deleted. Events you posted that were already published stay on the site without your name.", false)
	default:
		a.accountBack(w, r, "/account/settings", "Unknown action.", true)
	}
}

// deleteMember removes the account and its sessions. Published events stay
// (they are public information about an event, not about the person) but
// lose the link to the person; unpublished ones go with the account.
func (a *App) deleteMember(id int64, r *http.Request, who string) {
	_, _ = a.db.Exec(`DELETE FROM events WHERE member_id = ? AND status <> 'approved'`, id)
	_, _ = a.db.Exec(`UPDATE events SET member_id = NULL, submitter_email = '', submitter_name = '' WHERE member_id = ?`, id)
	_, _ = a.db.Exec(`DELETE FROM member_sessions WHERE member_id = ?`, id)
	_, _ = a.db.Exec(`DELETE FROM members WHERE id = ?`, id)
	a.audit(r, "member.delete", fmt.Sprint(id), who)
}

/* ---------- decisions reach the member ---------- */

// notifyMemberDecision tells the person who posted an event what happened
// to it. Nothing is sent for anonymous or admin-made events.
func (a *App) notifyMemberDecision(eventID, status string) {
	var email, name, title string
	err := a.db.QueryRow(`SELECT m.email, m.name, e.title FROM events e JOIN members m ON m.id = e.member_id WHERE e.id = ? AND m.status = 'active'`, eventID).Scan(&email, &name, &title)
	if err != nil {
		return
	}
	if status == "approved" {
		link := a.cfg.SiteURL + "/events.html?ev=" + url.QueryEscape(eventID)
		_ = a.send(Message{To: email, Kind: "member-approved", Subject: "Your event is live: " + title,
			Text: fmt.Sprintf("Hi %s,\n\n\"%s\" has been checked and is now on Helderberg Social:\n\n%s\n\nShare that link wherever you like. To change or remove the event, sign in:\n%s/account\n\nHelderberg Social\n%s\n", name, title, link, a.cfg.APIURL, a.cfg.SiteURL),
			HTML: a.htmlMemberMail("Your event is live", fmt.Sprintf("Hi %s, \"%s\" has been checked and is now on the site. Share the link wherever you like; to change or remove the event, sign in to your account.", name, title), "See it on the site", link)})
		return
	}
	_ = a.send(Message{To: email, Kind: "member-rejected", Subject: "About your event: " + title,
		Text: fmt.Sprintf("Hi %s,\n\nWe looked at \"%s\" and did not publish it. Usually that means it is outside the Helderberg, is not open to the public, or is a commercial promotion rather than a community event.\n\nYou can edit and resubmit it from your account:\n%s/account\n\nIf you think we got it wrong, reply to this email.\n\nHelderberg Social\n%s\n", name, title, a.cfg.APIURL, a.cfg.SiteURL)})
}

func (a *App) htmlMemberMail(title, body, button, link string) string {
	return a.render(mailView{Kind: "member", Title: title, What: body, Name: button, Link: link, Site: a.cfg.SiteURL,
		Foot: "Sent by Helderberg Social because this address has an account on " + a.cfg.SiteURL + "."})
}
