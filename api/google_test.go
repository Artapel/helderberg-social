package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeGoogle stands in for accounts.google.com: it hands out a code, checks
// the PKCE verifier against the challenge it was shown, and serves a profile.
type fakeGoogle struct {
	srv       *httptest.Server
	challenge string
	profile   map[string]any
	tokenHits int
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	f := &fakeGoogle{profile: map[string]any{"sub": "g-123", "email": "Ann@Example.org", "email_verified": true, "name": "Ann Example"}}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.tokenHits++
		if r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("code") != "the-code" ||
			r.PostForm.Get("client_id") != "id.apps.googleusercontent.com" || r.PostForm.Get("client_secret") != "shh" ||
			r.PostForm.Get("redirect_uri") != "https://api.helderbergsocial.co.za/account/google/callback" ||
			pkceChallenge(r.PostForm.Get("code_verifier")) != f.challenge {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"at-1","token_type":"Bearer","id_token":"ignored"}`))
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at-1" {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(f.profile)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	old := googleEndpoints
	googleEndpoints.Auth, googleEndpoints.Token, googleEndpoints.Userinfo = f.srv.URL+"/auth", f.srv.URL+"/token", f.srv.URL+"/userinfo"
	t.Cleanup(func() { googleEndpoints = old })
	return f
}

func googleApp(t *testing.T) (*App, string, *fakeGoogle) {
	t.Helper()
	a, mail := testApp(t)
	a.cfg.GoogleClientID, a.cfg.GoogleClientSecret = "id.apps.googleusercontent.com", "shh"
	return a, mail, newFakeGoogle(t)
}

// start walks /account/google/start and returns the state Google would echo.
func (f *fakeGoogle) start(t *testing.T, c *client, next string) string {
	t.Helper()
	rr := c.do("GET", "/account/google/start?next="+url.QueryEscape(next), nil)
	if rr.Code != 303 {
		t.Fatalf("start: %d", rr.Code)
	}
	u, err := url.Parse(rr.Header().Get("Location"))
	if err != nil || !strings.HasPrefix(u.String(), f.srv.URL+"/auth?") {
		t.Fatalf("start redirect: %s", rr.Header().Get("Location"))
	}
	q := u.Query()
	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("scope") != "openid email profile" || q.Get("client_id") != "id.apps.googleusercontent.com" {
		t.Fatalf("auth params: %v", q)
	}
	if c.cookies[cookieOAuth] == "" {
		t.Fatal("no oauth cookie")
	}
	f.challenge = q.Get("code_challenge")
	return q.Get("state")
}

func (f *fakeGoogle) callback(c *client, state, code string) *httptest.ResponseRecorder {
	return c.do("GET", "/account/google/callback?state="+url.QueryEscape(state)+"&code="+url.QueryEscape(code), nil)
}

func TestGoogleSignInCreatesAndReusesMember(t *testing.T) {
	a, mail, g := googleApp(t)
	c := newClient(t, a.routes(), "10.0.0.9")

	// the buttons show while Google is configured
	if rr := c.do("GET", "/account/login", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "Sign in with Google") || !strings.Contains(rr.Body.String(), "/account/google/start") {
		t.Fatalf("login page lacks the Google button: %d", rr.Code)
	}
	if rr := c.do("GET", "/account/register", nil); !strings.Contains(rr.Body.String(), "Continue with Google") {
		t.Fatal("register page lacks the Google button")
	}

	state := g.start(t, c, "/account/events/new")
	rr := g.callback(c, state, "the-code")
	if rr.Code != 303 || !strings.HasPrefix(rr.Header().Get("Location"), "/account/events/new") || strings.Contains(rr.Header().Get("Location"), "err=1") {
		t.Fatalf("callback: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if c.cookies[cookieMember] == "" || c.cookies[cookieOAuth] != "" {
		t.Fatalf("cookies after sign-in: member=%q oauth=%q", c.cookies[cookieMember], c.cookies[cookieOAuth])
	}
	m := a.memberByEmail("ann@example.org")
	if m == nil || m.GoogleSub != "g-123" || m.VerifiedAt == "" || m.HasPassword() || m.Name != "Ann Example" {
		t.Fatalf("member after google register: %+v", m)
	}
	if rr := c.do("GET", "/account", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "My events") {
		t.Fatalf("signed-in page: %d", rr.Code)
	}

	// second sign-in from a fresh browser: same member, no duplicate
	c2 := newClient(t, a.routes(), "10.0.0.10")
	state = g.start(t, c2, "/account")
	if rr := g.callback(c2, state, "the-code"); rr.Code != 303 || rr.Header().Get("Location") != "/account" {
		t.Fatalf("second callback: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if n := a.count(`SELECT COUNT(*) FROM members`); n != 1 {
		t.Fatalf("members = %d, want 1", n)
	}
	if g.tokenHits != 2 {
		t.Fatalf("token endpoint hit %d times", g.tokenHits)
	}

	// a signed-in member hitting start is just sent on
	if rr := c2.do("GET", "/account/google/start", nil); rr.Code != 303 || rr.Header().Get("Location") != "/account" {
		t.Fatalf("start while signed in: %d %s", rr.Code, rr.Header().Get("Location"))
	}

	// console shows the Google pill
	ac, _ := login(t, a, mail, "10.0.0.1")
	if rr := ac.do("GET", "/admin/members", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), `<span class="pill">Google</span>`) {
		t.Fatalf("members page lacks Google pill: %d", rr.Code)
	}
	if rr := ac.do("GET", "/admin/system", nil); !strings.Contains(rr.Body.String(), "Sign in with Google <span class=\"pill ok\">on</span>") || !strings.Contains(rr.Body.String(), "/account/google/callback") {
		t.Fatal("system page lacks the Google panel")
	}
	if rr := get(t, a.routes(), "/api/health"); !strings.Contains(rr.Body.String(), `"google":true`) {
		t.Fatalf("health: %s", rr.Body)
	}
}

func TestGoogleLinksExistingPasswordAccount(t *testing.T) {
	a, mail, g := googleApp(t)
	// an ordinary member, registered by email
	pc, _ := register(t, a, mail, "ann@example.org", "Ann", "correct horse battery", "10.0.0.2")
	pc.do("POST", "/account/logout", url.Values{"csrf": {accountCSRF(t, pc)}})

	c := newClient(t, a.routes(), "10.0.0.3")
	state := g.start(t, c, "/account")
	rr := g.callback(c, state, "the-code")
	if rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "linked") {
		t.Fatalf("link callback: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if n := a.count(`SELECT COUNT(*) FROM members`); n != 1 {
		t.Fatalf("members = %d, want 1 (linked, not duplicated)", n)
	}
	m := a.memberByEmail("ann@example.org")
	if m.GoogleSub != "g-123" || !m.HasPassword() || m.Name != "Ann" {
		t.Fatalf("linked member: %+v", m)
	}
	// the password still works too
	c3 := newClient(t, a.routes(), "10.0.0.4")
	if rr := c3.do("POST", "/account/login", url.Values{"email": {"ann@example.org"}, "password": {"correct horse battery"}}); rr.Code != 303 || c3.cookies[cookieMember] == "" {
		t.Fatalf("password login after link: %d", rr.Code)
	}
	// settings show the link and allow unlinking (a password exists)
	csrf := accountCSRF(t, c3)
	if rr := c3.do("GET", "/account/settings", nil); !strings.Contains(rr.Body.String(), "A Google account is linked") || !strings.Contains(rr.Body.String(), "Unlink Google") {
		t.Fatal("settings do not show the Google link")
	}
	if rr := c3.do("POST", "/account/settings", url.Values{"csrf": {csrf}, "action": {"google-unlink"}}); rr.Code != 303 || strings.Contains(rr.Header().Get("Location"), "err=1") {
		t.Fatalf("unlink: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if m := a.memberByEmail("ann@example.org"); m.GoogleSub != "" {
		t.Fatal("still linked after unlink")
	}
}

func TestGoogleLinkConfirmsUnverifiedAccount(t *testing.T) {
	a, _, g := googleApp(t)
	c0 := newClient(t, a.routes(), "10.0.0.2")
	c0.do("POST", "/account/register", url.Values{"name": {"Ann"}, "email": {"ann@example.org"}, "password": {"correct horse battery"}, "password2": {"correct horse battery"}})
	if m := a.memberByEmail("ann@example.org"); m == nil || m.VerifiedAt != "" {
		t.Fatal("precondition: unverified member")
	}
	c := newClient(t, a.routes(), "10.0.0.3")
	state := g.start(t, c, "/account")
	if rr := g.callback(c, state, "the-code"); rr.Code != 303 || c.cookies[cookieMember] == "" {
		t.Fatalf("callback: %d", rr.Code)
	}
	if m := a.memberByEmail("ann@example.org"); m.VerifiedAt == "" || m.GoogleSub != "g-123" {
		t.Fatalf("google did not confirm the address: %+v", m)
	}
}

func TestGoogleRefusals(t *testing.T) {
	a, _, g := googleApp(t)
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

	// wrong state
	c := newClient(t, a.routes(), "10.0.0.5")
	g.start(t, c, "/account")
	expectFail("state", g.callback(c, "not-the-state", "the-code"), c, "did not match")
	if g.tokenHits != 0 {
		t.Fatal("token endpoint was called with a bad state")
	}

	// no cookie at all (a replayed or stale link)
	c = newClient(t, a.routes(), "10.0.0.5")
	expectFail("nocookie", g.callback(c, "x", "the-code"), c, "expired")

	// the person cancelled at Google
	c = newClient(t, a.routes(), "10.0.0.5")
	state := g.start(t, c, "/account")
	expectFail("cancelled", c.do("GET", "/account/google/callback?state="+state+"&error=access_denied", nil), c, "cancelled")

	// Google rejects the code (or the verifier)
	c = newClient(t, a.routes(), "10.0.0.5")
	state = g.start(t, c, "/account")
	expectFail("badcode", g.callback(c, state, "wrong-code"), c, "could not confirm")

	// unverified address at Google
	g.profile["email_verified"] = false
	c = newClient(t, a.routes(), "10.0.0.5")
	state = g.start(t, c, "/account")
	expectFail("unverified", g.callback(c, state, "the-code"), c, "not verified")
	g.profile["email_verified"] = true
	if n := a.count(`SELECT COUNT(*) FROM members`); n != 0 {
		t.Fatalf("members created by refused sign-ins: %d", n)
	}

	// registrations paused: no new account, but an existing one still signs in
	_ = a.metaSet("set:registrations_on", "0")
	c = newClient(t, a.routes(), "10.0.0.5")
	state = g.start(t, c, "/account")
	expectFail("regoff", g.callback(c, state, "the-code"), c, "paused")
	_ = a.metaSet("set:registrations_on", "1")
	c = newClient(t, a.routes(), "10.0.0.5")
	state = g.start(t, c, "/account")
	if rr := g.callback(c, state, "the-code"); rr.Code != 303 || c.cookies[cookieMember] == "" {
		t.Fatalf("register: %d", rr.Code)
	}
	_ = a.metaSet("set:registrations_on", "0")
	c = newClient(t, a.routes(), "10.0.0.5")
	state = g.start(t, c, "/account")
	if rr := g.callback(c, state, "the-code"); rr.Code != 303 || c.cookies[cookieMember] == "" {
		t.Fatalf("existing member with registrations off: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	_ = a.metaSet("set:registrations_on", "1")

	// disabled account
	m := a.memberByEmail("ann@example.org")
	_, _ = a.db.Exec(`UPDATE members SET status = 'disabled' WHERE id = ?`, m.ID)
	c = newClient(t, a.routes(), "10.0.0.5")
	state = g.start(t, c, "/account")
	expectFail("disabled", g.callback(c, state, "the-code"), c, "disabled")
	_, _ = a.db.Exec(`UPDATE members SET status = 'active' WHERE id = ?`, m.ID)

	// blocked address cannot register through Google either
	_, _ = a.db.Exec(`DELETE FROM members`)
	_ = a.addBlock("email", emailHash("ann@example.org"), "test")
	c = newClient(t, a.routes(), "10.0.0.5")
	state = g.start(t, c, "/account")
	expectFail("blocked", g.callback(c, state, "the-code"), c, "Could not sign you in")
}

func TestGoogleOnlyMemberSettings(t *testing.T) {
	a, _, g := googleApp(t)
	c := newClient(t, a.routes(), "10.0.0.6")
	state := g.start(t, c, "/account")
	g.callback(c, state, "the-code")
	csrf := accountCSRF(t, c)

	rr := c.do("GET", "/account/settings", nil)
	body := rr.Body.String()
	if !strings.Contains(body, "Set password") || strings.Contains(body, `id="cur"`) || strings.Contains(body, `id="dpw"`) || !strings.Contains(body, "To unlink it, set a password first") {
		t.Fatal("settings for a Google-only member should offer Set password, no current-password fields, and no unlink")
	}
	// unlink is refused without a password
	if rr := c.do("POST", "/account/settings", url.Values{"csrf": {csrf}, "action": {"google-unlink"}}); !strings.Contains(rr.Header().Get("Location"), "Set+a+password+first") {
		t.Fatalf("unlink without password: %s", rr.Header().Get("Location"))
	}
	// the password login path says nothing special (no account enumeration)
	c2 := newClient(t, a.routes(), "10.0.0.7")
	if rr := c2.do("POST", "/account/login", url.Values{"email": {"ann@example.org"}, "password": {"whatever whatever"}}); !strings.Contains(rr.Header().Get("Location"), "Wrong+email+address+or+password") {
		t.Fatalf("password login on google-only account: %s", rr.Header().Get("Location"))
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

	// a fresh Google-only member can delete the account with the tick alone
	g.profile["sub"], g.profile["email"] = "g-999", "bob@example.org"
	c3 := newClient(t, a.routes(), "10.0.0.8")
	state = g.start(t, c3, "/account")
	g.callback(c3, state, "the-code")
	csrf3 := accountCSRF(t, c3)
	if rr := c3.do("POST", "/account/settings", url.Values{"csrf": {csrf3}, "action": {"delete"}, "confirm": {"yes"}}); !strings.Contains(rr.Header().Get("Location"), "account+is+deleted") {
		t.Fatalf("delete google-only account: %s", rr.Header().Get("Location"))
	}
	if a.memberByEmail("bob@example.org") != nil {
		t.Fatal("member not deleted")
	}
}

func TestGoogleOffHidesEverything(t *testing.T) {
	a, _ := testApp(t)
	c := newClient(t, a.routes(), "10.0.0.6")
	if rr := c.do("GET", "/account/login", nil); strings.Contains(rr.Body.String(), "google/start") {
		t.Fatal("Google button shown while off")
	}
	if rr := c.do("GET", "/account/google/start", nil); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "not+available") {
		t.Fatalf("start while off: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if rr := c.do("GET", "/account/google/callback?state=x&code=y", nil); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "err=1") {
		t.Fatalf("callback while off: %d", rr.Code)
	}
	if rr := get(t, a.routes(), "/api/health"); !strings.Contains(rr.Body.String(), `"google":false`) {
		t.Fatalf("health: %s", rr.Body)
	}
}

func TestGoogleConfigValidation(t *testing.T) {
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
	if c, err := loadConfig(); err != nil || c.GoogleClientID == "" {
		t.Fatalf("valid pair rejected: %v", err)
	}
}
