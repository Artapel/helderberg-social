// Helderberg Social API: subscriptions, event submissions, digests and
// source watching for https://helderbergsocial.co.za. One static binary,
// one SQLite file, no accounts, no passwords.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var version = "dev" // set at build time with -ldflags "-X main.version=…"

type App struct {
	cfg      *Config
	db       *sql.DB
	mailer   Mailer
	dkim     *dkimSigner
	wa       *waClient
	tmpl     *template.Template
	limGet   *limiter
	limPost  *limiter
	limAdmin *limiter
	watchMu  sync.Mutex
	version  string
	// console
	ctmpl *template.Template
	stats *stats
	tries tryCounter
	block blocklist
}

func newApp(cfg *Config) (*App, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return nil, err
	}
	db, err := openDB(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, db: db, tmpl: parseTemplates(), ctmpl: parseConsole(), stats: newStats(), version: version,
		limGet: newLimiter(60, 30), limPost: newLimiter(6, 6), limAdmin: newLimiter(120, 60)}
	a.loadBlocklist()
	if cfg.TOTPReset {
		a.totpReset()
		_, _ = a.db.Exec(`UPDATE sessions SET revoked = 1`)
		a.audit(nil, "totp.reset", "", "HS_TOTP_RESET=1 at start-up; remove the variable now")
		a.logf("WARNING: HS_TOTP_RESET=1 wiped the authenticator; unset it and restart")
	}
	if cfg.DKIMSelector != "" {
		signer, created, err := loadOrCreateDKIM(cfg.DataDir, a.mailDomain(), cfg.DKIMSelector)
		if err != nil {
			return nil, fmt.Errorf("dkim: %w", err)
		}
		a.dkim = signer
		if created {
			a.logf("dkim: generated a new 2048-bit key; publish TXT %s -> %s", signer.recordName(), signer.recordValue())
		}
	}
	a.wa = newWAClient(cfg, a.logf)
	switch {
	case cfg.DevMailDir != "":
		a.mailer = &fileMailer{dir: cfg.DevMailDir, from: cfg.MailFrom, dkim: a.dkim}
	case cfg.SMTPHost != "":
		a.mailer = &smtpMailer{host: cfg.SMTPHost, port: cfg.SMTPPort, user: cfg.SMTPUser, pass: cfg.SMTPPass, from: cfg.MailFrom, dkim: a.dkim}
	default:
		a.mailer = &directMailer{from: cfg.MailFrom, helo: cfg.MailHelo, dkim: a.dkim, logf: a.logf}
	}
	if err := a.seedSources(); err != nil {
		return nil, err
	}
	if err := a.seedEvents(); err != nil {
		return nil, err
	}
	return a, nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		// Used by the container healthcheck: the image has no curl.
		c := &http.Client{Timeout: 4 * time.Second}
		resp, err := c.Get("http://127.0.0.1:8102/api/health")
		if err != nil || resp.StatusCode != 200 {
			os.Exit(1)
		}
		os.Exit(0)
	}
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	app, err := newApp(cfg)
	if err != nil {
		log.Fatalf("start: %v", err)
	}
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           app.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go app.scheduler(ctx)
	go func() {
		log.Printf("helderberg-social api %s listening on %s (mail via %s, tz %s)", version, cfg.Listen, mailMode(cfg), cfg.TZ)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	app.flushStats()
	_ = app.db.Close()
	fmt.Println("stopped")
}

func mailMode(cfg *Config) string {
	switch {
	case cfg.DevMailDir != "":
		return "files in " + cfg.DevMailDir
	case cfg.SMTPHost != "":
		return fmt.Sprintf("relay %s:%d", cfg.SMTPHost, cfg.SMTPPort)
	}
	return "direct to recipient MX (HELO " + cfg.MailHelo + ")"
}
