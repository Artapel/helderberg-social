package main

import (
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var accountLinkRe = regexp.MustCompile(`https://api\.helderbergsocial\.co\.za/account/(verify|reset)\?t=[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`)

func futureDate(days int) string { return time.Now().AddDate(0, 0, days).Format("2006-01-02") }

// register creates and confirms an account and returns a signed-in client
// plus its CSRF token.
func register(t *testing.T, a *App, mail, email, name, pw, ip string) (*client, string) {
	t.Helper()
	c := newClient(t, a.routes(), ip)
	rr := c.do("POST", "/account/register", url.Values{"name": {name}, "email": {email}, "password": {pw}, "password2": {pw}})
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "sent you an email") {
		t.Fatalf("register: %d %s", rr.Code, rr.Body)
	}
	if c.cookies[cookieMember] != "" {
		t.Fatal("signed in before the address was confirmed")
	}
	body, _ := latestMail(t, mail, "member-verify")
	link := accountLinkRe.FindString(body)
	if link == "" {
		t.Fatalf("no verify link in mail:\n%s", body)
	}
	rr = c.do("GET", strings.TrimPrefix(link, a.cfg.APIURL), nil)
	if rr.Code != 303 || c.cookies[cookieMember] == "" {
		t.Fatalf("verify: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if rr = c.do("GET", link[len(a.cfg.APIURL):], nil); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "err=1") {
		t.Fatalf("verify link replayed: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	return c, accountCSRF(t, c)
}

func accountCSRF(t *testing.T, c *client) string {
	t.Helper()
	rr := c.do("GET", "/account", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "My events") {
		t.Fatalf("my events: %d", rr.Code)
	}
	m := csrfRe.FindStringSubmatch(rr.Body.String())
	if m == nil {
		t.Fatal("no csrf token in the account area")
	}
	return m[1]
}

func TestMemberPostsEventAdminApproves(t *testing.T) {
	a, mail := testApp(t)
	c, csrf := register(t, a, mail, "Jane@Example.org", "Jane", "correct horse battery", "10.1.1.1")

	m := a.memberByEmail("jane@example.org")
	if m == nil || m.VerifiedAt == "" || m.Name != "Jane" {
		t.Fatalf("member not stored/verified: %+v", m)
	}
	if strings.Contains(m.PwHash, "correct horse") || !strings.HasPrefix(m.PwHash, "$argon2id$") {
		t.Fatalf("password not hashed with argon2id: %s", m.PwHash)
	}

	// A forged form without the CSRF token is refused.
	form := url.Values{"title": {"Beach clean-up"}, "date": {futureDate(10)}, "time": {"08:00"}, "town": {"strand"}, "category": {"community"}, "cost": {"free"}, "summary": {"Meet at the pier with gloves; bags provided by the municipality."}}
	if rr := c.do("POST", "/account/events/save", form); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "expired") {
		t.Fatalf("csrf-less save accepted: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	form.Set("csrf", csrf)
	rr := c.do("POST", "/account/events/save", form)
	if rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "in+the+queue") {
		t.Fatalf("save: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	evs, _ := a.queryEvents(`member_id = ?`, m.ID)
	if len(evs) != 1 || evs[0].Status != "pending_review" || evs[0].Origin != "user" || evs[0].MemberID != m.ID || evs[0].SubmitterName != "Jane" {
		t.Fatalf("event not queued for review: %+v", evs)
	}
	id := evs[0].ID
	if body, err := latestMailMaybe(mail, "admin-event"); err != nil || !strings.Contains(body, "Beach clean-up") {
		t.Fatalf("admin not notified: %v", err)
	}
	// The public feed does not show it yet, the member's page does.
	pub := newClient(t, a.routes(), "10.9.9.9")
	if rr := pub.do("GET", "/api/events", nil); strings.Contains(rr.Body.String(), "Beach clean-up") {
		t.Fatal("pending member event leaked to the public feed")
	}
	if rr := c.do("GET", "/account", nil); !strings.Contains(rr.Body.String(), "Waiting for a check") {
		t.Fatal("member page does not show the pending event")
	}

	// The admin approves it from the console; the member is told.
	adm, acsrf := login(t, a, mail, "10.0.0.2")
	if rr := adm.do("GET", "/admin/members", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "jane@example.org") {
		t.Fatalf("members page: %d", rr.Code)
	}
	if rr := adm.do("GET", "/admin/queue", nil); !strings.Contains(rr.Body.String(), "member #") {
		t.Fatal("queue card does not show the member badge")
	}
	if rr := adm.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"approve"}, "id": {id}, "return": {"/admin/queue"}}); rr.Code != 303 {
		t.Fatalf("approve: %d", rr.Code)
	}
	body, err := latestMailMaybe(mail, "member-approved")
	if err != nil || !strings.Contains(body, "events.html?ev="+id) {
		t.Fatalf("member not told about approval: %v", err)
	}
	if rr := pub.do("GET", "/api/events", nil); !strings.Contains(rr.Body.String(), "Beach clean-up") {
		t.Fatal("approved member event missing from the public feed")
	}
	if rr := c.do("GET", "/account", nil); !strings.Contains(rr.Body.String(), "Published") || !strings.Contains(rr.Body.String(), "events.html?ev="+id) {
		t.Fatal("member page does not show the published state")
	}

	// Editing a published event pulls it back for review, and someone else
	// cannot touch it at all.
	form.Set("id", id)
	form.Set("title", "Beach clean-up (moved)")
	if rr := c.do("POST", "/account/events/save", form); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "back+in+the+queue") {
		t.Fatalf("edit: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	evs, _ = a.queryEvents(`id = ?`, id)
	if evs[0].Status != "pending_review" || evs[0].Title != "Beach clean-up (moved)" {
		t.Fatalf("edit did not return the event to review: %+v", evs[0])
	}
	other, ocsrf := register(t, a, mail, "bob@example.org", "Bob", "another fine passphrase", "10.1.1.2")
	form.Set("csrf", ocsrf)
	form.Set("title", "Hijacked")
	if rr := other.do("POST", "/account/events/save", form); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "not+one+of+yours") {
		t.Fatalf("cross-member edit: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if rr := other.do("GET", "/account/events/edit?id="+id, nil); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "not+one+of+yours") {
		t.Fatalf("cross-member edit form: %d", rr.Code)
	}
	if rr := other.do("POST", "/account/events/withdraw", url.Values{"csrf": {ocsrf}, "id": {id}, "confirm": {"yes"}}); !strings.Contains(rr.Header().Get("Location"), "not+one+of+yours") {
		t.Fatal("cross-member withdraw succeeded")
	}
	evs, _ = a.queryEvents(`id = ?`, id)
	if len(evs) != 1 || evs[0].Title == "Hijacked" {
		t.Fatal("another member changed the event")
	}

	// Rejecting tells the member too, then the member removes it.
	if rr := adm.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"reject"}, "id": {id}, "return": {"/admin/queue"}}); rr.Code != 303 {
		t.Fatalf("reject: %d", rr.Code)
	}
	if body, err := latestMailMaybe(mail, "member-rejected"); err != nil || !strings.Contains(body, "did not publish") {
		t.Fatalf("member not told about rejection: %v", err)
	}
	if rr := c.do("POST", "/account/events/withdraw", url.Values{"csrf": {csrf}, "id": {id}}); !strings.Contains(rr.Header().Get("Location"), "Tick+the+box") {
		t.Fatal("withdraw without confirmation went through")
	}
	if rr := c.do("POST", "/account/events/withdraw", url.Values{"csrf": {csrf}, "id": {id}, "confirm": {"yes"}}); !strings.Contains(rr.Header().Get("Location"), "removed") {
		t.Fatalf("withdraw: %s", rr.Header().Get("Location"))
	}
	if n := a.count(`SELECT COUNT(*) FROM events WHERE id = ?`, id); n != 0 {
		t.Fatal("withdrawn event still there")
	}

	// Sign out kills the session server-side.
	if rr := c.do("POST", "/account/logout", url.Values{"csrf": {csrf}}); rr.Code != 303 || c.cookies[cookieMember] != "" {
		t.Fatalf("logout: %d", rr.Code)
	}
	if rr := c.do("GET", "/account", nil); rr.Code != 303 || !strings.HasPrefix(rr.Header().Get("Location"), "/account/login") {
		t.Fatalf("signed-out client still in: %d", rr.Code)
	}
}

