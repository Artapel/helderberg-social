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
| Member accounts | Anyone can create an account (name, email, password) at `/account/register`, confirm the address by email, and then post events from `/account`. Each event goes into the same moderation queue; the member sees its state (waiting, published, not published), gets an email either way, and can edit (which sends it back for a check) or remove it. Accounts can change name and password, and delete themselves. Runbook and design in `docs/accounts.md`. |
| Promoters | Approved members post under an organisation name: scheduled and hideable events, noticeboard posts served by `GET /api/posts` (5 min cache, consumed by `notices.html` and the home page), .ics/.csv imports, up to three connected public calendars (SSRF-guarded), listing submissions. Promoter events in `/api/events` carry `promoted` and `by`. Everything is moderated unless the admin marks the promoter *trusted*. `docs/promoters.md`. |
| Ask | `GET /api/ask?q=…` and every WhatsApp text that is not STOP/confirm: answers "today", "week", "weekend in Strand", "free markets", "running clubs in Gordon's Bay" and free questions from the database's approved events plus the listings and events in the site's `data/data.js` (fetched hourly). Optional Ollama-compatible model for free-text questions via `HS_AI_URL`, off by default. `docs/whatsapp.md`, section *Ask*. |
| Submissions | Events and listings from the public. Submitter verifies by email; the item then lands in the moderation queue and the admin gets a mail with approve/reject links. Approved events publish immediately. Approved listings produce a ready-to-paste `data.js` block in the queue (listings stay curated in the repo). |
| Moderation | `moderate.html` asks for the admin address and emails a 12-hour sign-in link. The queue shows pending items, sources and lets the admin run a source check or preview a digest. No passwords exist anywhere. |
| Source watcher | Every `HS_WATCH_INTERVAL` (6 h) the sources in `api/sources.json` are fetched politely (1.5 s apart, 8 MB cap, 25 s timeout, identified user agent). ICS feeds yield events straight into the queue (deduplicated per UID); a feed that covers the whole province carries a `match` pattern and only events mentioning the Helderberg are queued. Plain pages are hashed after stripping tags and noise; a change emails the admin a "look at this" line. It never publishes anything on its own. |
| Housekeeping | Hourly: unconfirmed subscribers, submissions and member accounts deleted after 3 days, expired member sessions purged, submitter name/email scrubbed 90 days after a decision, used-token and mail-log rows expired. |

## Security model

- **No admin password.** Every public action link is an HMAC-SHA256 token (purpose,
  subject, expiry, nonce) signed with `HS_SECRET`; single-use tokens are recorded
  in `tokens_used`. Confirm/verify links last 72 h, moderation links 30 days.
- **Member passwords** are the one place a password exists: hashed with Argon2id
  (19 MiB, 2 passes, 16-byte salt; parameters stored in the hash so they can be
  raised later and old hashes are re-hashed on the next sign-in), at least 10
  characters, not the address, not on a short common-password list. Sign-in
  is locked for 15 minutes after 8 wrong passwords for an address (and per IP),
  an unknown address costs the same time as a wrong password, and the failure
  message is identical for both. Sessions are a random 256-bit cookie stored
  hashed, `HttpOnly; Secure; SameSite=Lax; Path=/account`, 30 days, 7 days
  idle; every form carries a per-session CSRF token; a password change or
  reset revokes every other session. Reset and confirmation links are the
  same single-use signed tokens as everywhere else (1 h and 24 h).
- **Admin sign-in is two-factor** (see *The admin console* below): an emailed
  single-use link (15 min) and then a time-based code from Google Authenticator
  (RFC 6238) or a one-time backup code. Moderation links in emails land inside
  the console and need a session, so a forwarded email can approve nothing.
- **Origin.** CORS allows only `HS_SITE_URL`; the API's own origin passes too, because browsers send `Origin` on same-origin form posts (the console and account pages). Any other origin is refused on non-GET. Requests are limited per IP
  (token bucket: 60 GET/min; 6 POST/min for public writes, the admin sign-in
  steps and the anonymous account forms (register, sign in, forgot, reset,
  resend); 120/min for the signed-in console and the signed-in account
  forms), bodies capped at 32 KB, honeypot fields (`website` on subscribe,
  `company` on submissions, `website_url` on registration) drop bots silently.
  Addresses and clients on the console blocklist are dropped silently.
