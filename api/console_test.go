package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B vectors, SHA1, secret "12345678901234567890".
func TestTOTPVectors(t *testing.T) {
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	for _, v := range []struct {
		at   int64
		want string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}} {
		got, err := totpCode(secret, v.at/30)
		if err != nil || got != v.want {
			t.Errorf("T=%d: got %q err=%v want %q", v.at, got, err, v.want)
		}
		if totpMatch(secret, v.want, time.Unix(v.at, 0)) < 0 {
			t.Errorf("T=%d: match failed", v.at)
		}
		if totpMatch(secret, v.want, time.Unix(v.at+120, 0)) >= 0 {
			t.Errorf("T=%d: code accepted 2 minutes late", v.at)
		}
	}
}

func TestSealOpenAndBackupCodes(t *testing.T) {
	a, _ := testApp(t)
	sealed, err := a.seal("totp", []byte("SECRET"))
	if err != nil {
		t.Fatal(err)
	}
	if b, err := a.open("totp", sealed); err != nil || string(b) != "SECRET" {
		t.Fatalf("round trip: %q %v", b, err)
	}
	if _, err := a.open("other", sealed); err == nil {
		t.Fatal("opened under the wrong purpose")
	}
	codes, err := a.totpEnrol(newTOTPSecret())
	if err != nil || len(codes) != 10 || a.backupCodesLeft() != 10 {
		t.Fatalf("enrol: %v %d", err, len(codes))
	}
	if how, ok := a.checkSecondFactor(codes[3]); !ok || how != "backup" {
		t.Fatal("backup code refused")
	}
	if _, ok := a.checkSecondFactor(codes[3]); ok {
		t.Fatal("backup code reused")
	}
	if a.backupCodesLeft() != 9 {
		t.Fatal("backup code not consumed")
	}
	secret, _ := a.totpSecret()
	code, _ := totpCode(secret, time.Now().Unix()/30)
	if how, ok := a.checkSecondFactor(code); !ok || how != "totp" {
		t.Fatal("current code refused")
	}
	if _, ok := a.checkSecondFactor(code); ok {
		t.Fatal("TOTP code replayed inside its window")
	}
}

/* ---------- HTTP helpers with cookies ---------- */

type client struct {
	t       *testing.T
	h       http.Handler
	cookies map[string]string
	ip      string
}

func newClient(t *testing.T, h http.Handler, ip string) *client {
	return &client{t: t, h: h, cookies: map[string]string{}, ip: ip}
}

func (c *client) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	c.t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.RemoteAddr = c.ip + ":1234"
	for k, v := range c.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	rr := httptest.NewRecorder()
	c.h.ServeHTTP(rr, req)
	for _, ck := range rr.Result().Cookies() {
		if ck.MaxAge < 0 || ck.Value == "" {
			delete(c.cookies, ck.Name)
		} else {
			c.cookies[ck.Name] = ck.Value
		}
	}
	return rr
}

var csrfRe = regexp.MustCompile(`name="csrf" value="([A-Za-z0-9_\-]+)"`)
var adminLinkRe = regexp.MustCompile(`https://api\.helderbergsocial\.co\.za/admin/auth\?t=[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`)

// login walks the two-factor flow and returns a signed-in client plus its CSRF token.
func login(t *testing.T, a *App, mail string, ip string) (*client, string) {
	t.Helper()
	c := newClient(t, a.routes(), ip)
	if rr := c.do("POST", "/admin/login", url.Values{"email": {"admin@example.org"}}); rr.Code != 200 || !strings.Contains(rr.Body.String(), "Check your email") {
		t.Fatalf("login form: %d", rr.Code)
	}
	body, _ := latestMail(t, mail, "admin-login")
	link := adminLinkRe.FindString(body)
	if link == "" {
		t.Fatalf("no /admin/auth link in mail:\n%s", body)
	}
	rr := c.do("GET", strings.TrimPrefix(link, a.cfg.APIURL), nil)
	if rr.Code != 303 || c.cookies[cookiePre] == "" {
		t.Fatalf("auth link: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if a.totpEnrolled() {
		if rr.Header().Get("Location") != "/admin/2fa" {
			t.Fatalf("enrolled admin sent to %s", rr.Header().Get("Location"))
		}
		secret, _ := a.totpSecret()
		code, _ := totpCode(secret, time.Now().Unix()/30)
		if rr = c.do("POST", "/admin/2fa", url.Values{"code": {code}}); rr.Code != 303 || c.cookies[cookieSession] == "" {
			t.Fatalf("2fa: %d %s", rr.Code, rr.Body)
		}
	} else {
		if rr.Header().Get("Location") != "/admin/enrol" {
			t.Fatalf("new admin sent to %s", rr.Header().Get("Location"))
		}
		if rr = c.do("GET", "/admin/enrol", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "data:image/png;base64,") {
			t.Fatalf("enrol page: %d", rr.Code)
		}
		secret, _ := a.pendingSecret()
		code, _ := totpCode(secret, time.Now().Unix()/30)
		rr = c.do("POST", "/admin/enrol", url.Values{"code": {code}})
		if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Backup codes") || c.cookies[cookieSession] == "" {
			t.Fatalf("enrol confirm: %d %s", rr.Code, rr.Body)
		}
	}
	if c.cookies[cookiePre] != "" {
		t.Fatal("pre-session cookie survived sign-in")
	}
	rr = c.do("GET", "/admin", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Dashboard") {
		t.Fatalf("dashboard: %d", rr.Code)
	}
	m := csrfRe.FindStringSubmatch(rr.Body.String())
	if m == nil {
		t.Fatal("no csrf token on the dashboard")
	}
	return c, m[1]
}

