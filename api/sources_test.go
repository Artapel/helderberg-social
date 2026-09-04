package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A regional feed with a filter only queues the events that mention the
// Helderberg; the rest are counted, not queued, and not offered again. The
// same feed without a filter queues everything in the window.
func TestWatchFilterOnRegionalFeed(t *testing.T) {
	a, _ := testApp(t)
	loc := a.cfg.TZ
	in := time.Now().In(loc).AddDate(0, 0, 10).Format("20060102")
	feed := "BEGIN:VCALENDAR\r\n" +
		"BEGIN:VEVENT\r\nUID:a@wpa\r\nSUMMARY:Blouberg Marathon\r\nDTSTART;VALUE=DATE:" + in + "\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:b@wpa\r\nSUMMARY:Gordon's Bay Beach Run\r\nDTSTART;VALUE=DATE:" + in + "\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:c@wpa\r\nSUMMARY:Club 10km\r\nDESCRIPTION:Start at the Strand Pavilion\r\nDTSTART;VALUE=DATE:" + in + "\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:d@wpa\r\nSUMMARY:Trail day\r\nLOCATION:Helderberg Nature Reserve\\, Somerset West\r\nDTSTART;VALUE=DATE:" + in + "\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		fmt.Fprint(w, feed)
	}))
	defer srv.Close()

	_, err := a.db.Exec(`INSERT INTO sources(url, kind, label, category, town, match) VALUES(?,?,?,?,?,?)`,
		srv.URL+"/filtered", "ics", "WPA (filtered)", "sport", "somerset-west", `somerset|strand|gordon|helderberg`)
	must(t, err)
	_, err = a.db.Exec(`INSERT INTO sources(url, kind, label, category, town) VALUES(?,?,?,?,?)`,
		srv.URL+"/open", "ics", "WPA (everything)", "sport", "strand")
	must(t, err)

	// Only the test server's sources: the seeded list points at real sites.
	watch := func() string { return a.runWatchWhere("manual", "url LIKE ?", srv.URL+"%") }
	summary := watch()
	if !strings.Contains(summary, "7 new events queued") {
		t.Fatalf("want 3 filtered + 4 unfiltered = 7 queued, got: %s", summary)
	}
	var st string
	must(t, a.db.QueryRow(`SELECT last_status FROM sources WHERE label = 'WPA (filtered)'`).Scan(&st))
	if st != "ok, 4 events in feed, 3 new, 1 outside the filter" {
		t.Fatalf("filtered status = %q", st)
	}
	must(t, a.db.QueryRow(`SELECT last_status FROM sources WHERE label = 'WPA (everything)'`).Scan(&st))
	if st != "ok, 4 events in feed, 4 new" {
		t.Fatalf("unfiltered status = %q", st)
	}
	var blouberg int
	must(t, a.db.QueryRow(`SELECT COUNT(*) FROM events e JOIN sources s ON s.id = e.source_id WHERE s.label = 'WPA (filtered)' AND e.title = 'Blouberg Marathon'`).Scan(&blouberg))
	if blouberg != 0 {
		t.Fatal("the out-of-area event was queued despite the filter")
	}
	// Queued events carry the source's category and town.
	var cat, town string
	must(t, a.db.QueryRow(`SELECT e.category, e.town FROM events e JOIN sources s ON s.id = e.source_id WHERE s.label = 'WPA (filtered)' AND e.title = 'Trail day'`).Scan(&cat, &town))
	if cat != "sport" || town != "somerset-west" {
		t.Fatalf("queued event has %s/%s", cat, town)
	}
	// A second run offers nothing new; the skipped one stays skipped.
	summary = watch()
	if !strings.Contains(summary, "0 new events queued") {
		t.Fatalf("second run: %s", summary)
	}

	// A bad pattern is refused by the console and reported by the watcher.
	if _, err := a.saveSource(map[string][]string{"url": {srv.URL + "/bad"}, "label": {"Bad"}, "kind": {"ics"}, "category": {"sport"}, "town": {"strand"}, "match": {"("}}); err == nil {
		t.Fatal("console accepted an invalid filter")
	}
	_, err = a.db.Exec(`INSERT INTO sources(url, kind, label, category, town, match) VALUES(?,?,?,?,?,?)`, srv.URL+"/bad", "ics", "Bad", "sport", "strand", "(")
	must(t, err)
	watch()
	must(t, a.db.QueryRow(`SELECT last_status FROM sources WHERE label = 'Bad'`).Scan(&st))
	if !strings.HasPrefix(st, "error") {
		t.Fatalf("bad filter status = %q", st)
	}
	if _, err := compileMatch(strings.Repeat("a", matchLimit+1)); err == nil {
		t.Fatal("over-long filter accepted")
	}
}

// Every seeded source has a valid kind, town, category and filter, and the
// new categories are known everywhere they need to be.
func TestSeedSourcesAndCategories(t *testing.T) {
	a, _ := testApp(t)
	rows, err := a.db.Query(`SELECT url, kind, category, town, match FROM sources`)
	must(t, err)
	defer rows.Close()
	n := 0
	for rows.Next() {
		var u, k, c, tw, m string
		must(t, rows.Scan(&u, &k, &c, &tw, &m))
		n++
		if !sourceKinds[k] || !categories[c] || !towns[tw] {
			t.Errorf("seeded source %s has kind=%s category=%s town=%s", u, k, c, tw)
		}
		if _, err := compileMatch(m); err != nil {
			t.Errorf("seeded source %s filter: %v", u, err)
		}
	}
	if n < 40 {
		t.Fatalf("only %d sources seeded", n)
	}
	for _, c := range []string{"sport", "music", "faith", "camping"} {
		if !categories[c] || catNames[c] == "" {
			t.Errorf("category %s missing from the vocabulary or its names", c)
		}
	}
	if len(catNames) != len(categories) {
		t.Fatalf("catNames has %d entries, categories %d", len(catNames), len(categories))
	}
}
