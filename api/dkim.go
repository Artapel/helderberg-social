package main

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DKIM (RFC 6376) signing with a 2048-bit RSA key that lives in the data
// volume. The first start generates it; the matching public record is
// published at <selector>._domainkey.<domain> and shown in the console so it
// can be copied into DNS. relaxed/relaxed canonicalisation, rsa-sha256.

type dkimSigner struct {
	domain, selector string
	key              *rsa.PrivateKey
	pubB64           string
}

// dkimSignedHeaders is the set of headers covered by the signature when
// present, in the order they are listed in h=. From is mandatory.
var dkimSignedHeaders = []string{"From", "To", "Subject", "Date", "Message-ID", "MIME-Version", "Content-Type", "Reply-To", "List-Unsubscribe", "List-Unsubscribe-Post", "Auto-Submitted"}

func loadOrCreateDKIM(dir, domain, selector string) (*dkimSigner, bool, error) {
	if selector == "" {
		return nil, false, nil
	}
	path := filepath.Join(dir, "dkim-"+selector+".key")
	created := false
	var key *rsa.PrivateKey
	if raw, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(raw)
		if block == nil || block.Type != "RSA PRIVATE KEY" {
			return nil, false, fmt.Errorf("%s: not a PEM RSA private key", path)
		}
		if key, err = x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
			return nil, false, fmt.Errorf("%s: %w", path, err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if key, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			return nil, false, err
		}
		out := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		if err = os.WriteFile(path, out, 0o600); err != nil {
			return nil, false, fmt.Errorf("write %s: %w", path, err)
		}
		created = true
	} else {
		return nil, false, err
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, false, err
	}
	return &dkimSigner{domain: domain, selector: selector, key: key, pubB64: base64.StdEncoding.EncodeToString(pub)}, created, nil
}

// recordName is the DNS owner of the public key.
func (d *dkimSigner) recordName() string { return d.selector + "._domainkey." + d.domain }

// recordValue is the TXT content to publish.
func (d *dkimSigner) recordValue() string { return "v=DKIM1; k=rsa; p=" + d.pubB64 }

/* ---------- canonicalisation (RFC 6376 §3.4) ---------- */

var wspRun = regexp.MustCompile(`[ \t]+`)

// canonHeaderRelaxed takes one unfolded header line ("Name: value") and
// returns "name:value\r\n" in relaxed form.
func canonHeaderRelaxed(line string) string {
	name, value, _ := strings.Cut(line, ":")
	name = strings.ToLower(strings.TrimSpace(name))
	value = strings.ReplaceAll(value, "\r\n", "")
	value = wspRun.ReplaceAllString(value, " ")
	value = strings.Trim(value, " \t")
	return name + ":" + value + "\r\n"
}

// canonBodyRelaxed strips trailing whitespace per line, collapses runs of
// whitespace, drops trailing empty lines, and ends a non-empty body with CRLF.
func canonBodyRelaxed(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		l = wspRun.ReplaceAllString(l, " ")
		lines[i] = strings.TrimRight(l, " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}

// splitMessage returns the unfolded header lines and the body of an RFC 5322
// message.
func splitMessage(raw []byte) (headers []string, body string) {
	head, rest, found := bytes.Cut(raw, []byte("\r\n\r\n"))
	if !found {
		head, rest = raw, nil
	}
	for _, l := range strings.Split(string(head), "\r\n") {
		if l == "" {
			continue
		}
		if (l[0] == ' ' || l[0] == '\t') && len(headers) > 0 {
			headers[len(headers)-1] += "\r\n" + l // keep the fold; canon removes it
			continue
		}
		headers = append(headers, l)
	}
	return headers, string(rest)
}

// sign returns the message with a DKIM-Signature header prepended.
func (d *dkimSigner) sign(raw []byte) ([]byte, error) {
	headers, body := splitMessage(raw)
	bh := sha256.Sum256([]byte(canonBodyRelaxed(body)))
	// Pick the last instance of each signed header (RFC 6376 §5.4.2).
	var hList []string
	var canon strings.Builder
	for _, want := range dkimSignedHeaders {
		for i := len(headers) - 1; i >= 0; i-- {
			name, _, _ := strings.Cut(headers[i], ":")
			if strings.EqualFold(strings.TrimSpace(name), want) {
				hList = append(hList, want)
				canon.WriteString(canonHeaderRelaxed(headers[i]))
				break
			}
		}
	}
	if len(hList) == 0 || hList[0] != "From" {
		return nil, fmt.Errorf("dkim: message has no From header")
	}
	sigValue := fmt.Sprintf("v=1; a=rsa-sha256; c=relaxed/relaxed; d=%s; s=%s; t=%d;\r\n\th=%s;\r\n\tbh=%s;\r\n\tb=",
		d.domain, d.selector, time.Now().Unix(), strings.Join(hList, ":"), base64.StdEncoding.EncodeToString(bh[:]))
	sigHeader := "DKIM-Signature: " + sigValue
	// The signature header itself is included, canonicalised, without its trailing CRLF.
	canon.WriteString(strings.TrimSuffix(canonHeaderRelaxed(sigHeader), "\r\n"))
	sum := sha256.Sum256([]byte(canon.String()))
	sig, err := rsa.SignPKCS1v15(rand.Reader, d.key, crypto.SHA256, sum[:])
	if err != nil {
		return nil, err
	}
	b64 := base64.StdEncoding.EncodeToString(sig)
	var folded strings.Builder
	for len(b64) > 72 {
		folded.WriteString(b64[:72] + "\r\n\t")
		b64 = b64[72:]
	}
	folded.WriteString(b64)
	out := make([]byte, 0, len(raw)+len(sigHeader)+len(folded.String())+4)
	out = append(out, sigHeader...)
	out = append(out, folded.String()...)
	out = append(out, "\r\n"...)
	return append(out, raw...), nil
}
