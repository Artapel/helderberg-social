package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// Admin sign-in is two factors, both possession: the emailed link proves the
// mailbox, the authenticator code proves the phone. Only after both does a
// session cookie exist. There is still no password anywhere.
//
//   /admin/login  -> email form -> link mailed (15 min, single use)
//   /admin/auth   -> link consumed -> short "pre" cookie (10 min)
//   /admin/enrol  -> first time only: QR + confirm a code + backup codes
//   /admin/2fa    -> code or backup code -> session cookie (12 h, 2 h idle)

const (
	cookiePre     = "hs_pre"
	cookieSession = "hs_admin"
	sessionMax    = 12 * time.Hour
	sessionIdle   = 2 * time.Hour
	preMax        = 10 * time.Minute
	linkMax       = 15 * time.Minute
	maxCodeTries  = 5
)

type sessKey int

const sessionCtx sessKey = 2

type session struct {
	Hash      string
	CreatedAt string
	LastSeen  string
	ExpiresAt string
	IPHash    string
	UA        string
	Current   bool
}

// codeTries counts failed second-factor attempts per pre-session so a stolen
// link buys at most five guesses.
type tryCounter struct {
	mu sync.Mutex
	m  map[string]int
}

func (t *tryCounter) bump(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.m == nil {
		t.m = map[string]int{}
	}
	t.m[key]++
	if len(t.m) > 10000 {
		t.m = map[string]int{}
	}
	return t.m[key]
}

func (t *tryCounter) reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, key)
}

func hashCookie(v string) string {
	s := sha256.Sum256([]byte("session:" + v))
	return hex.EncodeToString(s[:])
}

func (a *App) secureCookies() bool { return strings.HasPrefix(a.cfg.APIURL, "https://") }

func (a *App) setCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	// Lax, not Strict: the sign-in and moderation links are clicked in a
	// mail client, and browsers withhold Strict cookies on any navigation
	// that starts on another site, including the redirect after /admin/auth.
	// With Strict, the pre-sign-in cookie was never presented and every
	// link bounced to "start again". Lax still sends nothing on cross-site
	// POSTs, and every form carries a CSRF token besides.
	c := &http.Cookie{Name: name, Value: value, Path: "/admin", HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteLaxMode}
	if ttl <= 0 {
		c.MaxAge = -1
	} else {
		c.MaxAge = int(ttl.Seconds())
	}
	http.SetCookie(w, c)
}

func cookieVal(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil || len(c.Value) > 512 {
		return ""
	}
	return c.Value
}

/* ---------- audit ---------- */

func (a *App) audit(r *http.Request, action, target, detail string) {
	ip := ""
	if r != nil {
		ip = ipTag(ipOf(r))
	}
	_, _ = a.db.Exec(`INSERT INTO audit_log(at, action, target, detail, ip_hash) VALUES(?,?,?,?,?)`, now(), action, clean(target, 120), clean(detail, 400), ip)
	a.logf("audit: %s %s %s", action, target, detail)
}

/* ---------- sessions ---------- */

func (a *App) createSession(w http.ResponseWriter, r *http.Request) error {
	raw := randomID(32)
	h := hashCookie(raw)
	exp := time.Now().UTC().Add(sessionMax).Format(time.RFC3339)
	_, err := a.db.Exec(`INSERT INTO sessions(id_hash, created_at, last_seen_at, expires_at, ip_hash, ua) VALUES(?,?,?,?,?,?)`,
		h, now(), now(), exp, ipTag(ipOf(r)), clean(r.UserAgent(), 160))
	if err != nil {
		return err
	}
	a.setCookie(w, cookieSession, raw, sessionMax)
	return nil
}

