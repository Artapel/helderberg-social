# The API: subscriptions, submissions, source watching

The public site stays static on GitHub Pages. One small Go service adds the
dynamic parts. It runs as a single container on a Docker host behind Nginx Proxy
Manager (NPM) and is reachable at `https://api.helderbergsocial.co.za`.

```
browser ──(GitHub Pages)──> HTML/CSS/JS + data/data.js   (listings, offline event fallback)
browser ──(CORS, JSON)────> api.helderbergsocial.co.za    (approved events, forms)
container: HTTP API + digest scheduler + source watcher + SQLite in one process
```

Source: `api/`. Stdlib Go plus `modernc.org/sqlite` (pure Go, no cgo). Scratch
image, non-root, read-only filesystem, all capabilities dropped.

## What it does

| Area | Behaviour |
|---|---|
| Events | `GET /api/events` serves every approved event (ETag, 5 min cache). The site merges these over the fallback list in `data.js` at load time (`HS.onEvents`). |
| Email updates | Double opt-in. Subscriber picks daily or weekly, a 7/14/30-day horizon, towns and categories. Daily digests go out at `HS_DIGEST_HOUR` local time, weekly on `HS_WEEKLY_DAY`. Empty digests are skipped. Every mail has `List-Unsubscribe` (one-click) and a signed unsubscribe link; unsubscribing deletes the row. |
| Submissions | Events and listings from the public. Submitter verifies by email; the item then lands in the moderation queue and the admin gets a mail with approve/reject links. Approved events publish immediately. Approved listings produce a ready-to-paste `data.js` block in the queue (listings stay curated in the repo). |
| Moderation | `moderate.html` asks for the admin address and emails a 12-hour sign-in link. The queue shows pending items, sources and lets the admin run a source check or preview a digest. No passwords exist anywhere. |
| Source watcher | Every `HS_WATCH_INTERVAL` (6 h) the sources in `api/sources.json` are fetched politely (1.5 s apart, 2 MB cap, 25 s timeout, identified user agent). ICS feeds yield events straight into the queue (deduplicated per UID). Plain pages are hashed after stripping tags and noise; a change emails the admin a "look at this" line. It never publishes anything on its own. |
| Housekeeping | Hourly: unconfirmed subscribers and submissions deleted after 3 days, submitter name/email scrubbed 90 days after a decision, used-token and mail-log rows expired. |

## Security model

- **No credentials.** Every action link is an HMAC-SHA256 token (purpose, subject,
  expiry, nonce) signed with `HS_SECRET`; single-use tokens are recorded in
  `tokens_used`. Confirm/verify links last 48 h, moderation links 30 days, admin
  sign-in 12 h.
- **Origin.** CORS allows only `HS_SITE_URL`. Requests are limited per IP
  (token bucket: 60 GET/min, 6 POST/min), bodies capped at 32 KB, honeypot
  fields (`website` on subscribe, `company` on submissions) drop bots silently.
- **Input.** Strict vocabularies for town/category/audience/cost/kind, URLs
  must be http(s) without userinfo, control characters stripped, lengths
  enforced server-side. Everything rendered through `html/template`.
- **Headers.** `nosniff`, `no-referrer`, `X-Frame-Options: DENY`,
  `Content-Security-Policy: default-src 'none'` (admin pages allow inline style),
  HSTS, `Cache-Control: no-store` on everything but `/api/events`.
- **Mail.** Header-injection-safe builder; per-recipient budget (5/h, 20/day)
  so the service cannot be used to bomb an inbox; SMTP over STARTTLS/TLS 1.2+.
- **Privacy.** Logs and rate-limit keys hold a salted hash of the IP, never the
  address. Retention rules above. The site's `privacy.html` is the public notice.
- **Container.** Scratch base, `USER 65532`, `read_only`, `cap_drop: ALL`,
  `no-new-privileges`, only `/data` writable, bound to the internal interface
  and exposed to the internet solely through NPM with a Let's Encrypt cert.
- **Front end.** Every page carries a strict CSP meta (`script-src 'self'
  cdnjs`, `connect-src` the API only, `object-src 'none'`, `base-uri 'self'`),
  no inline scripts, Leaflet loaded with Subresource Integrity hashes.

Known limits: SQLite on one node (nightly backup, see `api/backup.sh`); a
compromised admin mailbox equals admin access, which is true of any magic-link
system; the source watcher cannot read JavaScript-rendered pages (Quicket,
Lourensford's event directory), those stay on the manual check list.

## Configuration

Copy `api/.env.example` to `api/.env` (mode 600) and fill in:

| Variable | Meaning |
|---|---|
| `HS_SECRET` | 32+ random characters (`openssl rand -base64 48`). Rotating it invalidates every outstanding link. |
| `HS_SITE_URL` / `HS_API_URL` | `https://helderbergsocial.co.za` / `https://api.helderbergsocial.co.za` |
| `HS_ADMIN_EMAIL` | The one address that can sign in to moderation and receives queue mail. |
| `HS_MAIL_FROM`, `HS_SMTP_HOST`, `HS_SMTP_PORT`, `HS_SMTP_USER`, `HS_SMTP_PASS` | Transactional mail. Any provider with SMTP works. Add its SPF include and DKIM records for `helderbergsocial.co.za` at HostAfrica or mail lands in spam. |
| `HS_BIND_IP` | Interface the container port binds to on the host (the internal address, never `0.0.0.0`). |
| `HS_TZ`, `HS_DIGEST_HOUR`, `HS_WEEKLY_DAY`, `HS_WATCH_INTERVAL` | Defaults: `Africa/Johannesburg`, 6, 4 (Thursday), 6h. |

## Deploy and operate

```bash
# on the Docker host, once
git clone https://github.com/Artapel/helderberg-social ~/helderberg-social
cd ~/helderberg-social && cp api/.env.example api/.env && chmod 600 api/.env && $EDITOR api/.env
bash api/deploy.sh                 # builds the image, starts it, waits for /api/health
# DNS: api A record -> the proxy's public IP (docs/dns-setup.py adds it)
NPM_USER=… NPM_PW=… NPM_EMAIL=… bash api/npm-proxy-host.sh   # proxy host + Let's Encrypt
# nightly backup
(crontab -l; echo "17 2 * * * bash $HOME/helderberg-social/api/backup.sh") | crontab -

# every later release
cd ~/helderberg-social && git pull --ff-only && bash api/deploy.sh
```

Health: `curl -sS https://api.helderbergsocial.co.za/api/health`. Logs:
`docker logs --since 1h helderberg-social`. Backups land in `~/hs-backups/`
(30 days kept). Restore: stop the container, copy the `.db` file into the
`helderberg-social_hs-data` volume, start it again.

Tests: `cd api && go vet ./... && go test ./...` (uses a file mailer, no network).

## Adding a source

Append to `api/sources.json` and redeploy. `kind` is `ics` for a calendar feed
(the good case: events arrive structured) or `html` for a page to watch. Give
`listing`, `category` and `town` so auto-captured events land in the right
place. Never guess a URL: record the one the organiser actually publishes.
