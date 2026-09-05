package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func rotaReq(t *testing.T, a *App, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	req.RemoteAddr = "10.0.0.1:1234"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	a.routes().ServeHTTP(rr, req)
	return rr
}

func TestRotaDoorsNeedTheToken(t *testing.T) {
	a, _ := testApp(t)
	// No token configured: the doors do not exist.
	if rr := rotaReq(t, a, "GET", "/api/fb/rota", "", nil); rr.Code != 404 {
		t.Fatalf("no token configured: %d", rr.Code)
	}
	a.cfg.RotaToken = "0123456789abcdef0123456789abcdef"
	if rr := rotaReq(t, a, "GET", "/api/fb/rota", "", nil); rr.Code != 404 {
		t.Fatalf("missing bearer: %d", rr.Code)
	}
	if rr := rotaReq(t, a, "GET", "/api/fb/rota", "0123456789abcdef0123456789abcdeX", nil); rr.Code != 404 {
		t.Fatalf("wrong bearer: %d", rr.Code)
	}
	if rr := rotaReq(t, a, "POST", "/api/fb/rota/result", "", map[string]any{"id": 1, "outcome": "posted"}); rr.Code != 404 {
		t.Fatalf("result without bearer: %d", rr.Code)
	}
	// A short token is refused at start-up.
	t.Setenv("HS_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("HS_ADMIN_EMAIL", "admin@example.org")
	t.Setenv("HS_FB_ROTA_TOKEN", "short")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "HS_FB_ROTA_TOKEN") {
		t.Fatalf("short token accepted: %v", err)
	}
	t.Setenv("HS_FB_ROTA_TOKEN", "0123456789abcdef0123456789abcdef")
	if c, err := loadConfig(); err != nil || c.RotaToken != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("token not loaded: %v", err)
	}
}

func TestRotaHandsOutDueGroupsAndRecordsResults(t *testing.T) {
	a, _ := testApp(t)
	a.cfg.RotaToken = "0123456789abcdef0123456789abcdef"
	tok := a.cfg.RotaToken
	_ = a.metaSet("set:fb_groups_per_day", "2")
	today := a.localDay(time.Now())

	var got struct {
		OK       bool        `json:"ok"`
		Today    string      `json:"today"`
		PerDay   int         `json:"per_day"`
		DueTotal int         `json:"due_total"`
		Groups   []rotaGroup `json:"groups"`
	}
	rr := rotaReq(t, a, "GET", "/api/fb/rota", tok, nil)
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &got) != nil || !got.OK {
		t.Fatalf("rota: %d %s", rr.Code, rr.Body.String())
	}
	if got.Today != today || got.PerDay != 2 || len(got.Groups) != 2 || got.DueTotal < 10 {
		t.Fatalf("rota shape: today=%s per_day=%d n=%d total=%d", got.Today, got.PerDay, len(got.Groups), got.DueTotal)
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("rota must not be cached")
	}
	g := got.Groups[0]
	if g.ID == 0 || g.FBID == "" || !strings.HasPrefix(g.URL, "https://www.facebook.com/groups/") || !strings.Contains(g.Text, "https://helderbergsocial.co.za") || !strings.Contains(g.Text, "Follow the Facebook page") || !strings.Contains(g.Text, "Join the community") {
		t.Fatalf("group shape: %+v", g)
	}
	// "all" lifts the day's cap.
	rr = rotaReq(t, a, "GET", "/api/fb/rota?limit=all", tok, nil)
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Groups) != got.DueTotal {
		t.Fatalf("limit=all gave %d of %d", len(got.Groups), got.DueTotal)
	}
	rr = rotaReq(t, a, "GET", "/api/fb/rota?limit=5", tok, nil)
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Groups) != 5 {
		t.Fatalf("limit=5 gave %d", len(got.Groups))
	}
	g1, g2, g3, g4 := got.Groups[0], got.Groups[1], got.Groups[2], got.Groups[3]

	// posted: counts, books the next date a cadence away, audited.
	var res struct {
		OK      bool   `json:"ok"`
		Posts   int    `json:"posts"`
		NextDue string `json:"next_due"`
		Enabled bool   `json:"enabled"`
	}
	rr = rotaReq(t, a, "POST", "/api/fb/rota/result", tok, map[string]any{"id": g1.ID, "outcome": "posted", "note": "pending admin approval"})
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &res) != nil || res.Posts != 1 || res.NextDue <= today || !res.Enabled {
		t.Fatalf("posted: %d %s", rr.Code, rr.Body.String())
	}
	// retry: not counted, due again in a few days.
	rr = rotaReq(t, a, "POST", "/api/fb/rota/result", tok, map[string]any{"id": g2.ID, "outcome": "retry", "note": "participation pending"})
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	want := time.Now().In(a.cfg.TZ).AddDate(0, 0, rotaRetryDays).Format("2006-01-02")
	if rr.Code != 200 || res.Posts != 0 || res.NextDue != want || !res.Enabled {
		t.Fatalf("retry: %d %s", rr.Code, rr.Body.String())
	}
	// blocked: switched off with the reason.
	rr = rotaReq(t, a, "POST", "/api/fb/rota/result", tok, map[string]any{"id": g3.ID, "outcome": "blocked", "note": "not a member"})
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if rr.Code != 200 || res.Enabled {
		t.Fatalf("blocked: %d %s", rr.Code, rr.Body.String())
	}
	if off := a.groups(`id = ?`, g3.ID); len(off) != 1 || off[0].SkipReason != "not a member" {
		t.Fatalf("blocked group not recorded: %+v", off)
	}
	// failed: moves one day so it cannot hog the batch.
	rr = rotaReq(t, a, "POST", "/api/fb/rota/result", tok, map[string]any{"id": g4.ID, "outcome": "failed", "note": "composer timeout"})
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if rr.Code != 200 || res.NextDue != time.Now().In(a.cfg.TZ).AddDate(0, 0, 1).Format("2006-01-02") {
		t.Fatalf("failed: %d %s", rr.Code, rr.Body.String())
	}
	// None of the four is in today's batch any more.
	rr = rotaReq(t, a, "GET", "/api/fb/rota?limit=all", tok, nil)
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	for _, g := range got.Groups {
		if g.ID == g1.ID || g.ID == g2.ID || g.ID == g3.ID || g.ID == g4.ID {
			t.Fatalf("group %d still due after its result", g.ID)
		}
	}
	// Bad input.
	if rr = rotaReq(t, a, "POST", "/api/fb/rota/result", tok, map[string]any{"id": g1.ID, "outcome": "maybe"}); rr.Code != 400 {
		t.Fatalf("bad outcome: %d", rr.Code)
	}
	if rr = rotaReq(t, a, "POST", "/api/fb/rota/result", tok, map[string]any{"id": 999999, "outcome": "posted"}); rr.Code != 404 {
		t.Fatalf("unknown group: %d", rr.Code)
	}
	// The audit trail names the runner's outcomes.
	kinds := map[string]bool{}
	for _, r := range a.auditRows(`action LIKE 'fb.group_%'`, 20) {
		kinds[r.Action] = true
	}
	for _, k := range []string{"fb.group_posted", "fb.group_retry", "fb.group_blocked", "fb.group_failed"} {
		if !kinds[k] {
			t.Fatalf("audit missing %s: %v", k, kinds)
		}
	}
	// Health says the runner doors are open.
	if body := get(t, a.routes(), "/api/health").Body.String(); !strings.Contains(body, `"fb_rota":true`) {
		t.Fatalf("health: %s", body)
	}
}
