# Member accounts

Built 2026-09-04. Anyone can create an account, confirm their email address, and
post events. Nothing they post goes live until the admin approves it in the
console, exactly as with the older anonymous email-verified form (which still
works and now points people at accounts first).

## What a member gets

| Page (on the API host) | What it does |
|---|---|
| `/account/register` | name, email, password (10+ characters, not the address, not on the common list). Sends a confirmation link valid 24 h, single use. A second registration with the same address looks identical on screen but emails the owner instead. Honeypot field `website_url`. |
| `/account/verify?t=` | confirms the address and signs the person in. |
| `/account/login` | email + password, or a *Sign in with Google / Microsoft / Yahoo* button per configured provider (see below). Same wording for an unknown address and a wrong password; 8 wrong tries lock the address for 15 minutes (per IP too). An unconfirmed account is told so and offered the confirmation mail again. |
| `/account/<provider>/start` → provider → `/account/<provider>/callback` | *Sign in with …* / *Continue with …* for `google`, `microsoft`, `yahoo`. No password; no confirmation mail when the provider vouches for the address. Details under *Sign in with Google, Microsoft or Yahoo*. |
| `/account/forgot` → `/account/reset?t=` | reset link valid 1 h, single use; saving a new password signs every other device out and signs this one in. |
| `/account` | *My events*: each event with its state (Waiting for a check / Published / Not published, plus "past"), a link to it on the site once published, Edit, Remove (tick-to-confirm). |
| `/account/events/new`, `/account/events/edit?id=` | the event form: title, date, optional end date, times, town, category, cost, website, summary. Same validation as the public form. Saving a new event puts it in the queue as `pending_review` with the member's name and email, and mails the admin the usual approve/reject links. Saving an **edit** of any event, even a published one, sets it back to `pending_review` (and cancels a queued Facebook post) so the change is checked before it shows again. 10 new events per account per day. |
| `/account/settings` | change name, change password (needs the current one; signs other devices out), link or unlink Google / Microsoft / Yahoo, delete the account (needs the password and a tick). A member who came in through Google and has no password can *set* one here without a current one, and deletes with the tick alone. |
| `POST /account/logout` | revokes the session server-side. |

Approving or rejecting in the console emails the member (`member-approved` with the
`events.html?ev=<id>` link, or `member-rejected` with the usual reasons and a
link back to their account). Events with no member (the anonymous form, admin-made,
watcher-found) get no such mail, as before.

Deleting an account, whether the member or the admin does it, removes the member
row, its sessions and its **unpublished** events. **Published** events stay on the
site with `member_id`, `submitter_name` and `submitter_email` cleared: they are
public information about an event, not about the person, and the privacy page says
so. A member who wants a published event gone removes it under *My events* first.

## Sign in with Google, Microsoft or Yahoo

Added 2026-09-05 (Google), extended the same day to Microsoft and Yahoo. One
implementation, `api/oauth.go`: the OpenID Connect authorization-code flow on
the standard library, no provider SDKs. Each provider is off until both
`HS_<PROVIDER>_CLIENT_ID` and `HS_<PROVIDER>_CLIENT_SECRET` are set; the
register and login pages show a button per provider that is on (the console's
*System* page lists all three with on/off, and `/api/health` reports
`"logins": [...]`).

