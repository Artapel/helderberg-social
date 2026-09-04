package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// testApp boots a full app against a temp SQLite file with mail written to
// disk, so the end-to-end flows below exercise real handlers and real SQL.
func testApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	mailDir := filepath.Join(dir, "mail")
	loc, _ := time.LoadLocation("Africa/Johannesburg")
	cfg := &Config{
		DataDir: dir, SiteURL: "https://helderbergsocial.co.za", APIURL: "https://api.helderbergsocial.co.za",
		Secret: []byte("0123456789abcdef0123456789abcdef-test"), AdminEmail: "admin@example.org",
		MailFrom: "Helderberg Social <hello@helderbergsocial.co.za>", DevMailDir: mailDir, TZ: loc,
		DigestHour: 6, WeeklyDay: time.Thursday, WatchInterval: time.Hour, TrustProxy: true,
	}
	a, err := newApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.db.Close() })
	return a, mailDir
}

func post(t *testing.T, h http.Handler, path string, body any, origin string) *httptest.ResponseRecorder {
	return postFrom(t, h, path, body, origin, "10.0.0.1")
}

func postFrom(t *testing.T, h http.Handler, path string, body any, origin, ip string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	req.RemoteAddr = ip + ":1234"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

var linkRe = regexp.MustCompile(`https://api\.helderbergsocial\.co\.za/api/[a-z]+\?t=[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`)

// latestMail returns the newest .eml of a kind and the first API link in it.
func latestMail(t *testing.T, dir, kind string) (string, string) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*-"+kind+"-*.eml"))
	if len(files) == 0 {
		t.Fatalf("no %s mail written", kind)
	}
	raw, _ := os.ReadFile(files[len(files)-1])
	body := string(raw)
	// html/template escapes = as &#43;? No: it escapes only in attribute
	// contexts; the text part is first and unescaped, so the first match wins.
	link := linkRe.FindString(body)
	return body, link
}

func latestMailMaybe(dir, kind string) (string, error) {
	files, _ := filepath.Glob(filepath.Join(dir, "*-"+kind+"-*.eml"))
	if len(files) == 0 {
		return "", fmt.Errorf("no %s mail", kind)
	}
	raw, err := os.ReadFile(files[len(files)-1])
	return string(raw), err
}

