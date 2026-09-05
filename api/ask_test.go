package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// repoDataJS reads the real data/data.js from the checkout, so the parser is
// tested against the file the site actually serves.
func repoDataJS(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../data/data.js")
	if err != nil {
		t.Skip("data/data.js not beside api/:", err)
	}
	return b
}

func TestParseRealDataJS(t *testing.T) {
	d, err := parseDataJS(repoDataJS(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Listings) < 70 || len(d.Events) < 1 {
		t.Fatalf("got %d listings, %d events", len(d.Listings), len(d.Events))
	}
	var archery, parkrun *Listing
	for i := range d.Listings {
		switch d.Listings[i].ID {
		case "helderberg-target-archery":
			archery = &d.Listings[i]
		case "somerset-west-parkrun":
			parkrun = &d.Listings[i]
		}
	}
	if archery == nil || archery.Town != "somerset-west" || archery.Category != "sport" || len(archery.Tags) == 0 {
		t.Fatalf("archery listing not parsed: %+v", archery)
	}
	if parkrun == nil || parkrun.Status.Kind != "paused" {
		t.Fatalf("parkrun status not parsed: %+v", parkrun)
	}
	for _, e := range d.Events {
		if e.Date == "" || e.Title == "" || e.Town == "" {
			t.Fatalf("event incomplete: %+v", e)
		}
	}
}

func TestJSToJSONEdgeCases(t *testing.T) {
	src := []byte(`window.X = {
  /* comment with "quotes" and a trailing, comma */
  a: 'it is', // line comment, with: colon
  b: "say \"hi\"", c: [1, 2, 3,], d: { e: true, f: null, }, g: "a/b // not a comment",
  h: 'don\'t',
};`)
	var v map[string]any
	d := jsToJSON(src[len("window.X ="):])
	d = []byte(strings.TrimSuffix(strings.TrimSpace(string(d)), ";"))
	if err := json.Unmarshal(d, &v); err != nil {
		t.Fatalf("%v\n%s", err, d)
	}
	if v["a"] != "it is" || v["b"] != `say "hi"` || len(v["c"].([]any)) != 3 || v["d"].(map[string]any)["e"] != true || v["g"] != "a/b // not a comment" || v["h"] != "don't" {
		t.Fatalf("parsed wrong: %+v", v)
	}
}

func TestReadQuestion(t *testing.T) {
	loc, _ := time.LoadLocation("Africa/Johannesburg")
	sat := time.Date(2026, 9, 5, 0, 0, 0, 0, loc) // a Saturday
	wed := time.Date(2026, 9, 9, 0, 0, 0, 0, loc)
	cases := []struct {
		in                       string
		now                      time.Time
		period, town, cat, wants string
		free                     bool
		from, to                 string
	}{
		{"@helderberg today", wed, "today", "", "", "events", false, "2026-09-09", "2026-09-09"},
		{"@Helderberg Social: Week", wed, "week", "", "", "events", false, "2026-09-09", "2026-09-16"},
		{"helderberg weekend", wed, "weekend", "", "", "events", false, "2026-09-12", "2026-09-13"},
		{"what's on this weekend in strand", sat, "weekend", "strand", "", "events", false, "2026-09-05", "2026-09-06"},
		{"free markets in Somerset West tomorrow", wed, "tomorrow", "somerset-west", "markets", "events", true, "2026-09-10", "2026-09-10"},
		{"running clubs in gordons bay", wed, "", "gordons-bay", "running", "listings", false, "", ""},
		{"is there archery?", wed, "", "", "sport", "", false, "", ""},
		{"kids things sir lowry's pass", wed, "", "sir-lowrys-pass", "family", "", false, "", ""},
	}
	for _, c := range cases {
		q := readQuestion(c.in, c.now)
		if q.period != c.period || q.town != c.town || q.category != c.cat || q.wants != c.wants || q.free != c.free || q.from != c.from || q.to != c.to {
			t.Errorf("%q: got period=%q town=%q cat=%q wants=%q free=%v from=%q to=%q", c.in, q.period, q.town, q.category, q.wants, q.free, q.from, q.to)
		}
	}
	if !readQuestion("", wed).help || !readQuestion("@helderberg help", wed).help {
		t.Fatal("help not detected")
	}
	q := readQuestion("is there archery?", wed)
	if len(q.words) != 1 || q.words[0] != "archery" {
		t.Fatalf("words: %v", q.words)
	}
}

// askApp is a test app whose data.js comes from the checkout, not the network.
func askApp(t *testing.T) *App {
	t.Helper()
	a, _ := testApp(t)
	src := repoDataJS(t)
	old := askFetch
	askFetch = func(*App) ([]byte, error) { return src, nil }
	askMu.Lock()
	askData = nil
	askMu.Unlock()
	t.Cleanup(func() { askFetch = old; askMu.Lock(); askData = nil; askMu.Unlock() })
	return a
}

func TestAnswerFromSiteAndDatabase(t *testing.T) {
	a := askApp(t)
	today := a.localDay(time.Now())
	tomorrow := a.localDay(time.Now().Add(24 * time.Hour))
	must := func(e Event) {
		if err := a.insertEvent(e, "h", "", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := a.db.Exec(`UPDATE events SET status='approved', decided_at=? WHERE id=?`, now(), e.ID); err != nil {
			t.Fatal(err)
		}
	}
	must(Event{ID: "t-strand-beach-clean", Title: "Strand beach clean-up", Date: today, Time: "08:00", Town: "strand", Category: "community", Summary: "Bring gloves.", Cost: "free", Status: "approved", Origin: "seed"})
	must(Event{ID: "t-gb-market", Title: "Gordon's Bay night market", Date: tomorrow, Time: "17:00", Town: "gordons-bay", Category: "markets", Summary: "Food stalls on the harbour.", Cost: "free", Status: "approved", Origin: "seed"})
	must(Event{ID: "t-hidden", Title: "Hidden thing", Date: today, Town: "strand", Category: "music", Status: "pending_review", Origin: "seed"})
	_, _ = a.db.Exec(`UPDATE events SET status='pending_review' WHERE id='t-hidden'`)

	r := a.answer("@helderberg today")
	if !strings.Contains(r, "Today, ") || !strings.Contains(r, "08:00 Strand beach clean-up · Strand · free") || strings.Contains(r, "night market") || strings.Contains(r, "Hidden thing") {
		t.Fatalf("today:\n%s", r)
	}
	r = a.answer("Helderberg week")
	if !strings.Contains(r, "next 7 days") || !strings.Contains(r, "beach clean-up") || !strings.Contains(r, "night market") || !strings.Contains(r, "_"+fmtDate(tomorrow)+"_") {
		t.Fatalf("week:\n%s", r)
	}
	r = a.answer("free things in gordons bay tomorrow")
	if !strings.Contains(r, "Tomorrow") || !strings.Contains(r, "night market") || strings.Contains(r, "beach clean") || !strings.Contains(r, "events.html?town=gordons-bay") {
		t.Fatalf("tomorrow gb:\n%s", r)
	}
	r = a.answer("is there archery?")
	if !strings.Contains(r, "*Helderberg Target Archery*") || !strings.Contains(r, "directory.html?cat=sport") {
		t.Fatalf("archery:\n%s", r)
	}
	r = a.answer("horse riding")
	if !strings.Contains(r, "Journey's End Horseback Rides") {
		t.Fatalf("horse:\n%s", r)
	}
	r = a.answer("running clubs in strand")
	if strings.Contains(r, "Today") || !strings.Contains(r, "Groups, activities and places in Strand · Running & walking") {
		t.Fatalf("running strand:\n%s", r)
	}
	r = a.answer("xyzzy plugh")
	if !strings.Contains(r, "could not find anything") {
		t.Fatalf("nothing:\n%s", r)
	}
	if r = a.answer("help"); !strings.Contains(r, "*today*") {
		t.Fatalf("help:\n%s", r)
	}
	if len(a.answer("week")) > askMaxReply {
		t.Fatal("reply too long")
	}
	// The public door gives the same answer.
	rec := get(t, a.routes(), "/api/ask?q=today")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Strand beach clean-up") {
		t.Fatalf("/api/ask: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAnswerUsesModelWhenConfigured(t *testing.T) {
	a := askApp(t)
	var gotSystem, gotUser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string              `json:"model"`
			Messages []map[string]string `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotSystem, gotUser = body.Messages[0]["content"], body.Messages[1]["content"]
		if r.URL.Path != "/api/chat" || body.Model != "llama3.1:8b" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"Yes: *Helderberg Target Archery* in Somerset West."}}`))
	}))
	defer srv.Close()
	a.cfg.AIURL, a.cfg.AIModel = srv.URL, "llama3.1:8b"
	r := a.answer("where can my son learn archery?")
	if !strings.HasPrefix(r, "Yes: *Helderberg Target Archery*") {
		t.Fatalf("model answer not used:\n%s", r)
	}
	if !strings.Contains(gotUser, "Helderberg Target Archery") || !strings.Contains(gotUser, "listing.html?id=helderberg-target-archery") || strings.Contains(gotUser, "27") && strings.Contains(gotUser, "tel:") {
		t.Fatalf("context wrong:\n%s", gotUser)
	}
	if !strings.Contains(gotSystem, "ONLY the site content") {
		t.Fatalf("system prompt: %s", gotSystem)
	}
	// A bare command never goes to the model.
	gotUser = ""
	_ = a.answer("today")
	if gotUser != "" {
		t.Fatal("bare command was sent to the model")
	}
	// Model down: plain answer, not an error.
	srv.Close()
	if r = a.answer("is there archery?"); !strings.Contains(r, "*Helderberg Target Archery*") {
		t.Fatalf("fallback:\n%s", r)
	}
}

