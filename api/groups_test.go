package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/* ---------- recurrence ---------- */

func TestRRuleOccurrences(t *testing.T) {
	loc, _ := time.LoadLocation("Africa/Johannesburg")
	at := func(s string) time.Time {
		tm, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	from, to := at("2026-09-01 00:00"), at("2026-12-31 23:59")
	cases := []struct {
		name  string
		ev    icsEvent
		max   int
		want  []string
		total int // -1 = do not check
	}{
		{"weekly service, first only", icsEvent{Start: at("2024-01-07 09:00"), RRule: "FREQ=WEEKLY;BYDAY=SU"}, 1, []string{"2026-09-06 09:00"}, -1},
		{"weekly service, all in window", icsEvent{Start: at("2024-01-07 09:00"), RRule: "FREQ=WEEKLY;BYDAY=SU"}, 100, nil, 17},
		{"fortnightly on Tue and Thu", icsEvent{Start: at("2026-09-01 18:30"), RRule: "FREQ=WEEKLY;INTERVAL=2;BYDAY=TU,TH"}, 4, []string{"2026-09-01 18:30", "2026-09-03 18:30", "2026-09-15 18:30", "2026-09-17 18:30"}, -1},
		{"first Sunday monthly", icsEvent{Start: at("2026-01-04 09:30"), RRule: "FREQ=MONTHLY;BYDAY=1SU"}, 2, []string{"2026-09-06 09:30", "2026-10-04 09:30"}, -1},
		{"last Friday monthly", icsEvent{Start: at("2026-01-30 19:00"), RRule: "FREQ=MONTHLY;BYDAY=-1FR"}, 2, []string{"2026-09-25 19:00", "2026-10-30 19:00"}, -1},
		{"monthly on the 15th", icsEvent{Start: at("2026-03-15 10:00"), RRule: "FREQ=MONTHLY;BYMONTHDAY=15"}, 2, []string{"2026-09-15 10:00", "2026-10-15 10:00"}, -1},
		{"monthly by DTSTART day", icsEvent{Start: at("2026-01-31 10:00"), RRule: "FREQ=MONTHLY"}, 3, []string{"2026-10-31 10:00", "2026-12-31 10:00"}, -1},
		{"daily with COUNT ended long ago", icsEvent{Start: at("2025-01-01 07:00"), RRule: "FREQ=DAILY;COUNT=10"}, 5, nil, 0},
		{"weekly UNTIL before window", icsEvent{Start: at("2026-01-05 07:00"), RRule: "FREQ=WEEKLY;UNTIL=20260630"}, 5, nil, 0},
		{"weekly UNTIL inside window", icsEvent{Start: at("2026-08-31 07:00"), RRule: "FREQ=WEEKLY;UNTIL=20260914T235959Z"}, 10, []string{"2026-09-07 07:00", "2026-09-14 07:00"}, -1},
		{"exdate skips one", icsEvent{Start: at("2026-09-01 18:00"), RRule: "FREQ=WEEKLY", ExDates: []time.Time{at("2026-09-08 18:00")}}, 3, []string{"2026-09-01 18:00", "2026-09-15 18:00", "2026-09-22 18:00"}, -1},
		{"yearly", icsEvent{Start: at("2020-12-16 08:00"), RRule: "FREQ=YEARLY"}, 2, []string{"2026-12-16 08:00"}, -1},
		{"one-off in window", icsEvent{Start: at("2026-10-02 18:00")}, 1, []string{"2026-10-02 18:00"}, -1},
		{"one-off outside window", icsEvent{Start: at("2027-10-02 18:00")}, 1, nil, 0},
		{"unknown frequency yields nothing", icsEvent{Start: at("2026-09-01 18:00"), RRule: "FREQ=HOURLY"}, 5, nil, 0},
	}
	for _, c := range cases {
		got := c.ev.occurrences(from, to, c.max)
		if c.total >= 0 && len(got) != c.total {
			t.Errorf("%s: %d occurrences, want %d", c.name, len(got), c.total)
			continue
		}
		if c.want != nil {
			var gs []string
			for _, g := range got {
				gs = append(gs, g.Format("2006-01-02 15:04"))
			}
			if strings.Join(gs, ",") != strings.Join(c.want, ",") {
				t.Errorf("%s: got %v want %v", c.name, gs, c.want)
			}
		}
	}
	if s := repeatText("FREQ=WEEKLY;BYDAY=SU", loc); s != "Repeats every week on Sunday." {
		t.Errorf("repeatText weekly = %q", s)
	}
	if s := repeatText("FREQ=MONTHLY;BYDAY=1SU;UNTIL=20261231", loc); s != "Repeats every month on first Sunday until 31 Dec 2026." {
		t.Errorf("repeatText monthly = %q", s)
	}
	if s := repeatText("FREQ=WEEKLY;INTERVAL=2;COUNT=6", loc); s != "Repeats every 2 weeks (6 times)." {
		t.Errorf("repeatText interval = %q", s)
	}
	if s := repeatText("", loc); s != "" {
		t.Errorf("repeatText empty = %q", s)
	}
}

// A feed with a weekly series queues one event, at the next date, with the
// repeat rule in the summary; an override instance (RECURRENCE-ID) is not
// queued on its own.
func TestWatchQueuesSeriesOnce(t *testing.T) {
	a, _ := testApp(t)
	feed := "BEGIN:VCALENDAR\r\n" +
		"BEGIN:VEVENT\r\nUID:sunday@church\r\nSUMMARY:Sunday service\r\nDTSTART;TZID=Africa/Johannesburg:20240107T090000\r\nDTEND;TZID=Africa/Johannesburg:20240107T103000\r\nRRULE:FREQ=WEEKLY;BYDAY=SU\r\nLOCATION:Gordon's Bay\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:sunday@church\r\nRECURRENCE-ID;TZID=Africa/Johannesburg:20260913T090000\r\nSUMMARY:Sunday service (moved)\r\nDTSTART;TZID=Africa/Johannesburg:20260913T100000\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:old@church\r\nSUMMARY:Lent course\r\nDTSTART;VALUE=DATE:20250305\r\nRRULE:FREQ=WEEKLY;COUNT=6\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, feed) }))
	defer srv.Close()
	_, err := a.db.Exec(`INSERT INTO sources(url, kind, label, category, town) VALUES(?,?,?,?,?)`, srv.URL+"/cal", "ics", "Church", "faith", "gordons-bay")
	must(t, err)
	summary := a.runWatchWhere("manual", "url LIKE ?", srv.URL+"%")
	if !strings.Contains(summary, "1 new events queued") {
		t.Fatalf("want exactly one queued event for the series, got: %s", summary)
	}
	var title, date, tm, endTm, sum string
	must(t, a.db.QueryRow(`SELECT title, date, time, end_time, summary FROM events WHERE source_id = (SELECT id FROM sources WHERE label='Church')`).Scan(&title, &date, &tm, &endTm, &sum))
	next, _ := time.Parse("2006-01-02", date)
	if title != "Sunday service" || next.Weekday() != time.Sunday || next.Before(time.Now().AddDate(0, 0, -1)) {
		t.Fatalf("queued %q on %s", title, date)
	}
	if tm != "09:00" || endTm != "10:30" {
		t.Fatalf("times %s-%s", tm, endTm)
	}
	if !strings.HasPrefix(sum, "Repeats every week on Sunday.") {
		t.Fatalf("summary %q", sum)
	}
	var st string
	must(t, a.db.QueryRow(`SELECT last_status FROM sources WHERE label='Church'`).Scan(&st))
	if st != "ok, 3 events in feed, 1 new, 1 recurring" {
		t.Fatalf("status %q", st)
	}
}

/* ---------- list sources ---------- */

func TestExtractLinks(t *testing.T) {
	rss := `<?xml version="1.0"?><rss><channel><title>All</title><link>https://allevents.in/somerset-west</link>
<item><title><![CDATA[Art & Wine Auction]]></title><link><![CDATA[https://allevents.in/somerset-west/art/1]]></link></item>
<item><title>Bird Walk</title><link>https://allevents.in/somerset-west/bird/2</link></item>
<item><title>Dup</title><link>https://allevents.in/somerset-west/bird/2</link></item></channel></rss>`
	got := extractLinks([]byte(rss), "https://allevents.in/somerset-west/RSS")
	if len(got) != 2 || got[0].Title != "Art & Wine Auction" || got[0].URL != "https://allevents.in/somerset-west/art/1" || got[1].Title != "Bird Walk" {
		t.Fatalf("rss links: %+v", got)
	}
	html := `<html><body><a href="/events/one/">One <b>event</b></a> <a href="https://x.example/two#frag">Two</a>
<a href="mailto:a@b.c">mail</a> <a href="/img.png">pic</a> <a href="/events/one/">again</a> <a href="#top">top</a></body></html>`
	got = extractLinks([]byte(html), "https://showme.example/helderberg/")
	if len(got) != 2 || got[0].URL != "https://showme.example/events/one/" || got[0].Title != "One event" || got[1].URL != "https://x.example/two" {
		t.Fatalf("html links: %+v", got)
	}
}

// A list source learns the page on its first check without a word, then
// reports only links that were not there before, and honours the filter.
func TestWatchListSource(t *testing.T) {
	a, mailDir := testApp(t)
	items := []string{`<item><title>Bird Walk in Somerset West</title><link>https://ae.example/sw/bird/1</link></item>`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<rss><channel>`+strings.Join(items, "")+`</channel></rss>`)
	}))
	defer srv.Close()
	_, err := a.db.Exec(`INSERT INTO sources(url, kind, label, category, town, match) VALUES(?,?,?,?,?,?)`, srv.URL+"/RSS", "list", "Aggregator", "community", "somerset-west", "somerset|strand|gordon")
	must(t, err)
	watch := func() string { return a.runWatchWhere("manual", "url LIKE ?", srv.URL+"%") }
	s := watch()
	if !strings.Contains(s, "0 pages changed") {
		t.Fatalf("first run must be silent: %s", s)
	}
	var st string
	must(t, a.db.QueryRow(`SELECT last_status FROM sources WHERE label='Aggregator'`).Scan(&st))
	if st != "ok, 1 links remembered" {
		t.Fatalf("first status %q", st)
	}
	before, _ := filepath.Glob(filepath.Join(mailDir, "*"))
	items = append(items,
		`<item><title>Comedy night at the Strand</title><link>https://ae.example/sw/comedy/2</link></item>`,
		`<item><title>Robin Schulz in Timmendorfer</title><link>https://ae.example/de/robin/3</link></item>`)
	s = watch()
	if !strings.Contains(s, "1 pages changed") {
		t.Fatalf("second run: %s", s)
	}
	must(t, a.db.QueryRow(`SELECT last_status FROM sources WHERE label='Aggregator'`).Scan(&st))
	if st != "ok, 3 links, 1 new" {
		t.Fatalf("second status %q", st)
	}
	after, _ := filepath.Glob(filepath.Join(mailDir, "*"))
	if len(after) != len(before)+1 {
		t.Fatalf("want one watch email, got %d", len(after)-len(before))
	}
	raw, _ := os.ReadFile(after[len(after)-1])
	if !strings.Contains(string(raw), "Comedy night at the Strand") || strings.Contains(string(raw), "Timmendorfer") || strings.Contains(string(raw), "Bird Walk") {
		t.Fatalf("watch mail lists the wrong links:\n%s", raw)
	}
	// Third run: nothing new, no mail.
	s = watch()
	if !strings.Contains(s, "0 pages changed") {
		t.Fatalf("third run: %s", s)
	}
	must(t, a.db.QueryRow(`SELECT last_status FROM sources WHERE label='Aggregator'`).Scan(&st))
	if st != "ok, 3 links, nothing new" {
		t.Fatalf("third status %q", st)
	}
	// The console accepts the kind.
	if _, err := a.saveSource(url.Values{"url": {"https://example.org/feed"}, "label": {"Feed"}, "kind": {"list"}, "category": {"community"}, "town": {"strand"}}); err != nil {
		t.Fatalf("saveSource list: %v", err)
	}
	if _, err := a.saveSource(url.Values{"url": {"https://example.org/x"}, "label": {"X"}, "kind": {"rss"}, "category": {"community"}, "town": {"strand"}}); err == nil {
		t.Fatal("saveSource accepted an unknown kind")
	}
}

/* ---------- facebook groups ---------- */

func TestGroupsSeedAndRota(t *testing.T) {
	a, mailDir := testApp(t)
	n := a.count(`SELECT COUNT(*) FROM fb_groups`)
	if n < 80 {
		t.Fatalf("only %d groups seeded", n)
	}
	// The UK group and the pending rentals group ship switched off.
	for _, id := range []string{"whatsonwestsomerset", "784544034975739"} {
		var en int
		var reason string
		must(t, a.db.QueryRow(`SELECT enabled, skip_reason FROM fb_groups WHERE fb_id = ?`, id).Scan(&en, &reason))
		if en != 0 || reason == "" {
			t.Errorf("%s should be off with a reason, got enabled=%d reason=%q", id, en, reason)
		}
	}
	// Reseeding keeps the admin's state: switch one on, reseed, still on.
	_, err := a.db.Exec(`UPDATE fb_groups SET enabled = 1, skip_reason = '', cadence_days = 45 WHERE fb_id = 'whatsonwestsomerset'`)
	must(t, err)
	must(t, a.seedGroups())
	var en, cad int
	must(t, a.db.QueryRow(`SELECT enabled, cadence_days FROM fb_groups WHERE fb_id = 'whatsonwestsomerset'`).Scan(&en, &cad))
	if en != 1 || cad != 45 {
		t.Fatalf("reseed clobbered admin state: enabled=%d cadence=%d", en, cad)
	}
	_, _ = a.db.Exec(`UPDATE fb_groups SET enabled = 0, skip_reason = 'uk' WHERE fb_id = 'whatsonwestsomerset'`)

	now := time.Now().In(a.cfg.TZ)
	today := now.Format("2006-01-02")
	enabled := a.count(`SELECT COUNT(*) FROM fb_groups WHERE enabled = 1`)
	due := a.dueGroups(today, 0)
	if len(due) != enabled {
		t.Fatalf("every enabled group is due on day one: %d due, %d enabled", len(due), enabled)
	}
	if batch := a.dueGroups(today, 4); len(batch) != 4 {
		t.Fatalf("batch of 4, got %d", len(batch))
	}
	// Mark one posted: it leaves the due list and comes back a cadence later.
	g := due[0]
	posted, err := a.markPosted(fmt.Sprint(g.ID), now)
	must(t, err)
	wantNext := now.AddDate(0, 0, g.Cadence).Format("2006-01-02")
	if posted.NextDue != wantNext || posted.Posts != 1 {
		t.Fatalf("after posting: next=%s posts=%d want %s/1", posted.NextDue, posted.Posts, wantNext)
	}
	for _, d := range a.dueGroups(today, 0) {
		if d.ID == g.ID {
			t.Fatal("a group just posted is still due")
		}
	}
	if len(a.dueGroups(wantNext, 0)) < 1 {
		t.Fatal("the group is not due again on its next date")
	}
	if st := posted.State(today); st != "scheduled" {
		t.Fatalf("state after posting = %q", st)
	}
	if st := posted.State(wantNext); st != "due today" {
		t.Fatalf("state on the due day = %q", st)
	}
	if st := posted.State("2099-01-01"); st != "overdue" {
		t.Fatalf("state long after = %q", st)
	}

	// The post text: lead by kind, upcoming events, site links; parents'
	// groups get family events first.
	_, err = a.db.Exec(`INSERT INTO events(id, title, date, town, category, summary, cost, status, origin, created_at) VALUES
		('kids-day', 'Kids fun day', ?, 'strand', 'family', 'x', 'free', 'approved', 'seed', ?),
		('wine-eve', 'Wine evening', ?, 'somerset-west', 'wine', 'x', 'R150', 'approved', 'seed', ?)`,
		now.AddDate(0, 0, 10).Format("2006-01-02"), now.UTC().Format(time.RFC3339), now.AddDate(0, 0, 3).Format("2006-01-02"), now.UTC().Format(time.RFC3339))
	must(t, err)
	mammas := a.groups(`fb_id = '654371487965184'`)[0]
	text := a.groupText(mammas, now)
	// A first post (posts = 0) introduces the three links BEFORE any events
	// and lists only a few events (default 3); no WhatsApp link set yet, so
	// it points to the subscribe page.
	for _, want := range []string{"For everyone in HELDERBERG MAMMAS", a.cfg.SiteURL + "\n", "Follow the Facebook page for daily updates: https://www.facebook.com/helderbergsocial", "Join the community and get the week's events by email (WhatsApp coming soon): " + a.cfg.SiteURL + "/subscribe.html", "A taste of what's coming up:", "/submit.html?kind=event"} {
		if !strings.Contains(text, want) {
			t.Fatalf("mammas first post lacks %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "Follow the Facebook page") > strings.Index(text, "Kids fun day") {
		t.Fatalf("first post must put the links before the events:\n%s", text)
	}
	if strings.Index(text, "Kids fun day") > strings.Index(text, "Wine evening") {
		t.Fatalf("family event should lead for a parents' group:\n%s", text)
	}
	// With the WhatsApp invite link set on the console it replaces the subscribe line.
	must(t, a.metaSet("set:whatsapp_group_url", "https://chat.whatsapp.com/ABC123"))
	if wt := a.groupText(mammas, now); !strings.Contains(wt, "Join the WhatsApp community group: https://chat.whatsapp.com/ABC123") || strings.Contains(wt, "/subscribe.html") {
		t.Fatalf("whatsapp link should replace the subscribe line:\n%s", wt)
	}
	// "events in a first post" 0 drops the taste list entirely.
	must(t, a.metaSet("set:fb_groups_events", "0"))
	if zt := a.groupText(mammas, now); strings.Contains(zt, "Kids fun day") || strings.Contains(zt, "A taste") {
		t.Fatalf("first post with 0 events should carry no list:\n%s", zt)
	}
	must(t, a.metaSet("set:fb_groups_events", "3"))
	// A later post (posts > 0) leads with the month's events and closes with the same links.
	later := mammas
	later.Posts = 2
	lt := a.groupText(later, now)
	if !strings.Contains(lt, "Coming up in the next month:") || strings.Index(lt, "Kids fun day") > strings.Index(lt, "Follow the Facebook page") || !strings.Contains(lt, "chat.whatsapp.com/ABC123") {
		t.Fatalf("later post shape:\n%s", lt)
	}
	must(t, a.metaSet("set:whatsapp_group_url", ""))
	market := a.groups(`kind = 'market' AND enabled = 1`)[0]
	if mt := a.groupText(market, now); !strings.HasPrefix(mt, "Not a sale, a free local resource") {
		t.Fatalf("market lead:\n%s", mt)
	}

	// The reminder: once a day at the set hour, at most per_day groups, not on Sundays.
	_ = a.metaSet("set:fb_groups_per_day", "3")
	hour := a.settingInt("fb_groups_hour")
	monday := time.Date(2026, 9, 7, hour, 5, 0, 0, a.cfg.TZ)
	before, _ := filepath.Glob(filepath.Join(mailDir, "*"))
	a.groupsTick(monday)
	a.groupsTick(monday.Add(20 * time.Minute)) // same day: no second mail
	after, _ := filepath.Glob(filepath.Join(mailDir, "*"))
	if len(after) != len(before)+1 {
		t.Fatalf("want one reminder, got %d", len(after)-len(before))
	}
	raw, _ := os.ReadFile(after[len(after)-1])
	body := string(raw)
	if !strings.Contains(body, "3 of ") || strings.Count(body, "facebook.com/groups/") != 3 || !strings.Contains(body, "/admin/facebook/groups") || strings.Contains(body, "events.html") {
		t.Fatalf("reminder body:\n%s", body)
	}
	sunday := time.Date(2026, 9, 13, hour, 0, 0, 0, a.cfg.TZ)
	a.groupsTick(sunday)
	after2, _ := filepath.Glob(filepath.Join(mailDir, "*"))
	if len(after2) != len(after) {
		t.Fatal("a reminder went out on a Sunday")
	}
	_ = a.metaSet("set:fb_groups_remind", "0")
	a.groupsTick(time.Date(2026, 9, 8, hour, 0, 0, 0, a.cfg.TZ))
	after3, _ := filepath.Glob(filepath.Join(mailDir, "*"))
	if len(after3) != len(after) {
		t.Fatal("a reminder went out with the setting off")
	}

	// Console form: a pasted URL is accepted, a duplicate is refused, a bad cadence is refused.
	if msg, err := a.saveGroup(url.Values{"fb_id": {"https://www.facebook.com/groups/999000111/?ref=share"}, "name": {"New group"}, "kind": {"community"}, "cadence": {"30"}}); err != nil || !strings.Contains(msg, "due now") {
		t.Fatalf("saveGroup: %v %q", err, msg)
	}
	if _, err := a.saveGroup(url.Values{"fb_id": {"999000111"}, "name": {"New group again"}, "kind": {"community"}}); err == nil {
		t.Fatal("duplicate group accepted")
	}
	if _, err := a.saveGroup(url.Values{"fb_id": {"999000112"}, "name": {"Fast"}, "kind": {"community"}, "cadence": {"1"}}); err == nil {
		t.Fatal("cadence of 1 day accepted")
	}
}

// The console page renders for a signed-in admin, with the batch, the
// preview and the actions wired through /admin/do.
func TestGroupsConsolePage(t *testing.T) {
	a, mailDir := testApp(t)
	c, csrf := login(t, a, mailDir, "10.0.0.7")
	rr := c.do("GET", "/admin/facebook/groups", nil)
	body := rr.Body.String()
	if rr.Code != 200 || !strings.Contains(body, "Today's batch") || !strings.Contains(body, "HELDERBERG MAMMAS") || !strings.Contains(body, "Email me today's batch now") {
		t.Fatalf("page: %d\n%s", rr.Code, body[:min(len(body), 400)])
	}
	g := a.groups(`fb_id = '654371487965184'`)[0]
	rr = c.do("GET", fmt.Sprintf("/admin/facebook/groups?preview=%d", g.ID), nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Post for HELDERBERG MAMMAS") || !strings.Contains(rr.Body.String(), "For everyone in HELDERBERG MAMMAS") {
		t.Fatalf("preview: %d", rr.Code)
	}
	do := func(action string, extra url.Values) *httptest.ResponseRecorder {
		f := url.Values{"csrf": {csrf}, "action": {action}, "id": {fmt.Sprint(g.ID)}}
		for k, v := range extra {
			f[k] = v
		}
		return c.do("POST", "/admin/do", f)
	}
	if rr = do("grp-posted", nil); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "/admin/facebook/groups") {
		t.Fatalf("grp-posted: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if g2 := a.groups(`id = ?`, g.ID)[0]; g2.Posts != 1 || g2.NextDue == "" {
		t.Fatalf("after grp-posted: %+v", g2)
	}
	do("grp-defer", url.Values{"days": {"10"}})
	d := a.groups(`id = ?`, g.ID)[0].NextDue
	want := time.Now().In(a.cfg.TZ).AddDate(0, 0, 10).Format("2006-01-02")
	if d != want {
		t.Fatalf("defer: next_due %s want %s", d, want)
	}
	do("grp-skip", url.Values{"reason": {"admins said no promotion"}})
	if g3 := a.groups(`id = ?`, g.ID)[0]; g3.Enabled || g3.SkipReason != "admins said no promotion" {
		t.Fatalf("skip: %+v", g3)
	}
	do("grp-enable", nil)
	if !a.groups(`id = ?`, g.ID)[0].Enabled {
		t.Fatal("enable did not switch the group on")
	}
	// A wrong CSRF token is refused.
	if rr = c.do("POST", "/admin/do", url.Values{"csrf": {"nope"}, "action": {"grp-delete"}, "id": {fmt.Sprint(g.ID)}}); rr.Code == 303 && strings.Contains(rr.Header().Get("Location"), "msg=Removed") {
		t.Fatal("delete went through with a bad csrf token")
	}
	if len(a.groups(`id = ?`, g.ID)) != 1 {
		t.Fatal("group deleted despite bad csrf")
	}
	rr = do("grp-remind", nil)
	if _, err := latestMailMaybe(mailDir, "fbgroups"); err != nil {
		t.Fatalf("grp-remind sent no mail: %v", err)
	}
}

/* ---------- seed retirement and fetch retry ---------- */

func TestSeedRetiresDeadSources(t *testing.T) {
	a, _ := testApp(t)
	var en int
	var st string
	must(t, a.db.QueryRow(`SELECT enabled, last_status FROM sources WHERE url = 'https://www.parkrun.co.za/somersetwest/'`).Scan(&en, &st))
	if en != 0 || !strings.HasPrefix(st, "retired: replaced by the news feed") {
		t.Fatalf("parkrun: enabled=%d status=%q", en, st)
	}
	must(t, a.db.QueryRow(`SELECT enabled FROM sources WHERE url LIKE '%469267d8275f0881%'`).Scan(&en))
	if en != 1 {
		t.Fatal("the GBYC calendar feed should be on")
	}
	// A changed reason lands even on a row that is already off.
	_, _ = a.db.Exec(`UPDATE sources SET last_status = 'retired: old reason' WHERE url = 'https://www.parkrun.co.za/somersetwest/'`)
	must(t, a.seedSources())
	must(t, a.db.QueryRow(`SELECT last_status FROM sources WHERE url = 'https://www.parkrun.co.za/somersetwest/'`).Scan(&st))
	if !strings.HasPrefix(st, "retired: replaced by the news feed") {
		t.Fatalf("reason not refreshed: %q", st)
	}
	// A retired row is not checked by the scheduled run.
	if n := a.count(`SELECT COUNT(*) FROM sources WHERE enabled = 1 AND last_status LIKE 'retired:%'`); n != 0 {
		t.Fatalf("%d retired sources still enabled", n)
	}
}

func TestFetchRetriesOnceOnServerError(t *testing.T) {
	a, _ := testApp(t)
	fetchRetryPause = 10 * time.Millisecond
	defer func() { fetchRetryPause = 3 * time.Second }()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/flaky":
			if hits == 1 {
				w.WriteHeader(503)
				return
			}
			fmt.Fprint(w, "<html>fine now</html>")
		case "/blocked":
			w.WriteHeader(403)
		}
	}))
	defer srv.Close()
	body, err := a.fetch(srv.URL + "/flaky")
	if err != nil || !strings.Contains(string(body), "fine now") || hits != 2 {
		t.Fatalf("flaky: err=%v hits=%d", err, hits)
	}
	hits = 0
	if _, err := a.fetch(srv.URL + "/blocked"); err == nil || err.Error() != "HTTP 403" || hits != 1 {
		t.Fatalf("blocked: err=%v hits=%d (a 4xx must not be retried)", err, hits)
	}
}