func TestMemberLoginLockoutAndReset(t *testing.T) {
	a, mail := testApp(t)
	register(t, a, mail, "kim@example.org", "Kim", "the first password here", "10.2.2.2")

	c := newClient(t, a.routes(), "10.2.2.3")
	if rr := c.do("POST", "/account/login", url.Values{"email": {"kim@example.org"}, "password": {"the first password here"}}); rr.Code != 303 || rr.Header().Get("Location") != "/account" || c.cookies[cookieMember] == "" {
		t.Fatalf("login: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	// Unknown address and wrong password answer identically.
	bad := newClient(t, a.routes(), "10.2.2.4")
	r1 := bad.do("POST", "/account/login", url.Values{"email": {"nobody@example.org"}, "password": {"x"}})
	r2 := bad.do("POST", "/account/login", url.Values{"email": {"kim@example.org"}, "password": {"x"}})
	loc1, _ := url.Parse(r1.Header().Get("Location"))
	loc2, _ := url.Parse(r2.Header().Get("Location"))
	if loc1.Query().Get("msg") != loc2.Query().Get("msg") {
		t.Fatalf("login leaks whether an address exists: %q vs %q", loc1.Query().Get("msg"), loc2.Query().Get("msg"))
	}
	// Lockout after repeated failures, even with the right password.
	for i := 0; i < memberMaxFails; i++ {
		bad = newClient(t, a.routes(), "10.2.2.5") // rate limiter is per IP; the lock is per address
		bad.ip = "10.2.3." + string(rune('1'+i))
		bad.do("POST", "/account/login", url.Values{"email": {"kim@example.org"}, "password": {"wrong"}})
	}
	rr := newClient(t, a.routes(), "10.2.4.1").do("POST", "/account/login", url.Values{"email": {"kim@example.org"}, "password": {"the first password here"}})
	if !strings.Contains(rr.Header().Get("Location"), "Too+many") {
		t.Fatalf("no lockout after %d failures: %s", memberMaxFails, rr.Header().Get("Location"))
	}

	// Reset by email: the link works once, clears the lock, signs the user
	// in and signs the old session out.
	if rr := newClient(t, a.routes(), "10.2.4.2").do("POST", "/account/forgot", url.Values{"email": {"kim@example.org"}}); rr.Code != 200 {
		t.Fatalf("forgot: %d", rr.Code)
	}
	body, _ := latestMail(t, mail, "member-reset")
	link := accountLinkRe.FindString(body)
	if link == "" {
		t.Fatalf("no reset link:\n%s", body)
	}
	tok := link[strings.Index(link, "t=")+2:]
	rc := newClient(t, a.routes(), "10.2.4.3")
	if rr := rc.do("GET", strings.TrimPrefix(link, a.cfg.APIURL), nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "New password") {
		t.Fatalf("reset page: %d", rr.Code)
	}
	if rr := rc.do("POST", "/account/reset", url.Values{"t": {tok}, "password": {"short"}, "password2": {"short"}}); !strings.Contains(rr.Header().Get("Location"), "at+least") {
		t.Fatalf("weak password accepted on reset: %s", rr.Header().Get("Location"))
	}
	if rr := rc.do("POST", "/account/reset", url.Values{"t": {tok}, "password": {"a brand new passphrase"}, "password2": {"a brand new passphrase"}}); rr.Code != 303 || rc.cookies[cookieMember] == "" {
		t.Fatalf("reset: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if rr := rc.do("POST", "/account/reset", url.Values{"t": {tok}, "password": {"yet another passphrase"}, "password2": {"yet another passphrase"}}); !strings.Contains(rr.Header().Get("Location"), "already+used") && !strings.Contains(rr.Header().Get("Location"), "invalid") {
		t.Fatalf("reset token replayed: %s", rr.Header().Get("Location"))
	}
	if rr := c.do("GET", "/account", nil); rr.Code != 303 {
		t.Fatal("old session survived a password reset")
	}
	if rr := newClient(t, a.routes(), "10.2.4.4").do("POST", "/account/login", url.Values{"email": {"kim@example.org"}, "password": {"a brand new passphrase"}}); rr.Code != 303 || rr.Header().Get("Location") != "/account" {
		t.Fatalf("login with the new password: %s", rr.Header().Get("Location"))
	}
	if rr := newClient(t, a.routes(), "10.2.4.5").do("POST", "/account/login", url.Values{"email": {"kim@example.org"}, "password": {"the first password here"}}); rr.Header().Get("Location") == "/account" {
		t.Fatal("old password still works after reset")
	}
}

func TestMemberRegistrationRules(t *testing.T) {
	a, mail := testApp(t)
	h := a.routes()
	try := func(ip string, f url.Values) string {
		rr := newClient(t, h, ip).do("POST", "/account/register", f)
		if rr.Code == 200 {
			return "sent"
		}
		loc, _ := url.Parse(rr.Header().Get("Location"))
		return loc.Query().Get("msg")
	}
	base := func() url.Values {
		return url.Values{"name": {"Ann"}, "email": {"ann@example.org"}, "password": {"a perfectly fine one"}, "password2": {"a perfectly fine one"}}
	}
	f := base()
	f.Set("password", "short")
	f.Set("password2", "short")
	if msg := try("10.3.0.1", f); !strings.Contains(msg, "at least") {
		t.Fatalf("short password: %q", msg)
	}
	f = base()
	f.Set("password", "password123")
	f.Set("password2", "password123")
	if msg := try("10.3.0.2", f); !strings.Contains(msg, "common") {
		t.Fatalf("weak password: %q", msg)
	}
	f = base()
	f.Set("password2", "different")
	if msg := try("10.3.0.3", f); !strings.Contains(msg, "do not match") {
		t.Fatalf("mismatch: %q", msg)
	}
	f = base()
	f.Set("website_url", "http://spam")
	if msg := try("10.3.0.4", f); msg != "sent" || a.memberByEmail("ann@example.org") != nil {
		t.Fatal("honeypot did not swallow the bot")
	}
	if msg := try("10.3.0.5", base()); msg != "sent" || a.memberByEmail("ann@example.org") == nil {
		t.Fatalf("good registration: %q", msg)
	}
	// A second registration with the same address does not say so on screen
	// but mails the owner.
	if msg := try("10.3.0.6", base()); msg != "sent" {
		t.Fatalf("duplicate leaked: %q", msg)
	}
	if n := a.count(`SELECT COUNT(*) FROM members`); n != 1 {
		t.Fatalf("duplicate created a second account: %d", n)
	}
	// Unverified accounts cannot sign in but can ask for the mail again.
	c := newClient(t, h, "10.3.0.7")
	if rr := c.do("POST", "/account/login", url.Values{"email": {"ann@example.org"}, "password": {"a perfectly fine one"}}); rr.Code != 200 || !strings.Contains(rr.Body.String(), "not been confirmed") || c.cookies[cookieMember] != "" {
		t.Fatalf("unverified login: %d", rr.Code)
	}
	// Registrations can be switched off.
	_, _ = a.db.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES('set:registrations_on', '0')`)
	f = base()
	f.Set("email", "second@example.org")
	if msg := try("10.3.0.8", f); !strings.Contains(msg, "paused") {
		t.Fatalf("registrations_on=0 ignored: %q", msg)
	}
	_, _ = a.db.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES('set:registrations_on', '1')`)
	// A blocked address is silently dropped.
	_ = a.addBlock("email", emailHash("blocked@example.org"), "test")
	f.Set("email", "blocked@example.org")
	if msg := try("10.3.0.9", f); msg != "sent" || a.memberByEmail("blocked@example.org") != nil {
		t.Fatal("blocked address registered")
	}
	_ = mail
}

func TestMemberAdminActionsAndDeletion(t *testing.T) {
	a, mail := testApp(t)
	c, csrf := register(t, a, mail, "sam@example.org", "Sam", "sam has a passphrase", "10.4.0.1")
	form := url.Values{"csrf": {csrf}, "title": {"Quiz night"}, "date": {futureDate(5)}, "town": {"somerset-west"}, "category": {"community"}, "cost": {"paid"}, "summary": {"Pub quiz at the club house, tables of four, R20 a head for the hall fund."}}
	if rr := c.do("POST", "/account/events/save", form); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "queue") {
		t.Fatalf("save: %s", rr.Header().Get("Location"))
	}
	form.Set("title", "Second quiz night")
	form.Set("date", futureDate(12))
	c.do("POST", "/account/events/save", form)
	m := a.memberByEmail("sam@example.org")
	evs, _ := a.queryEvents(`member_id = ?`, m.ID)
	if len(evs) != 2 {
		t.Fatalf("expected 2 events, got %d", len(evs))
	}
	adm, acsrf := login(t, a, mail, "10.0.0.3")
	a.decide("event", evs[0].ID, "approve")

	// Disable: signed out at once, cannot sign in, events untouched.
	if rr := adm.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"member-disable"}, "id": {itoa(m.ID)}, "return": {"/admin/members"}}); rr.Code != 303 {
		t.Fatalf("disable: %d", rr.Code)
	}
	if rr := c.do("GET", "/account", nil); rr.Code != 303 {
		t.Fatal("disabled member still signed in")
	}
	if rr := newClient(t, a.routes(), "10.4.0.2").do("POST", "/account/login", url.Values{"email": {"sam@example.org"}, "password": {"sam has a passphrase"}}); !strings.Contains(rr.Header().Get("Location"), "disabled") {
		t.Fatalf("disabled member can sign in: %s", rr.Header().Get("Location"))
	}
	if rr := adm.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"member-enable"}, "id": {itoa(m.ID)}, "return": {"/admin/members"}}); rr.Code != 303 {
		t.Fatalf("enable: %d", rr.Code)
	}
	c2 := newClient(t, a.routes(), "10.4.0.3")
	if rr := c2.do("POST", "/account/login", url.Values{"email": {"sam@example.org"}, "password": {"sam has a passphrase"}}); rr.Header().Get("Location") != "/account" {
		t.Fatalf("re-enabled member cannot sign in: %s", rr.Header().Get("Location"))
	}
	if rr := adm.do("GET", "/admin/members/view?id="+itoa(m.ID), nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "Quiz night") {
		t.Fatalf("member view: %d", rr.Code)
	}

	// Self-deletion needs the password; published events stay anonymised,
	// the pending one goes.
	csrf2 := accountCSRF(t, c2)
	if rr := c2.do("POST", "/account/settings", url.Values{"csrf": {csrf2}, "action": {"delete"}, "confirm": {"yes"}, "current": {"wrong"}}); !strings.Contains(rr.Header().Get("Location"), "wrong") {
		t.Fatal("deleted without the password")
	}
	if rr := c2.do("POST", "/account/settings", url.Values{"csrf": {csrf2}, "action": {"delete"}, "confirm": {"yes"}, "current": {"sam has a passphrase"}}); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "deleted") {
		t.Fatalf("self delete: %s", rr.Header().Get("Location"))
	}
	if a.memberByEmail("sam@example.org") != nil {
		t.Fatal("member row survived deletion")
	}
	var status, email, name string
	var mid *int64
	if err := a.db.QueryRow(`SELECT status, submitter_email, submitter_name, member_id FROM events WHERE id = ?`, evs[0].ID).Scan(&status, &email, &name, &mid); err != nil {
		t.Fatalf("published event gone after account deletion: %v", err)
	}
	if status != "approved" || email != "" || name != "" || mid != nil {
		t.Fatalf("published event not anonymised: %s %q %q %v", status, email, name, mid)
	}
	if n := a.count(`SELECT COUNT(*) FROM events WHERE id = ?`, evs[1].ID); n != 0 {
		t.Fatal("pending event survived account deletion")
	}
}

