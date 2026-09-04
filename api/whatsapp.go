package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// WhatsApp delivery through Meta's WhatsApp Business Platform (Cloud API).
//
// Everything we send unprompted must be a pre-approved message template
// (Meta's rule for business-initiated conversations), so there are exactly
// two: one that asks a new subscriber to confirm, one that carries the
// digest. Free-form text is only ever sent inside the 24-hour window after
// the person messaged us (their tap on Confirm or their STOP), which is what
// the short acknowledgements use. Template texts and the Meta-side setup are
// in docs/whatsapp.md; the code here assumes those names and parameters.
//
// Inbound (webhook): a signed POST from Meta carrying button taps, texts and
// delivery statuses. The signature (HMAC-SHA256 with the app secret over the
// raw body) is the only thing that makes a webhook trustworthy, so it is
// checked before the body is even parsed.

type waClient struct {
	phoneID, wabaID, token, appSecret, verifyToken string
	version, lang, tConfirm, tDigest               string
	http                                           *http.Client
	logf                                           func(string, ...any)
}

// waEndpoint is a var so tests can point the client at a local server.
var waEndpoint = "https://graph.facebook.com"

func newWAClient(cfg *Config, logf func(string, ...any)) *waClient {
	if cfg.WAPhoneID == "" {
		return nil
	}
	return &waClient{phoneID: cfg.WAPhoneID, wabaID: cfg.WAWABAID, token: cfg.WAToken, appSecret: cfg.WAAppSecret, verifyToken: cfg.WAVerifyToken,
		version: cfg.WAVersion, lang: cfg.WALang, tConfirm: cfg.WATemplateConfirm, tDigest: cfg.WATemplateDigest,
		http: &http.Client{Timeout: 25 * time.Second}, logf: logf}
}

func (a *App) waEnabled() bool { return a.wa != nil }

/* ---------- phone numbers ---------- */

var phoneDigits = regexp.MustCompile(`[^0-9+]`)

// normPhone turns what a person types into the digits-only E.164 form the
// API wants ("27821234567"). South African national numbers (0xx...) are
// assumed when there is no country code, since that is who the site is for.
// Anything that is not 8-15 digits after cleaning is rejected.
func normPhone(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	intl := strings.HasPrefix(s, "+") || strings.HasPrefix(s, "00")
	d := phoneDigits.ReplaceAllString(s, "")
	d = strings.TrimPrefix(d, "+")
	if strings.HasPrefix(s, "00") {
		d = strings.TrimPrefix(d, "00")
	}
	if !intl {
		if strings.HasPrefix(d, "0") && len(d) == 10 {
			d = "27" + d[1:]
		} else if strings.HasPrefix(d, "27") && len(d) == 11 {
			// already national with country code, typed without the plus
		} else if len(d) == 9 {
			d = "27" + d
		}
	}
	if len(d) < 8 || len(d) > 15 || strings.HasPrefix(d, "0") {
		return "", false
	}
	return d, true
}

// prettyPhone is how a stored number is shown to people: +27 82 123 4567.
func prettyPhone(d string) string {
	if strings.HasPrefix(d, "27") && len(d) == 11 {
		return "+27 " + d[2:4] + " " + d[4:7] + " " + d[7:]
	}
	return "+" + d
}

/* ---------- sending ---------- */

type waParam struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Payload string `json:"payload,omitempty"`
}

type waComponent struct {
	Type       string    `json:"type"`
	SubType    string    `json:"sub_type,omitempty"`
	Index      string    `json:"index,omitempty"`
	Parameters []waParam `json:"parameters,omitempty"`
}

// waParamText makes a value legal as a template parameter: Meta rejects
// newlines, tabs and runs of more than four spaces, and caps the body.
func waParamText(s string, max int) string {
	s = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(s)
	for strings.Contains(s, "     ") {
		s = strings.ReplaceAll(s, "     ", "    ")
	}
	s = strings.TrimSpace(s)
	if len(s) > max {
		cut := s[:max]
		if i := strings.LastIndex(cut, " "); i > max/2 {
			cut = cut[:i]
		}
		s = strings.TrimRight(cut, " ·,;") + "…"
	}
	return s
}

func (w *waClient) call(method, path string, body any) (map[string]any, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, waEndpoint+"/"+w.version+"/"+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := w.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("whatsapp: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if e, ok := out["error"].(map[string]any); ok {
		code, _ := e["code"].(float64)
		msg, _ := e["message"].(string)
		if d, ok := e["error_data"].(map[string]any); ok {
			if det, ok := d["details"].(string); ok && det != "" {
				msg += ": " + det
			}
		}
		return out, fmt.Errorf("whatsapp %d: %s", int(code), clean(msg, 200))
	}
	if res.StatusCode >= 300 {
		return out, fmt.Errorf("whatsapp: HTTP %d", res.StatusCode)
	}
	return out, nil
}

