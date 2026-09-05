package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// "Sign in with Google" for member accounts: OpenID Connect authorization
// code flow with PKCE, done with the standard library.
//
//   GET /account/google/start     -> state + PKCE verifier in a signed cookie,
//                                    redirect to Google's consent screen
//   GET /account/google/callback  -> state checked, code exchanged for tokens
//                                    server-side, profile fetched from the
//                                    userinfo endpoint, member found or made,
//                                    session cookie set
//
// The profile is trusted because it comes straight from Google over TLS in
// exchange for a code that only this service's client secret can redeem, so
// the ID token itself is not parsed. Google confirms the address
// (email_verified), so a member made this way needs no confirmation mail and
// an existing password account with the same confirmed address is linked
// rather than duplicated. A member who came in through Google has no
// password (pw_hash = ''); they can set one under Account, or use the reset
// link, and then sign in either way.
//
// Off unless HS_GOOGLE_CLIENT_ID and HS_GOOGLE_CLIENT_SECRET are both set;
// the sign-in and register pages hide the button while it is off.

const (
	cookieOAuth  = "hs_oauth"
	oauthMax     = 10 * time.Minute
	googleScopes = "openid email profile"
)

// googleEndpoints can be pointed at a fake by the tests.
var googleEndpoints = struct{ Auth, Token, Userinfo string }{
	Auth:     "https://accounts.google.com/o/oauth2/v2/auth",
	Token:    "https://oauth2.googleapis.com/token",
	Userinfo: "https://openidconnect.googleapis.com/v1/userinfo",
}

func (a *App) googleEnabled() bool {
	return a.cfg.GoogleClientID != "" && a.cfg.GoogleClientSecret != ""
}

func (a *App) googleRedirectURI() string { return a.cfg.APIURL + "/account/google/callback" }

func (a *App) googleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /account/google/start", a.googleStart)
	mux.HandleFunc("GET /account/google/callback", a.googleCallback)
}