// currentSession validates the cookie and refreshes idle time. Nil when
// there is no usable session.
func (a *App) currentSession(r *http.Request) *session {
	raw := cookieVal(r, cookieSession)
	if raw == "" {
		return nil
	}
	h := hashCookie(raw)
	var s session
	var revoked int
	err := a.db.QueryRow(`SELECT id_hash, created_at, last_seen_at, expires_at, ip_hash, ua, revoked FROM sessions WHERE id_hash = ?`, h).
		Scan(&s.Hash, &s.CreatedAt, &s.LastSeen, &s.ExpiresAt, &s.IPHash, &s.UA, &revoked)
	if err != nil || revoked == 1 {
		return nil
	}
	nowT := time.Now().UTC()
	exp, _ := time.Parse(time.RFC3339, s.ExpiresAt)
	seen, _ := time.Parse(time.RFC3339, s.LastSeen)
	if nowT.After(exp) || nowT.Sub(seen) > sessionIdle {
		_, _ = a.db.Exec(`UPDATE sessions SET revoked = 1 WHERE id_hash = ?`, h)
		return nil
	}
	if nowT.Sub(seen) > time.Minute {
		_, _ = a.db.Exec(`UPDATE sessions SET last_seen_at = ? WHERE id_hash = ?`, now(), h)
	}
	s.Current = true
	return &s
}

func (a *App) revokeSession(hash string) {
	_, _ = a.db.Exec(`UPDATE sessions SET revoked = 1 WHERE id_hash = ?`, hash)
}

func (a *App) sessions(current string) []session {
	rows, err := a.db.Query(`SELECT id_hash, created_at, last_seen_at, expires_at, ip_hash, ua FROM sessions WHERE revoked = 0 AND expires_at > ? ORDER BY last_seen_at DESC`, now())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []session
	for rows.Next() {
		var s session
		if rows.Scan(&s.Hash, &s.CreatedAt, &s.LastSeen, &s.ExpiresAt, &s.IPHash, &s.UA) == nil {
			s.Current = s.Hash == current
			out = append(out, s)
		}
	}
	return out
}

func sessionOf(r *http.Request) *session {
	s, _ := r.Context().Value(sessionCtx).(*session)
	return s
}

// csrf tokens are bound to the session; forms carry them, and SameSite=Lax
// cookies are never sent on a cross-site POST, so a foreign page cannot
// present the session to a form even before the token check.
func (a *App) csrfToken(s *session) string {
	mac := hmac.New(sha256.New, a.cfg.Secret)
	mac.Write([]byte("csrf:" + s.Hash))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *App) csrfOK(r *http.Request, s *session) bool {
	if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "" && sfs != "same-origin" && sfs != "none" {
		return false
	}
	got := r.PostFormValue("csrf")
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(a.csrfToken(s))) == 1
}

// requireAdmin wraps console handlers. GETs without a session go to the login
// page and come back afterwards; POSTs are refused outright.
func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := a.currentSession(r)
		if s == nil {
			a.setCookie(w, cookieSession, "", 0)
			if r.Method == http.MethodGet {
				next := r.URL.RequestURI()
				http.Redirect(w, r, "/admin/login?next="+url.QueryEscape(next), http.StatusSeeOther)
				return
			}
			a.page(w, 403, "Signed out", "Your session has ended. Sign in again.", "")
			return
		}
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil || !a.csrfOK(r, s) {
				a.audit(r, "csrf.reject", r.URL.Path, "")
				a.page(w, 403, "Request refused", "The form token did not match. Reload the page and try again.", "")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), sessionCtx, s)))
	}
}

// safeNext only ever sends the browser back into the console.
func safeNext(v string) string {
	if strings.HasPrefix(v, "/admin") && !strings.Contains(v, "//") && len(v) < 400 {
		return v
	}
	return "/admin"
}

/* ---------- handlers ---------- */

