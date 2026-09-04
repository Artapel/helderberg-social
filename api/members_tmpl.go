package main

import "html/template"

// Templates for the public account area (/account/*). They borrow the
// site's colours and shape but use system fonts and inline CSS only: the
// API's CSP allows no scripts and no external stylesheets, so every page here
// is a plain form that works without JavaScript.

func parseAccount() *template.Template {
	return template.Must(template.New("account").Funcs(consoleFuncs).Parse(accountSrc))
}

const accountCSS = `
:root{--bg:#f7f5f0;--surface:#fff;--surface-2:#efece4;--text:#1c2431;--muted:#5b6573;--line:#e2ddd2;--brand:#0f4c5c;--brand-2:#1c7c8c;--accent:#f26b3a;--ok:#2e7d4f;--warn:#b35c00;--no:#b3261e;--radius:14px}
@media (prefers-color-scheme:dark){:root{--bg:#0f151c;--surface:#171f28;--surface-2:#1f2933;--text:#e8edf2;--muted:#9aa7b5;--line:#2a3542;--brand:#5ec2d6;--brand-2:#8ad8e6;--accent:#ff8a5b;--ok:#6fd39a;--warn:#f0a04b;--no:#ff8a80}}
*{box-sizing:border-box}html{color-scheme:light dark}
body{margin:0;background:var(--bg);color:var(--text);font:16px/1.55 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}
a{color:var(--brand-2)}
header{background:var(--brand);color:#fff}
header .in{max-width:880px;margin:0 auto;padding:12px 20px;display:flex;align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap}
header .logo{color:#fff;text-decoration:none;font-weight:800;font-size:19px;letter-spacing:-.01em}
header nav{display:flex;gap:4px;flex-wrap:wrap;align-items:center}
header nav a,header nav button{color:#fff;text-decoration:none;padding:6px 10px;border-radius:8px;font-size:14px;background:none;border:0;cursor:pointer;font:inherit;font-size:14px}
header nav a.on{background:rgba(255,255,255,.16)}
header nav a:hover,header nav button:hover{background:rgba(255,255,255,.12)}
main{max-width:880px;margin:0 auto;padding:22px 20px 60px}
h1{font-size:26px;margin:6px 0 14px;letter-spacing:-.01em}h2{font-size:18px;margin:26px 0 10px}
.panel{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:18px 20px;margin:0 0 16px}
.panel.narrow{max-width:460px}
label{display:block;font-size:13px;font-weight:600;color:var(--muted);margin:12px 0 4px}
label.inline{display:flex;align-items:center;gap:8px;font-weight:500;color:var(--text)}
input[type=text],input[type=email],input[type=password],input[type=date],input[type=time],input[type=url],select,textarea{width:100%;padding:10px 12px;border:1px solid var(--line);border-radius:10px;background:var(--surface);color:var(--text);font:inherit}
textarea{min-height:120px;resize:vertical}
input:focus,select:focus,textarea:focus{outline:2px solid var(--brand-2);outline-offset:1px;border-color:var(--brand-2)}
.grid2{display:grid;grid-template-columns:1fr 1fr;gap:0 16px}
@media (max-width:600px){.grid2{grid-template-columns:1fr}}
.row{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-top:14px}
button,.btn{font:inherit;font-weight:600;padding:10px 16px;border-radius:10px;border:1px solid var(--line);background:var(--surface-2);color:var(--text);cursor:pointer;text-decoration:none;display:inline-block}
button.pri,.btn.pri{background:var(--accent);border-color:var(--accent);color:#fff}
button.no{background:transparent;color:var(--no);border-color:var(--no)}
button.sm,.btn.sm{padding:6px 10px;font-size:13px}
.msg{padding:10px 14px;border-radius:10px;margin:0 0 16px;background:#e4f3e9;color:#1d5b34;border:1px solid #b9dfc6}
.msg.err{background:#fbe7e5;color:#8c2a22;border-color:#f1c2bd}
@media (prefers-color-scheme:dark){.msg{background:#14301f;color:#a9e3bf;border-color:#245a3a}.msg.err{background:#3a1613;color:#ffb3ab;border-color:#6b2a24}}
.hint{font-size:13px;color:var(--muted);margin:6px 0 0}
.pill{display:inline-block;font-size:12px;font-weight:600;padding:2px 9px;border-radius:999px;background:var(--surface-2);color:var(--muted)}
.pill.ok{background:#e4f3e9;color:var(--ok)}.pill.warn{background:#fff1e0;color:var(--warn)}.pill.no{background:#fbe7e5;color:var(--no)}
@media (prefers-color-scheme:dark){.pill.ok{background:#14301f}.pill.warn{background:#3a2a10}.pill.no{background:#3a1613}}
.ev{display:grid;grid-template-columns:1fr auto;gap:6px 16px;padding:14px 0;border-top:1px solid var(--line)}
.ev:first-of-type{border-top:0;padding-top:4px}
.ev b{font-size:17px}.ev .meta{color:var(--muted);font-size:14px}.ev .acts{display:flex;gap:8px;align-items:flex-start;flex-wrap:wrap;justify-content:flex-end}
@media (max-width:600px){.ev{grid-template-columns:1fr}.ev .acts{justify-content:flex-start}}
.steps{margin:0;padding-left:20px}.steps li{margin:4px 0}
footer{max-width:880px;margin:0 auto;padding:0 20px 30px;color:var(--muted);font-size:13px}
.danger{border-color:#f1c2bd}
`

