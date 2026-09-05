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

// "Sign in with Google / Microsoft / Yahoo" for member accounts: the OpenID
// Connect authorization-code flow, done with the standard library.
//
//   GET /account/{provider}/start     -> state + PKCE verifier in a signed cookie,
//                                        redirect to the provider's consent screen
//   GET /account/{provider}/callback  -> state checked, code exchanged for tokens
//                                        server-side, profile read from the ID token
//                                        and the userinfo endpoint, member found or
//                                        made, session cookie set
//
// The profile is trusted because it comes straight from the provider over TLS
// in exchange for a code that only this service's client secret can redeem,
// so the ID token's signature is not checked (OpenID Connect Core 3.1.3.7
// allows exactly that for tokens received from the token endpoint directly).
//
// Which address to believe is the whole game. Google and Yahoo say
// email_verified; Microsoft does not, and an organisation's Entra ID admin
// can set any address they like on a work account (the "nOAuth" problem), so
// a Microsoft address counts as verified only for a personal account (the
// consumer tenant) or when the xms_edov claim says the domain is verified.
// A verified address links an existing member or creates one with no
// confirmation mail; an unverified one may create a member who still has to
// click our confirmation link, and never links itself to an existing member.
//
// Each provider is off unless HS_<PROVIDER>_CLIENT_ID and _CLIENT_SECRET are
// both set; the sign-in and register pages show a button per configured
// provider. A member signed in already can link a provider from Account.

const (
	cookieOAuth = "hs_oauth"
	oauthMax    = 10 * time.Minute
	// Microsoft's tenant for personal accounts (outlook.com, hotmail.com,
	// live.com and any address registered as a Microsoft account).
	msConsumerTenant = "9188040d-6c67-4c5b-b112-36a304b66dad"
)

type oauthProvider struct {
	Key, Name             string
	Auth, Token, Userinfo string
	Scopes                string
	Extra                 map[string]string // extra authorization parameters
	PKCE                  bool              // send code_challenge / code_verifier
	BasicAuth             bool              // client credentials in the Authorization header at the token endpoint
	IDSuffix              string            // what a client id must end with, when known
	Note                  string            // shown in the console next to the switch
}

// oauthProviders lists the providers in the order the buttons appear. Tests
// point the endpoints at a fake.
var oauthProviders = []*oauthProvider{
	{Key: "google", Name: "Google",
		Auth: "https://accounts.google.com/o/oauth2/v2/auth", Token: "https://oauth2.googleapis.com/token", Userinfo: "https://openidconnect.googleapis.com/v1/userinfo",
		Scopes: "openid email profile", Extra: map[string]string{"prompt": "select_account"}, PKCE: true, IDSuffix: ".apps.googleusercontent.com",
		Note: "Google Cloud console → APIs & Services → Credentials → OAuth client ID (Web application)."},
	{Key: "microsoft", Name: "Microsoft",
		Auth: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize", Token: "https://login.microsoftonline.com/common/oauth2/v2.0/token", Userinfo: "https://graph.microsoft.com/oidc/userinfo",
		Scopes: "openid email profile", Extra: map[string]string{"prompt": "select_account"}, PKCE: true,
		Note: "Entra admin centre → App registrations → accounts in any organisational directory and personal Microsoft accounts; add the optional claim xms_edov so work addresses can count as verified."},
	{Key: "yahoo", Name: "Yahoo",
		Auth: "https://api.login.yahoo.com/oauth2/request_auth", Token: "https://api.login.yahoo.com/oauth2/get_token", Userinfo: "https://api.login.yahoo.com/openid/v1/userinfo",
		Scopes: "openid email profile", BasicAuth: true,
		Note: "developer.yahoo.com → Create an app → OpenID Connect permissions: email, profile."},
}

func oauthProviderByKey(key string) *oauthProvider {
	for _, p := range oauthProviders {
		if p.Key == key {
			return p
		}
	}
	return nil
}

// oauthEnabled says whether a provider has credentials.
func (a *App) oauthEnabled(key string) bool {
	c, ok := a.cfg.OAuth[key]
	return ok && c.ID != "" && c.Secret != ""
}

