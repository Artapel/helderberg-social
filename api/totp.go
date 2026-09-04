package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Second factor for the admin: RFC 6238 time-based one-time passwords, the
// scheme Google Authenticator (and every other authenticator app) speaks.
// The shared secret is stored encrypted with a key derived from HS_SECRET,
// so a copy of the database alone is not enough to mint codes.

const (
	totpDigits = 6
	totpStep   = 30
	totpSkew   = 1 // accept the previous and next 30 s window (clock drift)
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func newTOTPSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b32.EncodeToString(b)
}

// totpCode computes the code for a given 30 s counter.
func totpCode(secret string, counter int64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1000000), nil
}

// totpMatch returns the counter that produced the code, or -1. The caller
// records the counter so the same code cannot be replayed inside its window.
func totpMatch(secret, code string, at time.Time) int64 {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return -1
	}
	base := at.Unix() / totpStep
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		want, err := totpCode(secret, base+d)
		if err != nil {
			return -1
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return base + d
		}
	}
	return -1
}

func totpURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {fmt.Sprint(totpDigits)}, "period": {fmt.Sprint(totpStep)}}
	return "otpauth://totp/" + label + "?" + q.Encode()
}

/* ---------- encryption at rest ---------- */

func (a *App) atRestKey(purpose string) []byte {
	h := hmac.New(sha256.New, a.cfg.Secret)
	h.Write([]byte("at-rest:" + purpose))
	return h.Sum(nil)
}

func (a *App) seal(purpose string, plain []byte) (string, error) {
	block, err := aes.NewCipher(a.atRestKey(purpose))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return hex.EncodeToString(append(nonce, gcm.Seal(nil, nonce, plain, []byte(purpose))...)), nil
}

func (a *App) open(purpose, sealed string) ([]byte, error) {
	raw, err := hex.DecodeString(sealed)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(a.atRestKey(purpose))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("sealed value too short")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], []byte(purpose))
}

/* ---------- enrolment state in the meta table ---------- */

func (a *App) totpEnrolled() bool { return a.metaGet("totp:secret") != "" }

func (a *App) totpSecret() (string, error) {
	sealed := a.metaGet("totp:secret")
	if sealed == "" {
		return "", errors.New("authenticator not enrolled")
	}
	b, err := a.open("totp", sealed)
	return string(b), err
}

// totpEnrol stores the secret and returns fresh backup codes (shown once).
func (a *App) totpEnrol(secret string) ([]string, error) {
	sealed, err := a.seal("totp", []byte(secret))
	if err != nil {
		return nil, err
	}
	if err := a.metaSet("totp:secret", sealed); err != nil {
		return nil, err
	}
	_ = a.metaSet("totp:enrolled_at", now())
	_ = a.metaSet("totp:last", "0")
	return a.newBackupCodes()
}

func (a *App) totpReset() {
	for _, k := range []string{"totp:secret", "totp:enrolled_at", "totp:last", "totp:backup", "totp:pending"} {
		_, _ = a.db.Exec(`DELETE FROM meta WHERE key = ?`, k)
	}
}

// newBackupCodes replaces the recovery codes. Only hashes are stored.
func (a *App) newBackupCodes() ([]string, error) {
	codes := make([]string, 10)
	hashes := make([]string, 10)
	for i := range codes {
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		raw := strings.ToLower(hex.EncodeToString(b))
		codes[i] = raw[:5] + "-" + raw[5:]
		hashes[i] = backupHash(codes[i])
	}
	j, _ := json.Marshal(hashes)
	return codes, a.metaSet("totp:backup", string(j))
}

func backupHash(code string) string {
	code = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	sum := sha256.Sum256([]byte("backup:" + code))
	return hex.EncodeToString(sum[:])
}

func (a *App) backupCodesLeft() int {
	var hashes []string
	_ = json.Unmarshal([]byte(a.metaGet("totp:backup")), &hashes)
	return len(hashes)
}

// checkSecondFactor accepts a current TOTP code or an unused backup code.
// It returns what was used so the audit log can say so.
func (a *App) checkSecondFactor(code string) (string, bool) {
	code = strings.TrimSpace(code)
	secret, err := a.totpSecret()
	if err != nil {
		return "", false
	}
	if c := totpMatch(secret, code, time.Now()); c >= 0 {
		var last int64
		fmt.Sscan(a.metaGet("totp:last"), &last)
		if c <= last {
			return "", false // that window was already used: replay
		}
		_ = a.metaSet("totp:last", fmt.Sprint(c))
		return "totp", true
	}
	// Backup code: constant-time scan, then remove the one that matched.
	var hashes []string
	_ = json.Unmarshal([]byte(a.metaGet("totp:backup")), &hashes)
	want := backupHash(code)
	idx := -1
	for i, h := range hashes {
		if subtle.ConstantTimeCompare([]byte(h), []byte(want)) == 1 {
			idx = i
		}
	}
	if idx < 0 {
		return "", false
	}
	hashes = append(hashes[:idx], hashes[idx+1:]...)
	j, _ := json.Marshal(hashes)
	_ = a.metaSet("totp:backup", string(j))
	return "backup", true
}
