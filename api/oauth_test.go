package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// fakeIdP stands in for Google, Microsoft or Yahoo: it hands out a code,
// checks the client credentials (body or Basic) and the PKCE verifier
// against the challenge it was shown, and serves an ID token and a profile.
type fakeIdP struct {
	t         *testing.T
	p         *oauthProvider
	srv       *httptest.Server
	challenge string
	profile   map[string]any // userinfo reply
	idClaims  map[string]any // ID token payload; nil for no id_token
	tokenHits int
	sawBasic  bool
	sawPKCE   bool
}

func newFakeIdP(t *testing.T, key string) *fakeIdP {
	t.Helper()
	p := oauthProviderByKey(key)
	if p == nil {
		t.Fatalf("no provider %s", key)
	}
	f := &fakeIdP{t: t, p: p, profile: map[string]any{"sub": key + "-123", "email": "Ann@Example.org", "email_verified": true, "name": "Ann Example"}}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.tokenHits++
		id, secret := r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
		if u, pw, ok := r.BasicAuth(); ok {
			f.sawBasic = true
			id, secret = u, pw
		}
		if v := r.PostForm.Get("code_verifier"); v != "" {
			f.sawPKCE = true
			if pkceChallenge(v) != f.challenge {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
		}
		if r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("code") != "the-code" ||
			id != "id."+key+".test" || secret != "shh-"+key ||
			r.PostForm.Get("redirect_uri") != "https://api.helderbergsocial.co.za/account/"+key+"/callback" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		reply := map[string]any{"access_token": "at-" + key, "token_type": "Bearer"}
		if f.idClaims != nil {
			payload, _ := json.Marshal(f.idClaims)
			reply["id_token"] = "eyJhbGciOiJSUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
		}
		_ = json.NewEncoder(w).Encode(reply)
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at-"+key {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(f.profile)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	oldAuth, oldTok, oldUI := p.Auth, p.Token, p.Userinfo
	p.Auth, p.Token, p.Userinfo = f.srv.URL+"/auth", f.srv.URL+"/token", f.srv.URL+"/userinfo"
	t.Cleanup(func() { p.Auth, p.Token, p.Userinfo = oldAuth, oldTok, oldUI })
	return f
}

// oauthApp is a test app with the named providers configured.
func oauthApp(t *testing.T, keys ...string) (*App, string, map[string]*fakeIdP) {
	t.Helper()
	a, mail := testApp(t)
	idps := map[string]*fakeIdP{}
	for _, k := range keys {
		a.cfg.OAuth[k] = oauthCreds{ID: "id." + k + ".test", Secret: "shh-" + k}
		idps[k] = newFakeIdP(t, k)
	}
	return a, mail, idps
}

// start walks /account/<p>/start and returns the state the provider would echo.
func (f *fakeIdP) start(c *client, next string, link bool) string {
	f.t.Helper()
	u := "/account/" + f.p.Key + "/start?next=" + url.QueryEscape(next)
	if link {
		u += "&link=1"
	}
	rr := c.do("GET", u, nil)
	if rr.Code != 303 {
		f.t.Fatalf("start: %d", rr.Code)
	}
	loc, err := url.Parse(rr.Header().Get("Location"))
	if err != nil || !strings.HasPrefix(loc.String(), f.srv.URL+"/auth?") {
		f.t.Fatalf("start redirect: %s", rr.Header().Get("Location"))
	}
	q := loc.Query()
	if q.Get("response_type") != "code" || q.Get("scope") != f.p.Scopes || q.Get("client_id") != "id."+f.p.Key+".test" || q.Get("redirect_uri") != "https://api.helderbergsocial.co.za/account/"+f.p.Key+"/callback" {
		f.t.Fatalf("auth params: %v", q)
	}
	if f.p.PKCE != (q.Get("code_challenge_method") == "S256") {
		f.t.Fatalf("PKCE mismatch for %s: %v", f.p.Key, q)
	}
	if c.cookies[cookieOAuth] == "" {
		f.t.Fatal("no oauth cookie")
	}
	f.challenge = q.Get("code_challenge")
	return q.Get("state")
}

func (f *fakeIdP) callback(c *client, state, code string) *httptest.ResponseRecorder {
	return c.do("GET", "/account/"+f.p.Key+"/callback?state="+url.QueryEscape(state)+"&code="+url.QueryEscape(code), nil)
}

// signIn runs the whole dance and returns the callback response.
func (f *fakeIdP) signIn(c *client, next string) *httptest.ResponseRecorder {
	f.t.Helper()
	return f.callback(c, f.start(c, next, false), "the-code")
}

func TestOAuthGoogleCreatesAndReusesMember(t *testing.T) {
	a, mail, idps := oauthApp(t, "google")
	g := idps["google"]
	c := newClient(t, a.routes(), "10.0.0.9")

	// the buttons show while a provider is configured
	if rr := c.do("GET", "/account/login", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "Sign in with Google") || !strings.Contains(rr.Body.String(), "/account/google/start") || strings.Contains(rr.Body.String(), "Microsoft") {
		t.Fatalf("login page buttons wrong: %d", rr.Code)
	}
	if rr := c.do("GET", "/account/register", nil); !strings.Contains(rr.Body.String(), "Continue with Google") {
		t.Fatal("register page lacks the Google button")
	}

	rr := g.signIn(c, "/account/events/new")
	if rr.Code != 303 || !strings.HasPrefix(rr.Header().Get("Location"), "/account/events/new") || strings.Contains(rr.Header().Get("Location"), "err=1") {
		t.Fatalf("callback: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if c.cookies[cookieMember] == "" || c.cookies[cookieOAuth] != "" {
		t.Fatalf("cookies after sign-in: member=%q oauth=%q", c.cookies[cookieMember], c.cookies[cookieOAuth])
	}
	if !g.sawPKCE || g.sawBasic {
		t.Fatalf("google token call: pkce=%v basic=%v", g.sawPKCE, g.sawBasic)
	}
	m := a.memberByEmail("ann@example.org")
	if m == nil || m.Logins != "google" || m.VerifiedAt == "" || m.HasPassword() || m.Name != "Ann Example" {
		t.Fatalf("member after google register: %+v", m)
	}
	if rr := c.do("GET", "/account", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "My events") {
		t.Fatalf("signed-in page: %d", rr.Code)
	}

	// second sign-in from a fresh browser: same member, no duplicate
	c2 := newClient(t, a.routes(), "10.0.0.10")
	if rr := g.signIn(c2, "/account"); rr.Code != 303 || rr.Header().Get("Location") != "/account" {
		t.Fatalf("second callback: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if n := a.count(`SELECT COUNT(*) FROM members`); n != 1 {
		t.Fatalf("members = %d, want 1", n)
	}
	if g.tokenHits != 2 {
		t.Fatalf("token endpoint hit %d times", g.tokenHits)
	}

	// a signed-in member hitting start without link=1 is just sent on
	if rr := c2.do("GET", "/account/google/start", nil); rr.Code != 303 || rr.Header().Get("Location") != "/account" {
		t.Fatalf("start while signed in: %d %s", rr.Code, rr.Header().Get("Location"))
	}

	// console shows the pill and the System panel
	ac, _ := login(t, a, mail, "10.0.0.1")
	if rr := ac.do("GET", "/admin/members", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), `<span class="pill">Google</span>`) {
		t.Fatalf("members page lacks Google pill: %d", rr.Code)
	}
	if rr := ac.do("GET", "/admin/system", nil); !strings.Contains(rr.Body.String(), "Google <span class=\"pill ok\">on</span>") || !strings.Contains(rr.Body.String(), "Microsoft <span class=\"pill\">off</span>") || !strings.Contains(rr.Body.String(), "/account/google/callback") {
		t.Fatal("system page lacks the providers panel")
	}
	if rr := get(t, a.routes(), "/api/health"); !strings.Contains(rr.Body.String(), `"logins":["google"]`) {
		t.Fatalf("health: %s", rr.Body)
	}
}

func TestOAuthLinksExistingPasswordAccount(t *testing.T) {
	a, mail, idps := oauthApp(t, "google", "yahoo")
	g, y := idps["google"], idps["yahoo"]
	pc, _ := register(t, a, mail, "ann@example.org", "Ann", "correct horse battery", "10.0.0.2")
	pc.do("POST", "/account/logout", url.Values{"csrf": {accountCSRF(t, pc)}})

	c := newClient(t, a.routes(), "10.0.0.3")
	if rr := g.signIn(c, "/account"); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "linked") {
		t.Fatalf("link callback: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if n := a.count(`SELECT COUNT(*) FROM members`); n != 1 {
		t.Fatalf("members = %d, want 1 (linked, not duplicated)", n)
	}
	m := a.memberByEmail("ann@example.org")
	if m.Logins != "google" || !m.HasPassword() || m.Name != "Ann" {
		t.Fatalf("linked member: %+v", m)
	}
	// the password still works too
	c3 := newClient(t, a.routes(), "10.0.0.4")
	if rr := c3.do("POST", "/account/login", url.Values{"email": {"ann@example.org"}, "password": {"correct horse battery"}}); rr.Code != 303 || c3.cookies[cookieMember] == "" {
		t.Fatalf("password login after link: %d", rr.Code)
	}
	csrf := accountCSRF(t, c3)
	body := c3.do("GET", "/account/settings", nil).Body.String()
	if !strings.Contains(body, "Other ways to sign in") || !strings.Contains(body, `name="provider" value="google"`) || !strings.Contains(body, "/account/yahoo/start?link=1") {
		t.Fatal("settings do not show linked Google and a Link Yahoo button")
	}

	// Yahoo: no PKCE, Basic client auth; linked from settings while signed in,
	// under a different address, and it still attaches to this account
	y.profile["email"], y.profile["sub"] = "other@yahoo.com", "y-9"
	state := y.start(c3, "/account", true)
	if rr := y.callback(c3, state, "the-code"); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "now+linked") {
		t.Fatalf("yahoo link: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if !y.sawBasic || y.sawPKCE {
		t.Fatalf("yahoo token call: basic=%v pkce=%v", y.sawBasic, y.sawPKCE)
	}
	if m := a.memberByEmail("ann@example.org"); m.Logins != "google,yahoo" || m.Ways() != 3 {
		t.Fatalf("after yahoo link: %+v", m)
	}
	// that Yahoo account now signs into Ann's account from anywhere
	c4 := newClient(t, a.routes(), "10.0.0.5")
	if rr := y.signIn(c4, "/account"); rr.Code != 303 || c4.cookies[cookieMember] == "" {
		t.Fatalf("yahoo sign-in after link: %d", rr.Code)
	}
	if n := a.count(`SELECT COUNT(*) FROM members`); n != 1 {
		t.Fatalf("members = %d after yahoo sign-in", n)
	}
	// a second member cannot link the same Yahoo account
	bc, bcsrf := register(t, a, mail, "bob@example.org", "Bob", "another fine password", "10.0.0.6")
	_ = bcsrf
	state = y.start(bc, "/account", true)
	if rr := y.callback(bc, state, "the-code"); !strings.Contains(rr.Header().Get("Location"), "different+Helderberg") {
		t.Fatalf("second link of same yahoo: %s", rr.Header().Get("Location"))
	}

	// unlink: refused for the last way in, fine otherwise
	if rr := c3.do("POST", "/account/settings", url.Values{"csrf": {csrf}, "action": {"unlink"}, "provider": {"google"}}); rr.Code != 303 || strings.Contains(rr.Header().Get("Location"), "err=1") {
		t.Fatalf("unlink google: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if m := a.memberByEmail("ann@example.org"); m.Logins != "yahoo" {
		t.Fatalf("after unlink: %q", m.Logins)
	}
	if rr := c3.do("POST", "/account/settings", url.Values{"csrf": {csrf}, "action": {"unlink"}, "provider": {"google"}}); !strings.Contains(rr.Header().Get("Location"), "not+linked") {
		t.Fatalf("unlink twice: %s", rr.Header().Get("Location"))
	}
}

func TestOAuthMicrosoftVerification(t *testing.T) {
	a, mail, idps := oauthApp(t, "microsoft")
	ms := idps["microsoft"]
	// Graph's userinfo has no email_verified; the ID token carries tid.
	delete(ms.profile, "email_verified")

	// 1. personal account (consumer tenant): verified, straight in
	ms.idClaims = map[string]any{"sub": "ms-1", "email": "ann@outlook.com", "name": "Ann", "tid": msConsumerTenant}
	ms.profile["sub"], ms.profile["email"] = "ms-1", "ann@outlook.com"
	c := newClient(t, a.routes(), "10.0.0.7")
	if rr := ms.signIn(c, "/account"); rr.Code != 303 || c.cookies[cookieMember] == "" {
		t.Fatalf("MSA sign-in: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if m := a.memberByEmail("ann@outlook.com"); m == nil || m.VerifiedAt == "" || m.Logins != "microsoft" {
		t.Fatalf("MSA member: %+v", m)
	}

	// 2. work account, no xms_edov: the address is NOT trusted. A new member
	// is created but must click our confirmation mail; and it cannot attach
	// itself to an existing account by address.
	ms.idClaims = map[string]any{"sub": "ms-2", "email": "carol@example.org", "name": "Carol", "tid": "11111111-2222-3333-4444-555555555555"}
	ms.profile["sub"], ms.profile["email"] = "ms-2", "carol@example.org"
	c2 := newClient(t, a.routes(), "10.0.0.8")
	rr := ms.signIn(c2, "/account")
	if rr.Code != 303 || c2.cookies[cookieMember] != "" || !strings.Contains(rr.Header().Get("Location"), "sent+an+email") {
		t.Fatalf("work account sign-in: %d %s cookie=%q", rr.Code, rr.Header().Get("Location"), c2.cookies[cookieMember])
	}
	m := a.memberByEmail("carol@example.org")
	if m == nil || m.VerifiedAt != "" || m.Logins != "microsoft" {
		t.Fatalf("unverified work member: %+v", m)
	}
	body, _ := latestMail(t, mail, "member-verify")
	link := accountLinkRe.FindString(body)
	if link == "" {
		t.Fatalf("no verify link mailed:\n%s", body)
	}
	// signing in again before confirming is refused with a pointer to the mail
	c3 := newClient(t, a.routes(), "10.0.0.8")
	if rr := ms.signIn(c3, "/account"); c3.cookies[cookieMember] != "" || !strings.Contains(rr.Header().Get("Location"), "confirmation+email") {
		t.Fatalf("unconfirmed second sign-in: %s", rr.Header().Get("Location"))
	}
	// the mailed link confirms and signs in; from then on Microsoft works
	if rr := c3.do("GET", strings.TrimPrefix(link, a.cfg.APIURL), nil); rr.Code != 303 || c3.cookies[cookieMember] == "" {
		t.Fatalf("verify: %d", rr.Code)
	}
	c4 := newClient(t, a.routes(), "10.0.0.8")
	if rr := ms.signIn(c4, "/account"); c4.cookies[cookieMember] == "" {
		t.Fatalf("confirmed work sign-in: %s", rr.Header().Get("Location"))
	}

	// 3. work account claiming an EXISTING password member's address: refused
	register(t, a, mail, "dave@example.org", "Dave", "correct horse battery", "10.0.0.9")
	ms.idClaims = map[string]any{"sub": "ms-3", "email": "dave@example.org", "name": "Mallory", "tid": "11111111-2222-3333-4444-555555555555"}
	ms.profile["sub"], ms.profile["email"] = "ms-3", "dave@example.org"
	c5 := newClient(t, a.routes(), "10.0.0.10")
	if rr := ms.signIn(c5, "/account"); c5.cookies[cookieMember] != "" || !strings.Contains(rr.Header().Get("Location"), "already+exists") {
		t.Fatalf("nOAuth attempt: %s cookie=%q", rr.Header().Get("Location"), c5.cookies[cookieMember])
	}
	if m := a.memberByEmail("dave@example.org"); m.Logins != "" {
		t.Fatalf("work account attached to Dave: %+v", m)
	}

	// 4. the same work account with xms_edov=true counts as verified and links
	ms.idClaims["xms_edov"] = "true"
	c6 := newClient(t, a.routes(), "10.0.0.11")
	if rr := ms.signIn(c6, "/account"); c6.cookies[cookieMember] == "" || !strings.Contains(rr.Header().Get("Location"), "linked") {
		t.Fatalf("xms_edov sign-in: %s", rr.Header().Get("Location"))
	}
	if m := a.memberByEmail("dave@example.org"); m.Logins != "microsoft" {
		t.Fatalf("xms_edov did not link: %+v", m)
	}
	if n := a.count(`SELECT COUNT(*) FROM members`); n != 3 {
		t.Fatalf("members = %d, want 3", n)
	}
}

func TestOAuthRefusals(t *testing.T) {
	a, _, idps := oauthApp(t, "google")
	g := idps["google"]
	expectFail := func(name string, rr *httptest.ResponseRecorder, c *client, want string) {
		t.Helper()
		loc := rr.Header().Get("Location")
		if rr.Code != 303 || !strings.Contains(loc, "err=1") || !strings.Contains(loc, url.QueryEscape(want)) {
			t.Fatalf("%s: %d %s", name, rr.Code, loc)
		}
		if c.cookies[cookieMember] != "" {
			t.Fatalf("%s: signed in", name)
		}
	}
	c := newClient(t, a.routes(), "10.0.0.5")
	g.start(c, "/account", false)
	expectFail("state", g.callback(c, "not-the-state", "the-code"), c, "did not match")
	if g.tokenHits != 0 {
		t.Fatal("token endpoint was called with a bad state")
	}
	c = newClient(t, a.routes(), "10.0.0.5")
	expectFail("nocookie", g.callback(c, "x", "the-code"), c, "expired")

	c = newClient(t, a.routes(), "10.0.0.5")
	state := g.start(c, "/account", false)
	expectFail("cancelled", c.do("GET", "/account/google/callback?state="+state+"&error=access_denied", nil), c, "cancelled")

	c = newClient(t, a.routes(), "10.0.0.5")
	state = g.start(c, "/account", false)
	expectFail("badcode", g.callback(c, state, "wrong-code"), c, "could not confirm")

	// a callback for a provider whose cookie was issued by another provider
	a.cfg.OAuth["yahoo"] = oauthCreds{ID: "id.yahoo.test", Secret: "shh-yahoo"}
	c = newClient(t, a.routes(), "10.0.0.5")
	state = g.start(c, "/account", false)
	expectFail("crossprovider", c.do("GET", "/account/yahoo/callback?state="+state+"&code=the-code", nil), c, "expired")
	delete(a.cfg.OAuth, "yahoo")

	// unverified Google address: a new member still has to confirm by mail
	g.profile["email_verified"] = false
	c = newClient(t, a.routes(), "10.0.0.5")
	if rr := g.signIn(c, "/account"); c.cookies[cookieMember] != "" || !strings.Contains(rr.Header().Get("Location"), "sent+an+email") {
		t.Fatalf("unverified: %s", rr.Header().Get("Location"))
	}
	if m := a.memberByEmail("ann@example.org"); m == nil || m.VerifiedAt != "" {
		t.Fatalf("unverified member: %+v", m)
	}
	_, _ = a.db.Exec(`DELETE FROM members`)
	g.profile["email_verified"] = true

	// no address at all
	delete(g.profile, "email")
	c = newClient(t, a.routes(), "10.0.0.5")
	expectFail("noemail", g.signIn(c, "/account"), c, "did not share an email")
	g.profile["email"] = "Ann@Example.org"
	if n := a.count(`SELECT COUNT(*) FROM members`); n != 0 {
		t.Fatalf("members created by refused sign-ins: %d", n)
	}

	// registrations paused: no new account, but an existing one still signs in
	_ = a.metaSet("set:registrations_on", "0")
	c = newClient(t, a.routes(), "10.0.0.5")
	expectFail("regoff", g.signIn(c, "/account"), c, "paused")
	_ = a.metaSet("set:registrations_on", "1")
	c = newClient(t, a.routes(), "10.0.0.5")
	if rr := g.signIn(c, "/account"); rr.Code != 303 || c.cookies[cookieMember] == "" {
		t.Fatalf("register: %d", rr.Code)
	}
	_ = a.metaSet("set:registrations_on", "0")
	c = newClient(t, a.routes(), "10.0.0.5")
	if rr := g.signIn(c, "/account"); rr.Code != 303 || c.cookies[cookieMember] == "" {
		t.Fatalf("existing member with registrations off: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	_ = a.metaSet("set:registrations_on", "1")

	// disabled account
	m := a.memberByEmail("ann@example.org")
	_, _ = a.db.Exec(`UPDATE members SET status = 'disabled' WHERE id = ?`, m.ID)
	c = newClient(t, a.routes(), "10.0.0.5")
	expectFail("disabled", g.signIn(c, "/account"), c, "disabled")
	_, _ = a.db.Exec(`UPDATE members SET status = 'active' WHERE id = ?`, m.ID)

	// blocked address cannot register through a provider either
	_, _ = a.db.Exec(`DELETE FROM members`)
	_ = a.addBlock("email", emailHash("ann@example.org"), "test")
	c = newClient(t, a.routes(), "10.0.0.5")
	expectFail("blocked", g.signIn(c, "/account"), c, "Could not sign you in")
}

func TestOAuthOnlyMemberSettings(t *testing.T) {
	a, _, idps := oauthApp(t, "google")
	g := idps["google"]
	c := newClient(t, a.routes(), "10.0.0.6")
	g.signIn(c, "/account")
	csrf := accountCSRF(t, c)

	body := c.do("GET", "/account/settings", nil).Body.String()
	if !strings.Contains(body, "Set password") || strings.Contains(body, `id="cur"`) || strings.Contains(body, `id="dpw"`) || !strings.Contains(body, "Set a password before unlinking") {
		t.Fatal("settings for a provider-only member should offer Set password, no current-password fields, and no unlink")
	}
	if rr := c.do("POST", "/account/settings", url.Values{"csrf": {csrf}, "action": {"unlink"}, "provider": {"google"}}); !strings.Contains(rr.Header().Get("Location"), "Set+a+password+first") {
		t.Fatalf("unlink without password: %s", rr.Header().Get("Location"))
	}
	// the password login path says nothing special (no account enumeration)
	c2 := newClient(t, a.routes(), "10.0.0.7")
	if rr := c2.do("POST", "/account/login", url.Values{"email": {"ann@example.org"}, "password": {"whatever whatever"}}); !strings.Contains(rr.Header().Get("Location"), "Wrong+email+address+or+password") {
		t.Fatalf("password login on provider-only account: %s", rr.Header().Get("Location"))
	}
	// setting a first password needs no current one
	if rr := c.do("POST", "/account/settings", url.Values{"csrf": {csrf}, "action": {"password"}, "password": {"correct horse battery"}, "password2": {"correct horse battery"}}); !strings.Contains(rr.Header().Get("Location"), "Password+set") {
		t.Fatalf("set password: %s", rr.Header().Get("Location"))
	}
	if !a.memberByEmail("ann@example.org").HasPassword() {
		t.Fatal("password not stored")
	}
	if rr := c2.do("POST", "/account/login", url.Values{"email": {"ann@example.org"}, "password": {"correct horse battery"}}); rr.Code != 303 || c2.cookies[cookieMember] == "" {
		t.Fatalf("password login after setting one: %d", rr.Code)
	}

	// a fresh provider-only member can delete the account with the tick alone
	g.profile["sub"], g.profile["email"] = "g-999", "bob@example.org"
	c3 := newClient(t, a.routes(), "10.0.0.8")
	g.signIn(c3, "/account")
	csrf3 := accountCSRF(t, c3)
	if rr := c3.do("POST", "/account/settings", url.Values{"csrf": {csrf3}, "action": {"delete"}, "confirm": {"yes"}}); !strings.Contains(rr.Header().Get("Location"), "account+is+deleted") {
		t.Fatalf("delete provider-only account: %s", rr.Header().Get("Location"))
	}
	if a.memberByEmail("bob@example.org") != nil {
		t.Fatal("member not deleted")
	}
	if n := a.count(`SELECT COUNT(*) FROM member_identities WHERE sub = 'g-999'`); n != 0 {
		t.Fatal("identity outlived the member")
	}
}

func TestOAuthOffHidesEverything(t *testing.T) {
	a, _ := testApp(t)
	c := newClient(t, a.routes(), "10.0.0.6")
	if rr := c.do("GET", "/account/login", nil); strings.Contains(rr.Body.String(), "/start") {
		t.Fatal("provider button shown while off")
	}
	for _, k := range []string{"google", "microsoft", "yahoo"} {
		if rr := c.do("GET", "/account/"+k+"/start", nil); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "not+available") {
			t.Fatalf("%s start while off: %d %s", k, rr.Code, rr.Header().Get("Location"))
		}
		if rr := c.do("GET", "/account/"+k+"/callback?state=x&code=y", nil); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "err=1") {
			t.Fatalf("%s callback while off: %d", k, rr.Code)
		}
	}
	if rr := c.do("GET", "/account/facebook/start", nil); rr.Code != 404 {
		t.Fatalf("unknown provider: %d", rr.Code)
	}
	if rr := get(t, a.routes(), "/api/health"); !strings.Contains(rr.Body.String(), `"logins":[]`) {
		t.Fatalf("health: %s", rr.Body)
	}
}

func TestOAuthConfigValidation(t *testing.T) {
	t.Setenv("HS_SECRET", "0123456789abcdef0123456789abcdef-test")
	t.Setenv("HS_ADMIN_EMAIL", "admin@example.org")
	t.Setenv("HS_DEV_MAIL_DIR", t.TempDir())
	t.Setenv("HS_GOOGLE_CLIENT_ID", "id.apps.googleusercontent.com")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "together") {
		t.Fatalf("id without secret accepted: %v", err)
	}
	t.Setenv("HS_GOOGLE_CLIENT_SECRET", "shh")
	t.Setenv("HS_GOOGLE_CLIENT_ID", "not-a-google-id")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "googleusercontent") {
		t.Fatalf("odd client id accepted: %v", err)
	}
	t.Setenv("HS_GOOGLE_CLIENT_ID", "id.apps.googleusercontent.com")
	t.Setenv("HS_MICROSOFT_CLIENT_SECRET", "only-secret")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "HS_MICROSOFT_CLIENT_ID") {
		t.Fatalf("microsoft secret without id accepted: %v", err)
	}
	t.Setenv("HS_MICROSOFT_CLIENT_ID", "1234-guid")
	t.Setenv("HS_YAHOO_CLIENT_ID", "dj0yJmk9")
	t.Setenv("HS_YAHOO_CLIENT_SECRET", "y-secret")
	c, err := loadConfig()
	if err != nil || len(c.OAuth) != 3 || c.OAuth["yahoo"].Secret != "y-secret" {
		t.Fatalf("valid trio rejected: %v %+v", err, c)
	}
}

// A v8 database has members.google_sub; v9 moves it into member_identities
// and drops the column. A v7 database (no column) must still come up.
func TestMigrateIdentitiesV8ToV9(t *testing.T) {
	for _, from := range []int{7, 8} {
		dir := t.TempDir()
		db, err := sql.Open("sqlite", filepath.Join(dir, "helderberg.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		stmts := []string{
			`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
			`INSERT INTO meta VALUES('schema_version','` + string(rune('0'+from)) + `')`,
			`CREATE TABLE members (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE, name TEXT NOT NULL, pw_hash TEXT NOT NULL, created_at TEXT NOT NULL, verified_at TEXT, last_login_at TEXT, status TEXT NOT NULL DEFAULT 'active', ip_hash TEXT NOT NULL DEFAULT ''` + map[int]string{7: "", 8: ", google_sub TEXT"}[from] + `)`,
			`INSERT INTO members(id, email, name, pw_hash, created_at, verified_at) VALUES(3,'old@example.org','Old','$argon2id$x','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
		}
		if from == 8 {
			stmts = append(stmts, `CREATE UNIQUE INDEX members_google ON members(google_sub)`,
				`INSERT INTO members(id, email, name, pw_hash, created_at, verified_at, google_sub) VALUES(4,'g@example.org','G','','2026-02-01T00:00:00Z','2026-02-01T00:00:00Z','g-sub-4')`)
		}
		for _, q := range stmts {
			if _, err := db.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
		db.Close()
		db, err = openDB(dir)
		if err != nil {
			t.Fatalf("openDB on a v%d database: %v", from, err)
		}
		if hasColumn(db, "members", "google_sub") {
			t.Fatalf("v%d: google_sub column still there", from)
		}
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM member_identities`).Scan(&n)
		if want := map[int]int{7: 0, 8: 1}[from]; n != want {
			t.Fatalf("v%d: identities = %d, want %d", from, n, want)
		}
		if from == 8 {
			var mid int64
			_ = db.QueryRow(`SELECT member_id FROM member_identities WHERE provider = 'google' AND sub = 'g-sub-4'`).Scan(&mid)
			if mid != 4 {
				t.Fatalf("google identity not carried over: %d", mid)
			}
		}
		if err := migrate(db); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}
