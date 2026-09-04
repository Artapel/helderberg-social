# Deploying

## Live setup (as of 2026-09-02)

- Repo: `github.com/Artapel/helderberg-social` (public), branch `master`.
- GitHub Pages: deploy-from-branch, `master` at `/`. Every push to `master` republishes in about
  30 seconds. No workflow files; `.nojekyll` skips the Jekyll build.
- Custom domain: `helderbergsocial.co.za`, set by the `CNAME` file in the repo root.
  `artapel.github.io/helderberg-social/` answers 301 to the custom domain.
- Domain registered 2026-09-03 at HostAfrica (auto-renew on, renews 2027-09-03). DNS is the
  registrar's zone (`ns1-4.host-ww.net`), records applied 2026-09-03 with `docs/dns-setup.py`.
  Note: the HostAfrica API reported success on a delete and an add that had not applied; the
  second (idempotent) run fixed both. Always re-read the zone after writing.
- HTTPS: GitHub's Let's Encrypt certificate was approved 2026-09-03 19:03 and
  `https_enforced` switched on the same minute. GitHub renews it automatically.

DNS records in the zone (from GitHub's docs, 2026-09-02):

| Host | Type | Value |
|---|---|---|
| `@` | A | `185.199.108.153` |
| `@` | A | `185.199.109.153` |
| `@` | A | `185.199.110.153` |
| `@` | A | `185.199.111.153` |
| `@` | AAAA | `2606:50c0:8000::153` |
| `@` | AAAA | `2606:50c0:8001::153` |
| `@` | AAAA | `2606:50c0:8002::153` |
| `@` | AAAA | `2606:50c0:8003::153` |
| `www` | CNAME | `artapel.github.io` |

GitHub also recommends verifying the domain in account Settings → Pages → Add a domain (a TXT
record) before or soon after adding it, to prevent takeover if the CNAME is ever removed.

## The API (events, email updates, submissions)

The static site is the whole public surface, but the forms, the email digests and the
approved-events feed come from one small container on our own Docker host behind Nginx
Proxy Manager at `https://api.helderbergsocial.co.za`. Build, configuration, security
model and runbook are in [`docs/api.md`](api.md). The site keeps working if the API is
down: events fall back to `data/data.js` and the forms show a copy-out block.

The site is managed from the API's own console at `https://api.helderbergsocial.co.za/admin`
(emailed link + Google Authenticator; runbook in `docs/api.md`, *The admin console*).

Mail leaves the container directly (its own DKIM-signed SMTP sender, no mail account).
The zone carries SPF, DKIM, DMARC and a null MX for it, published by
`docs/dns-setup.py --mail`; the console's System page checks them live. The domain does
not receive mail. Details in `docs/api.md`, *Outbound mail*.

## Alternatives

The site is static, so anything that serves files works. There is no build step: copy the folder
and serve it.

## Own server with nginx

```
# on the server
sudo mkdir -p /var/www/helderbergsocial
rsync -a --delete --exclude .git ./ user@server:/var/www/helderbergsocial/
```

Minimal nginx server block:

```
server {
    listen 80;
    server_name helderbergsocial.co.za www.helderbergsocial.co.za;
    root /var/www/helderbergsocial;
    index index.html;
    error_page 404 /404.html;
    location / { try_files $uri $uri/ =404; }
    location ~* \.(css|js|svg|webmanifest)$ { expires 7d; add_header Cache-Control "public"; }
}
```

Then `certbot --nginx -d helderbergsocial.co.za -d www.helderbergsocial.co.za` for TLS.

If the server already runs Docker, the equivalent is one `nginx:alpine` container with the folder
mounted read-only at `/usr/share/nginx/html`, behind whatever reverse proxy terminates TLS.

## Free static hosts

GitHub Pages, Cloudflare Pages or Netlify all serve this unchanged and cost nothing, which keeps
the domain fee as the only recurring cost. `404.html` uses root-relative paths, so the site must be
served from the domain root, not a sub-folder.

## Before launch checklist

- [ ] Set `site.submitEmail` (or `submitEndpoint`) in `data/data.js`.
- [ ] Replace `helderbergsocial.co.za` in `robots.txt`, `sitemap.xml` and this file if a different domain is bought.
- [ ] Verify at least the front-page listings (see `content-verification.md`) and flip them to `verified: true`.
- [ ] Add an OG image (`assets/img/og.png`, 1200x630) and reference it from each page's `<meta property="og:image">`.
- [ ] Decide on analytics (none included).