// sendTemplate sends one approved template. It returns Meta's message id,
// which later shows up in delivery statuses on the webhook.
func (w *waClient) sendTemplate(to, name string, comps []waComponent) (string, error) {
	body := map[string]any{
		"messaging_product": "whatsapp", "to": to, "type": "template",
		"template": map[string]any{"name": name, "language": map[string]string{"code": w.lang}, "components": comps},
	}
	out, err := w.call("POST", w.phoneID+"/messages", body)
	if err != nil {
		return "", err
	}
	return waMessageID(out), nil
}

// sendText is a free-form reply, valid only within 24h of the person's last
// message to us.
func (w *waClient) sendText(to, text string) error {
	_, err := w.call("POST", w.phoneID+"/messages", map[string]any{
		"messaging_product": "whatsapp", "to": to, "type": "text",
		"text": map[string]any{"body": text, "preview_url": false},
	})
	return err
}

func waMessageID(out map[string]any) string {
	if ms, ok := out["messages"].([]any); ok && len(ms) > 0 {
		if m, ok := ms[0].(map[string]any); ok {
			id, _ := m["id"].(string)
			return id
		}
	}
	return ""
}

// templateStatus asks Meta whether our two templates exist and are approved.
// Needs the WABA id; without it the System page just says "not checked".
func (w *waClient) templateStatus() (map[string]string, error) {
	if w.wabaID == "" {
		return nil, errors.New("HS_WA_WABA_ID not set")
	}
	out, err := w.call("GET", w.wabaID+"/message_templates?fields=name,status,category,language&limit=100", nil)
	if err != nil {
		return nil, err
	}
	st := map[string]string{}
	if data, ok := out["data"].([]any); ok {
		for _, d := range data {
			m, _ := d.(map[string]any)
			name, _ := m["name"].(string)
			status, _ := m["status"].(string)
			cat, _ := m["category"].(string)
			lang, _ := m["language"].(string)
			if lang == w.lang || st[name] == "" {
				st[name] = strings.ToLower(status) + " (" + strings.ToLower(cat) + ", " + lang + ")"
			}
		}
	}
	return st, nil
}

/* ---------- what the app sends ---------- */

// waSend wraps a template send with the same per-recipient budget and log
// as email, so a phone number cannot be flooded either.
func (a *App) waSend(to, kind, tmpl string, comps []waComponent) error {
	h := emailHash("tel:" + to)
	if !a.mailBudgetOK(h) {
		err := fmt.Errorf("message budget exceeded for recipient")
		a.logMail(h, kind, err)
		return err
	}
	_, err := a.wa.sendTemplate(to, tmpl, comps)
	a.logMail(h, kind, err)
	if err != nil {
		a.logf("whatsapp %s failed: %v", kind, err)
	}
	return err
}

// waConfirm asks a new subscriber to tap Confirm. Template hs_confirm:
// body {{1}} = "daily"/"weekly", {{2}} = horizon days; quick replies
// Confirm (payload CONFIRM) and Not me (payload STOP).
func (a *App) waConfirm(to, freq string, horizon int) error {
	comps := []waComponent{
		{Type: "body", Parameters: []waParam{{Type: "text", Text: freq}, {Type: "text", Text: fmt.Sprint(horizon)}}},
		{Type: "button", SubType: "quick_reply", Index: "0", Parameters: []waParam{{Type: "payload", Payload: "CONFIRM"}}},
		{Type: "button", SubType: "quick_reply", Index: "1", Parameters: []waParam{{Type: "payload", Payload: "STOP"}}},
	}
	return a.waSend(to, "wa-confirm", a.wa.tConfirm, comps)
}

// waDigest sends the digest. Template hs_digest: body {{1}} = horizon days,
// {{2}} = number of events, {{3}} = one-line list; button 0 = URL with the
// signed token as suffix (the full personalised list on the web), button 1 =
// quick reply Unsubscribe (payload STOP).
func (a *App) waDigest(s Subscriber, evs []Event, freq string) error {
	view := a.sign("view", fmt.Sprint(s.ID), 30*24*time.Hour)
	comps := []waComponent{
		{Type: "body", Parameters: []waParam{
			{Type: "text", Text: fmt.Sprint(s.Horizon)},
			{Type: "text", Text: fmt.Sprint(len(evs))},
			{Type: "text", Text: waDigestLine(evs, 600)},
		}},
		{Type: "button", SubType: "url", Index: "0", Parameters: []waParam{{Type: "text", Text: view}}},
		{Type: "button", SubType: "quick_reply", Index: "1", Parameters: []waParam{{Type: "payload", Payload: "STOP"}}},
	}
	return a.waSend(s.Phone, "wa-digest-"+freq, a.wa.tDigest, comps)
}

