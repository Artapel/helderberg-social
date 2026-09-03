# Deploying

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
