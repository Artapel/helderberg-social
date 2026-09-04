package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Signed links are the only credential in the system: there are no passwords.
// A token carries a purpose, a subject and an expiry, is HMAC-SHA256 signed
// with the server secret, and single-use purposes are recorded in the database
// once redeemed so a leaked link cannot be replayed.

type tokenPayload struct {
	Purpose string `json:"p"`
	Subject string `json:"s"`
	Expires int64  `json:"e"`
	ID      string `json:"j"`
}

var errBadToken = errors.New("invalid or expired link")

func randomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (a *App) sign(purpose, subject string, ttl time.Duration) string {
	p := tokenPayload{Purpose: purpose, Subject: subject, Expires: time.Now().Add(ttl).Unix(), ID: randomID(8)}
	body, _ := json.Marshal(p)
	enc := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, a.cfg.Secret)
	mac.Write([]byte(enc))
	return enc + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verify checks signature, expiry and purpose. It does not consume the token.
func (a *App) verify(token, purpose string) (*tokenPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(token) > 2048 {
		return nil, errBadToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errBadToken
	}
	mac := hmac.New(sha256.New, a.cfg.Secret)
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, errBadToken
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errBadToken
	}
	var p tokenPayload
	if err := json.Unmarshal(body, &p); err != nil || p.Purpose != purpose || p.ID == "" {
		return nil, errBadToken
	}
	if time.Now().Unix() > p.Expires {
		return nil, errBadToken
	}
	return &p, nil
}

// consume verifies and then burns the token id. Safe against concurrent
// redemption because the insert is a primary-key write.
func (a *App) consume(token, purpose string) (*tokenPayload, error) {
	p, err := a.verify(token, purpose)
	if err != nil {
		return nil, err
	}
	res, err := a.db.Exec(`INSERT OR IGNORE INTO tokens_used(jti, used_at) VALUES(?, ?)`, p.ID, now())
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errBadToken
	}
	return p, nil
}

// emailHash lets rate limits and audit rows refer to an address without
// storing it in plain text where it is not needed.
func emailHash(s string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(s))))
	return hex.EncodeToString(sum[:8])
}