const accountSrc = `
{{define "account_layout"}}<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex"><title>{{.Title}} · Helderberg Social</title><style>` + accountCSS + `</style></head>
<body><header><div class="in"><a class="logo" href="{{.Site}}/">Helderberg Social</a><nav>
{{if .Member}}<a href="/account" {{if eq .Active "/account"}}class="on"{{end}}>My events{{if .Pending}} <span class="pill warn">{{.Pending}} waiting</span>{{end}}</a><a href="/account/events/new" {{if eq .Active "/account/events/new"}}class="on"{{end}}>Post an event</a><a href="/account/settings" {{if eq .Active "/account/settings"}}class="on"{{end}}>Account</a><form method="post" action="/account/logout" style="display:inline"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>Sign out</button></form>
{{else}}<a href="{{.Site}}/events.html">Events</a><a href="/account/login" {{if eq .Active "/account/login"}}class="on"{{end}}>Sign in</a>{{if .RegOn}}<a href="/account/register" {{if eq .Active "/account/register"}}class="on"{{end}}>Create account</a>{{end}}{{end}}
</nav></div></header>
<main><h1>{{.Title}}</h1>{{if .Msg}}<div class="msg{{if .Err}} err{{end}}">{{.Msg}}</div>{{end}}
{{.Body}}</main>
<footer>Helderberg Social · <a href="{{.Site}}/privacy.html">Privacy</a> · <a href="{{.Site}}/about.html">About</a> · Your name and email are never shown on the site.</footer>
</body></html>{{end}}

{{define "acc_register"}}{{$d := .D}}
<div class="panel narrow"><p>An account lets you post events to Helderberg Social and see whether they were published. Every event is still checked by a person before it appears.</p>
<form method="post" action="/account/register"><input type="hidden" name="next" value="{{$d.Next}}"><div style="position:absolute;left:-9999px" aria-hidden="true"><label>Website</label><input type="text" name="website_url" tabindex="-1" autocomplete="off"></div>
<label for="name">Your name</label><input type="text" id="name" name="name" required maxlength="80" autocomplete="name">
<label for="email">Email address</label><input type="email" id="email" name="email" required maxlength="120" autocomplete="email"><p class="hint">We send a confirmation link here, and later tell you when an event is published.</p>
<label for="pw">Password</label><input type="password" id="pw" name="password" required minlength="10" autocomplete="new-password"><p class="hint">At least 10 characters. A short sentence you will remember works well.</p>
<label for="pw2">Password again</label><input type="password" id="pw2" name="password2" required minlength="10" autocomplete="new-password">
<div class="row"><button class="pri">Create account</button><a href="/account/login?next={{$d.Next}}">I already have one</a></div></form></div>
<p class="hint">By creating an account you agree that we keep your name and email address to run the account, as set out in the <a href="{{.V.Site}}/privacy.html">privacy notice</a>. You can delete the account yourself at any time.</p>
{{end}}

{{define "acc_sent"}}{{$d := .D}}
<div class="panel narrow">{{if eq $d.Kind "reset"}}<p>If that address has an account, an email with a reset link is on its way. The link works once, for one hour.</p>{{else}}<p>We have sent you an email with a confirmation link. Open it within 24 hours to finish.</p>{{end}}
<p class="hint">Nothing arrived? Look in the spam or promotions folder, and check the address for typos. Mail can take a minute or two.</p>
<p class="row"><a class="btn" href="/account/login">Back to sign in</a></p></div>
{{end}}

{{define "acc_login"}}{{$d := .D}}
<div class="panel narrow"><form method="post" action="/account/login"><input type="hidden" name="next" value="{{$d.Next}}">
<label for="email">Email address</label><input type="email" id="email" name="email" value="{{$d.Email}}" required autocomplete="email" autofocus>
<label for="pw">Password</label><input type="password" id="pw" name="password" required autocomplete="current-password">
<div class="row"><button class="pri">Sign in</button><a href="/account/forgot">Forgotten your password?</a></div></form></div>
{{if .V.RegOn}}<p>New here? <a href="/account/register?next={{$d.Next}}">Create an account</a> to post events.</p>{{end}}
{{end}}

{{define "acc_unverified"}}{{$d := .D}}
<div class="panel narrow"><p>Your account exists but the email address <b>{{$d.Email}}</b> has not been confirmed yet, so you cannot sign in. Look for the confirmation email (spam folder too), or ask for a fresh one.</p>
<form method="post" action="/account/resend"><input type="hidden" name="email" value="{{$d.Email}}"><div class="row"><button class="pri">Send the confirmation again</button><a href="/account/login">Back</a></div></form></div>
{{end}}

{{define "acc_forgot"}}
<div class="panel narrow"><p>Enter your email address and we will send a link to choose a new password.</p>
<form method="post" action="/account/forgot"><label for="email">Email address</label><input type="email" id="email" name="email" required autocomplete="email" autofocus>
<div class="row"><button class="pri">Send reset link</button><a href="/account/login">Back to sign in</a></div></form></div>
{{end}}

{{define "acc_reset"}}{{$d := .D}}
<div class="panel narrow"><form method="post" action="/account/reset"><input type="hidden" name="t" value="{{$d.Token}}">
<label for="pw">New password</label><input type="password" id="pw" name="password" required minlength="10" autocomplete="new-password" autofocus><p class="hint">At least 10 characters.</p>
<label for="pw2">New password again</label><input type="password" id="pw2" name="password2" required minlength="10" autocomplete="new-password">
<div class="row"><button class="pri">Save password</button></div></form>
<p class="hint">Saving signs you out on every other device.</p></div>
{{end}}

{{define "acc_events"}}{{$d := .D}}{{$c := .CSRF}}
<div class="panel">{{range $d.Events}}<div class="ev"><div><b>{{.Title}}</b> <span class="pill {{statusCls .Status}}">{{.StatusText}}</span><div class="meta">{{.When}} · {{.TownName}} · {{.CatName}}{{if .Live}} · <a href="{{.Live}}">see it on the site</a>{{end}}</div></div>
<div class="acts">{{if .Editable}}<a class="btn sm" href="/account/events/edit?id={{.ID}}">Edit</a>{{end}}<form method="post" action="/account/events/withdraw"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><label class="inline" style="margin:0;font-size:13px"><input type="checkbox" name="confirm" value="yes"> remove</label> <button class="no sm">Remove</button></form></div></div>
{{else}}<p>You have not posted anything yet.</p>{{end}}</div>
<p class="row"><a class="btn pri" href="/account/events/new">Post an event</a></p>
<h2>How it works</h2><ol class="steps"><li>You post an event; it shows here as <span class="pill warn">Waiting for a check</span>.</li><li>A person reads it, usually within a day. Community events anywhere in the Helderberg (Somerset West, Strand, Gordon's Bay, Sir Lowry's Pass) that are open to the public are welcome; commercial promotions are not.</li><li>You get an email when it is published, or when it is not (with a reason). Editing a published event sends it for a fresh check.</li></ol>
{{end}}

{{define "acc_event_form"}}{{$f := .D}}{{$e := $f.E}}
<form method="post" action="/account/events/save" class="panel"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="id" value="{{$e.ID}}">
{{if not $f.New}}<p class="hint">Saving sends the event back for a quick check before the change shows on the site.</p>{{end}}
<label for="title">Event title</label><input type="text" id="title" name="title" value="{{$e.Title}}" required maxlength="120" placeholder="e.g. Strand beach clean-up">
<div class="grid2"><div><label for="date">Date</label><input type="date" id="date" name="date" value="{{$e.Date}}" required></div><div><label for="end_date">End date (multi-day events only)</label><input type="date" id="end_date" name="end_date" value="{{$e.EndDate}}"></div>
<div><label for="time">Start time</label><input type="time" id="time" name="time" value="{{$e.Time}}"><p class="hint">Leave empty for an all-day event.</p></div><div><label for="end_time">End time</label><input type="time" id="end_time" name="end_time" value="{{$e.EndTime}}"></div>
<div><label for="town">Town</label><select id="town" name="town">{{range $f.Towns}}<option value="{{.}}" {{if eq . $e.Town}}selected{{end}}>{{town .}}</option>{{end}}</select></div><div><label for="category">Category</label><select id="category" name="category">{{range $f.Cats}}<option value="{{.}}" {{if eq . $e.Category}}selected{{end}}>{{cat .}}</option>{{end}}</select></div>
<div><label for="cost">Cost</label><select id="cost" name="cost">{{range $f.Costs}}<option value="{{.}}" {{if eq . $e.Cost}}selected{{end}}>{{title .}}</option>{{end}}</select></div><div><label for="website">Web page or Facebook event (optional)</label><input type="url" id="website" name="website" value="{{$e.Website}}" placeholder="https://"></div></div>
<label for="summary">What is it? (where exactly, who it is for, what to bring)</label><textarea id="summary" name="summary" required minlength="20" maxlength="800">{{$e.Summary}}</textarea><p class="hint">Plain text, up to 800 characters. Include the venue; the site only knows the town.</p>
<div class="row"><button class="pri">{{if $f.New}}Send for checking{{else}}Save changes{{end}}</button><a class="btn" href="/account">Cancel</a></div></form>
{{end}}

{{define "acc_settings"}}{{$m := .V.Member}}{{$c := .CSRF}}
<div class="grid2"><form method="post" action="/account/settings" class="panel"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="action" value="name"><h2 style="margin-top:0">Your name</h2>
<label for="name">Name</label><input type="text" id="name" name="name" value="{{$m.Name}}" required maxlength="80"><p class="hint">Only the moderator sees it. Signed in as <b>{{$m.Email}}</b>.</p>
<div class="row"><button>Save name</button></div></form>
<form method="post" action="/account/settings" class="panel"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="action" value="password"><h2 style="margin-top:0">Password</h2>
<label for="cur">Current password</label><input type="password" id="cur" name="current" required autocomplete="current-password">
<label for="pw">New password</label><input type="password" id="pw" name="password" required minlength="10" autocomplete="new-password">
<label for="pw2">New password again</label><input type="password" id="pw2" name="password2" required minlength="10" autocomplete="new-password">
<div class="row"><button>Change password</button></div></form></div>
<form method="post" action="/account/settings" class="panel danger"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="action" value="delete"><h2 style="margin-top:0">Delete this account</h2>
<p class="hint" style="margin-bottom:8px">Removes your account, your sign-ins and any event of yours that is not published. Events that are already on the site stay there, with your name and email removed from them (they are public information about the event, not about you). If you also want a published event gone, remove it from <a href="/account">My events</a> first.</p>
<label for="dpw">Your password</label><input type="password" id="dpw" name="current" autocomplete="current-password" style="max-width:320px">
<div class="row"><label class="inline" style="margin:0"><input type="checkbox" name="confirm" value="yes"> yes, delete my account</label><button class="no">Delete account</button></div></form>
{{end}}
`