// waDigestLine is the digest squeezed onto one line, since template
// parameters cannot hold line breaks: "Sat 6 Sep: Title (Town) · Sun 7 Sep:
// Title (Town) · +3 more".
func waDigestLine(evs []Event, max int) string {
	var parts []string
	for _, e := range evs {
		p := fmtDate(e.Date) + ": " + e.Title + " (" + e.TownName() + ")"
		if e.Cost == "free" {
			p += " · free"
		}
		parts = append(parts, waParamText(p, 120))
	}
	line := ""
	for i, p := range parts {
		next := line
		if next != "" {
			next += " · "
		}
		next += p
		if len(next) > max-12 && i < len(parts)-1 {
			line += fmt.Sprintf(" · +%d more", len(parts)-i)
			break
		}
		line = next
	}
	return waParamText(line, max)
}

/* ---------- webhook ---------- */

// waWebhookVerify answers Meta's one-time subscription handshake.
func (a *App) waWebhookVerify(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if a.wa == nil || q.Get("hub.mode") != "subscribe" || q.Get("hub.verify_token") == "" || !hmac.Equal([]byte(q.Get("hub.verify_token")), []byte(a.wa.verifyToken)) {
		http.Error(w, "forbidden", 403)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(q.Get("hub.challenge")))
}