| | Google | Microsoft | Yahoo |
|---|---|---|---|
| Env | `HS_GOOGLE_CLIENT_ID` / `_SECRET` | `HS_MICROSOFT_CLIENT_ID` / `_SECRET` | `HS_YAHOO_CLIENT_ID` / `_SECRET` |
| Redirect URI to register | `https://api.helderbergsocial.co.za/account/google/callback` | `…/account/microsoft/callback` | `…/account/yahoo/callback` |
| Endpoints | accounts.google.com / oauth2.googleapis.com / openidconnect.googleapis.com | login.microsoftonline.com/common (v2.0) / graph.microsoft.com/oidc/userinfo | api.login.yahoo.com |
| PKCE (S256) | yes | yes | no (not documented by Yahoo; the code is still bound to `state` and the cookie) |
| Client auth at the token endpoint | `client_secret` in the body | body | HTTP Basic (Yahoo's documented form) |
| Who vouches for the address | `email_verified` | consumer tenant, or `xms_edov` claim | `email_verified` |

**Setting each one up (once, by the site owner):**

*Google.* Cloud console → a project → *APIs & Services → OAuth consent screen*:
External, app name *Helderberg Social*, support email, homepage
`https://helderbergsocial.co.za`, privacy `https://helderbergsocial.co.za/privacy.html`,
authorised domain `helderbergsocial.co.za`, scopes `openid`, `email`, `profile`
only (non-sensitive, no review). Publish. *Credentials → Create credentials →
OAuth client ID*: **Web application**, authorised redirect URI exactly the one
above. The id ends in `.apps.googleusercontent.com` (the API refuses any other).

*Microsoft.* Entra admin centre (entra.microsoft.com) → *App registrations → New
registration*: name *Helderberg Social*; supported account types **Accounts in
any organizational directory and personal Microsoft accounts** (this is what
makes the `/common` endpoint work for outlook.com as well as work accounts);
Redirect URI platform **Web**, the URI above. Then *Certificates & secrets → New
client secret* (note the expiry: Entra secrets last at most 24 months, put the
date in the calendar). *API permissions*: `openid`, `email`, `profile` (Microsoft
Graph, delegated; already default). *Token configuration → Add optional claim →
ID → `email` and `xms_edov`*: without `xms_edov` every work-account address is
treated as unverified (see below), which is safe but means those people get a
confirmation mail. The client id is the *Application (client) ID* GUID.

*Yahoo.* developer.yahoo.com → *Create an app*: Web application; homepage
`https://helderbergsocial.co.za`; redirect URI the one above; *API permissions →
OpenID Connect permissions*: Email, Profile. Confidential client. Yahoo shows the
Client ID (`dj0y…`) and Client Secret once.

For each: put the pair in `api/.env` on the Docker host and redeploy
(`bash api/deploy.sh`). The API refuses to start with only one of a pair set.

**How a sign-in resolves.** `/account/<p>/start` writes a signed 10-minute cookie
(`hs_oauth`, path `/account`) holding a random `state`, the PKCE verifier, the
mode (sign-in or link) and the return path, and sends the browser to the
provider with `prompt=select_account` where supported. `/account/<p>/callback`
checks `state` against the cookie (constant time), redeems the code at the
provider's token endpoint server-side with the client secret (and the verifier),
reads the ID token's payload (no signature check: the token came straight from
the provider over TLS, which OpenID Connect Core 3.1.3.7 allows) and the userinfo
endpoint, and merges them (ID token wins; the subjects must agree). The cookie is
cleared either way. Then, in order:

1. a member with that `(provider, sub)` in `member_identities` → signed in;
2. else, if the provider **vouched for the address**, a member with that email
   address → the identity is attached to it, and if the address was never
   confirmed by our own mail the provider's word counts (`verified_at` set);
3. else, if the address is **not** vouched for and a member with it exists →
   refused with "sign in with your password, then link under Account". This is
   the whole defence against the Microsoft "nOAuth" problem: an Entra admin can
   put anyone's address on a work account, so an unverified address never
   attaches itself to an existing account;
4. else a new member: no password (`pw_hash = ''`), name from the provider (or
   the local part of the address), `status = active`, `registrations_on` and the
   email blocklist applying exactly as to the register form. Vouched-for address
   → `verified_at` set and signed in at once. Not vouched for → created
   unconfirmed, our usual confirmation mail is sent, and the person is told to
   open it; the provider button signs them in only once they have;
5. a `disabled` member is refused in every case.

What "vouched for" means per provider: Google and Yahoo send
`email_verified: true` only for addresses they have confirmed. Microsoft sends no
such claim; the address counts as verified when the ID token's `tid` is the
consumer tenant `9188040d-6c67-4c5b-b112-36a304b66dad` (a personal Microsoft
account, whose address is its login) or when `xms_edov` is true (the app
registration asks for it and the domain is verified in that tenant).

