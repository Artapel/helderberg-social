# Facebook page

The page: <https://www.facebook.com/profile.php?id=61594290261232>. It is linked from the site
footer (`site.social.facebook` in `data/data.js`) and every page carries Open Graph tags plus
`assets/img/og.png`, so a shared link renders as a full-width card.

Images (rendered from the HTML in `docs/social/` with headless Chrome, sizes exact):

| File | Use | Size |
|---|---|---|
| `docs/social/facebook-profile.png` | Profile picture (shown as a circle) | 720 x 720 |
| `docs/social/facebook-cover.png` | Cover photo (phones crop to the middle 1250 px, all text is inside that) | 1640 x 624 |
| `assets/img/og.png` | Link preview image for every shared page | 1200 x 630 |

Profile picture and cover uploaded 2026-09-04 (Facebook auto-posted "updated their cover photo").

To re-render after a change: open the `.html` in `docs/social/` with Chrome headless at the size
in its `<title>` and `--screenshot`.

## Page setup (fields in Facebook, in the order the page editor shows them)

- **Name:** Helderberg Social. *Still "Helderberg Socials, Events and Activities" (2026-09-04);
  a rename goes through Meta review, Corne's call.*
- **Username / vanity URL:** `helderbergsocial` (DONE 2026-09-04; https://www.facebook.com/helderbergsocial
  resolves; the site footer links it)
- **Category:** Community Organization + Local & travel website (DONE 2026-09-04; Facebook has no
  plain "Community" or "Website" category)
- **Bio (101 chars max, DONE 2026-09-04):** Groups, clubs, markets, trails and what's on this weekend in Somerset West, Strand & Gordon's Bay.
- **About / description (DONE 2026-09-04, with "email or WhatsApp"):** Helderberg Social is a free, community-run guide to the Helderberg:
  running and cycling clubs, hiking groups, weekend markets, wine estates, beaches, residents'
  associations and the events happening this weekend across Somerset West, Strand, Gordon's Bay
  and Sir Lowry's Pass. Anyone can add a group, place or event on the website; every listing is
  checked before it goes live. Get a weekly email of what's on at helderbergsocial.co.za.
- **Website:** https://helderbergsocial.co.za
- **Location:** Somerset West, Western Cape (no street address; do not show one). *A home
  street address, personal mobile and Daisy work email were public on the page until
  2026-09-04; all three removed.*
- **Service area:** Somerset West, Strand, Gordon's Bay (DONE 2026-09-04). Sir Lowry's Pass is
  not in Facebook's place list, so it cannot be added.
- **Email / phone:** leave blank (no phone; the site has no contact address yet, see
  `site.contactEmail` in data.js)
- **Action button:** "Learn more" -> https://helderbergsocial.co.za (DONE 2026-09-04; replaced a
  WhatsApp button that pointed at Corne's personal number)
- **Hours:** Always open (it is a website)
- **Privacy:** post visibility Public; profile visibility Public; page transparency on
- **Settings (DONE 2026-09-04):** Followers and public content -> "Hide posts and comments with
  profanity" ON. Page and tagging -> "Who can post on your Page" = Only me (visitors comment,
  they do not post); reviews OFF; "Review posts you're tagged in" ON. There is no "hide comments
  with links" switch in the current settings UI; the keyword filter ("Hide comments containing
  certain words") is left off.
- **Messaging (DONE 2026-09-04):** Meta Business Suite -> Inbox -> Automations -> "Auto reply"
  is ON with: "Thanks for the message. Helderberg Social is run by volunteers so replies can take
  a day or two. To add a group, place or event go to helderbergsocial.co.za/submit.html - it is
  free." (Business Suite silently created the automation with its default text on the first
  toggle click and refused the edit until the page was reloaded with the automation id in the
  URL; if "Change not saved" appears, reload and edit again.)
- **Tabs:** the current Pages UI has no tab picker; Reviews is hidden by the reviews switch
  above and no Shop exists.

