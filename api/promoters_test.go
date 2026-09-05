package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// applyAndApprove registers a member, applies as a promoter and has the
// admin approve. Returns the signed-in member client, its CSRF token, the
// admin client and the admin CSRF token.
func applyAndApprove(t *testing.T, a *App, mail, email, name, org string) (*client, string, *client, string) {
	t.Helper()
	c, csrf := register(t, a, mail, email, name, "a perfectly fine passphrase", "10.2.2.2")
	if rr := c.do("GET", "/account/promoter", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "Send application") {
		t.Fatalf("apply page: %d", rr.Code)
	}
	// Promoter-only pages refuse a plain member.
	if rr := c.do("GET", "/account/promoter/posts/new", nil); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "approved+promoters") {
		t.Fatalf("plain member reached the post form: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	form := url.Values{"csrf": {csrf}, "org": {org}, "kind": {"organiser"}, "website": {"https://example.org"}, "towns": {"strand", "somerset-west"}, "blurb": {"Weekly beach runs and a monthly clean-up, open to everyone, for the whole year."}}
	if rr := c.do("POST", "/account/promoter/apply", form); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "application+is+in") {
		t.Fatalf("apply: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if body, err := latestMailMaybe(mail, "admin-promoter"); err != nil || !strings.Contains(body, org) {
		t.Fatalf("admin not told about the application: %v", err)
	}
	if rr := c.do("POST", "/account/promoter/apply", form); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "already+waiting") {
		t.Fatalf("second application accepted: %s", rr.Header().Get("Location"))
	}
	if rr := c.do("GET", "/account/promoter", nil); !strings.Contains(rr.Body.String(), "Waiting for a look") {
		t.Fatal("pending state not shown")
	}
	m := a.memberByEmail(strings.ToLower(email))
	admin, acsrf := login(t, a, mail, "10.0.0.9")
	if rr := admin.do("GET", "/admin/queue", nil); !strings.Contains(rr.Body.String(), org) || !strings.Contains(rr.Body.String(), "promoter-approve") {
		t.Fatal("application not in the queue")
	}
	if rr := admin.do("GET", "/admin/promoters", nil); !strings.Contains(rr.Body.String(), "Applications waiting (1)") {
		t.Fatal("application not on the promoters page")
	}
	rr := admin.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"promoter-approve"}, "id": {itoa(m.ID)}, "return": {"/admin/promoters"}})
	if rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "approved+promoter") {
		t.Fatalf("approve: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if body, err := latestMailMaybe(mail, "promoter-approved"); err != nil || !strings.Contains(body, "/account/promoter") {
		t.Fatalf("promoter not told: %v", err)
	}
	if m = a.memberByID(m.ID); !m.IsPromoter() || m.Trusted {
		t.Fatalf("role not set: %+v", m)
	}
	if rr := c.do("GET", "/account/promoter", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "Approved promoter") || !strings.Contains(rr.Body.String(), "Add a post") {
		t.Fatalf("promoter dashboard: %d", rr.Code)
	}
	return c, csrf, admin, acsrf
}

func jsonNum(n int64) string { return strconv.FormatInt(n, 10) }

func promoEventForm(csrf, title string, days int) url.Values {
	return url.Values{"csrf": {csrf}, "title": {title}, "date": {futureDate(days)}, "time": {"08:00"}, "town": {"strand"}, "category": {"community"}, "cost": {"free"}, "summary": {"Meet at the pier with gloves; bags provided by the municipality."}}
}

func inFeed(t *testing.T, a *App, id string) bool {
	for _, x := range feedIDs(t, a) {
		if x == id {
			return true
		}
	}
	return false
}

