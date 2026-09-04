package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"sort"
	"strings"
	"time"
)

// directMailer delivers straight to the recipient's MX on port 25, the way a
// mail server does: EHLO with our own name, STARTTLS when offered, DKIM
// signed. No relay, no account. Deliverability then depends on DNS being
// right (SPF, DKIM, DMARC, and a PTR on the sending IP), which the System
// page checks live.
type directMailer struct {
	from, helo string
	dkim       *dkimSigner
	logf       func(string, ...any)
}

const smtpTimeout = 25 * time.Second

func (d *directMailer) Send(m Message) error {
	raw, err := build(d.from, m)
	if err != nil {
		return err
	}
	if raw, err = signIfEnabled(d.dkim, raw); err != nil {
		return err
	}
	fromAddr, _ := mail.ParseAddress(d.from)
	toAddr, _ := mail.ParseAddress(m.To)
	_, domain, _ := strings.Cut(toAddr.Address, "@")
	hosts, err := mxHosts(domain)
	if err != nil {
		return err
	}
	var last error
	for i, h := range hosts {
		if i == 3 {
			break
		}
		err := d.deliver(h, fromAddr.Address, toAddr.Address, raw)
		if err == nil {
			return nil
		}
		last = err
		var te *textproto.Error
		if errors.As(err, &te) && te.Code >= 500 {
			return fmt.Errorf("%s refused: %w", h, err) // permanent: do not try the next MX
		}
		d.logf("mail: %s via %s failed, trying next: %v", m.Kind, h, err)
	}
	return last
}

// mxHosts is the ordered list of hosts to try for a domain (RFC 5321 §5.1).
func mxHosts(domain string) ([]string, error) {
	if domain == "" {
		return nil, fmt.Errorf("recipient has no domain")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	mxs, err := net.DefaultResolver.LookupMX(ctx, domain)
	if err != nil || len(mxs) == 0 {
		// No MX: fall back to the A record, as the RFC says.
		if _, aerr := net.DefaultResolver.LookupIPAddr(ctx, domain); aerr != nil {
			return nil, fmt.Errorf("%s: no mail server: %v", domain, aerr)
		}
		return []string{domain}, nil
	}
	if len(mxs) == 1 && mxs[0].Host == "." {
		return nil, fmt.Errorf("%s does not accept mail (null MX)", domain)
	}
	sort.Slice(mxs, func(i, j int) bool { return mxs[i].Pref < mxs[j].Pref })
	var out []string
	for _, mx := range mxs {
		if h := strings.TrimSuffix(mx.Host, "."); h != "" {
			out = append(out, h)
		}
	}
	return out, nil
}

func (d *directMailer) deliver(host, from, to string, raw []byte) error {
	dial := net.Dialer{Timeout: smtpTimeout}
	conn, err := dial.Dial("tcp", net.JoinHostPort(host, "25"))
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("greeting: %w", err)
	}
	defer c.Close()
	if err = c.Hello(d.helo); err != nil {
		return fmt.Errorf("helo: %w", err)
	}
	// Opportunistic TLS: use it when offered and the certificate checks out;
	// otherwise carry on in the clear, as every MTA does.
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err = c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			c.Close()
			d.logf("mail: STARTTLS with %s failed (%v); retrying without TLS", host, err)
			return d.deliverPlain(host, from, to, raw)
		}
	}
	return transact(c, from, to, raw)
}

func (d *directMailer) deliverPlain(host, from, to string, raw []byte) error {
	dial := net.Dialer{Timeout: smtpTimeout}
	conn, err := dial.Dial("tcp", net.JoinHostPort(host, "25"))
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if err = c.Hello(d.helo); err != nil {
		return err
	}
	return transact(c, from, to, raw)
}

func transact(c *smtp.Client, from, to string, raw []byte) error {
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write(raw); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	return c.Quit()
}

func signIfEnabled(d *dkimSigner, raw []byte) ([]byte, error) {
	if d == nil {
		return raw, nil
	}
	return d.sign(raw)
}