- **Input.** Strict vocabularies for town/category/audience/cost/kind, URLs
  must be http(s) without userinfo, control characters stripped, lengths
  enforced server-side. Everything rendered through `html/template`.
- **Headers.** `nosniff`, `Referrer-Policy: same-origin` (not `no-referrer`: that makes browsers send `Origin: null` on form posts, which the origin check refuses), `X-Frame-Options: DENY`,
  `Content-Security-Policy: default-src 'none'` (admin pages allow inline style),
  HSTS, `Cache-Control: no-store` on everything but `/api/events` and `/api/posts`.
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
session: 12 h absolute, 2 h idle, cookie `HttpOnly; Secure; SameSite=Lax (Strict withheld the cookie on the redirect after the emailed link, so sign-in could never finish);
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
| Members | everyone with an account: filter (confirmed / unconfirmed / disabled), search name or email, per-member page with their events and active sessions; resend or force the confirmation, disable/enable (disabling signs them out everywhere), sign out everywhere, delete (published events stay, anonymised), block the address and delete |
| Subscribers | email address or WhatsApp number (channel pill), filter by channel, search either, edit preferences, resend/force confirmation (email link or WhatsApp template), remove, block address or number, add by hand (either), CSV export |
| Digests | schedule and next runs, preview to yourself, send now (with confirmation), 30-day history |
| Facebook | connection check, queue with cancel/post now, history with permalinks and retry, write or schedule a post, post any approved event, preview and queue this weekend's list |
| Sources | add/edit/enable/disable/delete watched pages, ICS feeds and `list` aggregators (with an optional filter pattern for regional feeds), check one or all now, forget a source's memory. Full-width compact table since 2026-09-05: one line per source, the full URL, status and filter in tooltips, edit folded under *edit* |
| Analytics | page views and visitors per day, top pages, API routes and errors, subscriber growth, events by town/category (7/30/90 days) |
| Logs | last 300 requests, mail log (hashes only), audit log, last 500 app log lines |
| Security | authenticator status, regenerate backup codes, remove authenticator, active sessions with revoke, sign-in history, blocklist |
| Settings | runtime overrides without restart: digest hour/day, pause digests, watch interval, pause watching, extra notification addresses, announcement banner, maintenance mode, pause submissions/subscriptions/new member accounts, public events window, Facebook automatic posting (approved events + delay, weekend list + day/hour); read-only view of the environment |
| System | version, uptime, memory, DB size and table counts, housekeeping now, integrity check, WAL checkpoint, test email, sign-in providers (Google, Microsoft, Yahoo) on/off with client id, redirect URI and members linked, WhatsApp configuration + template status + test message, outbound-mail DNS checks, JSON export of everything, snapshots (`VACUUM INTO`, newest 14 kept) with download |

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
| `HS_GOOGLE_CLIENT_ID`/`_SECRET`, `HS_MICROSOFT_CLIENT_ID`/`_SECRET`, `HS_YAHOO_CLIENT_ID`/`_SECRET` | *Sign in with Google / Microsoft / Yahoo* for member accounts, each pair both or none (the Google id must end in `.apps.googleusercontent.com`). Redirect URI is `HS_API_URL/account/<provider>/callback`. `/api/health` lists the ones that are on under `logins`. See `docs/accounts.md`. |
| `HS_AI_URL`, `HS_AI_MODEL`, `HS_AI_KEY` | Optional. Base URL of an Ollama-compatible server (e.g. `http://192.168.50.240:11434`) used only for free-text questions in *Ask*; model name (default `llama3.1:8b`); bearer token if the server wants one. Empty = plain answers only. |
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

One thing the zone cannot fix: **reverse DNS on `HS_MAIL_IP`**. Gmail
documents a PTR as a requirement for senders; in practice (2026-09-05) it
accepted the reminder mail from the PTR-less address and filed it as spam
with the reason "similar to messages identified as spam in the past", so the
missing PTR costs reputation rather than a 550. The record wanted is
`mail.helderbergsocial.co.za`, the `HS_MAIL_HELO` name, which resolves to the
address, so HELO, forward and reverse all agree.