func TestWhatsAppQuestionGetsAnswer(t *testing.T) {
	a, g := waApp(t)
	src := repoDataJS(t)
	old := askFetch
	askFetch = func(*App) ([]byte, error) { return src, nil }
	askMu.Lock()
	askData = nil
	askMu.Unlock()
	t.Cleanup(func() { askFetch = old; askMu.Lock(); askData = nil; askMu.Unlock() })

	a.waInbound("27821234567", "text", "@helderberg is there archery?", "")
	m := g.last()
	if m == nil || m["type"] != "text" {
		t.Fatalf("no text reply: %+v", m)
	}
	body := m["text"].(map[string]any)["body"].(string)
	if !strings.Contains(body, "Helderberg Target Archery") {
		t.Fatalf("reply: %s", body)
	}
	a.waInbound("27821234567", "text", "help", "")
	if body = g.last()["text"].(map[string]any)["body"].(string); !strings.Contains(body, "*today*") {
		t.Fatalf("help reply: %s", body)
	}
	// STOP still works and is not treated as a question.
	a.waInbound("27821234567", "text", "STOP", "")
	if body = g.last()["text"].(map[string]any)["body"].(string); !strings.Contains(body, "not subscribed") {
		t.Fatalf("stop reply: %s", body)
	}
	// Budget: after askPerHour answers in an hour the number gets silence.
	h := emailHash("tel:27821234567")
	for i := 0; i < askPerHour; i++ {
		a.logMail(h, "wa-answer", nil)
	}
	n := len(g.calls)
	a.waInbound("27821234567", "text", "today", "")
	if len(g.calls) != n {
		t.Fatal("answered past the budget")
	}
}