func (a *App) setOAuthCookie(w http.ResponseWriter, value string, ttl time.Duration) {
	c := &http.Cookie{Name: cookieOAuth, Value: value, Path: "/account/google", HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteLaxMode}
	if ttl <= 0 {
		c.MaxAge = -1
	} else {
		c.MaxAge = int(ttl.Seconds())
	}
	http.SetCookie(w, c)
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (a *App) googleStart(w http.ResponseWriter, r *http.Request) {
	next := accountNext(r.URL.Query().Get("next"))
	if !a.googleEnabled() {
		a.accountBack(w, r, "/account/login?next="+url.QueryEscape(next), "Google sign-in is not available at the moment. Sign in with your email address.", true)
		return
	}
	if a.currentMember(r) != nil {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	state := randomID(16)
	verifier := randomID(32) // 64 hex chars, within RFC 7636's 43..128
	// state, verifier and the return path travel in one signed, short-lived
	// cookie; the callback compares state against what Google echoes back.
	a.setOAuthCookie(w, a.sign("google-oauth", state+"|"+verifier+"|"+next, oauthMax), oauthMax)
	q := url.Values{
		"client_id":             {a.cfg.GoogleClientID},
		"redirect_uri":          {a.googleRedirectURI()},
		"response_type":         {"code"},
		"scope":                 {googleScopes},
		"state":                 {state},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
		"prompt":                {"select_account"},
	}
	http.Redirect(w, r, googleEndpoints.Auth+"?"+q.Encode(), http.StatusSeeOther)
}

type googleProfile struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

func (a *App) googleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	back := "/account/login"
	fail := func(msg string, audit string) {
		a.setOAuthCookie(w, "", 0)
		a.audit(r, "member.google_fail", audit, "")
		a.accountBack(w, r, back, msg, true)
	}
	if !a.googleEnabled() {
		fail("Google sign-in is not available at the moment.", "disabled")
		return
	}
	p, err := a.verify(cookieVal(r, cookieOAuth), "google-oauth")
	if err != nil {
		fail("That sign-in attempt has expired. Please try again.", "cookie")
		return
	}
	parts := strings.SplitN(p.Subject, "|", 3)
	if len(parts) != 3 {
		fail("That sign-in attempt has expired. Please try again.", "cookie")
		return
	}
	state, verifier, next := parts[0], parts[1], accountNext(parts[2])
	back = "/account/login?next=" + url.QueryEscape(next)
	if subtle.ConstantTimeCompare([]byte(state), []byte(q.Get("state"))) != 1 {
		fail("That sign-in attempt did not match. Please try again.", "state")
		return
	}
	if e := q.Get("error"); e != "" {
		if e == "access_denied" {
			fail("Google sign-in was cancelled.", "cancelled")
		} else {
			fail("Google could not sign you in ("+clean(e, 40)+"). Please try again.", "google:"+clean(e, 40))
		}
		return
	}
	code := q.Get("code")
	if code == "" || len(code) > 512 {
		fail("Google did not return a sign-in code. Please try again.", "nocode")
		return
	}
	prof, err := a.googleExchange(code, verifier)
	if err != nil {
		a.logf("google: %v", err)
		fail("Google could not confirm who you are. Please try again in a minute.", "exchange")
		return
	}
	email := normEmail(prof.Email)
	switch {
	case prof.Sub == "" || !validEmail(email):
		fail("Google did not share an email address for that account.", "noemail")
		return
	case !prof.EmailVerified:
		fail("That Google account's email address is not verified with Google, so we cannot use it. Sign in with your email address instead.", "unverified")
		return
	}
	// state and code are spent either way
	a.setOAuthCookie(w, "", 0)

	m := a.memberByGoogleSub(prof.Sub)
	how := "login"
	if m == nil {
		if m = a.memberByEmail(email); m != nil {
			// Same confirmed address: this is the same person, link the
			// accounts. Google's confirmation counts as ours.
			if _, err := a.db.Exec(`UPDATE members SET google_sub = ?, verified_at = COALESCE(verified_at, ?) WHERE id = ?`, prof.Sub, now(), m.ID); err != nil {
				a.logf("google link: %v", err)
				fail("Could not sign you in. Please try again.", "db")
				return
			}
			m = a.memberByID(m.ID)
			how = "link"
		}
	}
	if m == nil {
		if !a.settingBool("registrations_on") {
			fail("New accounts are paused for the moment. Please try again later.", "regoff")
			return
		}
		if a.isBlocked("email", emailHash(email)) {
			fail("Could not sign you in with that account.", "blocked")
			return
		}
		name := clean(prof.Name, 80)
		if len(name) < 2 {
			name, _, _ = strings.Cut(email, "@")
			name = clean(name, 80)
		}
		res, err := a.db.Exec(`INSERT INTO members(email, name, pw_hash, google_sub, created_at, verified_at, status, ip_hash) VALUES(?,?,?,?,?,?,'active',?)`,
			email, name, "", prof.Sub, now(), now(), ipTag(ipOf(r)))
		if err != nil {
			a.logf("google register: %v", err)
			fail("Could not create your account. Please try again.", "db")
			return
		}
		id, _ := res.LastInsertId()
		m = a.memberByID(id)
		how = "register"
	}
	if m.Status != "active" {
		a.audit(r, "member.login_disabled", fmt.Sprint(m.ID), "google")
		a.accountBack(w, r, back, "This account has been disabled. Email us if you think that is a mistake.", true)
		return
	}
	if m.Email != email {
		// The Google account's address moved (rare: a Workspace rename).
		// Keep ours; the person can still be reached and the link by sub holds.
		a.audit(r, "member.google_email_differs", fmt.Sprint(m.ID), emailHash(email))
	}
	a.lock.clear(emailHash(m.Email))
	if err := a.createMemberSession(w, r, m); err != nil {
		a.logf("member session: %v", err)
		a.accountBack(w, r, back, "Could not sign you in. Please try again.", true)
		return
	}
	a.audit(r, "member.google_"+how, fmt.Sprint(m.ID), "")
	msg := ""
	switch how {
	case "register":
		msg = "Welcome! Your account is ready; you can post an event straight away."
	case "link":
		msg = "Your Google account is now linked to your Helderberg Social account."
	}
	if msg != "" {
		a.accountBack(w, r, next, msg, false)
		return
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// googleExchange redeems the code for tokens and reads the profile.
func (a *App) googleExchange(code, verifier string) (*googleProfile, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	form := url.Values{
		"code":          {code},
		"client_id":     {a.cfg.GoogleClientID},
		"client_secret": {a.cfg.GoogleClientSecret},
		"redirect_uri":  {a.googleRedirectURI()},
		"grant_type":    {"authorization_code"},
		"code_verifier": {verifier},
	}
	resp, err := client.PostForm(googleEndpoints.Token, form)
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if resp.StatusCode != 200 {
		var e struct{ Error, ErrorDescription string }
		_ = json.Unmarshal(body, &e)
		return nil, fmt.Errorf("token: HTTP %d %s", resp.StatusCode, clean(e.Error, 60))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return nil, fmt.Errorf("token: no access_token in reply")
	}
	req, _ := http.NewRequest("GET", googleEndpoints.Userinfo, nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err = client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo: %w", err)
	}
	body, _ = io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("userinfo: HTTP %d", resp.StatusCode)
	}
	var prof googleProfile
	if err := json.Unmarshal(body, &prof); err != nil {
		return nil, fmt.Errorf("userinfo: %w", err)
	}
	return &prof, nil
}

func (a *App) memberByGoogleSub(sub string) *Member {
	if sub == "" {
		return nil
	}
	m, err := scanMember(a.db.QueryRow(`SELECT `+memberCols+` FROM members WHERE google_sub = ?`, sub))
	if err != nil {
		return nil
	}
	return m
}