func feedIDs(t *testing.T, a *App) []string {
	t.Helper()
	rr := get(t, a.routes(), "/api/events")
	var out struct {
		Events []Event `json:"events"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	var ids []string
	for _, e := range out.Events {
		ids = append(ids, e.ID)
	}
	return ids
}

func TestPromoterEventsScheduleHideAndTrust(t *testing.T) {
	a, mail := testApp(t)
	c, csrf, admin, acsrf := applyAndApprove(t, a, mail, "org@example.org", "Olga", "Strand Runners")
	other, ocsrf := register(t, a, mail, "x@example.org", "Xa", "another fine passphrase", "10.2.2.3")
	m := a.memberByEmail("org@example.org")

	// Moderated promoter: the event is queued, marked promoted, carries the
	// organisation as its submitter, and honours a show-from date.
	form := promoEventForm(csrf, "Pier run", 20)
	form.Set("visible_from", futureDate(5))
	if rr := c.do("POST", "/account/events/save", form); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "in+the+queue") || !strings.Contains(rr.Header().Get("Location"), "shows+on+the+site+from") {
		t.Fatalf("save: %s", rr.Header().Get("Location"))
	}
	evs, _ := a.queryEvents(`member_id = ?`, m.ID)
	if len(evs) != 1 || evs[0].Status != "pending_review" || !evs[0].Promoted || evs[0].SubmitterName != "Strand Runners" || evs[0].VisibleFrom != futureDate(5) {
		t.Fatalf("queued event wrong: %+v", evs)
	}
	id := evs[0].ID
	// A show-from after the event is refused.
	bad := promoEventForm(csrf, "Late", 3)
	bad.Set("visible_from", futureDate(9))
	if rr := c.do("POST", "/account/events/save", bad); !strings.Contains(rr.Header().Get("Location"), "on+or+before") {
		t.Fatalf("bad show-from accepted: %s", rr.Header().Get("Location"))
	}
	// Approved, but scheduled: not in the feed until the date.
	if rr := admin.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"approve"}, "id": {id}}); rr.Code != 303 {
		t.Fatalf("approve: %d", rr.Code)
	}
	if inFeed(t, a, id) {
		t.Fatal("scheduled event leaked into the feed")
	}
	if rr := c.do("GET", "/account", nil); !strings.Contains(rr.Body.String(), "shows from") {
		t.Fatal("schedule not shown on My events")
	}
	if _, err := a.db.Exec(`UPDATE events SET visible_from = '' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	rr := get(t, a.routes(), "/api/events")
	if !inFeed(t, a, id) || !strings.Contains(rr.Body.String(), `"promoted":true`) || !strings.Contains(rr.Body.String(), `"by":"Strand Runners"`) {
		t.Fatalf("live promoted event not in feed with its organisation: %s", rr.Body.String())
	}
	// Hide: off the feed, approval kept; show: back.
	if rr := c.do("POST", "/account/promoter/events/toggle", url.Values{"csrf": {csrf}, "id": {id}, "to": {"hide"}}); rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "hidden") {
		t.Fatalf("hide: %s", rr.Header().Get("Location"))
	}
	if evs, _ = a.queryEvents(`id = ?`, id); !evs[0].Hidden || evs[0].Status != "approved" {
		t.Fatalf("hide changed the wrong thing: %+v", evs[0])
	}
	if inFeed(t, a, id) {
		t.Fatal("hidden event still in feed")
	}
	if rr := admin.do("GET", "/admin/events?status=approved", nil); !strings.Contains(rr.Body.String(), "hidden by its promoter") {
		t.Fatal("console does not show the hidden mark")
	}
	if rr := c.do("POST", "/account/promoter/events/toggle", url.Values{"csrf": {csrf}, "id": {id}, "to": {"show"}, "return": {"/account/promoter"}}); rr.Code != 303 || !strings.HasPrefix(rr.Header().Get("Location"), "/account/promoter?") {
		t.Fatalf("show: %s", rr.Header().Get("Location"))
	}
	if !inFeed(t, a, id) {
		t.Fatal("shown event not back in feed")
	}
	// Another member cannot toggle it.
	if rr := other.do("POST", "/account/promoter/events/toggle", url.Values{"csrf": {ocsrf}, "id": {id}, "to": {"hide"}}); !strings.Contains(rr.Header().Get("Location"), "approved+promoters") {
		t.Fatalf("outsider toggled: %s", rr.Header().Get("Location"))
	}

	// Trusted: publishes at once, edits stay live, nothing queued for Facebook.
	if rr := admin.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"promoter-trust"}, "id": {itoa(m.ID)}}); !strings.Contains(rr.Header().Get("Location"), "trusted") {
		t.Fatalf("trust: %s", rr.Header().Get("Location"))
	}
	if rr := c.do("POST", "/account/events/save", promoEventForm(csrf, "Dawn run", 8)); !strings.Contains(rr.Header().Get("Location"), "Published") {
		t.Fatalf("trusted save: %s", rr.Header().Get("Location"))
	}
	evs, _ = a.queryEvents(`member_id = ? AND title = 'Dawn run'`, m.ID)
	if len(evs) != 1 || evs[0].Status != "approved" {
		t.Fatalf("trusted event not published: %+v", evs)
	}
	if n := a.count(`SELECT COUNT(*) FROM fb_queue WHERE ref = ?`, "event:"+evs[0].ID); n != 0 {
		t.Fatal("auto-published event was queued for Facebook")
	}
	if !inFeed(t, a, evs[0].ID) {
		t.Fatal("trusted event not in feed")
	}
	edit := promoEventForm(csrf, "Dawn run (moved)", 9)
	edit.Set("id", evs[0].ID)
	if rr := c.do("POST", "/account/events/save", edit); !strings.Contains(rr.Header().Get("Location"), "Saved+and+live") {
		t.Fatalf("trusted edit: %s", rr.Header().Get("Location"))
	}
	if evs, _ = a.queryEvents(`id = ?`, evs[0].ID); evs[0].Status != "approved" || evs[0].Title != "Dawn run (moved)" {
		t.Fatalf("trusted edit not live: %+v", evs[0])
	}
	// Revoke: back to a plain member, published items back in the queue.
	if rr := admin.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"promoter-revoke"}, "id": {itoa(m.ID)}, "note": {"too many adverts"}}); !strings.Contains(rr.Header().Get("Location"), "no+longer") {
		t.Fatalf("revoke: %s", rr.Header().Get("Location"))
	}
	if m = a.memberByID(m.ID); m.IsPromoter() || m.Trusted {
		t.Fatalf("revoke left the role: %+v", m)
	}
	if n := a.count(`SELECT COUNT(*) FROM events WHERE member_id = ? AND status = 'approved'`, m.ID); n != 0 {
		t.Fatalf("revoke left %d events live", n)
	}
	if rr := c.do("GET", "/account/promoter", nil); !strings.Contains(rr.Body.String(), "Not approved") || !strings.Contains(rr.Body.String(), "too many adverts") {
		t.Fatal("revoked state not shown with the note")
	}
}

