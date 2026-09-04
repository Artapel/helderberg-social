package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNormPhone(t *testing.T) {
	cases := map[string]string{
		"082 123 4567":     "27821234567",
		"0821234567":       "27821234567",
		"+27 82 123 4567":  "27821234567",
		"27821234567":      "27821234567",
		"0027821234567":    "27821234567",
		"821234567":        "27821234567",
		"+44 7911 123456":  "447911123456",
		"(082) 123-4567":   "27821234567",
		"12345":            "",
		"":                 "",
		"+0123456789":      "",
		"abc":              "",
		"+27 82 123 45678": "278212345678", // 12 digits: accepted, Meta will reject an unknown number
	}
	for in, want := range cases {
		got, ok := normPhone(in)
		if (want == "") == ok || got != want {
			t.Errorf("normPhone(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	if prettyPhone("27821234567") != "+27 82 123 4567" || prettyPhone("447911123456") != "+447911123456" {
		t.Fatal("prettyPhone")
	}
}

func TestWADigestLineIsOneLineAndBounded(t *testing.T) {
	var evs []Event
	for i := 0; i < 12; i++ {
		evs = append(evs, Event{Title: fmt.Sprintf("Event number %d with a fairly long title\nand a newline", i), Date: "2026-09-06", Town: "somerset-west", Cost: "free"})
	}
	line := waDigestLine(evs, 600)
	if strings.ContainsAny(line, "\n\t") || strings.Contains(line, "     ") {
		t.Fatalf("illegal characters for a template parameter: %q", line)
	}
	if len(line) > 600 || !strings.Contains(line, "more") {
		t.Fatalf("not bounded/truncated: %d %q", len(line), line)
	}
	if !strings.HasPrefix(line, "Sun 6 Sep: Event number 0") {
		t.Fatalf("format: %q", line)
	}
	if waDigestLine(evs[:1], 600) != "Sun 6 Sep: Event number 0 with a fairly long title and a newline (Somerset West) · free" {
		t.Fatalf("single: %q", waDigestLine(evs[:1], 600))
	}
}

// fakeGraph stands in for graph.facebook.com: it records every /messages
// call and answers like Meta does.
type fakeGraph struct {
	mu    sync.Mutex
	calls []map[string]any
	srv   *httptest.Server
}

func newFakeGraph(t *testing.T) *fakeGraph {
	f := &fakeGraph{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"message":"bad token","code":190}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/message_templates") {
			_, _ = w.Write([]byte(`{"data":[{"name":"hs_confirm","status":"APPROVED","category":"UTILITY","language":"en"},{"name":"hs_digest","status":"PENDING","category":"MARKETING","language":"en"}]}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		f.mu.Lock()
		f.calls = append(f.calls, m)
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"messaging_product":"whatsapp","messages":[{"id":"wamid.TEST"}]}`))
	}))
	t.Cleanup(f.srv.Close)
	old := waEndpoint
	waEndpoint = f.srv.URL
	t.Cleanup(func() { waEndpoint = old })
	return f
}

func (f *fakeGraph) last() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

func waApp(t *testing.T) (*App, *fakeGraph) {
	t.Helper()
	g := newFakeGraph(t)
	a, _ := testApp(t)
	a.cfg.WAPhoneID, a.cfg.WAWABAID, a.cfg.WAToken, a.cfg.WAAppSecret, a.cfg.WAVerifyToken = "111222333", "999", "test-token", "app-secret-xyz", "verify-token-0123456789"
	a.cfg.WAVersion, a.cfg.WALang, a.cfg.WATemplateConfirm, a.cfg.WATemplateDigest = "v22.0", "en", "hs_confirm", "hs_digest"
	a.cfg.AdminPhone = "27820000000"
	a.wa = newWAClient(a.cfg, a.logf)
	return a, g
}

