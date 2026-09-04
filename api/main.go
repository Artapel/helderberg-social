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
	cfg     *Config
	db      *sql.DB
	mailer  Mailer
	tmpl    *template.Template
	limGet  *limiter
	limPost *limiter
	watchMu sync.Mutex
	version string
}

func (a *App) logf(format string, args ...any) { log.Printf(format, args...) }

func newApp(cfg *Config) (*App, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return nil, err
	}
	db, err := openDB(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, db: db, tmpl: parseTemplates(), version: version,
		limGet: newLimiter(60, 30), limPost: newLimiter(6, 6)}
	if cfg.DevMailDir != "" {
		a.mailer = &fileMailer{dir: cfg.DevMailDir, from: cfg.MailFrom}
	} else {
		a.mailer = &smtpMailer{host: cfg.SMTPHost, port: cfg.SMTPPort, user: cfg.SMTPUser, pass: cfg.SMTPPass, from: cfg.MailFrom}
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
	_ = app.db.Close()
	fmt.Println("stopped")
}

func mailMode(cfg *Config) string {
	if cfg.DevMailDir != "" {
		return "files in " + cfg.DevMailDir
	}
	return fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
}