type waEvent struct {
	Entry []struct {
		Changes []struct {
			Value struct {
				Metadata struct {
					PhoneNumberID string `json:"phone_number_id"`
				} `json:"metadata"`
				Messages []struct {
					From      string `json:"from"`
					ID        string `json:"id"`
					Type      string `json:"type"`
					Timestamp string `json:"timestamp"`
					Text      struct {
						Body string `json:"body"`
					} `json:"text"`
					Button struct {
						Payload string `json:"payload"`
						Text    string `json:"text"`
					} `json:"button"`
					Interactive struct {
						ButtonReply struct {
							ID    string `json:"id"`
							Title string `json:"title"`
						} `json:"button_reply"`
					} `json:"interactive"`
				} `json:"messages"`
				Statuses []struct {
					ID          string `json:"id"`
					Status      string `json:"status"`
					RecipientID string `json:"recipient_id"`
					Errors      []struct {
						Code  int    `json:"code"`
						Title string `json:"title"`
					} `json:"errors"`
				} `json:"statuses"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

var waStopWords = regexp.MustCompile(`(?i)^\s*(stop|unsubscribe|cancel|end|quit|not me|nie)\s*[.!]?\s*$`)
var waYesWords = regexp.MustCompile(`(?i)^\s*(confirm|yes|ja|ok|okay|start|subscribe)\s*[.!]?\s*$`)

// waWebhook receives messages and statuses. It always answers 200 quickly
// (Meta retries and eventually disables the webhook otherwise); the work is
// small enough to do inline.
func (a *App) waWebhook(w http.ResponseWriter, r *http.Request) {
	if a.wa == nil {
		http.Error(w, "not configured", 404)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 512<<10))
	if err != nil || !a.wa.validSignature(r.Header.Get("X-Hub-Signature-256"), raw) {
		a.logf("whatsapp webhook: bad signature from %s", ipTag(ipOf(r)))
		http.Error(w, "forbidden", 403)
		return
	}
	var ev waEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	for _, e := range ev.Entry {
		for _, c := range e.Changes {
			v := c.Value
			if v.Metadata.PhoneNumberID != "" && v.Metadata.PhoneNumberID != a.wa.phoneID {
				continue // another number on the same app
			}
			for _, m := range v.Messages {
				if !a.waFirstSeen(m.ID) {
					continue
				}
				a.waInbound(m.From, m.Type, m.Text.Body, firstNonEmpty(m.Button.Payload, m.Interactive.ButtonReply.ID, m.Button.Text, m.Interactive.ButtonReply.Title))
			}
			for _, s := range v.Statuses {
				if s.Status == "failed" || s.Status == "undeliverable" {
					msg := s.Status
					for _, e := range s.Errors {
						msg += fmt.Sprintf(" %d %s", e.Code, e.Title)
					}
					a.logMail(emailHash("tel:"+s.RecipientID), "wa-status", errors.New(clean(msg, 200)))
					a.logf("whatsapp delivery to %s: %s", emailHash("tel:" + s.RecipientID)[:8], msg)
				}
			}
		}
	}
	w.WriteHeader(200)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func (w *waClient) validSignature(header string, body []byte) bool {
	sig, ok := strings.CutPrefix(header, "sha256=")
	if !ok || w.appSecret == "" {
		return false
	}
	want, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(w.appSecret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

// waFirstSeen dedupes Meta's redeliveries.
func (a *App) waFirstSeen(id string) bool {
	if id == "" {
		return true
	}
	res, err := a.db.Exec(`INSERT OR IGNORE INTO wa_seen(id, seen_at) VALUES(?,?)`, id, now())
	if err != nil {
		return true
	}
	n, _ := res.RowsAffected()
	return n == 1
}

// waInbound acts on what a person sent: a Confirm tap (or "yes") confirms
// their pending subscription, STOP (or the Not me / Unsubscribe buttons)
// deletes it. The sender's number is the only identity used; nothing in the
// payload is trusted to name a subscriber.
func (a *App) waInbound(from, typ, text, button string) {
	from, ok := normPhone("+" + from)
	if !ok {
		return
	}
	word := button
	if typ == "text" {
		word = text
	}
	switch {
	case strings.EqualFold(strings.TrimSpace(word), "STOP") || waStopWords.MatchString(word):
		res, _ := a.db.Exec(`DELETE FROM subscribers WHERE channel='whatsapp' AND phone = ?`, from)
		if n, _ := res.RowsAffected(); n > 0 {
			a.audit(nil, "wa-unsubscribe", "phone "+emailHash("tel:" + from)[:8], "STOP received on WhatsApp")
			_ = a.wa.sendText(from, "You're unsubscribed. Your number has been removed from Helderberg Social's records. Subscribe again any time at "+a.cfg.SiteURL+"/subscribe.html")
		} else {
			_ = a.wa.sendText(from, "This number is not subscribed to Helderberg Social updates, so there is nothing to stop.")
		}
	case strings.EqualFold(strings.TrimSpace(word), "CONFIRM") || waYesWords.MatchString(word):
		res, _ := a.db.Exec(`UPDATE subscribers SET confirmed_at = COALESCE(confirmed_at, ?) WHERE channel='whatsapp' AND phone = ?`, now(), from)
		if n, _ := res.RowsAffected(); n > 0 {
			var freq string
			var horizon int
			_ = a.db.QueryRow(`SELECT frequency, horizon FROM subscribers WHERE channel='whatsapp' AND phone = ?`, from).Scan(&freq, &horizon)
			when := "every morning"
			if freq == "weekly" {
				when = "on Thursday mornings"
			}
			_ = a.wa.sendText(from, fmt.Sprintf("You're subscribed. What's on in the Helderberg for the next %d days arrives here %s. Reply STOP at any time to unsubscribe.", horizon, when))
		} else {
			_ = a.wa.sendText(from, "This number has no pending subscription. Sign up at "+a.cfg.SiteURL+"/subscribe.html and we'll send a Confirm button.")
		}
	default:
		// Anything else: one polite pointer, no conversation. It is inside the
		// 24h window (they just wrote to us) so a plain text is allowed.
		_ = a.wa.sendText(from, "This is Helderberg Social's automated what's-on service. Reply STOP to unsubscribe, or visit "+a.cfg.SiteURL+" to change what you receive. We don't read replies here.")
	}
}

/* ---------- the personalised list on the web ---------- */

// digestView is GET /api/digest?t=…: the same list the digest carries,
// rendered as a page, for the WhatsApp "See all events" button.
func (a *App) digestView(w http.ResponseWriter, r *http.Request) {
	p, err := a.verify(r.URL.Query().Get("t"), "view")
	if err != nil {
		a.redirectSite(w, r, "invalid")
		return
	}
	subs, err := a.subscribers(`id = ? AND confirmed_at IS NOT NULL`, p.Subject)
	if err != nil || len(subs) != 1 {
		a.redirectSite(w, r, "invalid")
		return
	}
	s := subs[0]
	today := time.Now().In(a.cfg.TZ)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, a.cfg.TZ)
	all, err := a.approvedEvents(today, 30)
	if err != nil {
		a.fail(w, 500, "internal error")
		return
	}
	evs := filterEvents(all, today, s)
	unsub := a.cfg.APIURL + "/api/unsubscribe?t=" + a.sign("unsub", fmt.Sprint(s.ID), 365*24*time.Hour)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = io.WriteString(w, a.htmlDigest(evs, s, unsub))
}
