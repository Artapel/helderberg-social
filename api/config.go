package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config is read from the environment once at start-up. Secrets never come
// from argv or files inside the image; they arrive through the compose env
// file, which lives only on the host.
var waVersionRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+$`)
var waTemplateRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
var dkimSelectorRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)

type Config struct {
	Listen        string
	DataDir       string
	SiteURL       string // public site, used for links and CORS
	APIURL        string // this service's public URL, used in emailed links
	Secret        []byte // HMAC key for every signed link
	AdminEmail    string
	MailFrom      string
	SMTPHost      string
	SMTPPort      int
	SMTPUser      string
	SMTPPass      string
	DevMailDir    string // when set, mail is written to files instead of SMTP
	MailIP        string // public address mail leaves from; goes into the SPF record and the System-page checks
	MailHelo      string // EHLO name for direct delivery (defaults to the API host)
	DKIMSelector  string // empty disables signing
	TZ            *time.Location
	DigestHour    int
	WeeklyDay     time.Weekday
	WatchInterval time.Duration
	TrustProxy    bool
	TOTPReset     bool // HS_TOTP_RESET=1: break-glass, wipes the authenticator once at start-up
	AdminPhone    string
	// WhatsApp Business Platform (Cloud API). All four of phone id, token, app
	// secret and verify token are needed; with none set WhatsApp is simply off.
	WAPhoneID, WAWABAID, WAToken, WAAppSecret, WAVerifyToken string
	WAVersion, WALang, WATemplateConfirm, WATemplateDigest   string
	// Facebook page posting (Graph API). Page id + a Page access token; with
	// neither set posting is off and the console says so.
	FBPageID, FBToken, FBVersion string
	// "Sign in with Google / Microsoft / Yahoo" for members (OpenID Connect),
	// keyed by provider. A provider is on when both its id and secret are set;
	// with neither the button is hidden and /account/<provider>/* refuses
	// politely.
	OAuth map[string]oauthCreds
}

type oauthCreds struct{ ID, Secret string }

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func loadConfig() (*Config, error) {
	c := &Config{
		Listen:       env("HS_LISTEN", ":8102"),
		DataDir:      env("HS_DATA_DIR", "/data"),
		SiteURL:      strings.TrimRight(env("HS_SITE_URL", "https://helderbergsocial.co.za"), "/"),
		APIURL:       strings.TrimRight(env("HS_API_URL", "https://api.helderbergsocial.co.za"), "/"),
		AdminEmail:   strings.ToLower(env("HS_ADMIN_EMAIL", "")),
		MailFrom:     env("HS_MAIL_FROM", "Helderberg Social <hello@helderbergsocial.co.za>"),
		SMTPHost:     env("HS_SMTP_HOST", ""),
		SMTPUser:     env("HS_SMTP_USER", ""),
		SMTPPass:     env("HS_SMTP_PASS", ""),
		DevMailDir:   env("HS_DEV_MAIL_DIR", ""),
		MailIP:       env("HS_MAIL_IP", ""),
		MailHelo:     env("HS_MAIL_HELO", ""),
		DKIMSelector: env("HS_DKIM_SELECTOR", "hs1"),
		TrustProxy:   env("HS_TRUST_PROXY", "true") == "true",
		TOTPReset:    env("HS_TOTP_RESET", "") == "1",
		AdminPhone:   env("HS_ADMIN_PHONE", ""),
		WAPhoneID:    env("HS_WA_PHONE_ID", ""), WAWABAID: env("HS_WA_WABA_ID", ""), WAToken: env("HS_WA_TOKEN", ""),
		WAAppSecret: env("HS_WA_APP_SECRET", ""), WAVerifyToken: env("HS_WA_VERIFY_TOKEN", ""),
		WAVersion: env("HS_WA_API_VERSION", "v22.0"), WALang: env("HS_WA_LANG", "en"),
		WATemplateConfirm: env("HS_WA_TEMPLATE_CONFIRM", "hs_confirm"), WATemplateDigest: env("HS_WA_TEMPLATE_DIGEST", "hs_digest"),
		FBPageID: env("HS_FB_PAGE_ID", ""), FBToken: env("HS_FB_PAGE_TOKEN", ""), FBVersion: env("HS_FB_API_VERSION", "v22.0"),
		OAuth: map[string]oauthCreds{},
	}
	for _, p := range oauthProviders {
		key := "HS_" + strings.ToUpper(p.Key) + "_CLIENT_"
		cr := oauthCreds{ID: env(key+"ID", ""), Secret: env(key+"SECRET", "")}
		if (cr.ID == "") != (cr.Secret == "") {
			return nil, fmt.Errorf("%s sign-in needs %sID and %sSECRET together; leave both empty to disable", p.Name, key, key)
		}
		if cr.ID != "" && p.IDSuffix != "" && !strings.HasSuffix(cr.ID, p.IDSuffix) {
			return nil, fmt.Errorf("%sID should end in %s", key, p.IDSuffix)
		}
		if cr.ID != "" {
			c.OAuth[p.Key] = cr
		}
	}
	secret := env("HS_SECRET", "")
	if len(secret) < 32 {
		return nil, fmt.Errorf("HS_SECRET must be at least 32 characters (got %d)", len(secret))
	}
	c.Secret = []byte(secret)
	if c.AdminEmail == "" || !validEmail(c.AdminEmail) {
		return nil, fmt.Errorf("HS_ADMIN_EMAIL must be a valid address")
	}
	// Three ways out: files (dev), an authenticated relay (HS_SMTP_*), or direct
	// delivery to the recipient MX when no relay is configured.
	if c.DevMailDir == "" && c.SMTPHost != "" && (c.SMTPUser == "" || c.SMTPPass == "") {
		return nil, fmt.Errorf("HS_SMTP_USER and HS_SMTP_PASS are required when HS_SMTP_HOST is set")
	}
	if c.MailHelo == "" {
		if u, err := url.Parse(c.APIURL); err == nil && u.Hostname() != "" {
			c.MailHelo = u.Hostname()
		} else {
			c.MailHelo = "helderbergsocial.co.za"
		}
	}
	if c.MailIP != "" && net.ParseIP(c.MailIP) == nil {
		return nil, fmt.Errorf("HS_MAIL_IP must be an IP address")
	}
	if c.WAPhoneID != "" || c.WAToken != "" || c.WAAppSecret != "" || c.WAVerifyToken != "" {
		if c.WAPhoneID == "" || c.WAToken == "" || c.WAAppSecret == "" || len(c.WAVerifyToken) < 16 {
			return nil, fmt.Errorf("WhatsApp needs HS_WA_PHONE_ID, HS_WA_TOKEN, HS_WA_APP_SECRET and HS_WA_VERIFY_TOKEN (16+ chars) together; leave all four empty to disable")
		}
		if !waVersionRe.MatchString(c.WAVersion) || !waTemplateRe.MatchString(c.WATemplateConfirm) || !waTemplateRe.MatchString(c.WATemplateDigest) {
			return nil, fmt.Errorf("HS_WA_API_VERSION must look like v22.0 and template names must be lowercase letters, digits and underscores")
		}
	}
	if c.FBPageID != "" || c.FBToken != "" {
		if c.FBPageID == "" || c.FBToken == "" {
			return nil, fmt.Errorf("Facebook posting needs HS_FB_PAGE_ID and HS_FB_PAGE_TOKEN together; leave both empty to disable")
		}
		if !waVersionRe.MatchString(c.FBVersion) {
			return nil, fmt.Errorf("HS_FB_API_VERSION must look like v22.0")
		}
		for _, r := range c.FBPageID {
			if r < '0' || r > '9' {
				return nil, fmt.Errorf("HS_FB_PAGE_ID is the numeric page id, not the page name")
			}
		}
	}
	if c.AdminPhone != "" {
		p, ok := normPhone(c.AdminPhone)
		if !ok {
			return nil, fmt.Errorf("HS_ADMIN_PHONE is not a phone number")
		}
		c.AdminPhone = p
	}
	if c.DKIMSelector != "" && !dkimSelectorRe.MatchString(c.DKIMSelector) {
		return nil, fmt.Errorf("HS_DKIM_SELECTOR: letters, digits and hyphens only")
	}
	var err error
	if c.SMTPPort, err = strconv.Atoi(env("HS_SMTP_PORT", "587")); err != nil {
		return nil, fmt.Errorf("HS_SMTP_PORT: %w", err)
	}
	if c.TZ, err = time.LoadLocation(env("HS_TZ", "Africa/Johannesburg")); err != nil {
		return nil, fmt.Errorf("HS_TZ: %w", err)
	}
	if c.DigestHour, err = strconv.Atoi(env("HS_DIGEST_HOUR", "6")); err != nil || c.DigestHour < 0 || c.DigestHour > 23 {
		return nil, fmt.Errorf("HS_DIGEST_HOUR must be 0-23")
	}
	wd, err := strconv.Atoi(env("HS_WEEKLY_DAY", "4"))
	if err != nil || wd < 0 || wd > 6 {
		return nil, fmt.Errorf("HS_WEEKLY_DAY must be 0-6 (0=Sunday)")
	}
	c.WeeklyDay = time.Weekday(wd)
	if c.WatchInterval, err = time.ParseDuration(env("HS_WATCH_INTERVAL", "6h")); err != nil || c.WatchInterval < 15*time.Minute {
		return nil, fmt.Errorf("HS_WATCH_INTERVAL must be a duration of at least 15m")
	}
	return c, nil
}