func TestPromoterPostsAndFeed(t *testing.T) {
	a, mail := testApp(t)
	c, csrf, admin, acsrf := applyAndApprove(t, a, mail, "posts@example.org", "Pat", "Gordons Bay Yacht Club")
	other, ocsrf := register(t, a, mail, "y@example.org", "Ya", "another fine passphrase", "10.2.2.4")
	m := a.memberByEmail("posts@example.org")

	if rr := c.do("GET", "/account/promoter/posts/new", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), "noticeboard") {
		t.Fatalf("post form: %d", rr.Code)
	}
	form := url.Values{"csrf": {csrf}, "title": {"Learn-to-sail sign-ups open"}, "body": {"Six Saturday mornings from October, ages 10 and up, boats provided. Places are limited."}, "link": {"https://example.org/sail"}, "town": {"gordons-bay"}, "category": {"water"}, "starts": {futureDate(0)}, "ends": {futureDate(14)}}
	// Validation.
	for _, tc := range [][3]string{{"ends", futureDate(200), "within+90+days"}, {"starts", "2020-01-01", "within+the+next+year"}, {"body", "short", "20%2B+characters"}, {"link", "ftp://x", "http%28s%29"}, {"town", "cape-town", "Choose+a+town"}} {
		f2 := url.Values{}
		for k, v := range form {
			f2[k] = v
		}
		f2.Set(tc[0], tc[1])
		if rr := c.do("POST", "/account/promoter/posts/save", f2); !strings.Contains(rr.Header().Get("Location"), tc[2]) {
			t.Fatalf("%s=%q accepted: %s", tc[0], tc[1], rr.Header().Get("Location"))
		}
	}
	if rr := c.do("POST", "/account/promoter/posts/save", form); !strings.Contains(rr.Header().Get("Location"), "in+the+queue") {
		t.Fatalf("post save: %s", rr.Header().Get("Location"))
	}
	posts := a.queryPosts(`member_id = ?`, m.ID)
	if len(posts) != 1 || posts[0].Status != "pending_review" || !strings.HasPrefix(posts[0].ID, "learn-to-sail-sign-ups-open-") {
		t.Fatalf("post wrong: %+v", posts)
	}
	id := posts[0].ID
	if body, err := latestMailMaybe(mail, "admin-post"); err != nil || !strings.Contains(body, "Learn-to-sail") || !strings.Contains(body, "Gordons Bay Yacht Club") {
		t.Fatalf("admin not told: %v", err)
	}
	// Not public yet.
	rr := get(t, a.routes(), "/api/posts")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"posts":[]`) {
		t.Fatalf("posts feed before approval: %d %s", rr.Code, rr.Body.String())
	}
	// Queue shows it; approve from the console; member is told; feed has it with the org.
	if rr := admin.do("GET", "/admin/queue", nil); !strings.Contains(rr.Body.String(), "Posts waiting (1)") {
		t.Fatal("post not in the queue")
	}
	if rr := admin.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"post-approve"}, "id": {id}}); !strings.Contains(rr.Header().Get("Location"), "approved") {
		t.Fatalf("post approve: %s", rr.Header().Get("Location"))
	}
	if body, err := latestMailMaybe(mail, "member-approved"); err != nil || !strings.Contains(body, "notices.html?post="+id) {
		t.Fatalf("member not told: %v", err)
	}
	rr = get(t, a.routes(), "/api/posts")
	if !strings.Contains(rr.Body.String(), `"by":"Gordons Bay Yacht Club"`) || !strings.Contains(rr.Body.String(), `"id":"`+id+`"`) || strings.Contains(rr.Body.String(), "member") {
		t.Fatalf("posts feed: %s", rr.Body.String())
	}
	// Hide, show, edit (goes back to the queue), delete needs the tick.
	if rr := c.do("POST", "/account/promoter/posts/toggle", url.Values{"csrf": {csrf}, "id": {id}, "to": {"hide"}}); !strings.Contains(rr.Header().Get("Location"), "hidden") {
		t.Fatalf("hide: %s", rr.Header().Get("Location"))
	}
	if rr := get(t, a.routes(), "/api/posts"); !strings.Contains(rr.Body.String(), `"posts":[]`) {
		t.Fatal("hidden post still public")
	}
	if rr := c.do("POST", "/account/promoter/posts/toggle", url.Values{"csrf": {csrf}, "id": {id}, "to": {"show"}}); !strings.Contains(rr.Header().Get("Location"), "shows+again") {
		t.Fatalf("show: %s", rr.Header().Get("Location"))
	}
	form.Set("id", id)
	form.Set("title", "Learn-to-sail: two places left")
	if rr := c.do("POST", "/account/promoter/posts/save", form); !strings.Contains(rr.Header().Get("Location"), "back+in+the+queue") {
		t.Fatalf("edit: %s", rr.Header().Get("Location"))
	}
	if p := a.postByID(id); p.Status != "pending_review" || p.Title != "Learn-to-sail: two places left" {
		t.Fatalf("edit: %+v", p)
	}
	if rr := other.do("POST", "/account/promoter/posts/delete", url.Values{"csrf": {ocsrf}, "id": {id}, "confirm": {"yes"}}); !strings.Contains(rr.Header().Get("Location"), "approved+promoters") {
		t.Fatal("outsider deleted a post")
	}
	if rr := c.do("POST", "/account/promoter/posts/delete", url.Values{"csrf": {csrf}, "id": {id}}); !strings.Contains(rr.Header().Get("Location"), "Tick+the+box") {
		t.Fatalf("delete without tick: %s", rr.Header().Get("Location"))
	}
	if rr := c.do("POST", "/account/promoter/posts/delete", url.Values{"csrf": {csrf}, "id": {id}, "confirm": {"yes"}}); !strings.Contains(rr.Header().Get("Location"), "deleted") {
		t.Fatalf("delete: %s", rr.Header().Get("Location"))
	}
	if a.postByID(id) != nil {
		t.Fatal("post not deleted")
	}
	// Trusted: straight to the feed, and a rejected post tells the member.
	if rr := admin.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"promoter-trust"}, "id": {itoa(m.ID)}}); rr.Code != 303 {
		t.Fatal("trust")
	}
	form.Del("id")
	if rr := c.do("POST", "/account/promoter/posts/save", form); !strings.Contains(rr.Header().Get("Location"), "Published") {
		t.Fatalf("trusted post: %s", rr.Header().Get("Location"))
	}
	if rr := get(t, a.routes(), "/api/posts"); !strings.Contains(rr.Body.String(), "two places left") {
		t.Fatal("trusted post not public")
	}
	posts = a.queryPosts(`member_id = ?`, m.ID)
	if rr := admin.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"post-unapprove"}, "id": {posts[0].ID}}); !strings.Contains(rr.Header().Get("Location"), "queue") {
		t.Fatal("unapprove")
	}
	if rr := admin.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"post-reject"}, "id": {posts[0].ID}}); !strings.Contains(rr.Header().Get("Location"), "rejected") {
		t.Fatal("reject")
	}
	if body, err := latestMailMaybe(mail, "member-rejected"); err != nil || !strings.Contains(body, "did not publish") {
		t.Fatalf("rejection mail: %v", err)
	}
	// The nav counts the waiting post.
	if rr := c.do("GET", "/account/settings", nil); strings.Contains(rr.Body.String(), "waiting</span>") {
		t.Fatal("rejected post counted as waiting")
	}
}

func multipartFile(t *testing.T, fields map[string]string, name, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	fw, _ := w.CreateFormFile("file", name)
	fw.Write([]byte(content))
	w.Close()
	return &buf, w.FormDataContentType()
}

func (c *client) upload(path string, body *bytes.Buffer, ctype string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, body)
	req.Header.Set("Content-Type", ctype)
	req.RemoteAddr = c.ip + ":1234"
	for k, v := range c.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	rr := httptest.NewRecorder()
	c.h.ServeHTTP(rr, req)
	return rr
}

var tokenRe = regexp.MustCompile(`name="token" value="([^"]+)"`)