func (a *App) loginPage(w http.ResponseWriter, r *http.Request) {
	if a.currentSession(r) != nil {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	a.renderAuth(w, 200, "login", map[string]any{"Next": safeNext(r.URL.Query().Get("next")), "Msg": clean(r.URL.Query().Get("msg"), 160)})
}

func (a *App) loginPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := normEmail(r.PostForm.Get("email"))
	next := safeNext(r.PostForm.Get("next"))
	if email == a.cfg.AdminEmail {
		link := a.cfg.APIURL + "/admin/auth?t=" + a.sign("admin-link", next, linkMax)
		_ = a.send(Message{To: a.cfg.AdminEmail, Kind: "admin-login", Subject: "[HS] Your sign-in link",
			Text: "Sign in to the Helderberg Social console (valid 15 minutes, single use):\n\n" + link + "\n\nYou will be asked for your authenticator code next. If you did not request this, ignore it; nothing happens without the code.\n"})
		a.audit(r, "login.link_sent", "", "")
	} else {
		a.audit(r, "login.unknown_email", emailHash(email), "")
	}
	a.renderAuth(w, 200, "sent", nil)
}

// adminLogin is the JSON form of the same step, used by the site's
// moderate.html page.
func (a *App) adminLogin(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Email string `json:"email"`
	}
	if err := readJSON(r, &q); err != nil {
		a.fail(w, 400, err.Error())
		return
	}
	if normEmail(q.Email) == a.cfg.AdminEmail {
		link := a.cfg.APIURL + "/admin/auth?t=" + a.sign("admin-link", "/admin", linkMax)
		_ = a.send(Message{To: a.cfg.AdminEmail, Kind: "admin-login", Subject: "[HS] Your sign-in link",
			Text: "Sign in to the Helderberg Social console (valid 15 minutes, single use):\n\n" + link + "\n\nYou will be asked for your authenticator code next.\n"})
		a.audit(r, "login.link_sent", "", "via site")
	} else {
		a.audit(r, "login.unknown_email", emailHash(q.Email), "via site")
	}
	a.ok(w, "If that is the admin address, a link is on its way.")
}

func (a *App) authLink(w http.ResponseWriter, r *http.Request) {
	p, err := a.consume(r.URL.Query().Get("t"), "admin-link")
	if err != nil {
		a.audit(r, "login.bad_link", "", "")
		http.Redirect(w, r, "/admin/login?msg="+url.QueryEscape("That link is invalid, expired or already used."), http.StatusSeeOther)
		return
	}
	a.setCookie(w, cookiePre, a.sign("pre", p.Subject, preMax), preMax)
	a.audit(r, "login.link_ok", "", "")
	if a.totpEnrolled() {
		http.Redirect(w, r, "/admin/2fa", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/admin/enrol", http.StatusSeeOther)
	}
}

// preSession returns the link-stage token, or sends the user back to login.
func (a *App) preSession(w http.ResponseWriter, r *http.Request) *tokenPayload {
	p, err := a.verify(cookieVal(r, cookiePre), "pre")
	if err != nil {
		http.Redirect(w, r, "/admin/login?msg="+url.QueryEscape("Start again: the sign-in link stage has expired."), http.StatusSeeOther)
		return nil
	}
	return p
}

func (a *App) finishLogin(w http.ResponseWriter, r *http.Request, p *tokenPayload, how string) {
	if err := a.createSession(w, r); err != nil {
		a.logf("session: %v", err)
		a.page(w, 500, "Error", "Could not create a session.", "")
		return
	}
	a.tries.reset(p.ID)
	a.setCookie(w, cookiePre, "", 0)
	a.audit(r, "login.ok", "", how)
	http.Redirect(w, r, safeNext(p.Subject), http.StatusSeeOther)
}

func (a *App) enrolPage(w http.ResponseWriter, r *http.Request) {
	p := a.preSession(w, r)
	if p == nil {
		return
	}
	if a.totpEnrolled() {
		http.Redirect(w, r, "/admin/2fa", http.StatusSeeOther)
		return
	}
	secret, err := a.pendingSecret()
	if err != nil {
		a.page(w, 500, "Error", "Could not prepare enrolment.", "")
		return
	}
	a.renderAuth(w, 200, "enrol", a.enrolView(secret, ""))
}

func (a *App) enrolView(secret, msg string) map[string]any {
	uri := totpURI("Helderberg Social", a.cfg.AdminEmail, secret)
	png, _ := qrcode.Encode(uri, qrcode.Medium, 220)
	pretty := strings.Join(splitEvery(secret, 4), " ")
	return map[string]any{"QR": template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png)), "Secret": pretty, "Msg": msg, "Account": a.cfg.AdminEmail}
}