## First posts (in this order, one per day)

1. **Welcome (pinned).** DONE 2026-09-04 16:17 (text below, with the site link card; the earlier 07:45 group post was edited the same day to end with the website link).
   Hello Helderberg. This page goes with helderbergsocial.co.za, a free community guide to
   groups, clubs, markets, trails, wine estates and what's on this weekend across Somerset West,
   Strand, Gordon's Bay and Sir Lowry's Pass. It is run by residents, not a business, and every
   listing is free. Know a club or event we have missed? Add it here:
   https://helderbergsocial.co.za/submit.html
   *(attach `assets/img/og.png`)*

2. **This weekend.** Take the top three or four events from https://helderbergsocial.co.za/events.html
   and list them as "Fri · Sat · Sun" lines with a link to the events page. Mark anything not yet
   verified as "(check with the organiser)".

3. **Add your group.** Run a club, class, market or community group in the Helderberg? Add it to
   Helderberg Social for free. It takes two minutes, you get an email to confirm, and it goes live
   once we have checked it. https://helderbergsocial.co.za/submit.html

4. **Weekly email.** Rather have it in your inbox? A short Thursday email with the weekend's
   events, nothing else, unsubscribe in one click: https://helderbergsocial.co.za/subscribe.html
   *(only once the API is live, see `docs/deploy.md`)*

Then a steady rhythm: Thursday "this weekend" post, one listing spotlight per week (link to its
page on the site so the OG card renders), and the site's own new events as Facebook Events with
the same details. Tag the town in each post ("Strand", "Gordon's Bay") for local reach.

## Groups: posting in every group the page belongs to (built 2026-09-04)

The page profile has joined **87 Facebook groups** (one more, a rentals group, is still
pending), from the big community boards (Gordon's Bay, Somerset West, Strand, HELDERBERG
CONNECT) to markets, parents, hikers, cyclists, church and business groups. The aim is one post
in every group, then one a month in each. Two hard facts shape how that is done:

- Meta's Graph API **cannot post in groups** (the Groups API was removed in April 2024).
- Driving the browser by script is against Facebook's terms and gets the page restricted.

