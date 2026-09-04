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
| WhatsApp updates | The same subscription over WhatsApp instead of email (`channel='whatsapp'`, a phone number, no email). Confirmation is a button tap on a template message, the digest is a template with a one-line list plus a *See all events* button to a personalised page, STOP or the Unsubscribe button deletes the row. Automatic end to end; needs the Meta-side setup in `docs/whatsapp.md`. Off until `HS_WA_*` are set. |
| Facebook posting | Posts to the Facebook page through the Graph API from a queue the scheduler drains one item a minute: each approved event after a delay (off by default), a weekly "this weekend" list (off by default), and posts the admin writes or schedules in the console. One post per event ever; a queued post is cancelled when its event is taken back. Off until `HS_FB_*` are set; runbook in `docs/facebook.md`. |
| Submissions | Events and listings from the public. Submitter verifies by email; the item then lands in the moderation queue and the admin gets a mail with approve/reject links. Approved events publish immediately. Approved listings produce a ready-to-paste `data.js` block in the queue (listings stay curated in the repo). |
| Moderation | `moderate.html` asks for the admin address and emails a 12-hour sign-in link. The queue shows pending items, sources and lets the admin run a source check or preview a digest. No passwords exist anywhere. |
| Source watcher | Every `HS_WATCH_INTERVAL` (6 h) the sources in `api/sources.json` are fetched politely (1.5 s apart, 2 MB cap, 25 s timeout, identified user agent). ICS feeds yield events straight into the queue (deduplicated per UID). Plain pages are hashed after stripping tags and noise; a change emails the admin a "look at this" line. It never publishes anything on its own. |
| Housekeeping | Hourly: unconfirmed subscribers and submissions deleted after 3 days, submitter name/email scrubbed 90 days after a decision, used-token and mail-log rows expired. |

## Security model

- **No passwords.** Every public action link is an HMAC-SHA256 token (purpose,
  subject, expiry, nonce) signed with `HS_SECRET`; single-use tokens are recorded
  in `tokens_used`. Confirm/verify links last 72 h, moderation links 30 days.
- **Admin sign-in is two-factor** (see *The admin console* below): an emailed
  single-use link (15 min) and then a time-based code from Google Authenticator
  (RFC 6238) or a one-time backup code. Moderation links in emails land inside
  the console and need a session, so a forwarded email can approve nothing.
- **Origin.** CORS allows only `HS_SITE_URL`. Requests are limited per IP
  (token bucket: 60 GET/min; 6 POST/min for public writes and the sign-in
  steps; 120/min for the signed-in console), bodies capped at 32 KB, honeypot
  fields (`website` on subscribe, `company` on submissions) drop bots silently.
  Addresses and clients on the console blocklist are dropped silently.
- **Input.** Strict vocabularies for town/category/audience/cost/kind, URLs
  must be http(s) without userinfo, control characters stripped, lengths
  enforced server-side. Everything rendered through `html/template`.
- **Headers.** `nosniff`, `no-referrer`, `X-Frame-Options: DENY`,
  `Content-Security-Policy: default-src 'none'` (admin pages allow inline style),
  HSTS, `Cache-Control: no-store` on everything but `/api/events`.
- **Facebook.** The Page token lives only in the container's environment and is
  never logged or shown (the Settings page prints its length). Every post is
  built from an approved event or typed by a signed-in admin; nothing public
  can reach the queue. Posts are never deleted from Facebook by the API.
- **WhatsApp.** Webhooks are accepted only with a valid `X-Hub-Signature-256`
  (HMAC with the app secret) and deduplicated by message id; the sender's
  number is the only identity ever acted on, so a crafted payload cannot
  confirm or delete someone else. Outbound is restricted to the two approved
  templates plus replies inside the 24-hour window. Numbers share the mail
  budget and blocklist (as `tel:<number>` hashes).
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