// pendingSecret keeps the same secret across page reloads during enrolment.
func (a *App) pendingSecret() (string, error) {
	if sealed := a.metaGet("totp:pending"); sealed != "" {
		if b, err := a.open("totp-pending", sealed); err == nil {
			return string(b), nil
		}
	}
	s := newTOTPSecret()
	sealed, err := a.seal("totp-pending", []byte(s))
	if err != nil {
		return "", err
	}
	return s, a.metaSet("totp:pending", sealed)
}

func (a *App) enrolPost(w http.ResponseWriter, r *http.Request) {
	p := a.preSession(w, r)
	if p == nil {
		return
	}
	_ = r.ParseForm()
	secret, err := a.pendingSecret()
	if err != nil || a.totpEnrolled() {
		http.Redirect(w, r, "/admin/2fa", http.StatusSeeOther)
		return
	}
	if totpMatch(secret, r.PostForm.Get("code"), time.Now()) < 0 {
		if a.tries.bump(p.ID) >= maxCodeTries {
			a.setCookie(w, cookiePre, "", 0)
			a.audit(r, "enrol.locked", "", "")
			http.Redirect(w, r, "/admin/login?msg="+url.QueryEscape("Too many wrong codes. Request a new link."), http.StatusSeeOther)
			return
		}
		a.renderAuth(w, 200, "enrol", a.enrolView(secret, "That code did not match. Check the phone's clock and try the next code."))
		return
	}
	codes, err := a.totpEnrol(secret)
	if err != nil {
		a.page(w, 500, "Error", "Could not save the authenticator.", "")
		return
	}
	_, _ = a.db.Exec(`DELETE FROM meta WHERE key = 'totp:pending'`)
	if err := a.createSession(w, r); err != nil {
		a.page(w, 500, "Error", "Could not create a session.", "")
		return
	}
	a.setCookie(w, cookiePre, "", 0)
	a.audit(r, "enrol.ok", "", "authenticator enrolled, backup codes issued")
	a.renderAuth(w, 200, "backup", map[string]any{"Codes": codes, "Next": safeNext(p.Subject)})
}

func (a *App) twofaPage(w http.ResponseWriter, r *http.Request) {
	p := a.preSession(w, r)
	if p == nil {
		return
	}
	if !a.totpEnrolled() {
		http.Redirect(w, r, "/admin/enrol", http.StatusSeeOther)
		return
	}
	a.renderAuth(w, 200, "twofa", map[string]any{"Msg": ""})
}

func (a *App) twofaPost(w http.ResponseWriter, r *http.Request) {
	p := a.preSession(w, r)
	if p == nil {
		return
	}
	_ = r.ParseForm()
	how, ok := a.checkSecondFactor(r.PostForm.Get("code"))
	if !ok {
		n := a.tries.bump(p.ID)
		a.audit(r, "login.bad_code", "", "")
		if n >= maxCodeTries {
			a.setCookie(w, cookiePre, "", 0)
			a.audit(r, "login.locked", "", "")
			http.Redirect(w, r, "/admin/login?msg="+url.QueryEscape("Too many wrong codes. Request a new link."), http.StatusSeeOther)
			return
		}
		a.renderAuth(w, 200, "twofa", map[string]any{"Msg": "That code was not accepted."})
		return
	}
	a.finishLogin(w, r, p, how)
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if s := sessionOf(r); s != nil {
		a.revokeSession(s.Hash)
		a.audit(r, "logout", "", "")
	}
	a.setCookie(w, cookieSession, "", 0)
	http.Redirect(w, r, "/admin/login?msg="+url.QueryEscape("Signed out."), http.StatusSeeOther)
}

func splitEvery(s string, n int) []string {
	var out []string
	for len(s) > n {
		out = append(out, s[:n])
		s = s[n:]
	}
	return append(out, s)
}