/* ---------- the DNS this needs, and whether it is there ---------- */

type mailRecord struct {
	Name, Type, Want, Have, Why string
	OK                          bool
}

// mailRecords is what the domain must publish for direct sending to be
// trusted, each checked against live DNS.
func (a *App) mailRecords() []mailRecord {
	domain := a.mailDomain()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	res := net.DefaultResolver
	txt := func(name string, prefix string) string {
		vals, _ := res.LookupTXT(ctx, name)
		for _, v := range vals {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), prefix) {
				return v
			}
		}
		return ""
	}
	var out []mailRecord
	spf := "v=spf1 ip4:" + a.cfg.MailIP + " -all"
	if a.cfg.MailIP == "" {
		spf = "v=spf1 -all"
	}
	have := txt(domain, "v=spf1")
	out = append(out, mailRecord{Name: domain, Type: "TXT", Want: spf, Have: have, Why: "SPF: which address may send as this domain",
		OK: have != "" && (a.cfg.MailIP == "" || strings.Contains(have, a.cfg.MailIP))})
	if a.dkim != nil {
		have = txt(a.dkim.recordName(), "v=dkim1")
		out = append(out, mailRecord{Name: a.dkim.recordName(), Type: "TXT", Want: a.dkim.recordValue(), Have: have, Why: "DKIM: the public key that matches the signature on every message",
			OK: strings.ReplaceAll(have, " ", "") != "" && strings.Contains(strings.ReplaceAll(have, " ", ""), "p="+a.dkim.pubB64)})
	}
	dmarc := "v=DMARC1; p=quarantine; adkim=s; aspf=s; fo=1"
	have = txt("_dmarc."+domain, "v=dmarc1")
	out = append(out, mailRecord{Name: "_dmarc." + domain, Type: "TXT", Want: dmarc, Have: have, Why: "DMARC: what receivers do with mail that fails SPF/DKIM", OK: have != ""})
	mxs, _ := res.LookupMX(ctx, domain)
	var mxHave []string
	for _, mx := range mxs {
		mxHave = append(mxHave, fmt.Sprintf("%d %s", mx.Pref, mx.Host))
	}
	nullMX := len(mxs) == 1 && mxs[0].Host == "."
	out = append(out, mailRecord{Name: domain, Type: "MX", Want: "0 .  (null MX: the domain sends but does not receive)", Have: strings.Join(mxHave, ", "),
		Why: "MX: where inbound mail goes; a null MX makes replies bounce cleanly instead of timing out", OK: nullMX || (len(mxs) > 0 && !strings.Contains(mxHave[0], domain))})
	if a.cfg.MailIP != "" {
		names, _ := res.LookupAddr(ctx, a.cfg.MailIP)
		out = append(out, mailRecord{Name: a.cfg.MailIP, Type: "PTR", Want: "any hostname that resolves back to " + a.cfg.MailIP, Have: strings.Join(names, ", "),
			Why: "reverse DNS on the sending address; Gmail refuses port-25 connections without one. Set by the ISP, not in this zone", OK: len(names) > 0})
	}
	return out
}

func (a *App) mailDomain() string {
	addr, err := mail.ParseAddress(a.cfg.MailFrom)
	if err != nil {
		return "helderbergsocial.co.za"
	}
	_, d, _ := strings.Cut(addr.Address, "@")
	return d
}

// mailDNS is GET /api/mail-dns: the records to publish, as JSON, so
// docs/dns-setup.py can create them without anyone copying a key by hand.
// Everything here is public information.
func (a *App) mailDNS(w http.ResponseWriter, r *http.Request) {
	type rec struct {
		Name, Type, Value string `json:",omitempty"`
		OK                bool
	}
	var out []rec
	for _, m := range a.mailRecords() {
		if m.Type == "PTR" {
			continue
		}
		v := m.Want
		if m.Type == "MX" {
			v = "0 ."
		}
		out = append(out, rec{Name: m.Name, Type: m.Type, Value: v, OK: m.OK})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"domain": a.mailDomain(), "records": out})
}