// oauthOn lists the configured providers, in display order.
func (a *App) oauthOn() []*oauthProvider {
	var out []*oauthProvider
	for _, p := range oauthProviders {
		if a.oauthEnabled(p.Key) {
			out = append(out, p)
		}
	}
	return out
}

func (a *App) oauthRedirectURI(p *oauthProvider) string {
	return a.cfg.APIURL + "/account/" + p.Key + "/callback"
}

func (a *App) oauthRoutes(mux *http.ServeMux) {
	for _, p := range oauthProviders {
		p := p
		mux.HandleFunc("GET /account/"+p.Key+"/start", func(w http.ResponseWriter, r *http.Request) { a.oauthStart(w, r, p) })
		mux.HandleFunc("GET /account/"+p.Key+"/callback", func(w http.ResponseWriter, r *http.Request) { a.oauthCallback(w, r, p) })
	}
}

func (a *App) setOAuthCookie(w http.ResponseWriter, value string, ttl time.Duration) {
	c := &http.Cookie{Name: cookieOAuth, Value: value, Path: "/account", HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteLaxMode}
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

func (a *App) oauthStart(w http.ResponseWriter, r *http.Request, p *oauthProvider) {
	next := accountNext(r.URL.Query().Get("next"))
	if !a.oauthEnabled(p.Key) {
		a.accountBack(w, r, "/account/login?next="+url.QueryEscape(next), p.Name+" sign-in is not available at the moment. Sign in with your email address.", true)
		return
	}
	// A signed-in member may link the provider to this account ("link=1"
	// from Account); otherwise there is nothing to do.
	mode := "signin"
	if s := a.currentMember(r); s != nil {
		if r.URL.Query().Get("link") != "1" {
			http.Redirect(w, r, next, http.StatusSeeOther)
			return
		}
		mode = "link:" + fmt.Sprint(s.MemberID)
		next = "/account/settings"
	}
	state := randomID(16)
	verifier := randomID(32) // 64 hex chars, within RFC 7636's 43..128
	// state, verifier, mode and the return path travel in one signed,
	// short-lived cookie; the callback compares state against what the
	// provider echoes back.
	a.setOAuthCookie(w, a.sign("oauth-"+p.Key, state+"|"+verifier+"|"+mode+"|"+next, oauthMax), oauthMax)
	q := url.Values{
		"client_id":     {a.cfg.OAuth[p.Key].ID},
		"redirect_uri":  {a.oauthRedirectURI(p)},
		"response_type": {"code"},
		"scope":         {p.Scopes},
		"state":         {state},
	}
	if p.PKCE {
		q.Set("code_challenge", pkceChallenge(verifier))
		q.Set("code_challenge_method", "S256")
	}
	for k, v := range p.Extra {
		q.Set(k, v)
	}
	http.Redirect(w, r, p.Auth+"?"+q.Encode(), http.StatusSeeOther)
}

// oauthProfile is what the provider told us, after the ID token and the
// userinfo reply are merged and the provider-specific verification rule has
// been applied.
type oauthProfile struct {
	Sub, Email, Name string
	EmailVerified    bool
}

func (a *App) oauthCallback(w http.ResponseWriter, r *http.Request, p *oauthProvider) {
	q := r.URL.Query()
	back := "/account/login"
	fail := func(msg string, audit string) {
		a.setOAuthCookie(w, "", 0)
		a.audit(r, "member.oauth_fail", p.Key, audit)
		a.accountBack(w, r, back, msg, true)
	}
	if !a.oauthEnabled(p.Key) {
		fail(p.Name+" sign-in is not available at the moment.", "disabled")
		return
	}
	tok, err := a.verify(cookieVal(r, cookieOAuth), "oauth-"+p.Key)
	if err != nil {
		fail("That sign-in attempt has expired. Please try again.", "cookie")
		return
	}
	parts := strings.SplitN(tok.Subject, "|", 4)
	if len(parts) != 4 {
		fail("That sign-in attempt has expired. Please try again.", "cookie")
		return
	}
	state, verifier, mode, next := parts[0], parts[1], parts[2], accountNext(parts[3])
	linkTo := int64(0)
	if strings.HasPrefix(mode, "link:") {
		fmt.Sscan(strings.TrimPrefix(mode, "link:"), &linkTo)
		back = "/account/settings"
	} else {
		back = "/account/login?next=" + url.QueryEscape(next)
	}
	if subtle.ConstantTimeCompare([]byte(state), []byte(q.Get("state"))) != 1 {
		fail("That sign-in attempt did not match. Please try again.", "state")
		return
	}
	if e := q.Get("error"); e != "" {
		if e == "access_denied" || e == "consent_required" {
			fail(p.Name+" sign-in was cancelled.", "cancelled")
		} else {
			fail(p.Name+" could not sign you in ("+clean(e, 40)+"). Please try again.", "provider:"+clean(e, 40))
		}
		return
	}
	code := q.Get("code")
	if code == "" || len(code) > 2048 {
		fail(p.Name+" did not return a sign-in code. Please try again.", "nocode")
		return
	}
	prof, err := a.oauthExchange(p, code, verifier)
	if err != nil {
		a.logf("oauth %s: %v", p.Key, err)
		fail(p.Name+" could not confirm who you are. Please try again in a minute.", "exchange")
		return
	}
	email := normEmail(prof.Email)
	if prof.Sub == "" {
		fail(p.Name+" did not identify that account.", "nosub")
		return
	}
	// state and code are spent either way
	a.setOAuthCookie(w, "", 0)

	// Linking from Account: the person is signed in, so the address does
	// not matter; the provider account simply becomes another way in.
	if linkTo > 0 {
		s := a.currentMember(r)
		if s == nil || s.MemberID != linkTo {
			a.accountBack(w, r, "/account/login", "Sign in first, then link "+p.Name+" from Account.", true)
			return
		}
		if owner := a.memberByIdentity(p.Key, prof.Sub); owner != nil && owner.ID != s.MemberID {
			a.audit(r, "member.oauth_fail", p.Key, "link-taken")
			a.accountBack(w, r, back, "That "+p.Name+" account is already linked to a different Helderberg Social account.", true)
			return
		}
		if err := a.linkIdentity(s.MemberID, p.Key, prof.Sub, email); err != nil {
			a.accountBack(w, r, back, "Could not link "+p.Name+". Please try again.", true)
			return
		}
		a.audit(r, "member.oauth_link", fmt.Sprint(s.MemberID), p.Key)
		a.accountBack(w, r, back, p.Name+" is now linked; its button signs you into this account.", false)
		return
	}

	m := a.memberByIdentity(p.Key, prof.Sub)
	how := "login"
	if m == nil {
		if !validEmail(email) {
			fail(p.Name+" did not share an email address for that account. Create an account with your email address instead.", "noemail")
			return
		}
		if existing := a.memberByEmail(email); existing != nil {
			if !prof.EmailVerified {
				// Same address, but nobody has proved it is theirs: the
				// account owner can link from Account after signing in.
				fail("An account with "+email+" already exists. Sign in with your password (or reset it), then link "+p.Name+" under Account.", "unverified-existing")
				return
			}
			// Same confirmed address: this is the same person, link the
			// accounts. The provider's confirmation counts as ours.
			if err := a.linkIdentity(existing.ID, p.Key, prof.Sub, email); err != nil {
				a.logf("oauth link: %v", err)
				fail("Could not sign you in. Please try again.", "db")
				return
			}
			_, _ = a.db.Exec(`UPDATE members SET verified_at = COALESCE(verified_at, ?) WHERE id = ?`, now(), existing.ID)
			m = a.memberByID(existing.ID)
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
		var verifiedAt any
		if prof.EmailVerified {
			verifiedAt = now()
		}
		res, err := a.db.Exec(`INSERT INTO members(email, name, pw_hash, created_at, verified_at, status, ip_hash) VALUES(?,?,?,?,?,'active',?)`,
			email, name, "", now(), verifiedAt, ipTag(ipOf(r)))
		if err != nil {
			a.logf("oauth register: %v", err)
			fail("Could not create your account. Please try again.", "db")
			return
		}
		id, _ := res.LastInsertId()
		if err := a.linkIdentity(id, p.Key, prof.Sub, email); err != nil {
			a.logf("oauth register link: %v", err)
		}
		m = a.memberByID(id)
		how = "register"
		if !prof.EmailVerified {
			// The provider vouched for the person but not for the address:
			// our own confirmation mail closes that gap before any sign-in.
			a.sendMemberVerify(m, next)
			a.audit(r, "member.oauth_register_unverified", fmt.Sprint(m.ID), p.Key)
			a.accountBack(w, r, "/account/login", "Nearly there. "+p.Name+" could not confirm that "+email+" is yours, so we have sent an email there; open the link in it to finish creating your account.", false)
			return
		}
	}
	if m.Status != "active" {
		a.audit(r, "member.login_disabled", fmt.Sprint(m.ID), p.Key)
		a.accountBack(w, r, back, "This account has been disabled. Email us if you think that is a mistake.", true)
		return
	}
	if m.VerifiedAt == "" {
		// A member who registered through a provider that could not vouch
		// for the address, and has not clicked our mail yet.
		a.accountBack(w, r, "/account/login", "Please open the confirmation email we sent to "+m.Email+" first. Sign in with your email address to have it sent again.", true)
		return
	}
	if m.Email != email && email != "" {
		// The provider's address moved (a rename). Keep ours; the link by
		// subject holds and the person can still be reached.
		a.audit(r, "member.oauth_email_differs", fmt.Sprint(m.ID), p.Key+":"+emailHash(email))
	}
	a.lock.clear(emailHash(m.Email))
	if err := a.createMemberSession(w, r, m); err != nil {
		a.logf("member session: %v", err)
		a.accountBack(w, r, back, "Could not sign you in. Please try again.", true)
		return
	}
	a.audit(r, "member.oauth_"+how, fmt.Sprint(m.ID), p.Key)
	msg := ""
	switch how {
	case "register":
		msg = "Welcome! Your account is ready; you can post an event straight away."
	case "link":
		msg = "Your " + p.Name + " account is now linked to your Helderberg Social account."
	}
	if msg != "" {
		a.accountBack(w, r, next, msg, false)
		return
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// idClaims is the subset of ID-token / userinfo claims that matter here.
// email_verified and xms_edov arrive as bool from some providers and as the
// strings "true"/"false" from others, hence json.RawMessage.
type idClaims struct {
	Sub               string          `json:"sub"`
	Email             string          `json:"email"`
	EmailVerified     json.RawMessage `json:"email_verified"`
	Name              string          `json:"name"`
	PreferredUsername string          `json:"preferred_username"`
	TID               string          `json:"tid"`
	XmsEdov           json.RawMessage `json:"xms_edov"`
}

func rawTrue(v json.RawMessage) bool {
	s := strings.Trim(strings.TrimSpace(string(v)), `"`)
	return s == "true" || s == "1"
}

// oauthExchange redeems the code for tokens and reads the profile from the
// ID token (when there is one) and the userinfo endpoint.
func (a *App) oauthExchange(p *oauthProvider, code, verifier string) (*oauthProfile, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	creds := a.cfg.OAuth[p.Key]
	form := url.Values{
		"code":         {code},
		"redirect_uri": {a.oauthRedirectURI(p)},
		"grant_type":   {"authorization_code"},
	}
	if p.PKCE {
		form.Set("code_verifier", verifier)
	}
	if !p.BasicAuth {
		form.Set("client_id", creds.ID)
		form.Set("client_secret", creds.Secret)
	}
	req, _ := http.NewRequest("POST", p.Token, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if p.BasicAuth {
		req.SetBasicAuth(url.QueryEscape(creds.ID), url.QueryEscape(creds.Secret))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if resp.StatusCode != 200 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		return nil, fmt.Errorf("token: HTTP %d %s", resp.StatusCode, clean(e.Error, 60))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return nil, fmt.Errorf("token: no access_token in reply")
	}
	var claims idClaims
	if tok.IDToken != "" {
		if c, err := decodeIDToken(tok.IDToken); err == nil {
			claims = c
		} else {
			a.logf("oauth %s: id_token: %v", p.Key, err)
		}
	}
	if p.Userinfo != "" {
		req, _ := http.NewRequest("GET", p.Userinfo, nil)
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		req.Header.Set("Accept", "application/json")
		resp, err = client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("userinfo: %w", err)
		}
		body, _ = io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("userinfo: HTTP %d", resp.StatusCode)
		}
		var ui idClaims
		if err := json.Unmarshal(body, &ui); err != nil {
			return nil, fmt.Errorf("userinfo: %w", err)
		}
		// The ID token wins where both speak; userinfo fills the gaps.
		if claims.Sub == "" {
			claims.Sub = ui.Sub
		} else if ui.Sub != "" && ui.Sub != claims.Sub {
			return nil, fmt.Errorf("userinfo subject differs from the ID token")
		}
		if claims.Email == "" {
			claims.Email = ui.Email
		}
		if len(claims.EmailVerified) == 0 {
			claims.EmailVerified = ui.EmailVerified
		}
		if claims.Name == "" {
			claims.Name = ui.Name
		}
	}
	prof := &oauthProfile{Sub: claims.Sub, Email: claims.Email, Name: claims.Name}
	if prof.Email == "" && strings.Contains(claims.PreferredUsername, "@") {
		prof.Email = claims.PreferredUsername
	}
	switch p.Key {
	case "microsoft":
		prof.EmailVerified = claims.TID == msConsumerTenant || rawTrue(claims.XmsEdov)
	default:
		prof.EmailVerified = rawTrue(claims.EmailVerified)
	}
	return prof, nil
}

// decodeIDToken reads the payload of a JWT without checking its signature;
// see the note at the top of the file for why that is acceptable here.
func decodeIDToken(t string) (idClaims, error) {
	var c idClaims
	parts := strings.Split(t, ".")
	if len(parts) != 3 {
		return c, fmt.Errorf("not a JWT")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	return c, nil
}

/* ---------- identities ---------- */

type memberIdentity struct {
	Provider, Sub, Email, LinkedAt string
}

func (a *App) memberByIdentity(provider, sub string) *Member {
	if sub == "" {
		return nil
	}
	var id int64
	if err := a.db.QueryRow(`SELECT member_id FROM member_identities WHERE provider = ? AND sub = ?`, provider, sub).Scan(&id); err != nil {
		return nil
	}
	return a.memberByID(id)
}

func (a *App) linkIdentity(memberID int64, provider, sub, email string) error {
	_, err := a.db.Exec(`INSERT INTO member_identities(provider, sub, member_id, email, linked_at) VALUES(?,?,?,?,?) ON CONFLICT(provider, sub) DO UPDATE SET member_id = excluded.member_id, email = excluded.email, linked_at = excluded.linked_at`,
		provider, sub, memberID, email, now())
	return err
}

func (a *App) identities(memberID int64) []memberIdentity {
	rows, err := a.db.Query(`SELECT provider, sub, email, linked_at FROM member_identities WHERE member_id = ? ORDER BY linked_at`, memberID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []memberIdentity
	for rows.Next() {
		var i memberIdentity
		if rows.Scan(&i.Provider, &i.Sub, &i.Email, &i.LinkedAt) == nil {
			out = append(out, i)
		}
	}
	return out
}

// providerName turns a key into its display name ("google" -> "Google").
func providerName(key string) string {
	if p := oauthProviderByKey(key); p != nil {
		return p.Name
	}
	return key
}

// loginInfo feeds the console's System page.
type loginInfo struct {
	Key, Name, Note, ClientID, Redirect, Env string
	On                                       bool
	Members                                  int
}

// loginKeys lists configured provider keys for /api/health and the site.
func (a *App) loginKeys() []string {
	out := []string{}
	for _, p := range a.oauthOn() {
		out = append(out, p.Key)
	}
	return out
}