func TestTokens(t *testing.T) {
	a, _ := testApp(t)
	tok := a.sign("confirm", "42", time.Hour)
	p, err := a.verify(tok, "confirm")
	if err != nil || p.Subject != "42" {
		t.Fatalf("verify failed: %v", err)
	}
	if _, err := a.verify(tok, "unsub"); err == nil {
		t.Fatal("wrong purpose accepted")
	}
	if _, err := a.verify(tok[:len(tok)-2]+"zz", "confirm"); err == nil {
		t.Fatal("tampered signature accepted")
	}
	if _, err := a.consume(tok, "confirm"); err != nil {
		t.Fatal("first consume failed")
	}
	if _, err := a.consume(tok, "confirm"); err == nil {
		t.Fatal("token replayed")
	}
	old := a.sign("confirm", "1", -time.Minute)
	if _, err := a.verify(old, "confirm"); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestValidation(t *testing.T) {
	for _, bad := range []string{"", "a", "a@b", "a b@c.com", "<x>@c.com", "a@b.com\nBcc: x@y.z"} {
		if validEmail(bad) {
			t.Errorf("accepted bad email %q", bad)
		}
	}
	if !validEmail("someone@example.co.za") {
		t.Error("rejected good email")
	}
	for _, bad := range []string{"javascript:alert(1)", "ftp://x.y", "http://user:pw@x.y", "//x.y", "x.y"} {
		if _, ok := validURL(bad); ok {
			t.Errorf("accepted bad url %q", bad)
		}
	}
	if got := clean("  hello\x00 \n world\t!  ", 100); got != "hello world !" {
		t.Errorf("clean: %q", got)
	}
	if got := clean(strings.Repeat("é", 10), 5); got != "éé" {
		t.Errorf("clean cut a rune in half: %q", got)
	}
	if got := slugify("Lourensford Market: Opening Night! (2026)"); got != "lourensford-market-opening-night-2026" {
		t.Errorf("slugify: %q", got)
	}
}

func TestICS(t *testing.T) {
	loc, _ := time.LoadLocation("Africa/Johannesburg")
	raw := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:one@x\r\nSUMMARY:Night Market\\, opening\r\nDESCRIPTION:Line one\r\n  folded\r\nDTSTART;TZID=Africa/Johannesburg:20261004T170000\r\nDTEND;TZID=Africa/Johannesburg:20261004T220000\r\nURL:https://example.org/e\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nUID:two@x\r\nSUMMARY:All day\r\nDTSTART;VALUE=DATE:20261010\r\nDTEND;VALUE=DATE:20261012\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nUID:broken\r\nDTSTART:garbage\r\nSUMMARY:no date\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	evs := parseICS(raw, loc)
	if len(evs) != 2 {
		t.Fatalf("want 2 events, got %d", len(evs))
	}
	if evs[0].Summary != "Night Market, opening" || evs[0].Description != "Line one folded" || evs[0].Start.Format("15:04") != "17:00" || evs[0].AllDay {
		t.Errorf("event 0 parsed wrong: %+v", evs[0])
	}
	if !evs[1].AllDay || evs[1].Start.Format("2006-01-02") != "2026-10-10" {
		t.Errorf("event 1 parsed wrong: %+v", evs[1])
	}
}

func TestSubscribeConfirmDigestUnsubscribe(t *testing.T) {
	a, mail := testApp(t)
	h := a.routes()

	// CORS: a foreign origin cannot POST.
	if rr := post(t, h, "/api/subscribe", map[string]any{"email": "x@y.co.za", "frequency": "daily", "horizon": 7}, "https://evil.example"); rr.Code != 403 {
		t.Fatalf("foreign origin got %d", rr.Code)
	}
	// Honeypot filled: pretend success, store nothing.
	post(t, h, "/api/subscribe", map[string]any{"email": "bot@y.co.za", "frequency": "daily", "horizon": 7, "website": "spam"}, a.cfg.SiteURL)
	var n int
	a.db.QueryRow(`SELECT COUNT(*) FROM subscribers`).Scan(&n)
	if n != 0 {
		t.Fatal("honeypot submission stored")
	}
	// Real subscription.
	rr := post(t, h, "/api/subscribe", map[string]any{"email": "Reader@Example.co.za", "frequency": "daily", "horizon": 7, "towns": []string{"strand"}}, a.cfg.SiteURL)
	if rr.Code != 200 {
		t.Fatalf("subscribe: %d %s", rr.Code, rr.Body)
	}
	_, link := latestMail(t, mail, "confirm")
	if link == "" {
		t.Fatal("no confirm link in mail")
	}
	rr = get(t, h, strings.TrimPrefix(link, a.cfg.APIURL))
	if rr.Code != 303 || !strings.HasSuffix(rr.Header().Get("Location"), "m=subscribed") {
		t.Fatalf("confirm: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	// Replaying the same link must fail.
	if rr = get(t, h, strings.TrimPrefix(link, a.cfg.APIURL)); !strings.HasSuffix(rr.Header().Get("Location"), "m=invalid") {
		t.Fatal("confirm link replayable")
	}
	subs, _ := a.subscribers(`confirmed_at IS NOT NULL`)
	if len(subs) != 1 || subs[0].Email != "reader@example.co.za" || subs[0].Towns[0] != "strand" {
		t.Fatalf("subscriber not confirmed correctly: %+v", subs)
	}

	// Re-subscribing a confirmed address must not change its preferences.
	post(t, h, "/api/subscribe", map[string]any{"email": "reader@example.co.za", "frequency": "weekly", "horizon": 30}, a.cfg.SiteURL)
	subs, _ = a.subscribers(`email = 'reader@example.co.za'`)
	if subs[0].Frequency != "daily" {
		t.Fatal("stranger changed a confirmed subscriber's preferences")
	}

	// An approved event in Strand within 7 days -> digest goes out. One in
	// Somerset West must be filtered out by the town preference.
	today := time.Now().In(a.cfg.TZ).Format("2006-01-02")
	must(t, a.insertEvent(Event{ID: "ev-strand", Title: "Beach clean-up", Date: today, Town: "strand", Category: "nature", Cost: "free", Status: "approved", Origin: "admin"}, "", "", nil))
	must(t, a.insertEvent(Event{ID: "ev-sw", Title: "Other town", Date: today, Town: "somerset-west", Category: "nature", Status: "approved", Origin: "admin"}, "", "", nil))
	sent, err := a.runDigest("daily", false)
	if err != nil || sent != 1 {
		t.Fatalf("digest: sent=%d err=%v", sent, err)
	}
	body, unsub := latestMail(t, mail, "digest-daily")
	if !strings.Contains(body, "Beach clean-up") || strings.Contains(body, "Other town") {
		t.Fatalf("digest content wrong:\n%s", body)
	}
	if !strings.Contains(body, "List-Unsubscribe-Post: List-Unsubscribe=One-Click") {
		t.Fatal("digest lacks one-click unsubscribe header")
	}
	if !strings.Contains(unsub, "/api/unsubscribe?t=") {
		t.Fatalf("first link is not unsubscribe: %s", unsub)
	}
	rr = get(t, h, strings.TrimPrefix(unsub, a.cfg.APIURL))
	if rr.Code != 303 || !strings.HasSuffix(rr.Header().Get("Location"), "m=unsubscribed") {
		t.Fatalf("unsubscribe: %d", rr.Code)
	}
	a.db.QueryRow(`SELECT COUNT(*) FROM subscribers`).Scan(&n)
	if n != 0 {
		t.Fatal("unsubscribe did not delete the row")
	}
}

func TestEventSubmissionModeration(t *testing.T) {
	a, mail := testApp(t)
	h := a.routes()
	date := time.Now().In(a.cfg.TZ).AddDate(0, 0, 3).Format("2006-01-02")
	ev := map[string]any{"title": "Trail run <b>bold</b>", "date": date, "time": "07:00", "town": "gordons-bay", "category": "running",
		"summary": "A friendly 10 km trail run above the harbour. All paces welcome.", "cost": "free", "website": "https://example.org/run",
		"name": "Sam", "email": "sam@example.org"}
	if rr := post(t, h, "/api/submit/event", ev, a.cfg.SiteURL); rr.Code != 200 {
		t.Fatalf("submit: %d %s", rr.Code, rr.Body)
	}
	// Not public before verification + approval.
	if rr := get(t, h, "/api/events"); strings.Contains(rr.Body.String(), "Trail run") {
		t.Fatal("unverified event visible")
	}
	_, verify := latestMail(t, mail, "verify-event")
	rr := get(t, h, strings.TrimPrefix(verify, a.cfg.APIURL))
	if !strings.HasSuffix(rr.Header().Get("Location"), "m=verified") {
		t.Fatalf("verify: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if rr := get(t, h, "/api/events"); strings.Contains(rr.Body.String(), "Trail run") {
		t.Fatal("unapproved event visible")
	}
	body, _ := latestMail(t, mail, "admin-event")
	approve := regexp.MustCompile(`https://api\.helderbergsocial\.co\.za/admin/moderate\?t=[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`).FindString(body)
	if approve == "" {
		t.Fatalf("no approve link in admin mail:\n%s", body)
	}
	admin, _ := login(t, a, mail, "10.2.0.1")
	if rr := admin.do("GET", strings.TrimPrefix(approve, a.cfg.APIURL), nil); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "approved") {
		t.Fatalf("approve: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	rr = get(t, h, "/api/events")
	if !strings.Contains(rr.Body.String(), "Trail run \\u003cb\\u003ebold") && !strings.Contains(rr.Body.String(), "Trail run <b>bold") {
		t.Fatalf("approved event missing: %s", rr.Body)
	}
	var out struct {
		Events []Event `json:"events"`
	}
	json.Unmarshal(rr.Body.Bytes(), &out)
	found := false
	for _, e := range out.Events {
		if strings.HasPrefix(e.ID, "trail-run-") {
			found = true
			if e.Verified || e.Status != "" {
				t.Fatalf("public shape wrong: %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("approved event not in public list")
	}
	// Second use of the approve link must be refused.
	if rr := admin.do("GET", strings.TrimPrefix(approve, a.cfg.APIURL), nil); !strings.Contains(rr.Header().Get("Location"), "err=1") {
		t.Fatal("moderation link replayable")
	}
	// Bad payloads.
	i := 0
	for name, bad := range map[string]map[string]any{
		"past date":    {"date": "2001-01-01"},
		"bad town":     {"town": "cape-town"},
		"bad url":      {"website": "javascript:alert(1)"},
		"bad time":     {"time": "7am"},
		"short":        {"summary": "x"},
		"bad category": {"category": "sport"},
	} {
		m := map[string]any{}
		for k, v := range ev {
			m[k] = v
		}
		for k, v := range bad {
			m[k] = v
		}
		i++
		if rr := postFrom(t, h, "/api/submit/event", m, a.cfg.SiteURL, fmt.Sprintf("10.1.0.%d", i)); rr.Code != 400 {
			t.Errorf("%s accepted with %d", name, rr.Code)
		}
	}
}

func TestListingSubmissionAndAdminQueue(t *testing.T) {
	a, mail := testApp(t)
	h := a.routes()
	sub := map[string]any{"kind": "group", "name": "Strand Bridge Club", "category": "community", "town": "strand", "schedule": "Tuesdays 14:00",
		"summary": "Duplicate bridge every Tuesday afternoon at the bowling club. Beginners' table available.", "cost": "membership",
		"website": "https://example.org/bridge", "audience": []string{"seniors", "beginners"}, "yourName": "Pat", "email": "pat@example.org"}
	if rr := post(t, h, "/api/submit/listing", sub, a.cfg.SiteURL); rr.Code != 200 {
		t.Fatalf("submit listing: %d %s", rr.Code, rr.Body)
	}
	_, verify := latestMail(t, mail, "verify-listing")
	get(t, h, strings.TrimPrefix(verify, a.cfg.APIURL))
	body, _ := latestMail(t, mail, "admin-listing")
	if !strings.Contains(body, `id: "strand-bridge-club"`) || !strings.Contains(body, `audience: ["seniors","beginners"]`) {
		t.Fatalf("data.js block missing:\n%s", body)
	}
	// Admin login: wrong address gets the same answer and no mail.
	post(t, h, "/api/admin/login", map[string]any{"email": "nobody@example.org"}, a.cfg.SiteURL)
	if files, _ := filepath.Glob(filepath.Join(mail, "*-admin-login-*.eml")); len(files) != 0 {
		t.Fatal("login mail sent to non-admin")
	}
	post(t, h, "/api/admin/login", map[string]any{"email": "admin@example.org"}, a.cfg.SiteURL)
	body, _ = latestMail(t, mail, "admin-login")
	if !strings.Contains(body, "/admin/auth?t=") {
		t.Fatalf("site login mail lacks console link:\n%s", body)
	}
	admin, csrf := login(t, a, mail, "10.3.0.1")
	rr := admin.do("GET", "/admin/queue", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Strand Bridge Club") {
		t.Fatalf("queue page: %d", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("Content-Security-Policy"), "default-src 'none'") || rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing")
	}
	var id int64
	a.db.QueryRow(`SELECT id FROM listing_submissions WHERE name='Strand Bridge Club'`).Scan(&id)
	if rr = admin.do("POST", "/admin/do", url.Values{"action": {"accept"}, "csrf": {csrf}, "id": {fmt.Sprint(id)}, "return": {"/admin/queue"}}); rr.Code != 303 || strings.Contains(rr.Header().Get("Location"), "err=1") {
		t.Fatalf("accept: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if a.count(`SELECT COUNT(*) FROM listing_submissions WHERE status='accepted'`) != 1 {
		t.Fatal("listing not accepted")
	}
	if get(t, h, "/api/admin?t=garbage").Code != 303 {
		t.Fatal("legacy admin link did not redirect")
	}
}

func TestRateLimitAndBodySize(t *testing.T) {
	a, _ := testApp(t)
	h := a.routes()
	var last int
	for i := 0; i < 10; i++ {
		last = post(t, h, "/api/subscribe", map[string]any{"email": "bad"}, a.cfg.SiteURL).Code
	}
	if last != 429 {
		t.Fatalf("no rate limit after 10 POSTs (last %d)", last)
	}
	big := map[string]any{"email": "x@y.co.za", "frequency": "daily", "horizon": 7, "towns": []string{strings.Repeat("a", 64*1024)}}
	req := httptest.NewRequest("POST", "/api/subscribe", bytes.NewReader(mustJSON(big)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.9.9.9:1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Fatalf("oversized body got %d", rr.Code)
	}
}

func TestHousekeepingAndSeed(t *testing.T) {
	a, _ := testApp(t)
	var n int
	a.db.QueryRow(`SELECT COUNT(*) FROM events WHERE origin = 'seed' AND status = 'approved'`).Scan(&n)
	if n == 0 {
		t.Fatal("seed events not loaded")
	}
	a.db.QueryRow(`SELECT COUNT(*) FROM sources`).Scan(&n)
	if n == 0 {
		t.Fatal("sources not seeded")
	}
	old := time.Now().UTC().Add(-5 * 24 * time.Hour).Format(time.RFC3339)
	a.db.Exec(`INSERT INTO subscribers(email, frequency, horizon, created_at) VALUES('stale@x.co.za','daily',7,?)`, old)
	a.housekeeping()
	a.db.QueryRow(`SELECT COUNT(*) FROM subscribers WHERE email='stale@x.co.za'`).Scan(&n)
	if n != 0 {
		t.Fatal("stale unconfirmed subscriber kept")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
