# Registering the .co.za domain

Goal: pay only the annual domain fee. No hosting bundle, no add-ons. The site is hosted on our own
server.

Checked 2026-09-02 against the ZACR registry WHOIS (`whois -h whois.registry.net.za`).

## Availability at time of check

| Domain | Status |
|---|---|
| helderbergsocial.co.za | **Available** |
| helderbergsocials.co.za | Available |
| socialhelderberg.co.za | Available |
| helderberglife.co.za | Available |
| helderbergconnect.co.za | Available |
| helderbergcommunity.co.za | Available |
| helderberglocal.co.za | Available |
| helderbergvibe.co.za | Available |
| helderbergliving.co.za | Available |
| helderbergbuzz.co.za | Available |
| helderbergmeet.co.za | Available |
| helderbergpeople.co.za | Available |
| helderbergclubs.co.za | Available |
| whatsonhelderberg.co.za | Available |
| helderberghub.co.za | Taken (Afrihost) |
| helderbergevents.co.za | Taken (Afrihost) |
| myhelderberg.co.za | Taken (xneelo) |
| thehelderberg.co.za | Taken (Frikkadel) |

Availability changes by the minute. Re-check immediately before buying.

## Domain-only pricing (checked 2026-09-02 on each registrar's public page)

| Registrar | Register | Renew | Domain without hosting? |
|---|---|---|---|
| domains.co.za | R99.00/yr | R109.00/yr | Yes, parked free until you point it somewhere |
| Afrihost | R197.00/yr | R99.00/yr | Yes, with full DNS control |

Over three years the two cost about the same; domains.co.za is cheaper up front, Afrihost cheaper
on renewal. Other accredited registrars (xneelo, Cloudflare Registrar, and others) also sell
`.co.za` on its own; prices not checked.

## Steps

1. Create an account at the chosen registrar in your own name. Use an email address you will still
   own in ten years, because renewal reminders go there.
2. Search the name, add **domain only** to the cart, decline hosting, email and privacy add-ons, pay.
3. Turn on auto-renew and put the expiry date in your calendar anyway.
4. DNS: either use the registrar's free DNS and create an `A` record for `@` and `www` pointing at
   the server, or change the nameservers to wherever you run DNS.
5. Once the record resolves, issue the TLS certificate on the server (Let's Encrypt).

## Recommendation

Register **helderbergsocial.co.za**. It matches the site name, reads well, and was available at
the time of the check. `whatsonhelderberg.co.za` is a good second choice if the site leans towards
events.
