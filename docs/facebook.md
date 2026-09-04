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
- **Username / vanity URL:** `helderbergsocial` (fallback `helderbergsocialza`) — not set yet
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
- **Settings -> Page setup -> Audience and visibility:** Followers can comment: yes; profanity
  filter: strong; hide comments containing links from strangers: on
- **Messaging:** instant reply on, text: "Thanks for the message. Helderberg Social is run by
  volunteers so replies can take a day or two. To add a group, place or event go to
  helderbergsocial.co.za/submit.html — it is free."
- **Tabs to keep:** Posts, About, Events, Photos; hide Reviews (nothing to review) and Shop

## First posts (in this order, one per day)

1. **Welcome (pinned).**
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

## Automating it later

Facebook's Graph API needs a Meta developer app with a Page access token
(`pages_manage_posts`, `pages_read_engagement`) approved for the page. That token is a credential:
it goes in `api/.env` as `HS_FB_PAGE_TOKEN` on the server, never in this repo, and the API's
weekly digest job can then post the same "this weekend" summary to the page. Not built yet.