Who sets it: the ISP. Publicly, `5.221.41.in-addr.arpa` is delegated by
AfriNIC to `ns1`/`ns2.ecntelecoms.com` (`dig +trace` from 1.1.1.1 and
8.8.8.8, 2026-09-05), and those servers answer NXDOMAIN for `.36` with a zone
serial of `2022112512`, unchanged since November 2022. The ticket raised with
ECN on 2026-09-04 (DR040908, "done, allow 24 h") had therefore not reached
their nameservers a day later; chase it with that serial as evidence.

Do not check this from inside Daisy's network: the corporate resolver carries
its own copy of the same reverse zone, delegated to the domain controllers
(dc01-dc1, dc02-dc1 and the branch DCs), which is why a `dig -x` on 111.150
shows Daisy names for `.39` that the world never sees. The API's checks and
the System page use public resolvers for this reason.

Checking a real send: address a subscription at the domain
[mail-tester.com](https://www.mail-tester.com) shows and confirm it; the score
page lists SPF, DKIM, DMARC and PTR results individually. The *Send a test
email* button on the System page mails `HS_ADMIN_EMAIL`.

Relay mode (`HS_SMTP_HOST` set, with user and password) still signs with the
same key, so the DKIM record stays valid; add the relay's own SPF include to
the `TXT @` record by hand in that case.

## Adding a source

Append to `api/sources.json` and redeploy (`seedSources()` upserts by URL on
start, so editing an entry updates the live row; deleting one does not remove
it, disable it in the console). `kind` is `ics` for a calendar feed (the good
case: events arrive structured), `html` for a page to watch, or `list` for an
index page or RSS/Atom feed where only *new links* matter. Give `listing`,
`category` and `town` so auto-captured events land in the right place. Never
guess a URL: record the one the organiser actually publishes, and fetch it
first to see that it answers. When a shipped source turns out to be dead
(bot-blocked, superseded, gone), do not delete its entry: give it a
`retired` reason instead. The seed then switches the row off on every start
with `retired: <reason>` as its status, so the history stays and the console
shows why. A fetch that fails on the network or with a 5xx/429 is tried once
more after 3 seconds (small WordPress sites here time out during their own
night-time backups); a 4xx is the site's answer and is final.

A feed that covers more than the Helderberg (Western Province Athletics, the
canoe union, the Mountain Club's Google Calendar) takes a `match`: a
case-insensitive regular expression tested against each event's title,
description, location and link. Events that do not match are counted in the
source's status ("N outside the filter"), never queued, and remembered as seen,
so they are not offered again unless the admin presses *Forget*. The same
field is on the console's add and edit forms; a pattern that does not compile
is refused there and reported as an error on the source by the watcher.

### Recurring series in ICS feeds (2026-09-04)

`RRULE` is expanded: DAILY, WEEKLY (`BYDAY`), MONTHLY (`BYDAY` with an ordinal
such as `1SU` or `-1FR`, or `BYMONTHDAY`) and YEARLY, with `INTERVAL`, `COUNT`
and `UNTIL`, minus `EXDATE`s. A series is queued **once**, at its next
occurrence between yesterday and a year ahead, with the rule spelled out at
the top of the summary ("Repeats every week on Sunday."), and the source
status says how many series the feed holds (", 3 recurring"). Override
instances (`RECURRENCE-ID`) are not queued on their own. A series whose last
occurrence is in the past yields nothing. The expansion is capped at 5,000
periods per event so a malformed rule cannot spin.

### `list` sources: aggregators without the noise (2026-09-04)

An `html` source alerts whenever the page's text changes, which for a listing
site is every check. A `list` source instead extracts the links (RSS/Atom
items, CDATA-aware, or every `<a href>` on an HTML page, resolved against the
page address, minus images, mail and script links, first 500) and remembers
them in `seen_uids` keyed on the link. The first check only learns the page
("ok, 47 links remembered"); later checks report the links that were not
there before, title and address, up to 25 per source in the watch email
("ok, 52 links, 5 new"). `match` applies to title and address, so a Cape Town
wide feed can be narrowed to `somerset|strand|gordon|helderberg`. Nothing is
queued as an event; the admin follows the link and adds it if it is worth it.

### Where the current list came from (2026-09-04)

Every entry was fetched before it went in. What is there and what is not:

- **ICS feeds (13).** Added 2026-09-05 now that series expand: NG Kerk
  Gordonsbaai's Google Calendar (2,051 events, 52 series, no future one-offs;
  filtered to services, catechism, youth, seniors, choir, music group, the
  Narcotics Anonymous meeting and fetes, which drops the public holidays and
  the council's internal dates) and the Gordon's Bay Yacht Club "Club &
  Sailing Events" calendar (123 events, 12 series: quiz nights, Think & Drink,
  Sailing Academy courses). The club's other calendar is free/busy only.
  Then the originals: WordPress sites running *The Events Calendar* export at
  `/events/?ical=1`: Vergelegen, Idiom, Helderberg Hospice, Somerset West
  Cricket Club, Gordon's Bay Tourism and DistrictMail (the last two answered an
  empty calendar on the day, and will fill in when they publish), plus the
  filtered regional ones: Western Province Athletics (30 fixtures, road and
  track across the province), trailrunning.co.za, the Western Cape Canoe Union,
  BirdLife Overberg and the Mountain Club of SA Cape Town section (a public
  Google Calendar, ~4 MB, hence the 8 MB cap).
