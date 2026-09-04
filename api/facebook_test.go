package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePage stands in for graph.facebook.com's page endpoints: it records
// every /feed post and answers like Meta does, including its error shape.
type fakePage struct {
	mu    sync.Mutex
	posts []url.Values
	fail  string // when set, /feed answers with this Meta error JSON
	srv   *httptest.Server
}

func newFakePage(t *testing.T) *fakePage {
	f := &fakePage{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer page-token" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid OAuth access token.","code":190}}`))
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/v22.0/1352989477889743":
			_, _ = w.Write([]byte(`{"name":"Helderberg Social","link":"https://www.facebook.com/helderbergsocial","id":"1352989477889743"}`))
		case r.Method == "POST" && r.URL.Path == "/v22.0/1352989477889743/feed":
			_ = r.ParseForm()
			f.mu.Lock()
			f.posts = append(f.posts, r.PostForm)
			fail := f.fail
			f.mu.Unlock()
			if fail != "" {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(fail))
				return
			}
			_, _ = w.Write([]byte(`{"id":"1352989477889743_777"}`))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":{"message":"Unsupported get request.","code":100}}`))
		}
	}))
	t.Cleanup(f.srv.Close)
	old := fbEndpoint
	fbEndpoint = f.srv.URL
	t.Cleanup(func() { fbEndpoint = old })
	return f
}

func (f *fakePage) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.posts)
}

func (f *fakePage) last() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) == 0 {
		return nil
	}
	return f.posts[len(f.posts)-1]
}

func fbApp(t *testing.T) (*App, *fakePage) {
	t.Helper()
	p := newFakePage(t)
	a, _ := testApp(t)
	a.cfg.FBPageID, a.cfg.FBToken, a.cfg.FBVersion = "1352989477889743", "page-token", "v22.0"
	a.fb = newFBClient(a.cfg)
	return a, p
}

func TestFacebookConfigNeedsBoth(t *testing.T) {
	t.Setenv("HS_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("HS_ADMIN_EMAIL", "admin@example.org")
	t.Setenv("HS_FB_PAGE_ID", "123")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "HS_FB_PAGE_TOKEN") {
		t.Fatalf("page id without token accepted: %v", err)
	}
	t.Setenv("HS_FB_PAGE_ID", "helderbergsocial")
	t.Setenv("HS_FB_PAGE_TOKEN", "x")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "numeric") {
		t.Fatalf("page name accepted as id: %v", err)
	}
	t.Setenv("HS_FB_PAGE_ID", "123")
	c, err := loadConfig()
	if err != nil || c.FBPageID != "123" || c.FBVersion != "v22.0" {
		t.Fatalf("config: %+v %v", c, err)
	}
}

func TestFacebookOffByDefault(t *testing.T) {
	a, _ := testApp(t)
	if a.fbEnabled() {
		t.Fatal("enabled without config")
	}
	if rr := get(t, a.routes(), "/api/health"); !strings.Contains(rr.Body.String(), `"facebook":false`) {
		t.Fatalf("health: %s", rr.Body.String())
	}
	// Approving with nothing configured must not touch the queue.
	must(t, a.insertEvent(Event{ID: "ev1", Title: "Parkrun", Date: "2099-01-02", Town: "strand", Category: "sport", Status: "pending_review", Origin: "user"}, "", "", nil))
	_ = a.metaSet("set:fb_events_on", "1")
	if _, err := a.decide("event", "ev1", "approve"); err != nil {
		t.Fatal(err)
	}
	if n := a.count(`SELECT COUNT(*) FROM fb_posts`); n != 0 {
		t.Fatalf("queued %d posts with Facebook off", n)
	}
}