func TestPromoterImportCSVAndICS(t *testing.T) {
	a, mail := testApp(t)
	c, csrf, _, _ := applyAndApprove(t, a, mail, "imp@example.org", "Ima", "Helderberg Markets")
	other, ocsrf := register(t, a, mail, "z@example.org", "Za", "another fine passphrase", "10.2.2.5")
	m := a.memberByEmail("imp@example.org")

	csvText := "title,date,time,town,category,summary,website\n" +
		"Night market," + futureDate(12) + ",17:30,Somerset West,markets,Food stalls and live music at the old station yard.,https://example.org/nm\n" +
		"Farm market," + strings.ReplaceAll(futureDate(19), "-", "/") + ",,Gordon's Bay,,Fresh produce and crafts by the harbour.,\n" +
		"Broken row,not-a-date,,strand,markets,No date here so it cannot be added.,\n" +
		"Tiny,,,,,,\n"
	// The date in DD/MM/YYYY form: rebuild from futureDate(19).
	d := futureDate(19)
	csvText = strings.Replace(csvText, strings.ReplaceAll(d, "-", "/"), d[8:]+"/"+d[5:7]+"/"+d[:4], 1)
	body, ctype := multipartFile(t, map[string]string{"csrf": csrf, "town": "strand", "category": "community"}, "season.csv", csvText)
	rr := c.upload("/account/promoter/import", body, ctype)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "2 to add") || !strings.Contains(rr.Body.String(), "2 with problems") {
		t.Fatalf("csv preview: %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Gordon&#39;s Bay") || !strings.Contains(rr.Body.String(), "Markets") {
		t.Fatal("town name and category defaults not applied")
	}
	if a.count(`SELECT COUNT(*) FROM events WHERE member_id = ?`, m.ID) != 0 {
		t.Fatal("preview wrote events")
	}
	tok := tokenRe.FindStringSubmatch(rr.Body.String())
	if tok == nil {
		t.Fatal("no confirm token")
	}
	// Someone else's session cannot use the token.
	if rr := other.do("POST", "/account/promoter/import/confirm", url.Values{"csrf": {ocsrf}, "token": {tok[1]}}); !strings.Contains(rr.Header().Get("Location"), "approved+promoters") {
		t.Fatal("outsider confirmed an import")
	}
	if rr := c.do("POST", "/account/promoter/import/confirm", url.Values{"csrf": {csrf}, "token": {tok[1]}}); !strings.Contains(rr.Header().Get("Location"), "Added+2+events") {
		t.Fatalf("confirm: %s", rr.Header().Get("Location"))
	}
	evs, _ := a.queryEvents(`member_id = ?`, m.ID)
	if len(evs) != 2 || evs[0].Status != "pending_review" || !evs[0].Promoted || evs[0].SubmitterName != "Helderberg Markets" {
		t.Fatalf("imported events: %+v", evs)
	}
	for _, e := range evs {
		if e.Title == "Night market" && (e.Town != "somerset-west" || e.Time != "17:30" || e.Website != "https://example.org/nm") {
			t.Fatalf("csv columns lost: %+v", e)
		}
		if e.Title == "Farm market" && (e.Town != "gordons-bay" || e.Category != "community" || e.Date != d) {
			t.Fatalf("csv defaults wrong: %+v", e)
		}
	}
	if body, err := latestMailMaybe(mail, "admin-event"); err != nil || !strings.Contains(body, "2 imported events") {
		t.Fatalf("admin not told about the import: %v", err)
	}
	// The token is single-use.
	if rr := c.do("POST", "/account/promoter/import/confirm", url.Values{"csrf": {csrf}, "token": {tok[1]}}); !strings.Contains(rr.Header().Get("Location"), "expired") {
		t.Fatalf("token reused: %s", rr.Header().Get("Location"))
	}
	// Uploading the same file again: both rows are "already yours".
	body, ctype = multipartFile(t, map[string]string{"csrf": csrf, "town": "strand", "category": "community"}, "season.csv", csvText)
	if rr := c.upload("/account/promoter/import", body, ctype); !strings.Contains(rr.Body.String(), "2 already yours") || strings.Contains(rr.Body.String(), `name="token"`) {
		t.Fatalf("dedupe: %s", rr.Body.String())
	}

	// ICS: a weekly series comes in once with the pattern in the summary.
	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:a@x\r\nSUMMARY:Parkrun\r\nDTSTART;TZID=Africa/Johannesburg:" + strings.ReplaceAll(futureDate(3), "-", "") + "T080000\r\nDTEND;TZID=Africa/Johannesburg:" + strings.ReplaceAll(futureDate(3), "-", "") + "T090000\r\nRRULE:FREQ=WEEKLY\r\nLOCATION:Strand beachfront\r\nDESCRIPTION:Free timed 5 km.\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nUID:b@x\r\nSUMMARY:Old one\r\nDTSTART;VALUE=DATE:20200101\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	body, ctype = multipartFile(t, map[string]string{"csrf": csrf, "town": "strand", "category": "running"}, "cal.ics", ics)
	rr = c.upload("/account/promoter/import", body, ctype)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "1 to add") || !strings.Contains(rr.Body.String(), "Repeats every week") {
		t.Fatalf("ics preview: %d %s", rr.Code, rr.Body.String())
	}
	tok = tokenRe.FindStringSubmatch(rr.Body.String())
	if rr := c.do("POST", "/account/promoter/import/confirm", url.Values{"csrf": {csrf}, "token": {tok[1]}}); !strings.Contains(rr.Header().Get("Location"), "Added+1+event.") {
		t.Fatalf("ics confirm: %s", rr.Header().Get("Location"))
	}
	evs, _ = a.queryEvents(`member_id = ? AND title = 'Parkrun'`, m.ID)
	if len(evs) != 1 || evs[0].Date != futureDate(3) || evs[0].Time != "08:00" || evs[0].EndTime != "09:00" || evs[0].Category != "running" || !strings.Contains(evs[0].Summary, "Venue: Strand beachfront") || !strings.Contains(evs[0].Summary, "Free timed 5 km") {
		t.Fatalf("ics event: %+v", evs)
	}
	// Junk and oversize.
	body, ctype = multipartFile(t, map[string]string{"csrf": csrf}, "x.txt", "hello")
	if rr := c.upload("/account/promoter/import", body, ctype); !strings.Contains(rr.Header().Get("Location"), "header+row") {
		t.Fatalf("junk file: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	body, ctype = multipartFile(t, map[string]string{"csrf": csrf}, "big.csv", "title,date,summary\n"+strings.Repeat("x", promoterImportBytes+10))
	if rr := c.upload("/account/promoter/import", body, ctype); rr.Code == 200 {
		t.Fatal("oversize upload accepted")
	}
}

func TestPromoterCalendarSource(t *testing.T) {
	a, mail := testApp(t)
	allowLocalFetch = true
	t.Cleanup(func() { allowLocalFetch = false })
	c, csrf, admin, acsrf := applyAndApprove(t, a, mail, "cal@example.org", "Cal", "Somerset Chess Club")
	other, ocsrf := register(t, a, mail, "w@example.org", "Wa", "another fine passphrase", "10.2.2.6")
	m := a.memberByEmail("cal@example.org")

	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:c1@x\r\nSUMMARY:Club night\r\nDTSTART;VALUE=DATE:" + strings.ReplaceAll(futureDate(4), "-", "") + "\r\nDESCRIPTION:Casual games, all levels, boards provided.\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/page" {
			w.Write([]byte("<html>not a calendar</html>"))
			return
		}
		if r.URL.Path != "/cal.ics" {
			w.Write([]byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"))
			return
		}
		w.Header().Set("Content-Type", "text/calendar")
		w.Write([]byte(ics))
	}))
	defer srv.Close()
	host := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)

	if rr := c.do("POST", "/account/promoter/calendar", url.Values{"csrf": {csrf}, "url": {host + "/page"}, "town": {"somerset-west"}, "category": {"games"}}); !strings.Contains(rr.Header().Get("Location"), "does+not+return+a+calendar") {
		t.Fatalf("html accepted as calendar: %s", rr.Header().Get("Location"))
	}
	rr := c.do("POST", "/account/promoter/calendar", url.Values{"csrf": {csrf}, "url": {host + "/cal.ics"}, "label": {"Chess nights"}, "town": {"somerset-west"}, "category": {"games"}})
	if rr.Code != 303 || !strings.Contains(rr.Header().Get("Location"), "Calendar+connected") {
		t.Fatalf("connect: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	var sid, mid int64
	var label string
	if err := a.db.QueryRow(`SELECT id, member_id, label FROM sources WHERE url = ?`, host+"/cal.ics").Scan(&sid, &mid, &label); err != nil || mid != m.ID || label != "Chess nights" {
		t.Fatalf("source row: %v %d %s", err, mid, label)
	}
	// The first check ran and queued the event under the promoter.
	evs, _ := a.queryEvents(`member_id = ?`, m.ID)
	if len(evs) != 1 || evs[0].Title != "Club night" || evs[0].Status != "pending_review" || !evs[0].Promoted || evs[0].SubmitterName != "Somerset Chess Club" || evs[0].Category != "games" {
		t.Fatalf("calendar event: %+v", evs)
	}
	if rr := c.do("GET", "/account/promoter/import", nil); !strings.Contains(rr.Body.String(), "Chess nights") || !strings.Contains(rr.Body.String(), "watching") {
		t.Fatal("calendar not listed")
	}
	if rr := admin.do("GET", "/admin/sources", nil); !strings.Contains(rr.Body.String(), "Somerset Chess Club") {
		t.Fatal("console sources page does not name the promoter")
	}
	// Duplicate URL refused; limit of three; private addresses refused.
	if rr := c.do("POST", "/account/promoter/calendar", url.Values{"csrf": {csrf}, "url": {host + "/cal.ics"}, "town": {"strand"}, "category": {"games"}}); !strings.Contains(rr.Header().Get("Location"), "already+being+watched") {
		t.Fatalf("duplicate: %s", rr.Header().Get("Location"))
	}
	for i := 2; i <= promoterCalendars+1; i++ {
		rr := c.do("POST", "/account/promoter/calendar", url.Values{"csrf": {csrf}, "url": {host + "/cal" + jsonNum(int64(i)) + ".ics"}, "town": {"strand"}, "category": {"games"}})
		if i <= promoterCalendars && !strings.Contains(rr.Header().Get("Location"), "Calendar+connected") {
			t.Fatalf("calendar %d: %s", i, rr.Header().Get("Location"))
		}
		if i > promoterCalendars && !strings.Contains(rr.Header().Get("Location"), "Remove+one+first") {
			t.Fatalf("limit not applied: %s", rr.Header().Get("Location"))
		}
	}
	allowLocalFetch = false
	for _, u := range []string{"http://example.org/x.ics", "https://10.0.0.1/x.ics", "https://localhost/x.ics", "https://example.org:8443/x.ics"} {
		if rr := c.do("POST", "/account/promoter/calendar/remove", url.Values{"csrf": {csrf}, "id": {jsonNum(sid + 1)}}); rr.Code != 303 {
			t.Fatal("remove")
		}
		if rr := c.do("POST", "/account/promoter/calendar", url.Values{"csrf": {csrf}, "url": {u}, "town": {"strand"}, "category": {"games"}}); strings.Contains(rr.Header().Get("Location"), "Calendar+connected") {
			t.Fatalf("%s accepted", u)
		}
	}
	allowLocalFetch = true
	// Check now, remove (seen uids go with it), outsider refused.
	if rr := other.do("POST", "/account/promoter/calendar/remove", url.Values{"csrf": {ocsrf}, "id": {jsonNum(sid)}}); !strings.Contains(rr.Header().Get("Location"), "approved+promoters") {
		t.Fatal("outsider removed a calendar")
	}
	before := hits
	if rr := c.do("POST", "/account/promoter/calendar/check", url.Values{"csrf": {csrf}, "id": {jsonNum(sid)}}); rr.Code != 303 || hits != before+1 {
		t.Fatalf("check now: %d hits %d→%d", rr.Code, before, hits)
	}
	// Trusted promoter: the next new calendar entry publishes at once.
	if rr := admin.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"promoter-trust"}, "id": {itoa(m.ID)}}); rr.Code != 303 {
		t.Fatal("trust")
	}
	ics = strings.Replace(ics, "END:VCALENDAR", "BEGIN:VEVENT\r\nUID:c2@x\r\nSUMMARY:Blitz tournament\r\nDTSTART;VALUE=DATE:"+strings.ReplaceAll(futureDate(11), "-", "")+"\r\nDESCRIPTION:Five-minute games, small entry fee, prizes.\r\nEND:VEVENT\r\nEND:VCALENDAR", 1)
	if rr := c.do("POST", "/account/promoter/calendar/check", url.Values{"csrf": {csrf}, "id": {jsonNum(sid)}}); rr.Code != 303 {
		t.Fatal("check now 2")
	}
	evs, _ = a.queryEvents(`member_id = ? AND title = 'Blitz tournament'`, m.ID)
	if len(evs) != 1 || evs[0].Status != "approved" || evs[0].Origin != "auto" {
		t.Fatalf("trusted calendar event: %+v", evs)
	}
	if rr := c.do("POST", "/account/promoter/calendar/remove", url.Values{"csrf": {csrf}, "id": {jsonNum(sid)}}); !strings.Contains(rr.Header().Get("Location"), "Calendar+removed") {
		t.Fatalf("remove: %s", rr.Header().Get("Location"))
	}
	if a.count(`SELECT COUNT(*) FROM sources WHERE id = ?`, sid) != 0 || a.count(`SELECT COUNT(*) FROM seen_uids WHERE source_id = ?`, sid) != 0 {
		t.Fatal("source or its seen uids left behind")
	}
	if n := a.count(`SELECT COUNT(*) FROM events WHERE member_id = ?`, m.ID); n != 2 {
		t.Fatalf("removing the calendar took the events with it: %d", n)
	}
	// Revoking switches the remaining calendars off.
	if rr := admin.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"promoter-revoke"}, "id": {itoa(m.ID)}}); rr.Code != 303 {
		t.Fatal("revoke")
	}
	if n := a.count(`SELECT COUNT(*) FROM sources WHERE member_id = ? AND enabled = 1`, m.ID); n != 0 {
		t.Fatalf("revoke left %d calendars on", n)
	}
}