So the API is a **planner**, not a poster. The shipped list is `api/fb-groups.json` (upserted by
`fb_id` on start; the admin's cadence, on/off and history are kept), the table is `fb_groups`,
and the console page **Facebook -> Groups** (`/admin/facebook/groups`) shows what is due, the
prepared text for each group, and the full rota. The daily batch is also emailed (setting
`Facebook groups: daily reminder email`, default on, 08:00, 4 groups a day, never on Sundays)
as a short nudge: the group names, kinds, notes and links, and one link to the console page.
The post text is **not** in the mail. The first version carried it for every group (four
near-identical blocks with a dozen links and "free", "ADS" and "Marketplace" in every one) and
Gmail filed it as spam on 2026-09-05; the text differs per group and is copied from the
console anyway.

**Posting a batch (a person does this, 5 minutes a day):**

1. Open the group link from the console or the email.
2. In the composer switch the profile to **Helderberg Social** (post *as the page*, not as
   yourself). Read the group's rules; a group that forbids promotion gets a shorter, friendlier
   version, or nothing.
3. Paste the text. It is built per group: a lead paragraph by kind (community boards get a
   neighbourly intro; markets and business groups get "not a sale, a free resource"; parents',
   hikers' and cyclists' groups lead with their kind of event), up to 8 approved events in the
   next 30 days scored by the group's town and interests, and links to `/events.html`,
   `/submit.html?kind=event` and `/subscribe.html`.
4. Press **Mark posted** in the console. The group is booked again after its cadence (30 days
   by default, 7-120 allowed per group). **Later** pushes it out a week; **Switch off** with a
   reason takes it off the rota without losing its history.

Groups that ship switched off: *What's On West Somerset* (it is Somerset in the UK) and the
rentals group (join still pending). Macassar and Sir Lowry's Pass groups were left out of the
list on Corne's instruction (2026-09-04).

### More groups worth joining (found 2026-09-04, NOT joined yet)

Searched as the page for Somerset West, Strand, Gordon's Bay and Helderberg groups; 185 raw
hits curated to the 47 below. Joining is an outward-facing action and needs a go-ahead per
batch; a private group's join questions must be answered by hand. Add each joined group to the
console (or `api/fb-groups.json`) so it enters the rota.

**Tier A: community / what's-on / events / nature (recommended)**

| Group | Facebook | Size |
|---|---|---|
| Gordon's Bay | `https://www.facebook.com/groups/gordonsbay/` | Public 70K |
| Strand Community Information | `https://www.facebook.com/groups/254613258524095/` | Public 64K |
| Somerset West Community | `https://www.facebook.com/groups/somersetwest1/` | PRIVATE 52K (join request, may ask questions) |
| Gordonsbay Community | `https://www.facebook.com/groups/313891233041632/` | Public 39K |
| HELDERBERG CONNECT | `https://www.facebook.com/groups/3629705530503011/` | Public 37K |
| Gordon's Bay, Western Cape, South Africa | `https://www.facebook.com/groups/434603406886641/` | Public 20K |
| GORDON'S BAY Community | `https://www.facebook.com/groups/GORDONSBAYCOMMUNITY/` | Public 17K |
| This is Gordon's Bay and Strand | `https://www.facebook.com/groups/thisisgordonsbay/` | Public 12K |
| Strand Community | `https://www.facebook.com/groups/284941500201097/` | Public 10K |
| GORDON'S BAY | `https://www.facebook.com/groups/1466793730292518/` | Public 10K |
| Se jou Se - Die strand | `https://www.facebook.com/groups/979684890585480/` | Public 10K |
| Helderberg Ocean Awareness Movement | `https://www.facebook.com/groups/604855949952137/` | Public 9.5K |
| Strand group | `https://www.facebook.com/groups/376079167217610/` | Public 7.2K |
| Strand.Western Cape | `https://www.facebook.com/groups/106916612351988/` | Public 6.3K |
| Gordons Bay Shop talk. | `https://www.facebook.com/groups/gbmeetngreet/` | Public 5.7K |
| Upcoming markets in the Helderberg and Stellenbosch | `https://www.facebook.com/groups/3102730903191234/` | Public 4.8K |
| Gordon's Bay Attractions | `https://www.facebook.com/groups/735404033773779/` | Public 4.6K |
| Gordon's bay | `https://www.facebook.com/groups/1170204483812342/` | Public 4.2K |
| Somerset nd strand Gordon's bay | `https://www.facebook.com/groups/5043714199046569/` | Public 3K |
| Gordon's bay group | `https://www.facebook.com/groups/441055555340159/` | Public 2.7K |
| Helderberg Gemeente gesels lekker | `https://www.facebook.com/groups/667393276698474/` | Public 2.1K |
| Gordon's Bayers | `https://www.facebook.com/groups/637877397456908/` | Public 2K |
| Gordon's Bay Care Group | `https://www.facebook.com/groups/1723077057959411/` | Public 1.9K |
| Helderberg Yard Sale, Markets & Events | `https://www.facebook.com/groups/1052127465699364/` | Public 1.4K |
| Gordon's Bay Neighbours | `https://www.facebook.com/groups/1708482875906216/` | Public 1.1K |
| What to do in Gordon's bay | `https://www.facebook.com/groups/471537390934985/` | Public 975 |
| Somerset West Bird Club Group | `https://www.facebook.com/groups/555030771841728/` | Public 942 |
| Somerset West Holistic / Natural | `https://www.facebook.com/groups/267558530099471/` | Public 810 |
| Somerset West Cape Town | `https://www.facebook.com/groups/124755377598692/` | Public 758 |
| Gordon's Bay Moms | `https://www.facebook.com/groups/2849601105346097/` | Public 364 |
| HELDERBERG EVENTS | `https://www.facebook.com/groups/365495583633624/` | Public 150 |

**Tier B: advertising / business groups (same kind as several already joined)**

| Group | Facebook | Size |
|---|---|---|
| Gordon's Bay/Strand 1 | `https://www.facebook.com/groups/gordonbay1/` | PRIVATE 102K |
| Somerset West Business Services (Western Cape, South Africa only!) | `https://www.facebook.com/groups/599676986832992/` | Public 25K |
| Advertise We Can - Helderberg Basin & Stellenbosch | `https://www.facebook.com/groups/523249271818385/` | Public 17K |
| Helderberg Business Adverts | `https://www.facebook.com/groups/helderbergbusinessadverts/` | Public 15K |
| Our Helderberg Businesses | `https://www.facebook.com/groups/ourhelderberg/` | Public 13K |
| Helderberg Small Business Hub | `https://www.facebook.com/groups/263871911437043/` | Public 11K |
| Somerset West/Strand/Greenways/Gordon's Bay/Kleinbos Ads Exec | `https://www.facebook.com/groups/739286007100690/` | Public 9.2K |
| #ADS in Gordons Bay (Western Cape) | `https://www.facebook.com/groups/1613005609026696/` | Public 8.6K |
| Gordons Bay Business Adverts | `https://www.facebook.com/groups/1446655952279923/` | Public 7.7K |
| Somerset west business group | `https://www.facebook.com/groups/seeke/` | Public 7.5K |
| Advertise your Business - STRICTLY Helderberg | `https://www.facebook.com/groups/1611723175731016/` | Public 6.4K |
| Business Services - Helderberg & Surrounding Areas | `https://www.facebook.com/groups/helderbergservices1/` | Public 6K |
| Gordonsbay Business | `https://www.facebook.com/groups/908203736604663/` | Public 4.2K |
| Somerset West Advertising | `https://www.facebook.com/groups/1127589942594965/` | Public 1.5K |
| Kleinmond to Somerset West Business Club | `https://www.facebook.com/groups/444035468224122/` | Public 1.9K |
| Gordon's Bay Advertising | `https://www.facebook.com/groups/787944077509266/` | Public 462 |

**Skipped: Macassar and Sir Lowry's Pass (Corne), UK Somerset groups, Somerset East, jobs, rentals/property, pets, crime/security, niche sales (games, beauty, perfume, cars), township-specific Strand groups, singles/gay social (ask if wanted)**

| Group | Facebook | Size |
|---|---|---|

**Tier C: hobby groups, Cape Town-wide (found 5 September 2026).** Post there only when
the site has a games event or the shop's play nights to point at, never the general "what's on":

| Group | Facebook | Size |
|---|---|---|
| Cape Town Board/Card Gaming | `https://www.facebook.com/groups/tablegaming/` | Public 2.8K |
| Board Gaming & Role Playing in South Africa | `https://www.facebook.com/groups/2910281150/` | Public 3.1K |
| Dungeons & Dragons Cape Town | `https://www.facebook.com/groups/dungeonsanddragonscapetown/` | Public 1.3K |
| Warhammer And Miniatures Cape Town | `https://www.facebook.com/groups/170114453136028/` | PRIVATE 795 |
| Magic: The Gathering - Cape Town | `https://www.facebook.com/groups/120806814632531/` | PRIVATE 1K |
| Pokémon TCG South Africa | `https://www.facebook.com/groups/pokemontcgza/` | Public 6.1K |
| Buy Sell Video Games Helderberg | `https://www.facebook.com/groups/1219754824736244/` | Public 4.7K (sales group; low value) |

## Automatic posting (built 2026-09-04, off until the token is set)

The API can post to the page itself through the Graph API. It is off until `api/.env` has
`HS_FB_PAGE_ID` and `HS_FB_PAGE_TOKEN`; the console's **Facebook** page then shows what is
queued, what went out (with the permalink) and what failed, and the two automatic kinds are
each behind a switch under **Settings** that defaults to off:

| What | When | Switch |
|---|---|---|
| **Approved event** | each event you approve in the console is posted after a delay (default 30 min, so a typo can still be cancelled from the queue). Title, date/time, town, cost, summary, the event's own link as the card. One post per event, ever; taking the event back or deleting it cancels a queued post. Past events are skipped. | `Facebook: post approved events` + delay |
| **This weekend** | once a week (default Thursday 17:00) a list of the approved events on the coming Saturday and Sunday, capped at 12 lines, linking to `/events.html` and `/submit.html`. Skipped when nothing is on. | `Facebook: weekly "this weekend" post` + day + hour |
| **Written by you** | the Facebook page in the console: text, optional link, now or a local time up to 60 days ahead. | none, always allowed |

The console page can also queue any approved event by hand and queue this weekend's list right
now (with a preview of the exact text). Posts leave **one a minute, oldest first**, so a burst of
approvals never floods the page. A failed post retries three times a quarter-hour apart, except
when Meta says the token or permission is wrong (codes 190, 200-299, 10, 100), which fails at
once and shows on the page. Nothing here ever deletes a post from Facebook.

Meta's `scheduled_publish_time` is deliberately not used: our own queue means a scheduled post
can be inspected and cancelled in the console, and the container keeps the only copy of the
token. Page **Events** cannot be created through the API any more (Meta removed it), so an
event post is a normal post with the details in the text.

### Getting a Page token that does not expire

You need the same Meta business portfolio the page lives in, an app, and a system user. If the
WhatsApp setup in `docs/whatsapp.md` has been done, steps 1-2 and 4 are already there.

1. **App.** developers.facebook.com → the "Helderberg Social" app (type *Business*), or create
   it. Under *App settings → Basic* fill in the privacy policy URL
   (`https://helderbergsocial.co.za/privacy.html`) and a category, then switch the app to
   **Live**. Posts made by an app in *Development* mode are visible only to people with a role
   on the app, which looks like success in the console and nothing on the page.
2. **Permissions.** Under *Use cases* (or *App review → Permissions and features*) make sure
   `pages_manage_posts` and `pages_read_engagement` are there with *Standard access*. Standard
   access is enough for a page the token's user administers; App Review is only for other
   people's pages.
3. **Page ID.** The numeric id, not the username. It is the `asset_id` in any Meta Business
   Suite URL for the page, and under *About → Page transparency* on the page. This page's id
   is `1352989477889743` (the `profile.php?id=61594290261232` number is the profile, not the
   page; the Graph API wants the page id).
4. **System user.** business.facebook.com → *Settings → Users → System users* → the `hs-api`
   system user (Admin), or add one. *Add assets → Pages* → this page → **Manage page** (full
   control). Then **Generate new token**: pick the app, expiry **Never**, permissions
   `pages_manage_posts`, `pages_read_engagement`, `pages_show_list`. Copy the token once.
5. **Turn the system-user token into the Page token.** The token from step 4 is a user token
   for the system user; the API wants the page's own token. On the Docker host (never on a
   machine whose shell history is shared):

   ```
   read -rs SU_TOKEN; echo
   curl -s -H "Authorization: Bearer $SU_TOKEN" \
     "https://graph.facebook.com/v22.0/me/accounts?fields=id,name,access_token" | python3 -m json.tool
   ```

   Find the entry whose `id` is the page id and take its `access_token`. Because it was derived
   from a never-expiring system-user token it does not expire either.
6. **`.env` and deploy.** `HS_FB_PAGE_ID=1352989477889743`, `HS_FB_PAGE_TOKEN=<that token>`,
   then `bash api/deploy.sh`. On the console's Facebook page press **Check the connection**:
   it must answer with the page's name. Then, if you want the automatic kinds, switch them on
   under Settings.

If the token ever dies (someone removes the system user, the page changes ownership), every
queued post fails with code 190 and the Facebook page in the console shows it; make a new token
and re-queue them with *Post now*.
