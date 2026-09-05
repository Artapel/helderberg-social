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
| `/account/login` | email + password, or the *Sign in with Google* button (see below). Same wording for an unknown address and a wrong password; 8 wrong tries lock the address for 15 minutes (per IP too). An unconfirmed account is told so and offered the confirmation mail again. |
| `/account/google/start` → Google → `/account/google/callback` | *Sign in with Google* / *Continue with Google*. No password, no confirmation mail: Google has already confirmed the address. Details under *Sign in with Google*. |
| `/account/forgot` → `/account/reset?t=` | reset link valid 1 h, single use; saving a new password signs every other device out and signs this one in. |
| `/account` | *My events*: each event with its state (Waiting for a check / Published / Not published, plus "past"), a link to it on the site once published, Edit, Remove (tick-to-confirm). |
| `/account/events/new`, `/account/events/edit?id=` | the event form: title, date, optional end date, times, town, category, cost, website, summary. Same validation as the public form. Saving a new event puts it in the queue as `pending_review` with the member's name and email, and mails the admin the usual approve/reject links. Saving an **edit** of any event, even a published one, sets it back to `pending_review` (and cancels a queued Facebook post) so the change is checked before it shows again. 10 new events per account per day. |
| `/account/settings` | change name, change password (needs the current one; signs other devices out), unlink Google, delete the account (needs the password and a tick). A member who came in through Google and has no password can *set* one here without a current one, and deletes with the tick alone. |
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

## Sign in with Google

Added 2026-09-05. Standard OpenID Connect authorization-code flow with PKCE
(S256), written against the standard library; no Google SDK. Off until both
`HS_GOOGLE_CLIENT_ID` and `HS_GOOGLE_CLIENT_SECRET` are set, and the register and
login pages simply do not show the button while it is off (the *System* page in
the console says on/off, and `/api/health` reports `"google"`).

**Setting it up (once, in the Google Cloud console, by the site owner):**

1. Create a project (any name, e.g. *Helderberg Social*).
2. *APIs & Services → OAuth consent screen*: **External**; app name *Helderberg
   Social*; support email; homepage `https://helderbergsocial.co.za`; privacy
   policy `https://helderbergsocial.co.za/privacy.html`; authorised domain
   `helderbergsocial.co.za`. Scopes: only `openid`, `email`, `profile` (all
   non-sensitive, so no verification review is needed). Publish the app; in
   *Testing* status only listed test users can sign in.
3. *Credentials → Create credentials → OAuth client ID*: type **Web
   application**; authorised redirect URI exactly
   `https://api.helderbergsocial.co.za/account/google/callback`. Authorised
   JavaScript origins are not needed (nothing runs in the browser).
4. Put the client id (ends in `.apps.googleusercontent.com`) and the client secret
   in `api/.env` on the Docker host as `HS_GOOGLE_CLIENT_ID` / `HS_GOOGLE_CLIENT_SECRET`
   and redeploy (`bash api/deploy.sh`). The API refuses to start with only one of
   the two set, or with an id that does not end in `.apps.googleusercontent.com`.

**How a sign-in resolves.** `/start` writes a signed 10-minute cookie
(`hs_oauth`, path `/account/google`) holding a random `state`, the PKCE verifier
and the return path, and sends the browser to Google with `prompt=select_account`.
`/callback` checks `state` against the cookie (constant time), redeems the code at
Google's token endpoint server-side with the client secret **and** the verifier,
and reads `sub`, `email`, `email_verified`, `name` from the userinfo endpoint. The
cookie is cleared either way. Then, in order:

1. a member with that Google `sub` → signed in;
2. else a member with that **email address** → the Google account is linked to it
   (`google_sub` set) and, if the address was never confirmed by our own mail,
   Google's confirmation counts (`verified_at` set). This is safe because Google
   only returns `email_verified: true` for addresses it has confirmed itself, and
   we refuse anything else;
3. else a new member: `pw_hash = ''` (no password), `verified_at` set, `status =
   active`, name from Google (or the local part of the address). `registrations_on`
   and the email blocklist apply exactly as to the register form;
4. a `disabled` member is refused in every case.

A cancelled consent screen, a bad `state`, a missing or stale cookie, a rejected
code, an unverified Google address and a paused registration each land back on
the login page with a plain message and an audit row (`member.google_fail` with
the reason; successes audit `member.google_login|link|register`). If Google's
address for a linked `sub` no longer matches ours (a Workspace rename) ours is
kept and `member.google_email_differs` is audited.

**Members without a password.** Password sign-in with such an address gets the
same "wrong email address or password" as everyone else (no enumeration). Under
*Account* they can set a first password without a current one, or use the reset
link; after that both ways in work. *Unlink Google* is only offered once a
password exists, so nobody can lock themselves out. Admin *disable* stops Google
sign-in too (the session check and the callback both read `status`).

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
members          id, email UNIQUE, name, pw_hash ('' = Google only), created_at, verified_at,
                 last_login_at, status (active|disabled), ip_hash, google_sub UNIQUE (NULL when not linked)
member_sessions  id_hash PK, member_id, created_at, last_seen_at, expires_at, ip_hash, ua, revoked
events.member_id INTEGER (NULL for everything not posted from an account), index events_member
```

Schema version 8 (5 added `events.member_id`, 6 added `sources.match`, 7 widened
`sources.kind` to allow `list` by rebuilding the table, and added `fb_groups`
for the Facebook groups rota in `docs/facebook.md`; 8 added `members.google_sub`
with the unique index `members_google`);
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
route while still allowing the old confirm-by-email path.

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

`api/google_test.go` runs the whole Google flow against a fake Google
(`httptest.Server` behind `googleEndpoints`) that checks the PKCE verifier
against the challenge it was shown, the client secret and the redirect URI:
new member via Google (verified, no password, signed in, second sign-in reuses the
row, console pill and System panel, health flag); linking to an existing
password account by address (password still works, unlink); Google confirming a
never-confirmed account; refusals (wrong state before any token call, no cookie,
cancelled, rejected code, unverified address, registrations paused for new but
not existing members, disabled, blocklisted); the Google-only member's settings
(set first password without a current one, unlink refused without a password,
delete with the tick alone); everything hidden while off; config validation.