func TestPromoterListingAndDecline(t *testing.T) {
	a, mail := testApp(t)
	c, csrf, admin, acsrf := applyAndApprove(t, a, mail, "list@example.org", "Lee", "Strand Bowls")
	m := a.memberByEmail("list@example.org")
	form := url.Values{"csrf": {csrf}, "kind": {"group"}, "name": {"Strand Bowling Club"}, "category": {"sport"}, "town": {"strand"}, "cost": {"membership"}, "schedule": {"Wednesdays and Saturdays from 14:00"}, "summary": {"Lawn bowls for all ages on two greens behind the pavilion; coaching for beginners."}, "website": {"https://example.org/bowls"}, "audience": {"seniors", "beginners"}}
	if rr := c.do("POST", "/account/promoter/listing", form); !strings.Contains(rr.Header().Get("Location"), "listing+is+in+the+queue") {
		t.Fatalf("listing: %s", rr.Header().Get("Location"))
	}
	var mid int64
	var submitter, status string
	if err := a.db.QueryRow(`SELECT member_id, submitter_name, status FROM listing_submissions WHERE name = 'Strand Bowling Club'`).Scan(&mid, &submitter, &status); err != nil || mid != m.ID || submitter != "Strand Bowls" || status != "pending_review" {
		t.Fatalf("listing row: %v %d %s %s", err, mid, submitter, status)
	}
	if body, err := latestMailMaybe(mail, "admin-listing"); err != nil || !strings.Contains(body, "Strand Bowling Club") {
		t.Fatalf("admin not told: %v", err)
	}
	if rr := admin.do("GET", "/admin/queue", nil); !strings.Contains(rr.Body.String(), "Strand Bowling Club") {
		t.Fatal("listing not in the queue")
	}
	// A fresh applicant declined with a note gets the note by mail.
	d, dcsrf := register(t, a, mail, "dec@example.org", "Dee", "a perfectly fine passphrase", "10.2.2.7")
	if rr := d.do("POST", "/account/promoter/apply", url.Values{"csrf": {dcsrf}, "org": {"Crypto Pumps"}, "kind": {"marketer"}, "website": {"https://example.org/c"}, "towns": {"strand"}, "blurb": {"Daily posts about our token launches and trading signals for everyone."}}); rr.Code != 303 {
		t.Fatal("apply")
	}
	dm := a.memberByEmail("dec@example.org")
	if rr := admin.do("POST", "/admin/do", url.Values{"csrf": {acsrf}, "action": {"promoter-decline"}, "id": {itoa(dm.ID)}, "note": {"not a community thing"}}); !strings.Contains(rr.Header().Get("Location"), "declined") {
		t.Fatalf("decline: %s", rr.Header().Get("Location"))
	}
	if body, err := latestMailMaybe(mail, "promoter-declined"); err != nil || !strings.Contains(body, "not a community thing") {
		t.Fatalf("decline mail: %v", err)
	}
	if rr := d.do("GET", "/account/promoter", nil); !strings.Contains(rr.Body.String(), "Not approved") || !strings.Contains(rr.Body.String(), "Send application") {
		t.Fatal("declined member cannot re-apply")
	}
	if rr := admin.do("GET", "/admin/members/view?id="+itoa(dm.ID), nil); !strings.Contains(rr.Body.String(), "Crypto Pumps") || !strings.Contains(rr.Body.String(), "promoter-approve") {
		t.Fatal("member view lacks the promoter panel")
	}
}