func signed(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func webhookBody(from, typ, text, payload string) []byte {
	msg := map[string]any{"from": from, "id": "wamid." + from + typ + text + payload + fmt.Sprint(time.Now().UnixNano()), "type": typ}
	if typ == "text" {
		msg["text"] = map[string]string{"body": text}
	} else {
		msg["button"] = map[string]string{"payload": payload, "text": text}
	}
	b, _ := json.Marshal(map[string]any{"object": "whatsapp_business_account", "entry": []any{map[string]any{"changes": []any{map[string]any{"value": map[string]any{
		"messaging_product": "whatsapp", "metadata": map[string]string{"phone_number_id": "111222333"}, "messages": []any{msg}}}}}}})
	return b
}

func TestWhatsAppSubscribeConfirmDigestStop(t *testing.T) {
	a, g := waApp(t)
	h := a.routes()

	// Health advertises the channel to the site.
	rr := get(t, h, "/api/health")
	if !strings.Contains(rr.Body.String(), `"whatsapp":true`) {
		t.Fatalf("health: %s", rr.Body.String())
	}

	// Subscribe by phone: the confirm template goes out.
	rr = post(t, h, "/api/subscribe", map[string]any{"channel": "whatsapp", "phone": "082 123 4567", "frequency": "weekly", "horizon": 14}, "https://helderbergsocial.co.za")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Check WhatsApp") {
		t.Fatalf("subscribe: %d %s", rr.Code, rr.Body.String())
	}
	c := g.last()
	if c == nil || c["to"] != "27821234567" || c["type"] != "template" {
		t.Fatalf("no template sent: %v", c)
	}
	tpl := c["template"].(map[string]any)
	if tpl["name"] != "hs_confirm" {
		t.Fatalf("template: %v", tpl)
	}
	subs, _ := a.subscribers(`phone = ?`, "27821234567")
	if len(subs) != 1 || subs[0].Confirmed || subs[0].Channel != "whatsapp" || subs[0].Email != "" {
		t.Fatalf("row: %+v", subs)
	}

	// Webhook: bad signature is refused.
	body := webhookBody("27821234567", "button", "Confirm", "CONFIRM")
	req := httptest.NewRequest("POST", "/api/wa/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signed("wrong", body))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatalf("bad signature accepted: %d", rr.Code)
	}
	subs, _ = a.subscribers(`phone = ?`, "27821234567")
	if subs[0].Confirmed {
		t.Fatal("confirmed on a forged webhook")
	}

	// Good signature: Confirm tap confirms and a free-text reply goes back.
	req = httptest.NewRequest("POST", "/api/wa/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signed("app-secret-xyz", body))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("webhook: %d", rr.Code)
	}
	subs, _ = a.subscribers(`phone = ?`, "27821234567")
	if !subs[0].Confirmed {
		t.Fatal("not confirmed after Confirm tap")
	}
	if c = g.last(); c["type"] != "text" || !strings.Contains(c["text"].(map[string]any)["body"].(string), "You're subscribed") {
		t.Fatalf("no acknowledgement: %v", c)
	}

	// Replay of the same message id does nothing new.
	n := len(g.calls)
	req = httptest.NewRequest("POST", "/api/wa/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signed("app-secret-xyz", body))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if len(g.calls) != n {
		t.Fatal("replayed webhook acted twice")
	}

	// A digest reaches the WhatsApp subscriber as the digest template with a
	// one-line list, the URL-button token and the STOP quick reply.
	_, err := a.db.Exec(`INSERT INTO events(id, title, date, town, category, status, origin, created_at, cost) VALUES('e1','Parkrun','`+time.Now().In(a.cfg.TZ).AddDate(0, 0, 2).Format("2006-01-02")+`','strand','sport','approved','admin',?, 'free')`, now())
	if err != nil {
		t.Fatal(err)
	}
	sent, err := a.runDigest("weekly", false)
	if err != nil || sent != 1 {
		t.Fatalf("digest: %d %v", sent, err)
	}
	c = g.last()
	tpl = c["template"].(map[string]any)
	if c["to"] != "27821234567" || tpl["name"] != "hs_digest" {
		t.Fatalf("digest template: %v", c)
	}
	comps := tpl["components"].([]any)
	bodyParams := comps[0].(map[string]any)["parameters"].([]any)
	if bodyParams[0].(map[string]any)["text"] != "14" || bodyParams[1].(map[string]any)["text"] == "0" || !strings.Contains(bodyParams[2].(map[string]any)["text"].(string), "Parkrun (Strand) · free") {
		t.Fatalf("body params: %v", bodyParams)
	}
	urlBtn := comps[1].(map[string]any)
	tok := urlBtn["parameters"].([]any)[0].(map[string]any)["text"].(string)
	if urlBtn["sub_type"] != "url" || tok == "" {
		t.Fatalf("url button: %v", urlBtn)
	}
	// The token opens the personalised web view.
	rr = get(t, h, "/api/digest?t="+tok)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Parkrun") || rr.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("digest view: %d %s", rr.Code, rr.Header().Get("X-Robots-Tag"))
	}
	if rr = get(t, h, "/api/digest?t=garbage"); rr.Code != 303 {
		t.Fatalf("bad view token: %d", rr.Code)
	}

	// STOP by text deletes the row and acknowledges.
	body = webhookBody("27821234567", "text", "stop", "")
	req = httptest.NewRequest("POST", "/api/wa/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signed("app-secret-xyz", body))
	h.ServeHTTP(httptest.NewRecorder(), req)
	subs, _ = a.subscribers(`phone = ?`, "27821234567")
	if len(subs) != 0 {
		t.Fatal("STOP did not delete")
	}
	if c = g.last(); !strings.Contains(c["text"].(map[string]any)["body"].(string), "unsubscribed") {
		t.Fatalf("no STOP acknowledgement: %v", c)
	}

	// Template status is read for the System page.
	st, err := a.wa.templateStatus()
	if err != nil || !strings.HasPrefix(st["hs_confirm"], "approved") || !strings.HasPrefix(st["hs_digest"], "pending") {
		t.Fatalf("template status: %v %v", st, err)
	}

	// Verify handshake.
	rr = get(t, h, "/api/wa/webhook?hub.mode=subscribe&hub.verify_token=verify-token-0123456789&hub.challenge=12345")
	if rr.Code != 200 || rr.Body.String() != "12345" {
		t.Fatalf("verify: %d %s", rr.Code, rr.Body.String())
	}
	if rr = get(t, h, "/api/wa/webhook?hub.mode=subscribe&hub.verify_token=nope&hub.challenge=1"); rr.Code != 403 {
		t.Fatal("wrong verify token accepted")
	}
}

