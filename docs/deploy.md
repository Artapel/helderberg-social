# Deploying

The site is static, so it can be served by anything. Two sensible homes:

## 1. Docker on daisy-dev_111.150 (matches daisysolutions.co.za)

The existing `daisy-website` container on 111.150 is the pattern to copy. Outline:

```
# on 111.150
mkdir -p ~/helderberg-social && rsync -a --delete <this folder>/ ~/helderberg-social/
docker run -d --name helderberg-social --restart unless-stopped \
  -v ~/helderberg-social:/usr/share/nginx/html:ro nginx:alpine
```

Then add a proxy host in Nginx Proxy Manager (the public front door on 111.150) for
`helderbergsocial.co.za` pointing at the container, with a Let's Encrypt certificate, and create
the A record at the registrar/DNS host.

Nothing in this file has been run; it is the intended procedure. Do not run `docker` on the laptop.

## 2. Any static host

GitHub Pages, Cloudflare Pages or a plain Apache vhost all work unchanged. `404.html` uses
root-relative paths, so the site must be served from the domain root.

## Before launch checklist

- [ ] Set `site.submitEmail` (or `submitEndpoint`) in `data/data.js`.
- [ ] Replace `helderbergsocial.co.za` in `robots.txt` and `sitemap.xml` if a different domain is bought.
- [ ] Verify at least the front-page listings (see `content-verification.md`) and flip them to `verified: true`.
- [ ] Add a real OG image (`assets/img/og.png`, 1200x630) and reference it from each page's `<meta property="og:image">`.
- [ ] Decide on analytics (none included).