func TestMigratePromotersV9ToV10(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "helderberg.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO meta VALUES('schema_version','9')`,
		`CREATE TABLE members (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE, name TEXT NOT NULL, pw_hash TEXT NOT NULL, created_at TEXT NOT NULL, verified_at TEXT, last_login_at TEXT, status TEXT NOT NULL DEFAULT 'active', ip_hash TEXT NOT NULL DEFAULT '')`,
		`INSERT INTO members(id, email, name, pw_hash, created_at, verified_at) VALUES(3,'old@example.org','Old','$argon2id$x','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
		`CREATE TABLE events (id TEXT PRIMARY KEY, title TEXT NOT NULL, date TEXT NOT NULL, end_date TEXT NOT NULL DEFAULT '', time TEXT NOT NULL DEFAULT '', end_time TEXT NOT NULL DEFAULT '', town TEXT NOT NULL, category TEXT NOT NULL, listing TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '', cost TEXT NOT NULL DEFAULT 'varies', website TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, origin TEXT NOT NULL DEFAULT 'user', submitter_name TEXT NOT NULL DEFAULT '', submitter_email TEXT NOT NULL DEFAULT '', ip_hash TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, verified_at TEXT, decided_at TEXT, source_id INTEGER, member_id INTEGER)`,
		`INSERT INTO events(id, title, date, town, category, status, created_at, member_id) VALUES('old-1','Old event','2030-01-01','strand','community','approved','2026-01-01T00:00:00Z',3)`,
		`CREATE TABLE sources (id INTEGER PRIMARY KEY, url TEXT NOT NULL UNIQUE, kind TEXT NOT NULL CHECK (kind IN ('ics','html','list')), label TEXT NOT NULL, listing TEXT NOT NULL DEFAULT '', category TEXT NOT NULL DEFAULT 'community', town TEXT NOT NULL DEFAULT 'somerset-west', match TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1, last_checked_at TEXT, last_hash TEXT NOT NULL DEFAULT '', last_status TEXT NOT NULL DEFAULT '', last_changed_at TEXT)`,
		`CREATE TABLE listing_submissions (id INTEGER PRIMARY KEY, kind TEXT NOT NULL, existing_id TEXT NOT NULL DEFAULT '', name TEXT NOT NULL, category TEXT NOT NULL, town TEXT NOT NULL, schedule TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL, cost TEXT NOT NULL, website TEXT NOT NULL DEFAULT '', audience TEXT NOT NULL DEFAULT '[]', submitter_name TEXT NOT NULL, submitter_email TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL, verified_at TEXT, decided_at TEXT, ip_hash TEXT NOT NULL DEFAULT '')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	db, err = openDB(dir)
	if err != nil {
		t.Fatalf("openDB on a v9 database: %v", err)
	}
	defer db.Close()
	for _, c := range [][2]string{{"members", "role"}, {"members", "trusted"}, {"events", "visible_from"}, {"events", "hidden"}, {"events", "promoted"}, {"sources", "member_id"}, {"listing_submissions", "member_id"}} {
		if !hasColumn(db, c[0], c[1]) {
			t.Fatalf("%s.%s missing after migration", c[0], c[1])
		}
	}
	var role string
	var trusted, n int
	if err := db.QueryRow(`SELECT role, trusted FROM members WHERE id = 3`).Scan(&role, &trusted); err != nil || role != "member" || trusted != 0 {
		t.Fatalf("member defaults: %v %s %d", err, role, trusted)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE status = 'approved' AND `+liveEventsWhere, "2026-01-01").Scan(&n); err != nil || n != 1 {
		t.Fatalf("old event not live after migration: %v %d", err, n)
	}
	for _, tbl := range []string{"promoters", "posts"} {
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n); err != nil {
			t.Fatalf("%s table missing: %v", tbl, err)
		}
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
}
