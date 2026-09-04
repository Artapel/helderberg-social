# Helderberg Social

Community directory for the Helderberg (Somerset West, Strand, Gordon's Bay, Sir Lowry's Pass):
groups and clubs, regular activities, places, and dated events.

Static site. No build step, no backend, no framework. Open `index.html` in a browser or serve the
folder with any web server.

## Layout

| Path | What |
|---|---|
| `data/data.js` | **All content.** Site settings, towns, categories, audiences, listings, events. Edit this to change what the site shows. |
| `assets/css/site.css` | Styles. Light and dark themes via CSS custom properties. |
| `assets/js/site.js` | Shared runtime: header/footer, search, cards, event rows, ICS export, Leaflet map helper. |
| `index.html` | Home: search, category tiles, this weekend, featured groups, towns. |
| `directory.html` | Filterable directory (search, category, town, audience, cost, type), list or map view, URL-addressable filters. |
| `events.html` | Dated events grouped by month, plus weekly fixtures by day. |
| `places.html` | Map plus list of places. |
| `towns.html` | Guide to each town with listing counts. |
| `listing.html?id=…` | Detail page for one listing. |
| `submit.html` | Add-a-listing / report-a-change form. |
| `about.html`, `404.html` | Static pages. |
| `docs/` | Domain registration, deployment, content verification. |

Dynamic parts live in `api/` (Go, one container): approved events, daily/weekly email
or WhatsApp digests, posting to the Facebook page, public submissions with email verification, a source watcher that re-checks
organisers' pages and calendars, and an admin console (emailed link + Google
Authenticator) for moderation, subscribers, sources, analytics, logs and settings.
See `docs/api.md`.

Page scripts live in `assets/js/pages/`; there is no inline JavaScript because every
page ships a strict Content Security Policy.

## Content rules

- `verified: true` only after a person has confirmed the listing with the organiser or their official page.
- Never invent contact details. `contact` stays empty until the organiser supplies it.
- Every listing carries a `source` URL where one exists, so a reviewer can check it.
- Map coordinates are approximate (`coordsApprox: true`) until verified.

## Submissions

`submit.html` works three ways, chosen by `site.submitEndpoint` / `site.submitEmail` in `data/data.js`:

1. `submitEndpoint` set: the form POSTs JSON to it (Formspree, a tiny PHP/Node endpoint, anything that accepts JSON).
2. Only `submitEmail` set: the form opens the visitor's mail app with a pre-filled `mailto:`.
3. Neither set: the form shows the submission as text to copy and email.

## External dependencies

Google Fonts (Fraunces, Inter), Leaflet 1.9.4 from cdnjs, OpenStreetMap tiles. Everything else is local.

## Local preview

```
python -m http.server 8080
```

then open <http://localhost:8080/>.
