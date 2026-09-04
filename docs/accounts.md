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
| `/account/login` | email + password. Same wording for an unknown address and a wrong password; 8 wrong tries lock the address for 15 minutes (per IP too). An unconfirmed account is told so and offered the confirmation mail again. |
| `/account/forgot` → `/account/reset?t=` | reset link valid 1 h, single use; saving a new password signs every other device out and signs this one in. |
| `/account` | *My events*: each event with its state (Waiting for a check / Published / Not published, plus "past"), a link to it on the site once published, Edit, Remove (tick-to-confirm). |
| `/account/events/new`, `/account/events/edit?id=` | the event form: title, date, optional end date, times, town, category, cost, website, summary. Same validation as the public form. Saving a new event puts it in the queue as `pending_review` with the member's name and email, and mails the admin the usual approve/reject links. Saving an **edit** of any event, even a published one, sets it back to `pending_review` (and cancels a queued Facebook post) so the change is checked before it shows again. 10 new events per account per day. |
| `/account/settings` | change name, change password (needs the current one; signs other devices out), delete the account (needs the password and a tick). |
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
members          id, email UNIQUE, name, pw_hash, created_at, verified_at, last_login_at,
                 status (active|disabled), ip_hash
member_sessions  id_hash PK, member_id, created_at, last_seen_at, expires_at, ip_hash, ua, revoked
events.member_id INTEGER (NULL for everything not posted from an account), index events_member
```

Schema version 6 (5 added `events.member_id`, 6 added `sources.match`);
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
`apiBase`). `events.html` has a "Post your event" line, and `submit.html` shows a
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
