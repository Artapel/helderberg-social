# Promoter accounts

Added 2026-09-05. A **promoter** is a member whose account has been upgraded so
they can publish on the site themselves: organisers, venues, clubs, schools and
churches, marketers, media and creators. The public explanation is
`promote.html`; the account area is `/account/promoter` on the API host.

Everything here is server-rendered inside the member area (`api/promoters.go`,
templates in `api/promoters_tmpl.go`), so it needs no JavaScript and the API's
CSP is unchanged. The console side lives in `api/console.go` /
`api/members_console.go`.

## The model

```
promoters   member_id PK (CASCADE), org, kind, website, facebook, instagram,
            towns (JSON list of town keys), blurb, status (pending|approved|declined),
            applied_at, decided_at, note
members     role ('member'|'promoter'), trusted (0|1)
```

- A member applies once (`POST /account/promoter/apply`); the row is `pending`
  and the admin gets an `admin-promoter` mail. A declined member may apply again;
  the previous note is shown to them.
- **Approve** sets `members.role='promoter'`. Their events, posts, imports and
  calendar events all go through the normal moderation queue, exactly like a
  member's, with a *promoter* pill so the admin knows who it is.
- **Trust** (`members.trusted=1`) is the second step and is deliberately a
  separate button: a trusted promoter's events and posts publish the moment they
  are saved (status `approved`, `decided_at` set, no admin mail), and edits go
  live at once. Trusted items are never auto-queued to the Facebook page; that
  stays the admin's call.
- **Revoke** puts the member back to `role='member'`, `trusted=0`, moves every
  approved event and post of theirs back to `pending_review`, and disables their
  calendar sources. Nothing is deleted. **Decline** on a pending application
  mails the reason.
- `Member.IsPromoter()` gates every `/account/promoter/*` route
  (`requirePromoter`); an approved promoter who is later disabled as a member
  loses access with the member session.

## What a promoter can do

| Tool | Route | Rules |
|---|---|---|
| Events | the normal `/account/events/*` forms, plus **Show on the site from** (`events.visible_from`) and a **Hide / Show** toggle (`events.hidden`) | `visible_from` must be on or before the event date; a hidden event keeps its approval and comes straight back when shown. Limit `promoterEventsDay` = 60 new events a day (imports count), members 10. Public feed marks them `promoted` with `by` = the org name. |
| Posts (noticeboard) | `/account/promoter/posts/new`, `/edit`, `/toggle`, `/delete` | Title, body (≤ 600 chars), optional link, town, category, `starts`/`ends` (run ≤ 90 days, `postMaxRun`). 20 new posts a day. Same review/trust rules as events; the member gets `member-approved` / `member-rejected` mail. Live when approved, not hidden, `starts` ≤ today + 30 days and `ends` ≥ today. |
| Import | `/account/promoter/import` (multipart, ≤ 2 MB) | `.ics` (VEVENTs; recurring rules expand to the first upcoming occurrence with the repeat pattern in the text; `LOCATION` becomes "Venue: …") or `.csv` with header columns `title,date,summary,end_date,time,end_time,town,category,cost,website`. Rows are validated with the same `eventProblem` as the form, deduplicated on (member, lower(title), date), previewed, then confirmed with a signed single-use token (30 min, purpose `import:<memberID>`). The admin gets one `admin-event` summary mail per batch. |
| Connected calendars | `/account/promoter/calendars/add`, `/remove`, `/check` | Up to 3 `sources` rows with `member_id` set. The URL must be `https`, with no port and a host name, and the host must resolve to a public address (loopback, private, link-local, multicast and unspecified ranges are refused; redirects are re-checked and capped at 5). The watcher fetches them with everything else; events it finds are stamped with the promoter and follow the trust rule. Remove also forgets the seen UIDs. |
| Listings | `/account/promoter/listing` | Lands in `listing_submissions` with `member_id` and the org as submitter; listings stay curated in `data.js`, so the admin still pastes the block. |

## Console

- Nav **★ Promoters** (`/admin/promoters`): applications, approved and declined,
  with approve / decline (with reason) / trust / stop trusting / revoke.
- The queue has **Posts** and **Applications** sections; the dashboard has cards
  for both.
- The member page shows the promoter panel, their posts and their calendars.
- Events table pills: *hidden*, *from <date>*, *promoter*. Sources page shows an
  *Org* pill on member calendars.
- Actions: `post-approve`, `post-reject`, `post-unapprove`, `post-delete`,
  `promoter-approve|decline|revoke|trust|untrust`.
- Audit kinds: `promoter.apply`, `promoter.approve|decline|revoke|trust|untrust`,
  `promoter.post_new|post_edit|post_toggle|post_delete`, `promoter.import`,
  `promoter.calendar_add|calendar_remove`, `promoter.listing`,
  `member.event_toggle`, `post.approve|reject|delete|unapprove`.

## Public side

- `GET /api/posts` → `{ok, posts:[{id, title, body, link, town, category, starts, ends, by}], generated}`,
  `Cache-Control: public, max-age=300`. `notices.html` renders it with town and
  category filters; `?post=<id>` scrolls to and outlines one post (the permalink
  in the promoter's mail). The home page shows the first three under **On the
  noticeboard** when there are any.
- `GET /api/events` items carry `promoted: true` and `by: "<org>"` for promoter
  events; `HS.eventRow` shows a *From <org>* pill.
- `promote.html` explains the offer and sends people to
  `/account/register?next=/account/promoter` (or straight to the promoter page
  when signed in).
- Help bubbles (`HS.help("listing"|"event")`, CSS `.help`) sit next to every
  **Add a listing** and **Post an event** entry point (header, footer, events
  page, home band, submit page) and say what each is, how to use it and what it
  is for; hover, tap or keyboard focus opens them, Escape closes.

## Mail kinds

`admin-promoter`, `promoter-approved`, `promoter-declined`, `admin-post`,
`admin-event` (import summary), and the existing `member-approved` /
`member-rejected` for post decisions.

## Schema

Version 10: tables `promoters` and `posts`; `members.role`, `members.trusted`;
`events.visible_from`, `events.hidden`, `events.promoted`; `sources.member_id`;
`listing_submissions.member_id`. `migrate()` adds them to a v9 database on
start-up; `TestMigratePromotersV9ToV10` in `api/promoters_test.go` builds a
v9-shaped database and checks the upgrade, because the 2026-09-05 outage came
from an index created outside `migrate()`.

## Tests

`api/promoters_test.go`: apply → admin mail → decline with note → re-apply →
approve → post an event (pending, then trust → publishes at once, hide/show,
schedule refused past the date) → posts (limits, live window, permalink, reject
mail) → ICS and CSV import (preview, dedupe, oversize refused, junk refused,
confirm) → calendars (SSRF refusals, add, watcher stamps and publishes for a
trusted promoter, remove) → listing submission → revoke (items back to review,
calendars disabled) → v9 migration. Run `go test ./...` in `api/`.
