# WhatsApp updates

Subscribers can take the digest on WhatsApp instead of email. Delivery is fully
automatic once Meta's side is set up: the API sends through the WhatsApp
Business Platform (Cloud API), confirmation is a button tap, unsubscribing is
a button tap or a STOP reply, and the digest goes out on the same schedule as
the email one. Nothing is posted by hand.

This document is the one-off setup on Meta's side, the exact templates to
create, and how the pieces fit. Meta renames screens often, so the wording of
menus below is a guide, not gospel; the *values* the API needs are fixed.

## What the API needs from Meta

| `.env` key | What it is | Where it comes from |
|---|---|---|
| `HS_WA_PHONE_ID` | The numeric *Phone number ID* of the sending number (not the number itself). | App dashboard → WhatsApp → API Setup. |
| `HS_WA_WABA_ID` | The WhatsApp Business Account ID. Optional; lets the System page show template approval status. | Same screen, or Business Settings → WhatsApp accounts. |
| `HS_WA_TOKEN` | A **permanent System User token** with `whatsapp_business_messaging` and `whatsapp_business_management`. Not the 24-hour test token from the API Setup page. | Business Settings → Users → System users → Add → Generate token. |
| `HS_WA_APP_SECRET` | The app's secret; every webhook is HMAC-signed with it and the API refuses anything unsigned. | App dashboard → App settings → Basic → App secret. |
| `HS_WA_VERIFY_TOKEN` | A random string you make up (`openssl rand -hex 16`); Meta echoes it during the webhook handshake. | You. Paste the same value in the webhook config. |
| `HS_ADMIN_PHONE` | Your own WhatsApp number, for the *send test* button on the System page. | You. |
| `HS_WA_API_VERSION` | Graph API version, default `v22.0`. Bump when Meta retires it. | developers.facebook.com changelog. |

All of phone id, token, app secret and verify token must be set together; with
none of them set WhatsApp is off, the site hides the option and the System
page says so.

## Setup, in order

1. **Business portfolio.** business.facebook.com → the same portfolio the
   Facebook page lives in (or create one named Helderberg Social). Complete
   **business verification** under Security Centre (registration document or
   utility bill in the business or your name). Without it the display name
   shows as the raw number and sending is capped at 250 new conversations per
   day; with it the cap starts at 1,000 and rises automatically.
2. **App.** developers.facebook.com → Create app → type *Business* → name
   "Helderberg Social" → add the **WhatsApp** product. This creates or links a
   WhatsApp Business Account (WABA) in the portfolio.
3. **Phone number.** WhatsApp → API Setup → *Add phone number*. It must be a
   number **not currently registered on WhatsApp** (a spare SIM or a landline
   that can take a voice call for the code). Once on the platform it cannot be
   used in the WhatsApp app any more. Display name "Helderberg Social";
   category *Community* or *Media/News*; description and website filled in.
   Note the **Phone number ID** and **WABA ID** shown on that page.
4. **Payment method.** Business Settings → Billing → add a card to the WABA.
   Marketing templates are billed per message delivered (SA rate on Meta's
   pricing page); replies to a person's own message are free. Nothing sends
   without a payment method once the free test allowance is used.
5. **System user + token.** Business Settings → Users → System users → Add
   (name `hs-api`, role Admin) → *Add assets*: the app (full control) and the
   WABA (full control) → *Generate new token*: choose the app, expiry *Never*,
   permissions `whatsapp_business_messaging` and
   `whatsapp_business_management`. Copy it once; it is `HS_WA_TOKEN`.
6. **Webhook.** App dashboard → WhatsApp → Configuration → *Edit* callback:
   URL `https://api.helderbergsocial.co.za/api/wa/webhook`, verify token =
   `HS_WA_VERIFY_TOKEN`. The API must already be running with the same value
   or the handshake fails. Then *Manage* webhook fields → subscribe to
   **messages** (that one field carries inbound messages *and* delivery
   statuses).
7. **App mode.** App settings → Basic: fill in privacy policy URL
   (`https://helderbergsocial.co.za/privacy.html`) and category, then switch
   the app from Development to **Live**. In development mode only up to five
   pre-listed test numbers can be messaged.
8. **Templates.** WhatsApp Manager → Message templates → Create. Two
   templates, exactly as below. Approval usually takes minutes to a day;
   the System page shows each one's status once `HS_WA_WABA_ID` is set.
9. **`.env` and deploy** on the Docker host, then on the System page press
   *Send the confirm template to the admin phone*. If it arrives, everything
   between the container and Meta works.

## The two templates