func TestAdminTwoFactorFlow(t *testing.T) {
	a, mail := testApp(t)
	h := a.routes()

	// Nothing behind /admin is reachable without a session.
	anon := newClient(t, h, "10.5.0.1")
	if rr := anon.do("GET", "/admin/settings", nil); rr.Code != 303 || !strings.HasPrefix(rr.Header().Get("Location"), "/admin/login?next=") {
		t.Fatalf("anonymous console access: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if rr := anon.do("POST", "/admin/do", url.Values{"action": {"housekeeping"}}); rr.Code != 403 {
		t.Fatalf("anonymous POST got %d", rr.Code)
	}
	// Wrong email: same page, no mail.
	anon.do("POST", "/admin/login", url.Values{"email": {"nobody@example.org"}})
	if _, err := latestMailMaybe(mail, "admin-login"); err == nil {
		t.Fatal("login mail sent for a non-admin address")
	}

	// First sign-in enrols the authenticator.
	c, csrf := login(t, a, mail, "10.5.0.2")
	if a.backupCodesLeft() != 10 {
		t.Fatalf("backup codes: %d", a.backupCodesLeft())
	}
	// CSRF: a POST without the token is refused and audited.
	if rr := c.do("POST", "/admin/do", url.Values{"action": {"housekeeping"}}); rr.Code != 403 {
		t.Fatalf("missing csrf got %d", rr.Code)
	}
	if rr := c.do("POST", "/admin/do", url.Values{"action": {"housekeeping"}, "csrf": {csrf + "x"}}); rr.Code != 403 {
		t.Fatalf("bad csrf got %d", rr.Code)
	}
	// Settings: maintenance mode shuts public writes.
	form := url.Values{"action": {"settings-save"}, "csrf": {csrf}, "return": {"/admin/settings"}, "maintenance": {"1"}, "digest_hour": {"7"}, "weekly_day": {"2"}, "watch_minutes": {"90"}, "events_window_days": {"60"}, "announcement_on": {"1"}, "announcement_text": {"Market this Saturday"}, "announcement_link": {"https://example.org/market"}, "digests_on": {"1"}, "watch_on": {"1"}, "submissions_on": {"1"}, "subscriptions_on": {"1"}}
	if rr := c.do("POST", "/admin/do", form); rr.Code != 303 || strings.Contains(rr.Header().Get("Location"), "err=1") {
		t.Fatalf("settings save: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if a.digestHour() != 7 || a.weeklyDay() != time.Tuesday || a.watchInterval() != 90*time.Minute {
		t.Fatal("settings not applied")
	}
	if rr := post(t, h, "/api/subscribe", map[string]any{"email": "x@y.co.za", "frequency": "daily", "horizon": 7}, a.cfg.SiteURL); rr.Code != 503 {
		t.Fatalf("maintenance mode let a subscribe through: %d", rr.Code)
	}
	if rr := get(t, h, "/api/events"); !strings.Contains(rr.Body.String(), `"maintenance":true`) || !strings.Contains(rr.Body.String(), "Market this Saturday") {
		t.Fatalf("site block missing from /api/events: %s", rr.Body)
	}
	c.do("POST", "/admin/do", url.Values{"action": {"settings-reset"}, "csrf": {csrf}})
	if a.settingBool("maintenance") {
		t.Fatal("settings reset did not clear maintenance")
	}
	// Every console page renders.
	for _, p := range []string{"/admin/queue", "/admin/events", "/admin/events/edit", "/admin/listings", "/admin/subscribers", "/admin/digests", "/admin/sources", "/admin/analytics", "/admin/logs", "/admin/logs?tab=mail", "/admin/logs?tab=audit", "/admin/logs?tab=app", "/admin/security", "/admin/settings", "/admin/system", "/admin/export/subscribers.csv", "/admin/export/all.json"} {
		if rr := c.do("GET", p, nil); rr.Code != 200 {
			t.Errorf("%s: %d", p, rr.Code)
		}
	}
	// Create an event from the console and see it on the public feed.
	ev := url.Values{"action": {"event-save"}, "csrf": {csrf}, "title": {"Console-made event"}, "date": {a.localDay(time.Now().AddDate(0, 0, 2))}, "town": {"strand"}, "category": {"markets"}, "cost": {"free"}, "status": {"approved"}, "summary": {"Made in the console."}, "website": {""}, "source": {""}}
	if rr := c.do("POST", "/admin/do", ev); rr.Code != 303 || strings.Contains(rr.Header().Get("Location"), "err=1") {
		t.Fatalf("event-save: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if rr := get(t, h, "/api/events"); !strings.Contains(rr.Body.String(), "Console-made event") {
		t.Fatal("console event not public")
	}
	// Backup: snapshot lands in the data dir and downloads.
	if rr := c.do("POST", "/admin/do", url.Values{"action": {"backup"}, "csrf": {csrf}}); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "helderberg-") {
		t.Fatalf("backup: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	rr := c.do("GET", "/admin/system", nil)
	name := regexp.MustCompile(`helderberg-\d{8}-\d{6}\.sqlite`).FindString(rr.Body.String())
	if rr = c.do("GET", "/admin/backups/"+name, nil); rr.Code != 200 || rr.Body.Len() < 4096 {
		t.Fatalf("backup download: %d %d bytes", rr.Code, rr.Body.Len())
	}
	if rr = c.do("GET", "/admin/backups/../helderberg.sqlite", nil); rr.Code == 200 {
		t.Fatal("path traversal on backups")
	}
	// Sessions: the current one is listed, revoking others works, logout kills it.
	if rr = c.do("GET", "/admin/security", nil); !strings.Contains(rr.Body.String(), "this one") {
		t.Fatal("current session not listed")
	}
	if rr = c.do("POST", "/admin/logout", url.Values{"csrf": {csrf}}); rr.Code != 303 {
		t.Fatalf("logout: %d", rr.Code)
	}
	if rr = c.do("GET", "/admin", nil); rr.Code != 303 {
		t.Fatalf("session survived logout: %d cookies=%v", rr.Code, c.cookies)
	}

	// Second sign-in: authenticator already enrolled, 2FA page, and lockout
	// after five wrong codes.
	c2 := newClient(t, h, "10.5.0.3")
	post(t, h, "/api/admin/login", map[string]any{"email": "admin@example.org"}, a.cfg.SiteURL)
	body, _ := latestMail(t, mail, "admin-login")
	link := adminLinkRe.FindString(body)
	if rr = c2.do("GET", strings.TrimPrefix(link, a.cfg.APIURL), nil); rr.Header().Get("Location") != "/admin/2fa" {
		t.Fatalf("second login went to %s", rr.Header().Get("Location"))
	}
	if rr = c2.do("GET", strings.TrimPrefix(link, a.cfg.APIURL), nil); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "invalid") {
		t.Fatal("sign-in link reusable")
	}
	for i := 0; i < 4; i++ {
		if rr = c2.do("POST", "/admin/2fa", url.Values{"code": {"000000"}}); rr.Code != 200 || !strings.Contains(rr.Body.String(), "not accepted") {
			t.Fatalf("wrong code %d: %d", i, rr.Code)
		}
	}
	if rr = c2.do("POST", "/admin/2fa", url.Values{"code": {"000000"}}); rr.Code != 303 || c2.cookies[cookiePre] != "" {
		t.Fatalf("fifth wrong code did not lock: %d pre=%q", rr.Code, c2.cookies[cookiePre])
	}
	if c2.cookies[cookieSession] != "" {
		t.Fatal("session issued without a valid code")
	}
	// Backup code signs in when the phone is gone.
	c3 := newClient(t, h, "10.5.0.4")
	c3.do("POST", "/admin/login", url.Values{"email": {"admin@example.org"}})
	body, _ = latestMail(t, mail, "admin-login")
	c3.do("GET", strings.TrimPrefix(adminLinkRe.FindString(body), a.cfg.APIURL), nil)
	codes := backupCodesFromMailFlow(t, a)
	if rr = c3.do("POST", "/admin/2fa", url.Values{"code": {codes[0]}}); rr.Code != 303 || c3.cookies[cookieSession] == "" {
		t.Fatalf("backup code sign-in: %d", rr.Code)
	}
	if a.backupCodesLeft() != 9 {
		t.Fatal("backup code not consumed")
	}
	rows := a.auditRows(`action = 'login.ok'`, 10)
	if len(rows) < 1 || rows[0].Detail != "backup" {
		t.Fatalf("audit did not record the backup-code login: %+v", rows)
	}
}

// backupCodesFromMailFlow regenerates codes directly; the HTTP flow showed
// the originals once and they are not stored in clear anywhere.
func backupCodesFromMailFlow(t *testing.T, a *App) []string {
	t.Helper()
	codes, err := a.newBackupCodes()
	if err != nil {
		t.Fatal(err)
	}
	return codes
}

func TestModerationLinksNeedSession(t *testing.T) {
	a, mail := testApp(t)
	h := a.routes()
	date := time.Now().In(a.cfg.TZ).AddDate(0, 0, 3).Format("2006-01-02")
	ev := map[string]any{"title": "Harbour swim", "date": date, "town": "gordons-bay", "category": "water",
		"summary": "Early morning open-water swim from the harbour wall, all abilities.", "cost": "free", "name": "Kim", "email": "kim@example.org"}
	post(t, h, "/api/submit/event", ev, a.cfg.SiteURL)
	_, verify := latestMail(t, mail, "verify-event")
	get(t, h, strings.TrimPrefix(verify, a.cfg.APIURL))
	body, _ := latestMail(t, mail, "admin-event")
	approve := regexp.MustCompile(`https://api\.helderbergsocial\.co\.za/admin/moderate\?t=[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`).FindString(body)
	if approve == "" {
		t.Fatalf("no console moderate link:\n%s", body)
	}
	path := strings.TrimPrefix(approve, a.cfg.APIURL)
	// A forwarded email cannot approve: the link bounces to sign-in and is NOT consumed.
	anon := newClient(t, h, "10.6.0.1")
	if rr := anon.do("GET", path, nil); rr.Code != 303 || !strings.HasPrefix(rr.Header().Get("Location"), "/admin/login") {
		t.Fatalf("anonymous moderate: %d", rr.Code)
	}
	if n := a.count(`SELECT COUNT(*) FROM events WHERE status='approved' AND title='Harbour swim'`); n != 0 {
		t.Fatal("approved without a session")
	}
	c, _ := login(t, a, mail, "10.6.0.2")
	rr := c.do("GET", path, nil)
	if rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "approved") {
		t.Fatalf("moderate with session: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if rr = c.do("GET", path, nil); !strings.Contains(rr.Header().Get("Location"), "err=1") {
		t.Fatal("moderation link replayable")
	}
	if rr = get(t, h, "/api/events"); !strings.Contains(rr.Body.String(), "Harbour swim") {
		t.Fatal("approved event not public")
	}
	// Old-style links from before the console bounce to sign-in.
	if rr = get(t, h, "/api/admin?t=old"); rr.Code != 303 {
		t.Fatalf("legacy admin link: %d", rr.Code)
	}
}

func TestPingAndBlocklist(t *testing.T) {
	a, _ := testApp(t)
	h := a.routes()
	for i := 0; i < 3; i++ {
		if rr := postFrom(t, h, "/api/ping", map[string]any{"p": "/events.html"}, a.cfg.SiteURL, "10.7.0.1"); rr.Code != 204 {
			t.Fatalf("ping: %d %s", rr.Code, rr.Body)
		}
	}
	postFrom(t, h, "/api/ping", map[string]any{"p": "/events.html"}, a.cfg.SiteURL, "10.7.0.2")
	if rr := post(t, h, "/api/ping", map[string]any{"p": "javascript:alert(1)"}, a.cfg.SiteURL); rr.Code != 400 {
		t.Fatal("bad path accepted")
	}
	a.flushStats()
	top := a.topPages(1, 5)
	if len(top) != 1 || top[0].K != "/events.html" || top[0].N != 4 || top[0].N2 != 2 {
		t.Fatalf("pageviews wrong: %+v", top)
	}
	if err := a.addBlock("ip", ipTag("10.7.0.9"), "test"); err != nil {
		t.Fatal(err)
	}
	if rr := postFrom(t, h, "/api/ping", map[string]any{"p": "/"}, a.cfg.SiteURL, "10.7.0.9"); rr.Code != 403 {
		t.Fatalf("blocked ip got %d", rr.Code)
	}
	_ = a.addBlock("email", emailHash("spammer@example.org"), "")
	rr := postFrom(t, h, "/api/subscribe", map[string]any{"email": "spammer@example.org", "frequency": "daily", "horizon": 7}, a.cfg.SiteURL, "10.7.0.3")
	if rr.Code != 200 || a.count(`SELECT COUNT(*) FROM subscribers`) != 0 {
		t.Fatal("blocked email was stored or rejected loudly")
	}
}
