package main

import (
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Message is one outbound email. Text is mandatory; HTML optional. Extra
// headers are used for List-Unsubscribe on digests.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
	Headers map[string]string
	Kind    string // for the mail log
}

type Mailer interface {
	Send(m Message) error
}

func (a *App) send(m Message) error {
	h := emailHash(m.To)
	if !a.mailBudgetOK(h) {
		err := fmt.Errorf("mail budget exceeded for recipient")
		a.logMail(h, m.Kind, err)
		return err
	}
	err := a.mailer.Send(m)
	a.logMail(h, m.Kind, err)
	if err != nil {
		a.logf("mail %s failed: %v", m.Kind, err)
	}
	return err
}

// build renders RFC 5322 bytes. Header values are sanitised against
// injection (no CR/LF), and the subject is RFC 2047 encoded when needed.
func build(from string, m Message) ([]byte, error) {
	if _, err := mail.ParseAddress(from); err != nil {
		return nil, fmt.Errorf("bad from address: %w", err)
	}
	if _, err := mail.ParseAddress(m.To); err != nil {
		return nil, fmt.Errorf("bad to address: %w", err)
	}
	hdr := func(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }
	var b strings.Builder
	boundary := "hs-" + randomID(12)
	fmt.Fprintf(&b, "From: %s\r\n", hdr(from))
	fmt.Fprintf(&b, "To: %s\r\n", hdr(m.To))
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", hdr(m.Subject)))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@helderbergsocial.co.za>\r\n", randomID(16))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Auto-Submitted: auto-generated\r\n")
	for k, v := range m.Headers {
		fmt.Fprintf(&b, "%s: %s\r\n", hdr(k), hdr(v))
	}
	if m.HTML == "" {
		b.WriteString("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
		b.WriteString(crlf(m.Text))
		return []byte(b.String()), nil
	}
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", boundary, crlf(m.Text))
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", boundary, crlf(m.HTML))
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return []byte(b.String()), nil
}

func crlf(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// smtpMailer speaks submission on 587 (STARTTLS) or 465 (implicit TLS) with
// PLAIN auth, which every commercial relay accepts. TLS is mandatory in both
// modes; there is no plaintext fallback.
type smtpMailer struct {
	host, user, pass, from string
	port                   int
}

func (s *smtpMailer) Send(m Message) error {
	raw, err := build(s.from, m)
	if err != nil {
		return err
	}
	fromAddr, _ := mail.ParseAddress(s.from)
	addr := net.JoinHostPort(s.host, fmt.Sprint(s.port))
	tlsCfg := &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}
	var c *smtp.Client
	d := net.Dialer{Timeout: 20 * time.Second}
	if s.port == 465 {
		conn, err := tls.DialWithDialer(&d, "tcp", addr, tlsCfg)
		if err != nil {
			return fmt.Errorf("smtp dial: %w", err)
		}
		if c, err = smtp.NewClient(conn, s.host); err != nil {
			return fmt.Errorf("smtp client: %w", err)
		}
	} else {
		conn, err := d.Dial("tcp", addr)
		if err != nil {
			return fmt.Errorf("smtp dial: %w", err)
		}
		if c, err = smtp.NewClient(conn, s.host); err != nil {
			return fmt.Errorf("smtp client: %w", err)
		}
		if err = c.StartTLS(tlsCfg); err != nil {
			c.Close()
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	defer c.Close()
	if err = c.Auth(smtp.PlainAuth("", s.user, s.pass, s.host)); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err = c.Mail(fromAddr.Address); err != nil {
		return err
	}
	if err = c.Rcpt(m.To); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write(raw); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// fileMailer writes each message as an .eml file. Used by tests and by a
// local run without an SMTP account.
type fileMailer struct{ dir, from string }

func (f *fileMailer) Send(m Message) error {
	raw, err := build(f.from, m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(f.dir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s-%s.eml", time.Now().UTC().Format("20060102T150405"), m.Kind, emailHash(m.To))
	return os.WriteFile(filepath.Join(f.dir, name), raw, 0o600)
}