Create both in language **English** (`en`); the API asks for `HS_WA_LANG`,
default `en`. Names must match `HS_WA_TEMPLATE_CONFIRM` / `HS_WA_TEMPLATE_DIGEST`
(defaults `hs_confirm`, `hs_digest`). Parameters are the numbered `{{n}}`
placeholders; give Meta the sample values shown so the reviewer sees a real
message.

### `hs_confirm` — category **Utility**

Body:

```
Hi! Someone (hopefully you) asked for {{1}} WhatsApp updates from Helderberg Social covering the next {{2}} days: a short list of what's on in Somerset West, Strand, Gordon's Bay and Sir Lowry's Pass.

Tap Confirm to start. If it wasn't you, tap Not me and nothing will be sent.
```

Samples: `{{1}}` = `weekly`, `{{2}}` = `14`.

Buttons (type *Quick reply*): **Confirm**, **Not me** — in that order. The API
sets their payloads to `CONFIRM` and `STOP`.

Footer (optional): `Reply STOP at any time to unsubscribe.`

### `hs_digest` — category **Marketing**

Body:

```
What's on in the Helderberg over the next {{1}} days: {{2}} events.

{{3}}

Reply STOP at any time to unsubscribe.
```

Samples: `{{1}}` = `7`, `{{2}}` = `3`, `{{3}}` = `Sat 6 Sep: Parkrun (Strand) · free · Sat 6 Sep: Lourensford Market (Somerset West) · Sun 7 Sep: Beach clean-up (Gordon's Bay) · free`.

Buttons, in this order:

1. type *Visit website*, text **See all events**, URL type *Dynamic*:
   `https://api.helderbergsocial.co.za/api/digest?t={{1}}` with sample
   `abc123` for the variable part. The API fills it with a signed token that
   opens that subscriber's personalised list (valid 30 days).
2. type *Quick reply*, text **Unsubscribe**. The API sets its payload to
   `STOP`.

Why one line for the list: Meta rejects template parameters containing line
breaks, so the API squeezes the events onto one line separated by ` · `,
capped at about 600 characters with "+N more", and the button carries the
full list. The email digest is unchanged.

## How it behaves

- **Subscribe:** the site's form offers *Email / WhatsApp* only when
  `GET /api/health` reports `"whatsapp":true`. A number is normalised to E.164
  (`082 123 4567` → `27821234567`; other countries with their `+` code) and
  stored in `subscribers.phone` with `channel='whatsapp'`; the row has no
  email. The `hs_confirm` template is sent. Unconfirmed rows are deleted after
  three days like email ones.
- **Confirm:** the tap on *Confirm* (or a reply of yes/confirm/ok) arrives on
  the webhook. The **sender's number** is the only identity used; nothing in
  the payload can name another subscriber. The row is confirmed and a plain
  "You're subscribed" text is sent back, which is allowed because the person
  just messaged us (24-hour service window).
- **Digest:** the scheduler treats WhatsApp subscribers exactly like email
  ones (same filters, same times); `runDigest` sends `hs_digest` per person
  with a 200 ms gap. Sends and failures land in the same `mail_log`
  (kinds `wa-confirm`, `wa-digest-daily`, `wa-digest-weekly`, `wa-status`)
  under the hash of `tel:<number>`, and the per-recipient budget (5/hour,
  20/day) applies.
- **Stop:** *Not me*, *Unsubscribe*, or a reply of STOP/unsubscribe/cancel
  deletes the row outright and acknowledges. Any other text gets one short
  automated pointer and no conversation.
- **Delivery failures** reported by Meta (`failed`, `undeliverable`) are
  logged with Meta's error code and title, visible under Logs → mail log.
- **Webhook security:** `X-Hub-Signature-256` is verified against
  `HS_WA_APP_SECRET` before the body is parsed; the verify handshake uses a
  constant-time compare; message ids are recorded in `wa_seen` for 7 days so
  Meta's redeliveries do nothing twice; events for another phone number on
  the same app are ignored.
- **Console:** Subscribers shows the number with a *WhatsApp* pill, filters
  by channel, searches numbers, exports both columns; *Add by hand* accepts a
  number; *Resend confirmation* resends the template. System shows the
  configuration, template status and the test button.

## Limits worth knowing

- Business-initiated messages are **only** templates. There is no way to send
  arbitrary text to someone who has not messaged you in the last 24 hours;
  this is Meta's rule, not ours, and it is why the digest has the fixed shape
  above.
- Meta may **recategorise** a template (Utility → Marketing) at review time;
  it changes the price, not the behaviour.
- **Quality rating** on the number drops if people block or report it; too
  many complaints pause sending. Double opt-in, the STOP handling and the
  Unsubscribe button on every message are there to keep that rating green.
- The same number cannot be used for WhatsApp Channels or the normal app.