func TestWhatsAppOffRejectsChannel(t *testing.T) {
	a, _ := testApp(t)
	h := a.routes()
	rr := post(t, h, "/api/subscribe", map[string]any{"channel": "whatsapp", "phone": "0821234567", "frequency": "weekly", "horizon": 7}, "https://helderbergsocial.co.za")
	if rr.Code != 400 || !strings.Contains(rr.Body.String(), "not available") {
		t.Fatalf("expected refusal: %d %s", rr.Code, rr.Body.String())
	}
	if rr = get(t, h, "/api/wa/webhook?hub.mode=subscribe&hub.verify_token=x&hub.challenge=1"); rr.Code != 403 {
		t.Fatal("webhook verify should fail when off")
	}
}

// A database written by schema v2 (email NOT NULL, no phone) must come up
// with its subscribers intact.
func TestMigrateSubscribersV2ToV3(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "helderberg.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO meta VALUES('schema_version','2')`,
		`CREATE TABLE subscribers (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE, frequency TEXT NOT NULL CHECK (frequency IN ('daily','weekly')), horizon INTEGER NOT NULL CHECK (horizon IN (7,14,30)), towns TEXT NOT NULL DEFAULT '[]', categories TEXT NOT NULL DEFAULT '[]', created_at TEXT NOT NULL, confirmed_at TEXT, last_sent_at TEXT, ip_hash TEXT NOT NULL DEFAULT '')`,
		`INSERT INTO subscribers(id, email, frequency, horizon, towns, created_at, confirmed_at) VALUES(7,'old@example.org','daily',30,'["strand"]','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	db, err = openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var email, channel, towns string
	var phone sql.NullString
	if err := db.QueryRow(`SELECT email, phone, channel, towns FROM subscribers WHERE id = 7`).Scan(&email, &phone, &channel, &towns); err != nil {
		t.Fatal(err)
	}
	if email != "old@example.org" || phone.Valid || channel != "email" || towns != `["strand"]` {
		t.Fatalf("migrated row wrong: %s %v %s %s", email, phone, channel, towns)
	}
	var v string
	_ = db.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&v)
	if v != fmt.Sprint(schemaVersion) {
		t.Fatalf("version %s", v)
	}
	// A phone-only row is now allowed; a row with neither is not.
	if _, err := db.Exec(`INSERT INTO subscribers(phone, channel, frequency, horizon, created_at) VALUES('27821234567','whatsapp','weekly',7,'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscribers(channel, frequency, horizon, created_at) VALUES('email','weekly',7,'x')`); err == nil {
		t.Fatal("row with no address accepted")
	}
	// Re-running the migration on the new shape is a no-op.
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
}