**Linking from Account.** A signed-in member can press *Link Google / Microsoft /
Yahoo* under *Other ways to sign in* (`/account/<p>/start?link=1`). Because the
person is already authenticated, the provider account is attached whatever
address it carries; a provider account already attached to a different member is
refused. Each linked provider shows with an *Unlink* button, offered only while
another way in remains (a password or another provider), so nobody can lock
themselves out. Audit rows: `member.oauth_login|link|register|
register_unverified|unlink|fail|email_differs`, with the provider key in the
detail.

**Members without a password.** Password sign-in with such an address gets the
same "wrong email address or password" as everyone else (no enumeration). Under
*Account* they can set a first password without a current one, or use the reset
link; after that both ways in work. Admin *disable* stops provider sign-in too
(the session check and the callback both read `status`).

## Promoter accounts

A member can apply, from **Promote with us** in the account nav, to become a
promoter: post many events, schedule and hide them, run noticeboard posts, import
.ics/.csv files, connect public calendars and submit listings, all under an
organisation name. The admin approves, and separately *trusts*, each one. The
whole model, routes, limits, console side and tests are in `docs/promoters.md`
(added 2026-09-05).

## What the admin sees

- **Members** in the console nav: counts (confirmed / unconfirmed / disabled /
  events posted by members), filter and search, and per member: resend or force
  the confirmation, disable (signs them out everywhere and refuses sign-in; events
  untouched), enable, sign out everywhere, delete, block-and-delete (stores the
  address hash on the blocklist, so it cannot register or submit again).
- The per-member page lists their events with the normal event cards, so approving
  from there works too.
- Event cards everywhere (queue, events, member page) carry a `member #N` pill that
  links to the member.
- The dashboard has a members card.
- **Settings → Accept new member accounts** (`registrations_on`, default on) pauses
  registration; existing members keep working. **Accept public submissions**
  (`submissions_on`) also stops members posting. **Maintenance mode** answers every
  account form except sign-out with the maintenance page.

## Storage

```
members            id, email UNIQUE, name, pw_hash ('' = provider only), created_at, verified_at,
                   last_login_at, status (active|disabled), ip_hash
member_identities  (provider, sub) PK, member_id (CASCADE), email, linked_at; index on member_id
member_sessions    id_hash PK, member_id, created_at, last_seen_at, expires_at, ip_hash, ua, revoked
events.member_id INTEGER (NULL for everything not posted from an account), index events_member
members.role, members.trusted, promoters, posts, events.visible_from/hidden/promoted,
sources.member_id, listing_submissions.member_id   (promoters, docs/promoters.md)
```

Schema version 9 (5 added `events.member_id`, 6 added `sources.match`, 7 widened
`sources.kind` to allow `list` by rebuilding the table, and added `fb_groups`
for the Facebook groups rota in `docs/facebook.md`; 8 added `members.google_sub`;
9 replaced it with `member_identities`, copying any linked Google row across and
dropping the column; 10 added the promoter tables and columns above);
`migrate()` adds the missing columns to an older database on start-up. Housekeeping (hourly) deletes unconfirmed members after 3 days and
expired or revoked member sessions after a day. The 90-day scrub of
`submitter_name`/`submitter_email` on decided events applies to member events too;
the `member_id` link stays, so the console still shows who posted it.

## Security notes

- Passwords: Argon2id, 19 MiB / 2 passes / 1 lane / 16-byte salt / 32-byte key,
  encoded PHC-style with the parameters, so raising them later is a one-line
  change and old hashes are re-hashed on the next successful sign-in. Verification
  is constant-time. An unknown address is checked against a dummy hash so the
  response time does not reveal whether it exists.
- Session cookie `hs_member`: 256-bit random value, only its SHA-256 stored,
  `HttpOnly; Secure; SameSite=Lax; Path=/account`, 30 days absolute and 7 days
  idle, revoked on sign-out, password change, reset, disable and delete. Idle
  clock refreshed at most once a minute.
