package main

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// RFC 6376 §3.4.5 example: relaxed canonicalisation of headers and body.
func TestDKIMRelaxedCanonRFCExample(t *testing.T) {
	raw := []byte(" A: X\r\nB : Y\t\r\n\tZ  \r\n\r\n C \r\nD \t E\r\n\r\n\r\n")
	headers, body := splitMessage(raw)
	if len(headers) != 2 {
		t.Fatalf("headers: %q", headers)
	}
	if got := canonHeaderRelaxed(headers[0]) + canonHeaderRelaxed(headers[1]); got != "a:X\r\nb:Y Z\r\n" {
		t.Fatalf("header canon: %q", got)
	}
	if got := canonBodyRelaxed(body); got != " C\r\nD E\r\n" {
		t.Fatalf("body canon: %q", got)
	}
	if canonBodyRelaxed("") != "" || canonBodyRelaxed("\r\n\r\n") != "" {
		t.Fatal("empty body must canonicalise to empty")
	}
}

// Sign a real built message, then verify it the way a receiver would: parse
// the DKIM-Signature tags, recompute bh over the body, rebuild the signed
// header block from h=, and check the RSA signature with the public key.
func TestDKIMSignAndVerify(t *testing.T) {
	dir := t.TempDir()
	s, created, err := loadOrCreateDKIM(dir, "helderbergsocial.co.za", "hs1")
	if err != nil || !created {
		t.Fatalf("keygen: %v created=%v", err, created)
	}
	if _, created, err = loadOrCreateDKIM(dir, "helderbergsocial.co.za", "hs1"); err != nil || created {
		t.Fatalf("second load must reuse the key: %v created=%v", err, created)
	}
	if st, _ := os.Stat(filepath.Join(dir, "dkim-hs1.key")); st == nil || (runtime.GOOS != "windows" && st.Mode().Perm() != 0o600) {
		t.Fatal("key file missing or not 0600")
	}
	if !strings.HasPrefix(s.recordValue(), "v=DKIM1; k=rsa; p=") || s.recordName() != "hs1._domainkey.helderbergsocial.co.za" {
		t.Fatalf("record: %s %s", s.recordName(), s.recordValue())
	}
	raw, err := build("Helderberg Social <hello@helderbergsocial.co.za>", Message{To: "someone@example.org", Subject: "Wéékend: what's on", Text: "Line one  \nLine two\n\n\n", HTML: "<p>Line one</p>", Headers: map[string]string{"List-Unsubscribe": "<https://example.org/u>"}})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := s.sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(signed), "DKIM-Signature: v=1; a=rsa-sha256; c=relaxed/relaxed; d=helderbergsocial.co.za; s=hs1;") {
		t.Fatalf("no signature header:\n%s", signed[:200])
	}
	// --- independent verification ---
	headers, body := splitMessage(signed)
	var sigLine string
	for _, h := range headers {
		if strings.HasPrefix(strings.ToLower(h), "dkim-signature:") {
			sigLine = h
		}
	}
	tags := map[string]string{}
	for _, kv := range strings.Split(strings.ReplaceAll(strings.TrimPrefix(strings.SplitN(sigLine, ":", 2)[1], " "), "\r\n", ""), ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if ok {
			tags[strings.TrimSpace(k)] = strings.Map(func(r rune) rune {
				if r == ' ' || r == '\t' {
					return -1
				}
				return r
			}, v)
		}
	}
	bh := sha256.Sum256([]byte(canonBodyRelaxed(body)))
	if tags["bh"] != base64.StdEncoding.EncodeToString(bh[:]) {
		t.Fatalf("body hash mismatch: %s", tags["bh"])
	}
	for _, must := range []string{"From", "To", "Subject", "Date", "Message-ID", "MIME-Version", "Content-Type", "List-Unsubscribe", "Auto-Submitted"} {
		if !strings.Contains(":"+tags["h"]+":", ":"+must+":") {
			t.Fatalf("h= lacks %s: %s", must, tags["h"])
		}
	}
	var canon strings.Builder
	for _, name := range strings.Split(tags["h"], ":") {
		for i := len(headers) - 1; i >= 0; i-- {
			n, _, _ := strings.Cut(headers[i], ":")
			if strings.EqualFold(strings.TrimSpace(n), name) {
				canon.WriteString(canonHeaderRelaxed(headers[i]))
				break
			}
		}
	}
	// The signature header with b= emptied, canonicalised, no trailing CRLF.
	stripped := regexp.MustCompile(`b=[^;]*$`).ReplaceAllString(sigLine, "b=")
	canon.WriteString(strings.TrimSuffix(canonHeaderRelaxed(stripped), "\r\n"))
	sum := sha256.Sum256([]byte(canon.String()))
	sig, err := base64.StdEncoding.DecodeString(tags["b"])
	if err != nil {
		t.Fatal(err)
	}
	if err := rsa.VerifyPKCS1v15(&s.key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("signature does not verify: %v\n%s", err, signed)
	}
	// Tamper: the body hash must stop matching.
	if canonBodyRelaxed(body+"x") == canonBodyRelaxed(body) {
		t.Fatal("canon ignores body change")
	}
}

func TestMailDNSEndpointAndMXLookup(t *testing.T) {
	a, _ := testApp(t)
	h := a.routes()
	rr := get(t, h, "/api/mail-dns")
	if rr.Code != 200 {
		t.Fatalf("mail-dns: %d", rr.Code)
	}
	var out struct {
		Domain  string
		Records []struct{ Name, Type, Value string }
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Domain != "helderbergsocial.co.za" || len(out.Records) < 3 {
		t.Fatalf("unexpected: %+v", out)
	}
	types := map[string]bool{}
	for _, r := range out.Records {
		types[r.Type+" "+r.Name] = true
		if r.Type == "TXT" && strings.Contains(r.Name, "_domainkey") && !strings.Contains(r.Value, "p="+a.dkim.pubB64) {
			t.Fatal("dkim record value does not carry the key")
		}
	}
	for _, want := range []string{"TXT helderbergsocial.co.za", "TXT hs1._domainkey.helderbergsocial.co.za", "TXT _dmarc.helderbergsocial.co.za", "MX helderbergsocial.co.za"} {
		if !types[want] {
			t.Fatalf("missing %s in %v", want, types)
		}
	}
	if _, err := mxHosts(""); err == nil {
		t.Fatal("empty domain accepted")
	}
}