func TestFacebookEventTextAndWeekend(t *testing.T) {
	a, _ := fbApp(t)
	e := Event{ID: "trail-run", Title: "Helderberg trail run", Date: "2026-09-12", Time: "07:00", EndTime: "10:00", Town: "somerset-west", Category: "sport", Cost: "R50",
		Summary: "A 10 km loop above the nature reserve.\nBring water.", Website: "https://example.org/run"}
	txt := a.fbEventText(e)
	for _, want := range []string{"Helderberg trail run\n", "Sat 12 Sep, 07:00 to 10:00 · Somerset West · R50", "Bring water.", "https://helderbergsocial.co.za/events.html"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("event text lacks %q:\n%s", want, txt)
		}
	}
	// Weekend: a Thursday looks at the coming Sat/Sun; a Sunday at the current one.
	loc := a.cfg.TZ
	thu := time.Date(2026, 9, 10, 17, 0, 0, 0, loc)
	sat, sun := weekendOf(thu)
	if sat.Format("2006-01-02") != "2026-09-12" || sun.Format("2006-01-02") != "2026-09-13" {
		t.Fatalf("weekend of Thursday: %s %s", sat, sun)
	}
	if s, _ := weekendOf(time.Date(2026, 9, 13, 9, 0, 0, 0, loc)); s.Format("2006-01-02") != "2026-09-12" {
		t.Fatalf("weekend of Sunday: %s", s)
	}
	if s, _ := weekendOf(time.Date(2026, 9, 12, 9, 0, 0, 0, loc)); s.Format("2006-01-02") != "2026-09-12" {
		t.Fatalf("weekend of Saturday: %s", s)
	}
	if text, ref := a.fbWeekendText(thu); text != "" || ref != "2026-09-12" {
		t.Fatalf("empty weekend should yield no text: %q %q", text, ref)
	}
	must(t, a.insertEvent(Event{ID: "sat", Title: "Parkrun", Date: "2026-09-12", Time: "08:00", Town: "strand", Category: "sport", Cost: "free", Status: "approved", Origin: "admin"}, "", "", nil))
	must(t, a.insertEvent(Event{ID: "sun", Title: "Market", Date: "2026-09-13", Town: "somerset-west", Category: "market", Status: "approved", Origin: "admin"}, "", "", nil))
	must(t, a.insertEvent(Event{ID: "span", Title: "Festival", Date: "2026-09-11", EndDate: "2026-09-13", Town: "gordons-bay", Category: "music", Status: "approved", Origin: "admin"}, "", "", nil))
	must(t, a.insertEvent(Event{ID: "mon", Title: "Not this weekend", Date: "2026-09-14", Town: "strand", Category: "sport", Status: "approved", Origin: "admin"}, "", "", nil))
	must(t, a.insertEvent(Event{ID: "pend", Title: "Unapproved", Date: "2026-09-12", Town: "strand", Category: "sport", Status: "pending_review", Origin: "user"}, "", "", nil))
	text, _ := a.fbWeekendText(thu)
	for _, want := range []string{"this weekend (12 Sep to 13 Sep)", "• Sat: Parkrun (Strand) 08:00 · free", "• Sun: Market (Somerset West)", "• Sat/Sun: Festival", "/submit.html?kind=event"} {
		if !strings.Contains(text, want) {
			t.Fatalf("weekend text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Not this weekend") || strings.Contains(text, "Unapproved") {
		t.Fatalf("weekend text includes the wrong events:\n%s", text)
	}
}

func TestFacebookApproveQueuesAndSchedulerPosts(t *testing.T) {
	a, p := fbApp(t)
	future := time.Now().In(a.cfg.TZ).AddDate(0, 0, 10).Format("2006-01-02")
	past := time.Now().In(a.cfg.TZ).AddDate(0, 0, -10).Format("2006-01-02")
	must(t, a.insertEvent(Event{ID: "ev1", Title: "Beach clean-up", Date: future, Town: "strand", Category: "nature", Cost: "free", Status: "pending_review", Origin: "user"}, "", "", nil))
	must(t, a.insertEvent(Event{ID: "old", Title: "Already happened", Date: past, Town: "strand", Category: "nature", Status: "pending_review", Origin: "user"}, "", "", nil))

	// Automatic posting is off by default: approving queues nothing.
	if _, err := a.decide("event", "ev1", "approve"); err != nil {
		t.Fatal(err)
	}
	if n := a.count(`SELECT COUNT(*) FROM fb_posts`); n != 0 {
		t.Fatalf("queued with fb_events_on off: %d", n)
	}
	_, _ = a.db.Exec(`UPDATE events SET status='pending_review' WHERE id='ev1'`)

	// On, with a 30-minute delay: queued but not due.
	_ = a.metaSet("set:fb_events_on", "1")
	if _, err := a.decide("event", "ev1", "approve"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.decide("event", "old", "approve"); err != nil {
		t.Fatal(err)
	}
	q := a.fbPosts(`status='queued'`, 10)
	if len(q) != 1 || q[0].Kind != "event" || q[0].Ref != "ev1" || !strings.HasPrefix(q[0].Message, "Beach clean-up\n") {
		t.Fatalf("queue: %+v", q)
	}
	if due, _ := time.Parse(time.RFC3339, q[0].DueAt); due.Before(time.Now().Add(25 * time.Minute)) {
		t.Fatalf("due too soon: %s", q[0].DueAt)
	}
	a.fbTick(time.Now().In(a.cfg.TZ))
	if p.count() != 0 {
		t.Fatal("posted before the delay ran out")
	}

	// Re-approving the same event never queues a second post.
	_, _ = a.db.Exec(`UPDATE events SET status='pending_review' WHERE id='ev1'`)
	_, _ = a.decide("event", "ev1", "approve")
	if n := a.count(`SELECT COUNT(*) FROM fb_posts WHERE ref='ev1'`); n != 1 {
		t.Fatalf("duplicate queue rows: %d", n)
	}

	// Make it due; the tick posts it and records Meta's id.
	_, _ = a.db.Exec(`UPDATE fb_posts SET due_at=? WHERE ref='ev1'`, now())
	a.fbTick(time.Now().In(a.cfg.TZ))
	if p.count() != 1 {
		t.Fatalf("posts made: %d", p.count())
	}
	if l := p.last(); !strings.HasPrefix(l.Get("message"), "Beach clean-up") || l.Get("link") != "https://helderbergsocial.co.za/events.html" {
		t.Fatalf("post form: %v", l)
	}
	done := a.fbPosts(`ref='ev1'`, 1)
	if len(done) != 1 || done[0].Status != "posted" || done[0].FBID != "1352989477889743_777" || done[0].Permalink() != "https://www.facebook.com/1352989477889743_777" {
		t.Fatalf("posted row: %+v", done)
	}

	// Taking an event back cancels a queued post but leaves a posted one.
	must(t, a.insertEvent(Event{ID: "ev2", Title: "Second event", Date: future, Town: "strand", Category: "nature", Status: "pending_review", Origin: "user"}, "", "", nil))
	_, _ = a.decide("event", "ev2", "approve")
	_, _ = a.db.Exec(`UPDATE events SET status='pending_review' WHERE id='ev2'`)
	a.fbCancelRef("event", "ev2")
	if st := a.fbPosts(`ref='ev2'`, 1); len(st) != 1 || st[0].Status != "cancelled" {
		t.Fatalf("cancel: %+v", st)
	}
	if st := a.fbPosts(`ref='ev1'`, 1); st[0].Status != "posted" {
		t.Fatalf("posted row touched: %+v", st)
	}
}

func TestFacebookRetryAndPermanentFailure(t *testing.T) {
	a, p := fbApp(t)
	if _, err := a.fbQueue("manual", "", "Hello Helderberg, this is a test post.", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	// A transient error retries later.
	p.fail = `{"error":{"message":"Service temporarily unavailable","code":2}}`
	a.fbTick(time.Now().In(a.cfg.TZ))
	q := a.fbPosts(`kind='manual'`, 1)
	if q[0].Status != "queued" || q[0].Tries != 1 || !strings.Contains(q[0].Err, "facebook 2") {
		t.Fatalf("after transient failure: %+v", q[0])
	}
	if due, _ := time.Parse(time.RFC3339, q[0].DueAt); due.Before(time.Now().Add(10 * time.Minute)) {
		t.Fatalf("retry not pushed out: %s", q[0].DueAt)
	}
	// Not due yet, so a tick does nothing.
	a.fbTick(time.Now().In(a.cfg.TZ))
	if p.count() != 1 {
		t.Fatalf("retried early: %d", p.count())
	}
	// A dead token fails for good at once.
	_, _ = a.db.Exec(`UPDATE fb_posts SET due_at=?`, now())
	p.fail = `{"error":{"message":"Error validating access token: Session has expired","code":190}}`
	a.fbTick(time.Now().In(a.cfg.TZ))
	q = a.fbPosts(`kind='manual'`, 1)
	if q[0].Status != "failed" || !strings.Contains(q[0].Err, "190") {
		t.Fatalf("after token failure: %+v", q[0])
	}
	// Three transient failures also give up.
	p.fail = `{"error":{"message":"flaky","code":2}}`
	if _, err := a.fbQueue("manual", "", "Second test post for the retry counter.", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		_, _ = a.db.Exec(`UPDATE fb_posts SET due_at=? WHERE status='queued'`, now())
		a.fbTick(time.Now().In(a.cfg.TZ))
	}
	if q = a.fbPosts(`status='failed'`, 5); len(q) != 2 {
		t.Fatalf("expected both failed: %+v", q)
	}
}

func TestFacebookWeekendScheduleIsOncePerDay(t *testing.T) {
	a, p := fbApp(t)
	_ = a.metaSet("set:fb_weekend_on", "1")
	_ = a.metaSet("set:fb_weekend_day", "4") // Thursday
	_ = a.metaSet("set:fb_weekend_hour", "17")
	thu := time.Date(2026, 9, 10, 17, 3, 0, 0, a.cfg.TZ)
	must(t, a.insertEvent(Event{ID: "sat", Title: "Parkrun", Date: "2026-09-12", Town: "strand", Category: "sport", Cost: "free", Status: "approved", Origin: "admin"}, "", "", nil))
	a.fbTick(thu)
	a.fbTick(thu.Add(time.Minute))
	if n := a.count(`SELECT COUNT(*) FROM fb_posts WHERE kind='weekend'`); n != 1 {
		t.Fatalf("weekend posts queued: %d", n)
	}
	if p.count() != 1 || !strings.Contains(p.last().Get("message"), "Parkrun") {
		t.Fatalf("weekend post not published: %d %v", p.count(), p.last())
	}
	// Wrong hour or day: nothing.
	_, _ = a.db.Exec(`DELETE FROM fb_posts`)
	_, _ = a.db.Exec(`DELETE FROM meta WHERE key='last:fb:weekend'`)
	a.fbTick(thu.Add(time.Hour))
	a.fbTick(thu.AddDate(0, 0, 1))
	if n := a.count(`SELECT COUNT(*) FROM fb_posts WHERE kind='weekend'`); n != 0 {
		t.Fatalf("weekend post queued off-schedule: %d", n)
	}
}

func TestFacebookConsolePageAndActions(t *testing.T) {
	a, p := fbApp(t)
	c, csrf := login(t, a, testMailDir(a), "10.0.0.9")
	do := func(action string, extra url.Values) *httptest.ResponseRecorder {
		f := url.Values{"csrf": {csrf}, "action": {action}}
		for k, v := range extra {
			f[k] = v
		}
		return c.do("POST", "/admin/do", f)
	}
	future := time.Now().In(a.cfg.TZ).AddDate(0, 0, 3).Format("2006-01-02")
	must(t, a.insertEvent(Event{ID: "ev1", Title: "Beach clean-up", Date: future, Town: "strand", Category: "nature", Status: "approved", Origin: "admin"}, "", "", nil))

	rr := c.do("GET", "/admin/facebook", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "connected") || !strings.Contains(rr.Body.String(), "Beach clean-up") {
		t.Fatalf("page: %d", rr.Code)
	}
	// Connection check goes to the fake page.
	rr = do("fb-check", nil)
	rr = c.do("GET", rr.Header().Get("Location"), nil)
	if !strings.Contains(rr.Body.String(), "Helderberg Social") || !strings.Contains(rr.Body.String(), "Connected") {
		t.Fatalf("check: %s", rr.Body.String()[:200])
	}
	// Compose, scheduled for later: queued, not posted.
	later := time.Now().In(a.cfg.TZ).Add(2 * time.Hour).Format("2006-01-02T15:04")
	rr = do("fb-compose", url.Values{"message": {"Hello Helderberg. This page goes with the site."}, "link": {"https://helderbergsocial.co.za"}, "when": {later}})
	if !strings.Contains(rr.Header().Get("Location"), "/admin/facebook") {
		t.Fatalf("compose redirect: %s", rr.Header().Get("Location"))
	}
	q := a.fbPosts(`kind='manual'`, 5)
	if len(q) != 1 || q[0].Link != "https://helderbergsocial.co.za" {
		t.Fatalf("composed: %+v", q)
	}
	a.fbTick(time.Now().In(a.cfg.TZ))
	if p.count() != 0 {
		t.Fatal("scheduled post went out early")
	}
	// Post now, then the tick publishes it.
	do("fb-now", url.Values{"id": {fmtInt(q[0].ID)}})
	a.fbTick(time.Now().In(a.cfg.TZ))
	if p.count() != 1 || p.last().Get("message") != "Hello Helderberg. This page goes with the site." {
		t.Fatalf("post now: %d %v", p.count(), p.last())
	}
	// An approved event by hand, then cancel it from the queue.
	do("fb-event", url.Values{"id": {"ev1"}})
	q = a.fbPosts(`kind='event' AND status='queued'`, 5)
	if len(q) != 1 {
		t.Fatalf("event queue: %+v", q)
	}
	do("fb-cancel", url.Values{"id": {fmtInt(q[0].ID)}})
	if st := a.fbPosts(`kind='event'`, 1); st[0].Status != "cancelled" {
		t.Fatalf("cancel: %+v", st)
	}
	// The page shows the history and the permalink of the posted item.
	rr = c.do("GET", "/admin/facebook", nil)
	if !strings.Contains(rr.Body.String(), "facebook.com/1352989477889743_777") || !strings.Contains(rr.Body.String(), "cancelled") {
		t.Fatalf("history missing")
	}
	// Bad input is refused with a message, not a 500.
	rr = do("fb-compose", url.Values{"message": {"short"}})
	if !strings.Contains(rr.Header().Get("Location"), "err=1") {
		t.Fatalf("short post accepted: %s", rr.Header().Get("Location"))
	}
	// Settings validate the Facebook fields.
	form := url.Values{"csrf": {csrf}, "action": {"settings-save"}}
	for _, d := range settingDefs {
		form.Set(d.Key, a.setting(d.Key))
	}
	form.Set("fb_events_delay", "5000")
	if rr = c.do("POST", "/admin/do", form); !strings.Contains(rr.Header().Get("Location"), "err=1") {
		t.Fatal("delay 5000 accepted")
	}
	form.Set("fb_events_delay", "10")
	form.Set("fb_weekend_hour", "18")
	form.Set("fb_weekend_on", "1")
	if rr = c.do("POST", "/admin/do", form); strings.Contains(rr.Header().Get("Location"), "err=1") {
		t.Fatal("valid settings refused")
	}
	if a.settingInt("fb_events_delay") != 10 || a.settingInt("fb_weekend_hour") != 18 || !a.settingBool("fb_weekend_on") {
		t.Fatal("settings not saved")
	}
}

func fmtInt(n int64) string { return strconv.FormatInt(n, 10) }

func testMailDir(a *App) string { return a.cfg.DevMailDir }