- CSRF: every signed-in form carries `HMAC(secret, "member-csrf:" + session hash)`
  and the handler compares in constant time before doing anything.
- Rate limits: register / login / forgot / reset / resend share the tight 6-a-minute
  bucket with the admin sign-in steps; the signed-in forms use the 120-a-minute
  bucket, so editing a few events in a row never trips a 429 (a test caught that).
- Nothing in the account area needs JavaScript; the API's CSP stays
  `default-src 'none'; style-src 'unsafe-inline'`.
- Emails to members are the same DKIM-signed direct-MX mail as everything else,
  with the `member-*` kinds in the mail log.
- The audit log records `member.register`, `member.verified`, `member.login`,
  `member.login_fail`, `member.login_locked`, `member.reset_sent`,
  `member.reset_done`, `member.password`, `member.event_new`, `member.event_edit`,
  `member.event_withdraw`, `member.delete` and the admin's `member.disable` /
  `member.enable` / `member.verify_admin` / `member.block` / `member.signout`.
  Promoter kinds are listed in `docs/promoters.md`.

## Site side

The site's header gained a **Post an event** button and the footer *Get involved*
column a **Post an event** link and **Sign in / my account**, all pointing at the
API host (`HS.accountURL()` in `assets/js/site.js`; omitted on a copy with no
`apiBase`). The admin console has its own, deliberately quiet, way in: a faint
middle dot after the copyright line in the footer (`.console-link`, 35% opacity,
full on hover, `rel="nofollow"`) links to `/admin/login` on the API host. It is
not a secret, the console has its own sign-in and second factor; it is just kept
out of visitors' way (added 2026-09-05). `events.html` has a "Post your event" line, and `submit.html` shows a
notice above the form when *A dated event* is chosen, pointing at the account
route while still allowing the old confirm-by-email path. Since 2026-09-05 every
**Add a listing** / **Post an event** entry point carries a `?` help bubble
(`HS.help`) explaining what a listing and an event are, how to submit one and
what happens next, and the footer, home band and events page link to
`promote.html`.

## Tests

`api/members_test.go`: register → confirm (link single-use) → post (CSRF required)
→ pending, not in the public feed → admin approves from the console → member
mailed → in the feed → edit returns it to review → another member cannot edit,
withdraw or open it → reject mails the member → withdraw needs the tick → logout
kills the session. Lockout after 8 failures, identical wording for unknown address
and wrong password, reset flow (weak password refused, token single-use, old
sessions and old password dead). Registration rules (short/common/mismatched
passwords, honeypot, duplicate address silent, unconfirmed cannot sign in,
`registrations_on=0`, blocklist). Admin disable/enable/view, self-deletion needs
the password and anonymises the published event while deleting the pending one.
Argon2id round trip, salt, garbage hash, stale detection.

`api/oauth_test.go` runs the whole flow against a fake provider
(`httptest.Server` swapped into `oauthProviders[i]`'s endpoints) that checks the
client credentials (body or Basic), the PKCE verifier against the challenge it
was shown, and the redirect URI, and can serve an ID token: new member via
Google (verified, no password, signed in, second sign-in reuses the row,
console pill and System panel, health `logins`); linking to an existing password
account by address, linking Yahoo from Account under a different address
(Basic auth, no PKCE), the same Yahoo account refused for a second member,
unlink rules; Microsoft's verification rule (consumer tenant straight in; work
account without `xms_edov` created unconfirmed with our mail, refused a second
sign-in until confirmed, refused attaching to an existing member's address;
with `xms_edov` linked); refusals (wrong state before any token call, no
cookie, cancelled, rejected code, a cookie from another provider, no address,
unverified Google address → confirmation mail, registrations paused for new but
not existing members, disabled, blocklisted); the provider-only member's
settings; everything hidden while off and unknown providers 404; config
validation for all three; migration from v7 and v8 databases (google_sub rows
carried into `member_identities`, column dropped).