Known limits: SQLite on one node (nightly backup, see `api/backup.sh`, plus
snapshots from the console); a compromised admin mailbox is *not* enough on its
own any more, but mailbox plus phone is; the source watcher cannot read
JavaScript-rendered pages (Quicket, Lourensford's event directory), those stay
on the manual check list.

## The admin console

`https://api.helderbergsocial.co.za/admin` (no JavaScript, works under a CSP
that forbids scripts; every control is a form).

**Signing in.** `/admin/login` asks for the admin email. If it matches
`HS_ADMIN_EMAIL` a single-use link is mailed (15 min). Opening it sets a 10 min
"pre" cookie and goes to `/admin/2fa`. The first time it goes to `/admin/enrol`
instead: scan the QR with Google Authenticator, confirm one code, and write down
the 10 backup codes (shown once, stored hashed). A correct code creates a
session: 12 h absolute, 2 h idle, cookie `HttpOnly; Secure; SameSite=Strict;
Path=/admin`. Five wrong codes kill the link. TOTP codes cannot be replayed
inside their window; the secret is stored AES-GCM encrypted with a key derived
from `HS_SECRET`, so a copy of the database alone cannot mint codes. All POSTs
carry a per-session CSRF token and are checked against `Sec-Fetch-Site`.
Everything is written to `audit_log`.

**Lost phone:** sign in with a backup code, then *Security -> Remove
authenticator* and enrol the new phone. **Lost everything:** set
`HS_TOTP_RESET=1` in `.env` for one restart (wipes the authenticator and all
sessions, audited), then remove it.

**Pages** (left sidebar):

| Page | What you can do |
|---|---|
| Dashboard | queue counts, subscribers, traffic today, mail failures, 14-day views, status, recent audit |
| Queue | approve / reject events and listing submissions (the `data.js` block is shown per listing) |
| Events | filter/search everything, edit any field, create events (published immediately, marked verified), unpublish, reopen, delete |
| Listings | every submission by status, delete old ones |
| Subscribers | email address or WhatsApp number (channel pill), filter by channel, search either, edit preferences, resend/force confirmation (email link or WhatsApp template), remove, block address or number, add by hand (either), CSV export |
| Digests | schedule and next runs, preview to yourself, send now (with confirmation), 30-day history |
| Facebook | connection check, queue with cancel/post now, history with permalinks and retry, write or schedule a post, post any approved event, preview and queue this weekend's list |
| Sources | add/edit/enable/disable/delete watched pages and ICS feeds, check one or all now, forget a source's memory |
| Analytics | page views and visitors per day, top pages, API routes and errors, subscriber growth, events by town/category (7/30/90 days) |
| Logs | last 300 requests, mail log (hashes only), audit log, last 500 app log lines |
| Security | authenticator status, regenerate backup codes, remove authenticator, active sessions with revoke, sign-in history, blocklist |
| Settings | runtime overrides without restart: digest hour/day, pause digests, watch interval, pause watching, extra notification addresses, announcement banner, maintenance mode, pause submissions/subscriptions, public events window, Facebook automatic posting (approved events + delay, weekend list + day/hour); read-only view of the environment |
| System | version, uptime, memory, DB size and table counts, housekeeping now, integrity check, WAL checkpoint, test email, WhatsApp configuration + template status + test message, outbound-mail DNS checks, JSON export of everything, snapshots (`VACUUM INTO`, newest 14 kept) with download |

**Settings the site reacts to.** `/api/events` carries a `site` object
(`announcement`, `maintenance`, `submissions`, `subscriptions`). The static site
shows the announcement banner on every page within its 5-minute cache and
disables the subscribe/submit forms when they are paused. Maintenance mode makes
every public POST answer 503 with the configured message.

**Statistics.** Each page sends `POST /api/ping {"p":"/path"}`. The server
counts views per day and path, and one visitor per day per path using
`HMAC(secret, day) || ip` truncated, which cannot be reversed or joined across
days. Nothing else is recorded: no user agent, no referrer, no cookie. Request
counts per route/status per day come from the middleware. Retention: page views
and route counts 400 days, unique hashes 35 days, audit log 1 year, sessions 1
day after expiry.

## Configuration

Copy `api/.env.example` to `api/.env` (mode 600) and fill in:

| Variable | Meaning |
|---|---|
| `HS_SECRET` | 32+ random characters (`openssl rand -base64 48`). Rotating it invalidates every outstanding link. |
| `HS_SITE_URL` / `HS_API_URL` | `https://helderbergsocial.co.za` / `https://api.helderbergsocial.co.za` |
| `HS_ADMIN_EMAIL` | The one address that can sign in to the console (emailed link + authenticator code) and receives queue mail. |
| `HS_MAIL_FROM` | Sender shown on every mail. Its domain is the one DKIM signs for and the DNS checks look at. |
| `HS_SMTP_HOST`, `HS_SMTP_PORT`, `HS_SMTP_USER`, `HS_SMTP_PASS` | Optional relay. Leave the host empty for direct delivery (see *Outbound mail*). |
| `HS_MAIL_IP`, `HS_MAIL_HELO`, `HS_DKIM_SELECTOR` | Public sending address (for SPF and the PTR check), EHLO name (defaults to the API host), DKIM selector (`hs1`; empty disables signing). |
| `HS_WA_PHONE_ID`, `HS_WA_TOKEN`, `HS_WA_APP_SECRET`, `HS_WA_VERIFY_TOKEN` | WhatsApp Business Platform: sending number id, permanent system-user token, app secret (webhook signature), webhook verify token. All four or none. `HS_WA_WABA_ID` (template status on the System page) and `HS_ADMIN_PHONE` (test button) are optional. See `docs/whatsapp.md`. |
| `HS_FB_PAGE_ID`, `HS_FB_PAGE_TOKEN` | Facebook page posting: the numeric page id and a non-expiring Page access token. Both or none. `HS_FB_API_VERSION` defaults to `v22.0`. See `docs/facebook.md`. |
| `HS_BIND_IP` | Interface the container port binds to on the host (the internal address, never `0.0.0.0`). |
| `HS_TZ`, `HS_DIGEST_HOUR`, `HS_WEEKLY_DAY`, `HS_WATCH_INTERVAL` | Defaults: `Africa/Johannesburg`, 6, 4 (Thursday), 6h. |
| `HS_TOTP_RESET` | Set to `1` for one restart only, to wipe a lost authenticator and every session (audited). Remove it again straight away. |

## Deploy and operate

```bash
# on the Docker host, once
git clone https://github.com/Artapel/helderberg-social ~/helderberg-social
cd ~/helderberg-social && cp api/.env.example api/.env && chmod 600 api/.env && $EDITOR api/.env
bash api/deploy.sh                 # builds the image, starts it, waits for /api/health
# DNS: api A record -> the proxy's public IP (docs/dns-setup.py adds it)
NPM_USER=… NPM_PW=… bash api/npm-proxy-host.sh   # proxy host + Let's Encrypt
# mail DNS (SPF, DKIM, DMARC, null MX) once the container is up and serving /api/mail-dns
HA_TOKEN=… python docs/dns-setup.py --mail
# nightly backup
(crontab -l; echo "17 2 * * * bash $HOME/helderberg-social/api/backup.sh") | crontab -

# every later release
cd ~/helderberg-social && git pull --ff-only && bash api/deploy.sh
```

Health: `curl -sS https://api.helderbergsocial.co.za/api/health`. Logs:
`docker logs --since 1h helderberg-social`. Backups land in `~/hs-backups/`
(30 days kept). Restore: stop the container, copy the `.db` file into the
`helderberg-social_hs-data` volume, start it again.

Tests: `cd api && go vet ./... && go test ./...` (uses a file mailer; the only
network use is one MX lookup in the mail-DNS test).

## Outbound mail

There is no mail account anywhere. With `HS_SMTP_HOST` empty the API is its
own mail transfer agent: it looks up the recipient's MX records, connects to
port 25, EHLOs as `HS_MAIL_HELO`, upgrades to TLS when the receiver offers
STARTTLS, and hands the message over. Up to three MX hosts are tried in
preference order; a 5xx from any of them is treated as a permanent refusal.
Every message, in every mode, is DKIM-signed (RFC 6376, rsa-sha256,
relaxed/relaxed) with a 2048-bit key the API generates on first start into the
data volume as `dkim-<selector>.key`, mode 600. The key never leaves the
container; the console only ever shows the public half.

Direct sending is trusted only when DNS says it is. The four records below are
what receivers check; `GET /api/mail-dns` serves them as JSON (public
information) and `docs/dns-setup.py --mail` publishes them at HostAfrica. The
System page in the console resolves each one live and shows *DNS complete* or
*DNS incomplete* with the want/have pair per record. Those checks ask public
resolvers (1.1.1.1, then 8.8.8.8), not the host's, because the host sits on a
corporate resolver that carries a private copy of the sending address's reverse
zone and would never show the ISP's PTR.

| Record | Value | Why |
|---|---|---|
| `TXT @` | `v=spf1 ip4:<HS_MAIL_IP> -all` | Only that address may send as the domain. |
| `A mail` | `<HS_MAIL_IP>` | The sending host's own name (`HS_MAIL_HELO`), replacing the registrar's CNAME to the apex, so the ISP's PTR on that address is forward-confirmed. |
| `TXT mail` | `v=spf1 ip4:<HS_MAIL_IP> -all` | The same SPF on the EHLO name, so the greeting passes SPF too (SpamAssassin's `SPF_HELO_NONE`). |
| `TXT hs1._domainkey` | `v=DKIM1; k=rsa; p=<public key>` | The key that matches the signature. Longer than 255 octets, so the script republishes it as quoted 255-octet strings if the registrar rejects the single string. |
| `TXT _dmarc` | `v=DMARC1; p=quarantine; adkim=s; aspf=s; fo=1` | Receivers junk (not just accept) mail that fails both checks; strict alignment on the `From` domain. No reporting address yet, as the domain does not receive mail. |
| `MX @` | `0 .` (null MX, RFC 7505), or no MX at all | The domain sends but never receives. HostAfrica rejects `.` as an exchange, so the script deletes the registrar's default MX instead; with no MX, receivers fall back to the A record (GitHub Pages, no port 25) and fail fast rather than pointing replies at a web host on purpose. |

One thing the zone cannot fix: **reverse DNS on `HS_MAIL_IP`**. Gmail and
Outlook refuse port-25 connections from an address without a PTR. The ISP that
owns the address sets it; ask for `mail.helderbergsocial.co.za`, which is the
`HS_MAIL_HELO` name and resolves to the address, so HELO, forward and reverse
all agree (requested 2026-09-04). Until then, expect Gmail recipients to be
refused with a 550 and the mail log to say so. The System page shows the PTR
check with the rest.

Checking a real send: address a subscription at the domain
[mail-tester.com](https://www.mail-tester.com) shows and confirm it; the score
page lists SPF, DKIM, DMARC and PTR results individually. The *Send a test
email* button on the System page mails `HS_ADMIN_EMAIL`.

Relay mode (`HS_SMTP_HOST` set, with user and password) still signs with the
same key, so the DKIM record stays valid; add the relay's own SPF include to
the `TXT @` record by hand in that case.

## Adding a source

Append to `api/sources.json` and redeploy. `kind` is `ics` for a calendar feed
(the good case: events arrive structured) or `html` for a page to watch. Give
`listing`, `category` and `town` so auto-captured events land in the right
place. Never guess a URL: record the one the organiser actually publishes.