- **List sources (2, added later the same day).** allevents.in's Somerset West
  RSS and capetownmagazine.com/events with a Helderberg `match`. Probed and
  dropped: allevents' Strand feed is Timmendorfer Strand in Germany, its
  Gordon's Bay feed is an empty channel, ShowMe redirects to a directory index.
- **newgen.co.za and the night shelter (added later the same day).** NewGen
  church publishes no calendar; its `/connect/` page (recurring gatherings)
  and home page are watched as `html`. The only homeless shelter in the
  Helderberg with a public presence is the Somerset West Night Shelter; its
  site's *Get involved* and home pages are watched, and its two announced
  fundraisers (Art & Wine Auction, 2 Oct 2026; Golf Day, 3 Dec 2026, both
  Facebook-only events) went in as seed events.
- **Retired (2, 2026-09-05).** The parkrun page watch, replaced by the run's
  news feed as a `list` source (the WAF in front of parkrun.co.za refused the
  bare `HelderbergSocialBot/1.0` User-Agent with a 403 on every page but lets
  the standard `Mozilla/5.0 (compatible; HelderbergSocialBot/1.0; +url)` form
  through, so the bot now sends that; the feed was open either way). That feed
  is how it came out that Somerset West parkrun has been closed indefinitely
  since 29 May 2025. The GBYC calendar page is superseded by the club's ICS
  above.
- **Watched pages (42, two of them retired).** Theatres and music (Playhouse, Drama Factory,
  Helderberg Nature Reserve concerts, Triggerfish), nature (reserve walks and
  talks, Somerset West Bird Club programme and events, Helderberg Farm),
  camping (CapeNature's Kogelberg page and events page, the Kogel Bay campsite
  listing), sport (Helderberg Rugby, the two golf clubs, GBYC calendar, WP
  Cycling calendar), faith (Christ Church, NG Kerk Somerset-Wes and
  Gordonsbaai, Strand Baptist, Liberty, Every Nation, the Catholic parish page
  on the archdiocese site), community (Village Collective what's on, HSFA
  newsletters, helderberg.biz RSS) and the markets and running pages that were
  there before.
- **Left out on purpose.** Eventbrite, Quicket and Webtickets search pages
  render nothing without a browser; Cape Town ETC's feed is news posts, not
  events. (allevents.in and capetownmagazine went in once the `list` kind
  existed, and NG Kerk Gordonsbaai's Google Calendar can go in now that
  recurring series expand.) Facebook-only organisers (Winter Wonderland,
  Skilpad Theatre, Red Sky Brew, most churches) cannot be watched by a server;
  the groups rota in `docs/facebook.md` is how those get covered by hand.
  Stellenbosch-side estates and Overberg-only outings are outside the area,
  and Macassar and Sir Lowry's Pass were dropped from the regional filters
  on 2026-09-04.
- **Dead or empty on the day.** `redskybrew.co.za` and `playhousetheatre.co.za`
  do not resolve, `hendon.co.za` is a JavaScript shell, Somerset Mall's site
  renders nothing without a browser, Quicket's search pages likewise.

Categories were widened at the same time: `sport` (Sport & fitness), `music`
(Music & shows), `faith` (Faith & worship) and `camping` (Camping & outdoors)
join the original eleven in `data/data.js`, `api/validate.go` and the console.