func TestPasswordHashing(t *testing.T) {
	h := hashPassword("hello there general")
	if ok, stale := checkPassword(h, "hello there general"); !ok || stale {
		t.Fatalf("round trip: ok=%v stale=%v", ok, stale)
	}
	if ok, _ := checkPassword(h, "hello there generaL"); ok {
		t.Fatal("wrong password accepted")
	}
	if ok, _ := checkPassword("garbage", "x"); ok {
		t.Fatal("garbage hash accepted")
	}
	old := "$argon2id$v=19$m=1024,t=1,p=1$" + strings.SplitN(h, "$", 6)[4] + "$" + strings.SplitN(h, "$", 6)[5]
	if _, stale := checkPassword(old, "whatever"); stale {
		t.Fatal("stale reported for a non-matching password")
	}
	if h2 := hashPassword("hello there general"); h2 == h {
		t.Fatal("no salt")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// Browsers put an Origin header on same-origin form posts. The console and
// the account pages live on the API host, so its own origin must pass; the
// site's origin gets CORS headers; anything else is refused.
func TestSameOriginFormPostsAllowed(t *testing.T) {
	a, _ := testApp(t)
	h := a.routes()
	try := func(origin, path string) int {
		req := httptest.NewRequest("POST", path, strings.NewReader("email=x@example.org"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
		req.RemoteAddr = "10.5.0.1:1"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}
	if c := try("https://api.helderbergsocial.co.za", "/admin/login"); c != 200 {
		t.Fatalf("console form post from its own origin: %d", c)
	}
	if c := try("https://api.helderbergsocial.co.za", "/account/login"); c != 303 {
		t.Fatalf("account form post from its own origin: %d", c)
	}
	if c := try("https://evil.example", "/account/login"); c != 403 {
		t.Fatalf("foreign origin accepted: %d", c)
	}
	if c := try("https://evil.example", "/admin/login"); c != 403 {
		t.Fatalf("foreign origin accepted on the console: %d", c)
	}
}
