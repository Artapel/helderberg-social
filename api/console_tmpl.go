package main

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

// Console templates. No JavaScript anywhere: every control is a form, so
// the page works under a CSP that forbids scripts entirely. Styling is a
// single inline stylesheet.

var consoleFuncs = template.FuncMap{
	"ago":  ago,
	"ts":   fmtTS,
	"town": townName,
	"cat":  catName,
	"date": fmtDate,
	"pct": func(n, max int) int {
		if max <= 0 {
			return 0
		}
		return n * 100 / max
	},
	"join":  strings.Join,
	"title": func(s string) string { return strings.ToUpper(s[:1]) + s[1:] },
	"add":   func(a, b int) int { return a + b },
	"sub":   func(a, b int) int { return a - b },
	"short": func(s string, n int) string {
		if r := []rune(s); len(r) > n {
			return string(r[:n])
		}
		return s
	},
	"hasPrefix": strings.HasPrefix,
	"has": func(list []string, v string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	},
	"townList": func(keys []string) []string {
		out := make([]string, 0, len(keys))
		for _, k := range keys {
			out = append(out, townName(k))
		}
		return out
	},
	"list": func(v ...string) []string { return v },
	"dict": func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[fmt.Sprint(kv[i])] = kv[i+1]
		}
		return m
	},
	"eqs":     func(a, b string) bool { return a == b },
	"weekday": func(n int) string { return time.Weekday(n).String() },
	"statusCls": func(s string) string {
		switch s {
		case "approved", "accepted", "ok":
			return "ok"
		case "rejected":
			return "no"
		case "pending_review":
			return "warn"
		}
		return "gh"
	},
}

func fmtTS(s string) string {
	if s == "" {
		return "never"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	loc, _ := time.LoadLocation("Africa/Johannesburg")
	return t.In(loc).Format("02 Jan 15:04")
}

func ago(s string) string {
	if s == "" {
		return "never"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func parseConsole() *template.Template {
	return template.Must(template.New("console").Funcs(consoleFuncs).Parse(consoleSrc))
}

const consoleCSS = `
:root{--ink:#1c2431;--mute:#6b7280;--line:#e5e9ee;--bg:#f4f6f8;--card:#fff;--teal:#0f4c5c;--teal2:#2a93a5;--sun:#f26b3a;--ok:#1f7a4d;--no:#c0392b;--warn:#b7791f}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:14px/1.45 -apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif}
a{color:var(--teal)}h1{font-size:22px;margin:0 0 14px}h2{font-size:15px;margin:22px 0 8px;letter-spacing:.02em}h3{font-size:13px;margin:14px 0 6px;color:var(--mute);text-transform:uppercase;letter-spacing:.06em}
.shell{display:flex;min-height:100vh}aside{width:212px;background:var(--teal);color:#dbe9ec;padding:18px 12px;display:flex;flex-direction:column;flex-shrink:0}
.brand{color:#fff;font-weight:700;font-size:17px;padding:4px 10px 14px}.brand em{font-style:italic;color:#ffc27a}.brand span{display:block;font-size:11px;font-weight:500;letter-spacing:.12em;text-transform:uppercase;color:#9fd2dc}
nav a{display:flex;align-items:center;gap:10px;color:#dbe9ec;text-decoration:none;padding:8px 10px;border-radius:8px;font-weight:500}nav a i{font-style:normal;width:16px;text-align:center;opacity:.8}nav a.on,nav a:hover{background:rgba(255,255,255,.12);color:#fff}
.badge{margin-left:auto;background:var(--sun);color:#fff;border-radius:10px;padding:0 7px;font-size:11px}.signout{margin-top:auto;padding:10px}.signout button{width:100%;background:rgba(255,255,255,.12);color:#fff}.ver{font-size:11px;color:#9fd2dc;text-align:center;margin:6px 0 0}
main{flex:1;padding:22px 28px;max-width:1240px;min-width:0}main:has(.wide){max-width:none}
table.compact td,table.compact th{padding:3px 6px;line-height:1.3}table.compact details[open]{flex-basis:100%}table.compact details form{max-width:520px}
.msg{background:#e7f6ec;border:1px solid #bfe3cb;color:#155a36;padding:8px 12px;border-radius:8px}.msg.bad{background:#fdecea;border-color:#f5c2bd;color:#8a1c12}
.cards{display:grid;grid-template-columns:repeat(auto-fill,minmax(150px,1fr));gap:10px;margin-bottom:8px}.card{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:10px 12px}.card b{display:block;font-size:22px;line-height:1.2}.card small{color:var(--mute)}.card.hot{border-color:var(--sun)}
.panel{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:14px 16px;margin-bottom:12px}
table{border-collapse:collapse;width:100%}td,th{text-align:left;padding:6px 8px;border-bottom:1px solid var(--line);vertical-align:top;font-size:13px}th{color:var(--mute);font-weight:600;font-size:12px}tr:hover td{background:#fafbfc}td.num,th.num{text-align:right;font-variant-numeric:tabular-nums}
.row{display:flex;gap:8px;flex-wrap:wrap;align-items:center}.row.tight{gap:4px}.inline{display:inline}
button,.btn{border:0;border-radius:6px;padding:6px 12px;cursor:pointer;font-weight:600;font-size:13px;background:var(--line);color:var(--ink);text-decoration:none;display:inline-block}button.ok{background:var(--ok);color:#fff}button.no{background:var(--no);color:#fff}button.pri,.btn.pri{background:var(--sun);color:#fff}button.sm,.btn.sm{padding:3px 8px;font-size:12px}
input[type=text],input[type=email],input[type=url],input[type=date],input[type=number],select,textarea{border:1px solid #cfd6de;border-radius:6px;padding:6px 8px;font:inherit;width:100%;background:#fff}textarea{min-height:90px}label{display:block;font-size:12px;color:var(--mute);margin:8px 0 3px;font-weight:600}
.grid2{display:grid;grid-template-columns:1fr 1fr;gap:0 16px}.grid3{display:grid;grid-template-columns:1fr 1fr 1fr;gap:0 16px}.mute{color:var(--mute)}.small{font-size:12px}.mono{font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12px}
.pill{display:inline-block;padding:1px 8px;border-radius:10px;font-size:11px;font-weight:600;background:var(--line)}.pill.ok{background:#e7f6ec;color:var(--ok)}.pill.no{background:#fdecea;color:var(--no)}.pill.warn{background:#fff4e0;color:var(--warn)}
.bars{display:flex;align-items:flex-end;gap:3px;height:120px;padding:6px 0;border-bottom:1px solid var(--line)}.bars div{flex:1;background:var(--teal2);border-radius:3px 3px 0 0;min-height:2px;position:relative}.bars div span{position:absolute;bottom:-18px;left:0;right:0;font-size:9px;color:var(--mute);text-align:center;white-space:nowrap;overflow:hidden}.bars div.u{background:var(--sun)}
.tabs{display:flex;gap:4px;margin-bottom:12px}.tabs a{padding:6px 12px;border-radius:6px;text-decoration:none;background:var(--line);color:var(--ink);font-weight:600;font-size:13px}.tabs a.on{background:var(--teal);color:#fff}
pre{background:var(--bg);padding:8px 10px;border-radius:6px;overflow:auto;font-size:12px;white-space:pre-wrap;word-break:break-word;margin:6px 0}.log{max-height:70vh;overflow:auto}
.pager{display:flex;gap:8px;align-items:center;margin-top:10px;font-size:13px}.filters{display:flex;gap:8px;flex-wrap:wrap;align-items:end;margin-bottom:12px}.filters label{margin:0 0 3px}.filters>div{min-width:130px}
details summary{cursor:pointer;color:var(--teal);font-size:12px}.danger{border-color:#f5c2bd}.codes{columns:2;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:15px;line-height:1.8}
.auth{max-width:420px;margin:60px auto;background:#fff;border:1px solid var(--line);border-radius:12px;padding:26px 28px}.auth h1{font-size:20px}.auth .brand{color:var(--teal);padding:0 0 10px}.auth .brand em{color:var(--sun)}.auth .brand span{color:var(--mute)}
@media(max-width:760px){.shell{flex-direction:column}aside{width:auto;flex-direction:row;flex-wrap:wrap;align-items:center}nav{display:flex;flex-wrap:wrap}.signout{margin:0}.grid2,.grid3{grid-template-columns:1fr}main{padding:14px}}
`

const consoleSrc = `
{{define "head"}}<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex,nofollow"><title>{{.}} · HS console</title><style>` + consoleCSS + `</style></head>{{end}}

{{define "layout"}}{{template "head" .Title}}
<body><div class="shell"><aside><div class="brand">Helderberg <em>Social</em><span>console</span></div>
<nav>{{range .Nav}}<a href="{{.Path}}" {{if eq .Path $.Active}}class="on"{{end}}><i>{{.Icon}}</i>{{.Label}}{{if and (eq .Path "/admin/queue") $.Pending}}<b class="badge">{{$.Pending}}</b>{{end}}</a>{{end}}</nav>
<form method="post" action="/admin/logout" class="signout"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>Sign out</button></form><p class="ver">v{{.Version}}</p></aside>
<main><h1>{{.Title}}</h1>{{if .Msg}}<p class="msg{{if .Err}} bad{{end}}">{{.Msg}}</p>{{end}}{{.Body}}</main></div></body></html>{{end}}

{{define "auth_shell"}}{{template "head" .Title}}<body><div class="auth"><div class="brand">Helderberg <em>Social</em><span>console</span></div>{{.Body}}<p class="small mute" style="margin-top:22px">v{{.Version}}</p></div></body></html>{{end}}

{{define "auth_login"}}{{template "head" "Sign in"}}<body><div class="auth"><div class="brand">Helderberg <em>Social</em><span>console</span></div>
<h1>Sign in</h1>{{if .D.Msg}}<p class="msg bad">{{.D.Msg}}</p>{{end}}
<p class="mute">Step 1 of 2: a single-use link is emailed to the admin address. Step 2 is your authenticator app.</p>
<form method="post" action="/admin/login"><input type="hidden" name="next" value="{{.D.Next}}"><label>Admin email</label><input type="email" name="email" required autocomplete="email" autofocus><p><button class="pri">Email me a sign-in link</button></p></form>
<p class="small mute">v{{.Version}}</p></div></body></html>{{end}}

{{define "auth_sent"}}{{template "head" "Check your email"}}<body><div class="auth"><div class="brand">Helderberg <em>Social</em><span>console</span></div>
<h1>Check your email</h1><p>If that is the admin address, a sign-in link is on its way. It works once and for 15 minutes.</p><p class="mute small">Nothing happens without the authenticator code, so a link in the wrong hands is useless on its own.</p>
<p class="small mute">v{{.Version}}</p></div></body></html>{{end}}

{{define "auth_enrol"}}{{template "head" "Set up your authenticator"}}<body><div class="auth" style="max-width:520px"><div class="brand">Helderberg <em>Social</em><span>console</span></div>
<h1>Set up your authenticator</h1>{{if .D.Msg}}<p class="msg bad">{{.D.Msg}}</p>{{end}}
<p>Open <b>Google Authenticator</b> (or any TOTP app), tap <b>+</b>, and scan this code.</p>
<p style="text-align:center"><img src="{{.D.QR}}" width="220" height="220" alt="QR code for the authenticator app"></p>
<details><summary>Can't scan? Enter the key by hand</summary><p class="mono">{{.D.Secret}}</p><p class="small mute">Account: {{.D.Account}} · Type: time-based · 6 digits · 30 seconds · SHA1</p></details>
<form method="post" action="/admin/enrol"><label>Enter the 6-digit code the app shows now</label><input type="text" name="code" inputmode="numeric" pattern="[0-9 ]*" autocomplete="one-time-code" required autofocus maxlength="7"><p><button class="pri">Confirm and finish</button></p></form>
<p class="small mute">The secret is stored encrypted. You will get 10 backup codes on the next page.</p></div></body></html>{{end}}

{{define "auth_backup"}}{{template "head" "Backup codes"}}<body><div class="auth"><div class="brand">Helderberg <em>Social</em><span>console</span></div>
<h1>Backup codes</h1><p>Your authenticator is set. <b>Write these down and keep them somewhere safe.</b> Each works once, in place of an authenticator code, if you lose your phone. They are shown only now.</p>
<div class="codes panel">{{range .D.Codes}}{{.}}<br>{{end}}</div>
<p><a class="btn pri" href="{{.D.Next}}">I have saved them, open the console</a></p></div></body></html>{{end}}

{{define "auth_twofa"}}{{template "head" "Authenticator code"}}<body><div class="auth"><div class="brand">Helderberg <em>Social</em><span>console</span></div>
<h1>Authenticator code</h1>{{if .D.Msg}}<p class="msg bad">{{.D.Msg}}</p>{{end}}
<p class="mute">Step 2 of 2: the 6-digit code from your authenticator app, or one of your backup codes.</p>
<form method="post" action="/admin/2fa"><label>Code</label><input type="text" name="code" inputmode="numeric" autocomplete="one-time-code" required autofocus maxlength="11"><p><button class="pri">Sign in</button></p></form>
<p class="small mute">Five wrong codes cancel this sign-in and you start again from the email link.</p></div></body></html>{{end}}

{{/* ---------- dashboard ---------- */}}
{{define "p_dashboard"}}{{$d := .D}}
{{if $d.Maintenance}}<p class="msg bad">Maintenance mode is ON: public submissions and subscriptions are refused. <a href="/admin/settings">Settings</a></p>{{end}}
<div class="cards">
<a class="card{{if $d.PendingEvents}} hot{{end}}" href="/admin/queue" style="text-decoration:none;color:inherit"><b>{{$d.PendingEvents}}</b><small>events to moderate</small></a>
<a class="card{{if $d.PendingListings}} hot{{end}}" href="/admin/queue" style="text-decoration:none;color:inherit"><b>{{$d.PendingListings}}</b><small>listings to moderate</small></a>
<a class="card{{if $d.PendingPosts}} hot{{end}}" href="/admin/queue" style="text-decoration:none;color:inherit"><b>{{$d.PendingPosts}}</b><small>posts to moderate</small></a>
<a class="card{{if $d.PendingPromoters}} hot{{end}}" href="/admin/promoters" style="text-decoration:none;color:inherit"><b>{{$d.PendingPromoters}}</b><small>promoter applications</small></a>
<a class="card" href="/admin/promoters" style="text-decoration:none;color:inherit"><b>{{$d.Promoters}}</b><small>promoters · {{$d.LivePosts}} posts live</small></a>
{{if $d.FBOn}}<a class="card{{if $d.FBFailed}} hot{{end}}" href="/admin/facebook" style="text-decoration:none;color:inherit"><b>{{$d.FBQueued}}{{if $d.FBFailed}} <span class="pill no">{{$d.FBFailed}} failed</span>{{end}}</b><small>Facebook posts queued</small></a>{{end}}
<div class="card"><b>{{$d.Upcoming}}</b><small>upcoming approved events</small></div>
<div class="card"><b>{{$d.Subs}}</b><small>subscribers <span class="mute">(+{{$d.SubsPending}} unconfirmed)</span></small></div>
<div class="card"><b>{{$d.Members}}</b><small><a href="/admin/members">members</a> <span class="mute">(+{{$d.MembersPending}} unconfirmed)</span></small></div>
<div class="card"><b>{{$d.PVToday}}</b><small>page views today · {{$d.UniqToday}} visitors</small></div>
<div class="card"><b>{{$d.ReqToday}}</b><small>API requests today · {{$d.ErrToday}} errors</small></div>
<div class="card{{if $d.MailFail}} hot{{end}}"><b>{{$d.MailDay}}</b><small>emails in 24h · {{$d.MailFail}} failed</small></div>
<div class="card"><b>{{$d.SourcesOn}}<span class="mute" style="font-size:14px">/{{$d.Sources}}</span></b><small>sources watched</small></div>
</div>
<div class="grid2">
<div class="panel"><h3>Page views, last 14 days</h3><div class="bars">{{$max := 1}}{{range $d.PV}}{{if gt .N $max}}{{$max = .N}}{{end}}{{end}}{{range $d.PV}}<div style="height:{{pct .N $max}}%" title="{{.Day}}: {{.N}} views, {{.N2}} visitors"><span>{{slice .Day 5}}</span></div>{{end}}</div><p class="small mute" style="margin-top:22px">Counts come from the page beacon; visitors are unique per day, hashed with a daily salt.</p></div>
<div class="panel"><h3>Status</h3><table>
<tr><td>Uptime</td><td>{{$d.Uptime}}</td></tr>
<tr><td>Last daily digest</td><td>{{or $d.LastDaily "never"}}</td></tr>
<tr><td>Last weekly digest</td><td>{{or $d.LastWeekly "never"}}</td></tr>
<tr><td>Last source check</td><td>{{ago $d.LastWatch}}</td></tr>
<tr><td>Authenticator</td><td>{{if $d.Enrolled}}enrolled {{ago $d.Enrolled}} · {{$d.Backup}} backup codes left{{else}}<span class="pill no">not enrolled</span>{{end}}</td></tr>
<tr><td>Active sessions</td><td>{{$d.Sessions}}</td></tr>
<tr><td>Announcement banner</td><td>{{if $d.Announcement}}<span class="pill ok">showing</span>{{else}}off{{end}}</td></tr>
</table>
<div class="row" style="margin-top:10px"><form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="return" value="/admin"><button name="action" value="watch" class="sm">Check sources now</button></form>
<form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="return" value="/admin"><input type="hidden" name="freq" value="weekly"><button name="action" value="digest-preview" class="sm">Preview weekly digest</button></form>
<form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="return" value="/admin"><button name="action" value="backup" class="sm">Back up now</button></form></div></div>
</div>
<div class="panel"><h3>Recent activity</h3><table>{{range $d.Audit}}<tr><td class="mute">{{ts .At}}</td><td><b>{{.Action}}</b></td><td>{{.Target}}</td><td class="mute">{{.Detail}}</td><td class="mono mute">{{.IP}}</td></tr>{{else}}<tr><td class="mute">Nothing yet.</td></tr>{{end}}</table><p class="small"><a href="/admin/logs?tab=audit">Full audit log</a></p></div>
{{end}}

{{/* ---------- queue ---------- */}}
{{define "event_card"}}<div class="panel"><b>{{.E.Title}}</b> <span class="pill {{statusCls .E.Status}}">{{.E.Status}}</span> <span class="pill">{{.E.Origin}}</span>{{if .E.MemberID}} <a class="pill ok" href="/admin/members/view?id={{.E.MemberID}}">{{if .E.Promoted}}promoter{{else}}member{{end}} #{{.E.MemberID}}</a>{{end}}{{if .E.Hidden}} <span class="pill warn">hidden by its promoter</span>{{end}}{{if .E.VisibleFrom}} <span class="pill">shows from {{date .E.VisibleFrom}}</span>{{end}}<br>
<span class="mute small">{{.E.When}} · {{town .E.Town}} · {{cat .E.Category}} · {{.E.Cost}}{{if .E.Listing}} · listing: {{.E.Listing}}{{end}}{{if .E.SubmitterName}} · from {{.E.SubmitterName}}{{end}} · added {{ago .E.CreatedAt}}</span>
{{if .E.Summary}}<div style="margin-top:6px;white-space:pre-wrap">{{.E.Summary}}</div>{{end}}
{{if .E.Website}}<div class="small" style="margin-top:4px"><a href="{{.E.Website}}" rel="noopener noreferrer" target="_blank">{{.E.Website}}</a></div>{{end}}{{if and .E.Source (ne .E.Source .E.Website)}}<div class="small mute">source: {{.E.Source}}</div>{{end}}
<form method="post" action="/admin/do" class="row" style="margin-top:8px"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="id" value="{{.E.ID}}"><input type="hidden" name="return" value="{{.Return}}">
{{if eq .E.Status "pending_review"}}<button class="ok" name="action" value="approve">Approve</button><button class="no" name="action" value="reject">Reject</button>{{end}}
<a class="btn" href="/admin/events/edit?id={{.E.ID}}">Edit</a></form></div>{{end}}

{{define "listing_card"}}<div class="panel"><b>{{.L.Name}}</b> <span class="pill {{statusCls .L.Status}}">{{.L.Status}}</span> <span class="mute small">#{{.L.ID}} · {{.L.Kind}}{{if .L.Existing}} → {{.L.Existing}}{{end}} · {{cat .L.Category}} · {{town .L.Town}} · {{.L.Cost}} · from {{.L.Submitter}} · {{ago .L.CreatedAt}}</span>
<div style="margin-top:6px;white-space:pre-wrap">{{.L.Summary}}</div>
{{if .L.Schedule}}<div class="small mute">When: {{.L.Schedule}}</div>{{end}}
{{if .L.Website}}<div class="small"><a href="{{.L.Website}}" rel="noopener noreferrer" target="_blank">{{.L.Website}}</a></div>{{end}}
<details><summary>Block for data/data.js</summary><pre>{{.L.DataJS}}</pre></details>
<form method="post" action="/admin/do" class="row" style="margin-top:8px"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="id" value="{{.L.ID}}"><input type="hidden" name="return" value="{{.Return}}">
{{if eq .L.Status "pending_review"}}<button class="ok" name="action" value="accept">Mark handled</button><button class="no" name="action" value="reject-listing">Reject</button>{{else}}<button class="sm" name="action" value="listing-delete">Delete</button>{{end}}</form></div>{{end}}

{{define "p_queue"}}{{$c := .CSRF}}
<h2>Events waiting for a decision ({{len .D.Events}})</h2>
{{range .D.Events}}{{template "event_card" (dict "E" . "CSRF" $c "Return" "/admin/queue")}}{{else}}<p class="mute">Nothing waiting.</p>{{end}}
<h2>Listing submissions waiting ({{len .D.Listings}})</h2>
{{range .D.Listings}}{{template "listing_card" (dict "L" . "CSRF" $c "Return" "/admin/queue")}}{{else}}<p class="mute">Nothing waiting.</p>{{end}}
<h2>Posts waiting ({{len .D.Posts}})</h2>
{{range .D.Posts}}{{template "post_card" (dict "P" . "CSRF" $c "Return" "/admin/queue")}}{{else}}<p class="mute">Nothing waiting.</p>{{end}}
<h2>Promoter applications ({{len .D.Applicants}})</h2>
{{range .D.Applicants}}{{template "promoter_card" (dict "P" . "CSRF" $c "Return" "/admin/queue")}}{{else}}<p class="mute">Nothing waiting.</p>{{end}}
{{end}}

{{define "post_card"}}<div class="panel"><b>{{.P.Title}}</b> <span class="pill {{statusCls .P.Status}}">{{.P.Status}}</span>{{if .P.Hidden}} <span class="pill warn">hidden by its promoter</span>{{end}} <a class="pill ok" href="/admin/members/view?id={{.P.MemberID}}">{{if .P.Org}}{{.P.Org}}{{else}}member #{{.P.MemberID}}{{end}}</a><br>
<span class="mute small">{{.P.When}} · {{town .P.Town}} · {{cat .P.Category}} · added {{ago .P.CreatedAt}}{{if ne .P.UpdatedAt .P.CreatedAt}} · edited {{ago .P.UpdatedAt}}{{end}}</span>
<div style="margin-top:6px;white-space:pre-wrap">{{.P.Body}}</div>
{{if .P.Link}}<div class="small" style="margin-top:4px"><a href="{{.P.Link}}" rel="noopener noreferrer" target="_blank">{{.P.Link}}</a></div>{{end}}
<form method="post" action="/admin/do" class="row" style="margin-top:8px"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="id" value="{{.P.ID}}"><input type="hidden" name="return" value="{{.Return}}">
{{if eq .P.Status "pending_review"}}<button class="ok" name="action" value="post-approve">Approve</button><button class="no" name="action" value="post-reject">Reject</button>{{else if eq .P.Status "approved"}}<button name="action" value="post-unapprove">Pull back to queue</button>{{end}}<button class="sm" name="action" value="post-delete">Delete</button></form></div>{{end}}

{{define "promoter_card"}}{{$p := .P}}<div class="panel"><b>{{$p.Org}}</b> <span class="pill {{if eq $p.Status "approved"}}ok{{else if eq $p.Status "pending"}}warn{{else}}no{{end}}">{{$p.Status}}</span>{{if $p.Trusted}} <span class="pill ok">trusted</span>{{end}} <span class="pill">{{$p.KindName}}</span> <a class="pill ok" href="/admin/members/view?id={{$p.MemberID}}">{{$p.Name}} · {{$p.Email}}</a><br>
<span class="mute small">{{join (townList $p.Towns) ", "}} · applied {{ago $p.AppliedAt}}{{if $p.DecidedAt}} · decided {{ago $p.DecidedAt}}{{end}} · {{$p.Events}} events · {{$p.Posts}} posts · {{$p.Calendars}} calendars</span>
<div class="small" style="margin-top:4px">{{if $p.Website}}<a href="{{$p.Website}}" rel="noopener noreferrer" target="_blank">{{$p.Website}}</a> {{end}}{{if $p.Facebook}}· fb: {{$p.Facebook}} {{end}}{{if $p.Instagram}}· ig: {{$p.Instagram}}{{end}}</div>
<div style="margin-top:6px;white-space:pre-wrap">{{$p.Blurb}}</div>{{if $p.Note}}<div class="small mute">note: {{$p.Note}}</div>{{end}}
<form method="post" action="/admin/do" class="row" style="margin-top:8px"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="id" value="{{$p.MemberID}}"><input type="hidden" name="return" value="{{.Return}}">
{{if eq $p.Status "approved"}}{{if $p.Trusted}}<button name="action" value="promoter-untrust">Stop trusting</button>{{else}}<button class="ok" name="action" value="promoter-trust">Trust (publish without a check)</button>{{end}}<input type="text" name="note" placeholder="note to them (optional)" style="width:220px"><button class="no" name="action" value="promoter-revoke">Revoke</button>{{else}}<button class="ok" name="action" value="promoter-approve">Approve</button>{{if eq $p.Status "pending"}}<input type="text" name="note" placeholder="reason (goes in their email, optional)" style="width:260px"><button class="no" name="action" value="promoter-decline">Decline</button>{{end}}{{end}}</form></div>{{end}}

{{define "p_promoters"}}{{$d := .D}}{{$c := .CSRF}}
<p class="mute small">Promoters post on their own behalf from <span class="mono">/account/promoter</span>: events with a show-from date and a hide switch, posts for the noticeboard, file imports and connected calendars, listings. Everything of theirs comes through the queue unless they are <b>trusted</b>, in which case it publishes at once and shows here and under Events marked <i>promoter</i>. Auto-published items are never queued for the Facebook page. Revoking pulls their published items back into the queue and switches their calendars off.</p>
<h2>Applications waiting ({{len $d.Pending}})</h2>
{{range $d.Pending}}{{template "promoter_card" (dict "P" . "CSRF" $c "Return" "/admin/promoters")}}{{else}}<p class="mute">None.</p>{{end}}
<h2>Approved ({{len $d.Approved}})</h2>
{{range $d.Approved}}{{template "promoter_card" (dict "P" . "CSRF" $c "Return" "/admin/promoters")}}{{else}}<p class="mute">None yet.</p>{{end}}
{{if $d.Declined}}<h2>Declined or revoked ({{len $d.Declined}})</h2>
{{range $d.Declined}}{{template "promoter_card" (dict "P" . "CSRF" $c "Return" "/admin/promoters")}}{{end}}{{end}}
<h2>Posts</h2>
<form method="get" action="/admin/promoters" class="filters"><div><label>Status</label><select name="posts"><option value="">any</option>{{range $x := (list "pending_review" "approved" "rejected")}}<option value="{{$x}}" {{if eq $x $d.Status}}selected{{end}}>{{$x}}</option>{{end}}</select></div><div><label>&nbsp;</label><button>Filter</button></div></form>
{{range $d.Posts}}{{template "post_card" (dict "P" . "CSRF" $c "Return" "/admin/promoters")}}{{else}}<p class="mute">No posts.</p>{{end}}
{{end}}

{{/* ---------- events ---------- */}}
{{define "p_events"}}{{$d := .D}}{{$c := .CSRF}}
<form method="get" action="/admin/events" class="filters">
<div><label>Status</label><select name="status"><option value="">any</option>{{range $d.Statuses}}<option value="{{.}}" {{if eq . $d.Status}}selected{{end}}>{{.}}</option>{{end}}</select></div>
<div><label>Origin</label><select name="origin"><option value="">any</option>{{range $d.Origins}}<option value="{{.}}" {{if eq . $d.Origin}}selected{{end}}>{{.}}</option>{{end}}</select></div>
<div><label>Town</label><select name="town"><option value="">any</option>{{range $k, $v := $d.Towns}}<option value="{{$k}}" {{if eq $k $d.Town}}selected{{end}}>{{$v}}</option>{{end}}</select></div>
<div style="min-width:220px"><label>Search</label><input type="text" name="q" value="{{$d.Q}}" placeholder="title, id or text"></div>
<div><button>Filter</button> <a class="btn pri" href="/admin/events/edit">New event</a></div></form>
<p class="small mute">{{$d.Total}} events</p>
<div class="panel" style="padding:0 6px"><table><tr><th>Date</th><th>Title</th><th>Where</th><th>Status</th><th>Origin</th><th></th></tr>
{{range $d.Events}}<tr><td style="white-space:nowrap">{{date .Date}}{{if .EndDate}} → {{date .EndDate}}{{end}}{{if .Time}}<br><span class="mute">{{.Time}}{{if .EndTime}}–{{.EndTime}}{{end}}</span>{{end}}</td>
<td><a href="/admin/events/edit?id={{.ID}}"><b>{{.Title}}</b></a><br><span class="mute small mono">{{.ID}}</span></td><td>{{town .Town}}<br><span class="mute small">{{cat .Category}}</span></td>
<td><span class="pill {{statusCls .Status}}">{{.Status}}</span>{{if .Hidden}} <span class="pill warn" title="hidden by its promoter">hidden</span>{{end}}{{if .VisibleFrom}} <span class="pill" title="scheduled by its promoter">from {{date .VisibleFrom}}</span>{{end}}</td><td>{{.Origin}}{{if .Promoted}} <span class="pill ok">promoter</span>{{end}}</td>
<td><form method="post" action="/admin/do" class="row tight"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="return" value="/admin/events?status={{$d.Status}}&origin={{$d.Origin}}&town={{$d.Town}}&q={{$d.Q}}&page={{$d.Page}}">
{{if eq .Status "pending_review"}}<button class="ok sm" name="action" value="approve">Approve</button><button class="no sm" name="action" value="reject">Reject</button>{{else if eq .Status "approved"}}<button class="sm" name="action" value="event-unapprove">Unpublish</button>{{else if eq .Status "rejected"}}<button class="ok sm" name="action" value="event-unapprove">Reopen</button>{{end}}</form></td></tr>
{{else}}<tr><td colspan="6" class="mute">No events match.</td></tr>{{end}}</table></div>
{{if gt $d.Pages 1}}<div class="pager">{{if gt $d.Page 1}}<a href="/admin/events?status={{$d.Status}}&origin={{$d.Origin}}&town={{$d.Town}}&q={{$d.Q}}&page={{sub $d.Page 1}}">← Newer</a>{{end}}<span class="mute">page {{$d.Page}} of {{$d.Pages}}</span>{{if lt $d.Page $d.Pages}}<a href="/admin/events?status={{$d.Status}}&origin={{$d.Origin}}&town={{$d.Town}}&q={{$d.Q}}&page={{add $d.Page 1}}">Older →</a>{{end}}</div>{{end}}
{{end}}

{{define "p_event_edit"}}{{$f := .D}}{{$e := $f.E}}
<form method="post" action="/admin/do" class="panel"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="id" value="{{$e.ID}}"><input type="hidden" name="return" value="{{if $f.New}}/admin/events{{else}}/admin/events/edit?id={{$e.ID}}{{end}}">
{{if not $f.New}}<p class="small mute">id <span class="mono">{{$e.ID}}</span> · origin {{$e.Origin}} · created {{ts $e.CreatedAt}}{{if $e.SubmitterName}} · submitted by {{$e.SubmitterName}}{{if $f.SubmitterEmail}} &lt;{{$f.SubmitterEmail}}&gt;{{end}}{{end}}{{if $f.IPHash}} · {{$f.IPHash}}{{end}}</p>{{end}}
<label>Title</label><input type="text" name="title" value="{{$e.Title}}" required maxlength="120">
<div class="grid3"><div><label>Date</label><input type="date" name="date" value="{{$e.Date}}" required></div><div><label>End date (optional)</label><input type="date" name="end_date" value="{{$e.EndDate}}"></div><div><label>Status</label><select name="status">{{range $s := (list "approved" "pending_review" "rejected")}}<option value="{{$s}}" {{if eq $s $e.Status}}selected{{end}}>{{$s}}</option>{{end}}</select></div></div>
<div class="grid2"><div><label>Start time</label><input type="text" name="time" value="{{$e.Time}}" placeholder="18:30" pattern="([01][0-9]|2[0-3]):[0-5][0-9]"></div><div><label>End time</label><input type="text" name="end_time" value="{{$e.EndTime}}" placeholder="21:00" pattern="([01][0-9]|2[0-3]):[0-5][0-9]"></div></div>
<div class="grid3"><div><label>Town</label><select name="town">{{range $f.Towns}}<option value="{{.}}" {{if eq . $e.Town}}selected{{end}}>{{town .}}</option>{{end}}</select></div><div><label>Category</label><select name="category">{{range $f.Cats}}<option value="{{.}}" {{if eq . $e.Category}}selected{{end}}>{{cat .}}</option>{{end}}</select></div><div><label>Cost</label><select name="cost">{{range $f.Costs}}<option value="{{.}}" {{if eq . $e.Cost}}selected{{end}}>{{.}}</option>{{end}}</select></div></div>
<label>Summary</label><textarea name="summary" maxlength="800">{{$e.Summary}}</textarea>
<div class="grid3"><div><label>Website</label><input type="url" name="website" value="{{$e.Website}}"></div><div><label>Source (where it came from)</label><input type="url" name="source" value="{{$e.Source}}"></div><div><label>Listing id (optional)</label><input type="text" name="listing" value="{{$e.Listing}}" placeholder="lourensford-market"></div></div>
<p class="row"><button class="pri" name="action" value="event-save">{{if $f.New}}Create event{{else}}Save changes{{end}}</button> <a class="btn" href="/admin/events">Back</a></p></form>
{{if not $f.New}}<form method="post" action="/admin/do" class="panel danger"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="id" value="{{$e.ID}}"><h3>Danger zone</h3><p class="row"><label class="inline" style="margin:0"><input type="checkbox" name="confirm" value="yes" required> I want to delete this event permanently</label><button class="no sm" name="action" value="event-delete">Delete event</button></p></form>{{end}}
{{end}}

{{/* ---------- listings ---------- */}}
{{define "p_listings"}}{{$d := .D}}{{$c := .CSRF}}
<div class="tabs">{{range $s := (list "" "pending_review" "accepted" "rejected" "pending_email")}}<a href="/admin/listings?status={{$s}}" {{if eq $s $d.Status}}class="on"{{end}}>{{or $s "all"}}</a>{{end}}</div>
<p class="small mute">{{$d.Total}} submissions. Listings live in data/data.js in the repo: accepting a submission means you have copied its block into the file.</p>
{{range $d.Listings}}{{template "listing_card" (dict "L" . "CSRF" $c "Return" (printf "/admin/listings?status=%s&page=%d" $d.Status $d.Page))}}{{else}}<p class="mute">Nothing here.</p>{{end}}
{{if gt $d.Pages 1}}<div class="pager">{{if gt $d.Page 1}}<a href="/admin/listings?status={{$d.Status}}&page={{sub $d.Page 1}}">← Newer</a>{{end}}<span class="mute">page {{$d.Page}} of {{$d.Pages}}</span>{{if lt $d.Page $d.Pages}}<a href="/admin/listings?status={{$d.Status}}&page={{add $d.Page 1}}">Older →</a>{{end}}</div>{{end}}
{{end}}

{{/* ---------- subscribers ---------- */}}
{{define "p_subscribers"}}{{$d := .D}}{{$c := .CSRF}}
<div class="cards"><div class="card"><b>{{$d.Confirmed}}</b><small>confirmed</small></div><div class="card"><b>{{$d.Pending}}</b><small>awaiting confirmation</small></div><div class="card"><b>{{$d.Daily}}</b><small>daily</small></div><div class="card"><b>{{$d.Weekly}}</b><small>weekly</small></div></div>
<form method="get" action="/admin/subscribers" class="filters"><div><label>Show</label><select name="f">{{range $s := (list "" "confirmed" "pending" "daily" "weekly" "email" "whatsapp")}}<option value="{{$s}}" {{if eq $s $d.Filter}}selected{{end}}>{{or $s "all"}}</option>{{end}}</select></div><div style="min-width:220px"><label>Email or number contains</label><input type="text" name="q" value="{{$d.Q}}"></div><div><button>Filter</button> <a class="btn" href="/admin/export/subscribers.csv">Export CSV</a></div></form>
<div class="panel" style="padding:0 6px"><table><tr><th>Subscriber</th><th>Plan</th><th>Filters</th><th>Status</th><th>Last digest</th><th></th></tr>
{{range $d.Subs}}<tr><td><a href="/admin/subscribers/edit?id={{.ID}}">{{if eq .Channel "whatsapp"}}{{.PhonePretty}}{{else}}{{.Email}}{{end}}</a> {{if eq .Channel "whatsapp"}}<span class="pill">WhatsApp</span>{{end}}<br><span class="mute small">joined {{ago .CreatedAt}}</span></td><td>{{.Frequency}} · {{.Horizon}}d</td><td class="small">{{if .Towns}}{{join .Towns ", "}}{{else}}all towns{{end}}<br>{{if .Categories}}{{join .Categories ", "}}{{else}}all categories{{end}}</td>
<td>{{if .Confirmed}}<span class="pill ok">confirmed</span>{{else}}<span class="pill warn">pending</span>{{end}}</td><td>{{ago .LastSent}}</td>
<td><form method="post" action="/admin/do" class="row tight"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="return" value="/admin/subscribers?f={{$d.Filter}}&q={{$d.Q}}&page={{$d.Page}}">
{{if not .Confirmed}}<button class="sm" name="action" value="sub-resend">Resend</button><button class="ok sm" name="action" value="sub-confirm">Confirm</button>{{end}}<button class="sm" name="action" value="sub-delete">Remove</button></form></td></tr>
{{else}}<tr><td colspan="6" class="mute">No subscribers match.</td></tr>{{end}}</table></div>
{{if gt $d.Pages 1}}<div class="pager">{{if gt $d.Page 1}}<a href="/admin/subscribers?f={{$d.Filter}}&q={{$d.Q}}&page={{sub $d.Page 1}}">← Newer</a>{{end}}<span class="mute">page {{$d.Page}} of {{$d.Pages}}</span>{{if lt $d.Page $d.Pages}}<a href="/admin/subscribers?f={{$d.Filter}}&q={{$d.Q}}&page={{add $d.Page 1}}">Older →</a>{{end}}</div>{{end}}
<form method="post" action="/admin/do" class="panel"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/subscribers"><h3>Add a subscriber by hand</h3><p class="small mute">Only for people who asked you directly: this skips the confirmation step (POPIA consent is on you). An email address adds an email subscriber; a phone number adds a WhatsApp one.</p><div class="row"><input type="text" name="email" placeholder="name@example.co.za or 082 123 4567" style="max-width:320px" required><button name="action" value="sub-add">Add as confirmed</button></div></form>
{{end}}

{{/* ---------- members ---------- */}}
{{define "p_members"}}{{$d := .D}}{{$c := .CSRF}}
<div class="cards"><div class="card"><b>{{$d.Active}}</b><small>confirmed members</small></div><div class="card"><b>{{$d.Pending}}</b><small>awaiting email confirmation</small></div><div class="card"><b>{{$d.Disabled}}</b><small>disabled</small></div><div class="card"><b>{{$d.EventsFromMembers}}</b><small>events posted by members</small></div></div>
<p class="small mute">People who created an account at <a href="{{$d.AccountURL}}/account/register">{{$d.AccountURL}}/account</a> to post events. Every event they post still lands in the <a href="/admin/queue">queue</a>; nothing goes live without you. Registrations are {{if $d.RegOn}}<span class="pill ok">open</span>{{else}}<span class="pill no">paused</span>{{end}} (<a href="/admin/settings">Settings</a>).</p>
<form method="get" action="/admin/members" class="filters"><div><label>Show</label><select name="f">{{range $s := (list "" "active" "pending" "disabled")}}<option value="{{$s}}" {{if eq $s $d.Filter}}selected{{end}}>{{or $s "all"}}</option>{{end}}</select></div><div style="min-width:220px"><label>Email or name contains</label><input type="text" name="q" value="{{$d.Q}}"></div><div><button>Filter</button></div></form>
<div class="panel" style="padding:0 6px"><table><tr><th>Member</th><th>Status</th><th class="num">Events</th><th>Joined</th><th>Last sign-in</th><th></th></tr>
{{range $d.Members}}<tr><td><a href="/admin/members/view?id={{.ID}}">{{.Name}}</a><br><span class="mute small">{{.Email}}</span></td>
<td>{{if ne .Status "active"}}<span class="pill no">disabled</span>{{else if .VerifiedAt}}<span class="pill ok">confirmed</span>{{else}}<span class="pill warn">unconfirmed</span>{{end}}{{range .LoginList}} <span class="pill">{{.}}</span>{{end}}</td><td class="num">{{.Events}}</td><td>{{ago .CreatedAt}}</td><td>{{ago .LastLoginAt}}</td>
<td><form method="post" action="/admin/do" class="row tight"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="return" value="/admin/members?f={{$d.Filter}}&q={{$d.Q}}&page={{$d.Page}}">
{{if not .VerifiedAt}}<button class="sm" name="action" value="member-resend">Resend</button><button class="ok sm" name="action" value="member-verify">Confirm</button>{{end}}{{if eq .Status "active"}}<button class="sm" name="action" value="member-disable">Disable</button>{{else}}<button class="ok sm" name="action" value="member-enable">Enable</button>{{end}}</form></td></tr>
{{else}}<tr><td colspan="6" class="mute">No members match.</td></tr>{{end}}</table></div>
{{if gt $d.Pages 1}}<div class="pager">{{if gt $d.Page 1}}<a href="/admin/members?f={{$d.Filter}}&q={{$d.Q}}&page={{sub $d.Page 1}}">&larr; Newer</a>{{end}}<span class="mute">page {{$d.Page}} of {{$d.Pages}}</span>{{if lt $d.Page $d.Pages}}<a href="/admin/members?f={{$d.Filter}}&q={{$d.Q}}&page={{add $d.Page 1}}">Older &rarr;</a>{{end}}</div>{{end}}
{{end}}

{{define "p_member_view"}}{{$d := .D}}{{$m := $d.M}}{{$c := .CSRF}}
<div class="panel"><p><b>{{$m.Name}}</b> &middot; {{$m.Email}} &middot; {{if ne $m.Status "active"}}<span class="pill no">disabled</span>{{else if $m.VerifiedAt}}<span class="pill ok">confirmed</span> {{ts $m.VerifiedAt}}{{else}}<span class="pill warn">email not confirmed</span>{{end}}{{range $m.LoginList}} &middot; <span class="pill">{{.}}</span>{{end}}{{if not $m.HasPassword}} <span class="mute small">(no password)</span>{{end}}</p>
<table><tr><td>Member id</td><td class="mono">#{{$m.ID}}</td></tr><tr><td>Joined</td><td>{{ts $m.CreatedAt}}</td></tr><tr><td>Last sign-in</td><td>{{ago $m.LastLoginAt}}</td></tr><tr><td>Active sessions</td><td>{{$d.Sessions}}</td></tr><tr><td>Sign-up IP tag</td><td class="mono">{{$m.IPHash}}</td></tr></table>
<form method="post" action="/admin/do" class="row" style="margin-top:10px"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{$m.ID}}"><input type="hidden" name="return" value="/admin/members/view?id={{$m.ID}}">
{{if not $m.VerifiedAt}}<button name="action" value="member-resend">Resend confirmation</button><button class="ok" name="action" value="member-verify">Confirm now</button>{{end}}{{if eq $m.Status "active"}}<button name="action" value="member-disable">Disable account</button>{{else}}<button class="ok" name="action" value="member-enable">Enable account</button>{{end}}{{if $d.Sessions}}<button name="action" value="member-signout">Sign out everywhere</button>{{end}}<a class="btn" href="/admin/members">Back</a></form>
<p class="small mute">Disabling keeps the account and its events but the person cannot sign in; their published events stay on the site. Enable to let them back in.</p></div>
{{if $d.P}}<h2>Promoter</h2>{{template "promoter_card" (dict "P" $d.P "CSRF" $c "Return" (printf "/admin/members/view?id=%d" $m.ID))}}
{{if $d.Calendars}}<div class="panel"><b>Connected calendars</b><table>{{range $d.Calendars}}<tr><td>{{.Label}} {{if .Enabled}}<span class="pill ok">on</span>{{else}}<span class="pill no">off</span>{{end}}</td><td class="small"><a href="{{.URL}}" rel="noopener noreferrer" target="_blank">{{short .URL 60}}</a></td><td class="small">{{if .Checked}}{{ago .Checked}}: {{short .Status 48}}{{else}}not yet checked{{end}}</td><td><a class="btn sm" href="/admin/sources">Sources</a></td></tr>{{end}}</table></div>{{end}}
<h3>Posts from this member ({{len $d.Posts}})</h3>
{{range $d.Posts}}{{template "post_card" (dict "P" . "CSRF" $c "Return" (printf "/admin/members/view?id=%d" $m.ID))}}{{else}}<p class="mute">None yet.</p>{{end}}{{end}}
<h2>Events from this member ({{len $d.Events}})</h2>
{{range $d.Events}}{{template "event_card" (dict "E" . "CSRF" $c "Return" (printf "/admin/members/view?id=%d" $m.ID))}}{{else}}<p class="mute">None yet.</p>{{end}}
<form method="post" action="/admin/do" class="panel danger"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{$m.ID}}"><h3>Danger zone</h3><p class="row"><label class="inline" style="margin:0"><input type="checkbox" name="confirm" value="yes"> yes, I mean it</label><button class="sm" name="action" value="member-delete">Delete account</button><button class="no sm" name="action" value="member-block">Block address and delete</button></p><p class="small mute">Deleting removes the account, its sessions and its unpublished events; published events stay on the site with the name and email removed (the same as when a member deletes their own account). Blocking also stores a hash of the address so it cannot register or submit again.</p></form>
{{end}}

{{define "p_subscriber_edit"}}{{$f := .D}}{{$s := $f.S}}
<form method="post" action="/admin/do" class="panel"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="id" value="{{$s.ID}}"><input type="hidden" name="return" value="/admin/subscribers/edit?id={{$s.ID}}">
<p><b>{{if eq $s.Channel "whatsapp"}}{{$s.PhonePretty}} <span class="pill">WhatsApp</span>{{else}}{{$s.Email}}{{end}}</b> · {{if $s.Confirmed}}confirmed {{ts $s.ConfirmedAt}}{{else}}<span class="pill warn">not confirmed</span>{{end}} · joined {{ts $s.CreatedAt}} · last digest {{ago $s.LastSent}}</p>
<div class="grid2"><div><label>Frequency</label><select name="frequency">{{range $x := (list "daily" "weekly")}}<option value="{{$x}}" {{if eq $x $s.Frequency}}selected{{end}}>{{$x}}</option>{{end}}</select></div><div><label>Horizon (days)</label><select name="horizon">{{range $x := (list "7" "14" "30")}}<option value="{{$x}}" {{if eq $x (printf "%d" $s.Horizon)}}selected{{end}}>{{$x}}</option>{{end}}</select></div></div>
<label>Towns (none = all)</label><div class="row">{{range $f.Towns}}<label class="inline" style="margin:0"><input type="checkbox" name="towns" value="{{.}}" {{if index $f.TownSet .}}checked{{end}}> {{town .}}</label>{{end}}</div>
<label>Categories (none = all)</label><div class="row">{{range $f.Cats}}<label class="inline" style="margin:0"><input type="checkbox" name="categories" value="{{.}}" {{if index $f.CatSet .}}checked{{end}}> {{cat .}}</label>{{end}}</div>
<p class="row" style="margin-top:12px"><button class="pri" name="action" value="sub-save">Save</button>{{if not $s.Confirmed}}<button name="action" value="sub-resend">Resend confirmation</button><button class="ok" name="action" value="sub-confirm">Confirm now</button>{{end}}<a class="btn" href="/admin/subscribers">Back</a></p></form>
<form method="post" action="/admin/do" class="panel danger"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="id" value="{{$s.ID}}"><h3>Danger zone</h3><p class="row"><button class="sm" name="action" value="sub-delete">Remove subscriber</button><button class="no sm" name="action" value="sub-block">Block this address and remove</button></p><p class="small mute">Blocking stores only a hash of the address; future subscriptions and submissions from it are silently dropped.</p></form>
{{end}}

{{/* ---------- digests ---------- */}}
{{define "p_digests"}}{{$d := .D}}{{$c := .CSRF}}
<div class="cards"><div class="card"><b>{{$d.Daily}}</b><small>daily subscribers</small></div><div class="card"><b>{{$d.Weekly}}</b><small>weekly subscribers</small></div><div class="card"><b>{{$d.Upcoming7}}</b><small>approved events, next 7 days</small></div><div class="card"><b>{{$d.Upcoming30}}</b><small>next 30 days</small></div></div>
<div class="grid2"><div class="panel"><h3>Schedule</h3><table><tr><td>Digests</td><td>{{if $d.On}}<span class="pill ok">on</span>{{else}}<span class="pill no">paused</span>{{end}}</td></tr><tr><td>Send hour</td><td>{{$d.Hour}}:00 local</td></tr><tr><td>Weekly day</td><td>{{$d.Day}}</td></tr><tr><td>Next daily</td><td>{{$d.NextDaily}}</td></tr><tr><td>Next weekly</td><td>{{$d.NextWeekly}}</td></tr><tr><td>Last daily sent</td><td>{{or $d.LastDaily "never"}}</td></tr><tr><td>Last weekly sent</td><td>{{or $d.LastWeekly "never"}}</td></tr></table><p class="small"><a href="/admin/settings">Change the schedule</a></p></div>
<div class="panel"><h3>Send</h3>
<form method="post" action="/admin/do" class="row"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/digests"><select name="freq" style="width:auto"><option value="daily">daily</option><option value="weekly" selected>weekly</option></select><button name="action" value="digest-preview">Send me a preview</button></form>
<p class="small mute">A preview goes only to the admin address, 7-day horizon, all towns.</p>
<form method="post" action="/admin/do" class="row" style="margin-top:14px"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/digests"><select name="freq" style="width:auto"><option value="daily">daily</option><option value="weekly">weekly</option></select><label class="inline" style="margin:0"><input type="checkbox" name="confirm" value="yes"> yes, send to every subscriber now</label><button class="pri" name="action" value="digest-send">Send now</button></form>
<p class="small mute">Sending now also marks today's scheduled run as done, so nobody gets it twice.</p></div></div>
<div class="panel"><h3>History (from the mail log, last 30 days)</h3><table><tr><th>Day</th><th class="num">Daily sent</th><th class="num">Weekly sent</th><th class="num">Failed</th></tr>{{range $d.History}}<tr><td>{{.Day}}</td><td class="num">{{.Daily}}</td><td class="num">{{.Weekly}}</td><td class="num">{{if .Failed}}<span class="pill no">{{.Failed}}</span>{{else}}0{{end}}</td></tr>{{else}}<tr><td class="mute" colspan="4">No digests sent yet.</td></tr>{{end}}</table></div>
{{end}}

{{/* ---------- facebook ---------- */}}
{{define "fb_tabs"}}<div class="tabs"><a href="/admin/facebook" {{if eq . "/admin/facebook"}}class="on"{{end}}>Page</a><a href="/admin/facebook/groups" {{if eq . "/admin/facebook/groups"}}class="on"{{end}}>Groups</a></div>{{end}}

{{define "p_facebook"}}{{$d := .D}}{{$c := .CSRF}}{{template "fb_tabs" "/admin/facebook"}}
<div class="cards"><div class="card"><b>{{index $d.Counts "queued"}}</b><small>queued</small></div><div class="card"><b>{{index $d.Counts "posted"}}</b><small>posted</small></div><div class="card{{if index $d.Counts "failed"}} hot{{end}}"><b>{{index $d.Counts "failed"}}</b><small>failed</small></div><div class="card"><b>{{len $d.Candidates}}</b><small>approved events, next 60 days</small></div></div>
<div class="grid2"><div class="panel"><h3>Page {{if $d.On}}<span class="pill ok">connected</span>{{else}}<span class="pill">off</span>{{end}}</h3>
{{if $d.On}}<table><tr><td>Page id</td><td class="mono">{{$d.PageID}}</td></tr><tr><td>Graph API</td><td class="mono">{{$d.Version}}</td></tr><tr><td>Approved events</td><td>{{if $d.EventsOn}}<span class="pill ok">posted automatically</span> {{$d.Delay}} min after approval{{else}}<span class="pill">not posted automatically</span>{{end}}</td></tr><tr><td>Weekend list</td><td>{{if $d.WeekendOn}}<span class="pill ok">on</span> {{$d.WeekendDay}}s at {{$d.WeekendHour}}:00, next {{$d.NextWeekend}}{{else}}<span class="pill">off</span>{{end}}</td></tr><tr><td>Last weekend list</td><td>{{or $d.LastWeekend "never"}}</td></tr></table>
<p class="row" style="margin-top:8px"><form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{$c}}"><button class="sm" name="action" value="fb-check">Check the connection</button></form> <a class="btn sm" href="/admin/settings">Change the automatic posting</a></p>
<p class="small mute">Posts leave one a minute, oldest first, so a burst of approvals never floods the page. A failed post retries three times a quarter-hour apart unless Meta says the token or permission is wrong. Token setup: <span class="mono">docs/facebook.md</span>.</p>
{{else}}<p class="small mute">Not configured. Set <span class="mono">HS_FB_PAGE_ID</span> and <span class="mono">HS_FB_PAGE_TOKEN</span> in <span class="mono">api/.env</span> (how to get a non-expiring Page token: <span class="mono">docs/facebook.md</span>) and restart. Until then nothing here posts anywhere.</p>{{end}}</div>
<div class="panel"><h3>Write a post</h3>
<form method="post" action="/admin/do"><input type="hidden" name="csrf" value="{{$c}}"><label>Text</label><textarea name="message" maxlength="5000" required placeholder="What's on, who it's for, where to find out more."></textarea>
<div class="grid2"><div><label>Link (optional, shown as a card)</label><input type="url" name="link" placeholder="https://helderbergsocial.co.za/..."></div><div><label>When (blank = now)</label><input type="datetime-local" name="when" min="{{$d.Now}}"></div></div>
<p class="row" style="margin-top:10px"><button class="pri" name="action" value="fb-compose" {{if not $d.On}}disabled{{end}}>Queue the post</button><span class="small mute">Local time. Up to 60 days ahead.</span></p></form></div></div>
<div class="grid2"><div class="panel"><h3>Post an approved event</h3>{{if $d.Candidates}}<form method="post" action="/admin/do" class="row"><input type="hidden" name="csrf" value="{{$c}}"><select name="id" style="width:auto;max-width:420px">{{range $d.Candidates}}<option value="{{.ID}}" {{if index $d.Posted .ID}}disabled{{end}}>{{date .Date}} · {{.Title}} ({{town .Town}}){{if index $d.Posted .ID}} · already queued/posted{{end}}</option>{{end}}</select><button name="action" value="fb-event" {{if not $d.On}}disabled{{end}}>Queue it</button></form><p class="small mute">The post is the event's title, date, time, town, cost and summary, with the event's own link as the card. An event is posted at most once.</p>{{else}}<p class="mute">No approved events in the next 60 days.</p>{{end}}</div>
<div class="panel"><h3>This weekend's list</h3>{{if $d.WeekendPreview}}<pre>{{$d.WeekendPreview}}</pre><form method="post" action="/admin/do" class="row"><input type="hidden" name="csrf" value="{{$c}}"><button name="action" value="fb-weekend" {{if or (not $d.On) $d.WeekendQueued}}disabled{{end}}>{{if $d.WeekendQueued}}Already queued or posted{{else}}Queue it now{{end}}</button><span class="small mute">Weekend of {{$d.WeekendRef}}.</span></form>{{else}}<p class="mute">Nothing approved for the weekend of {{$d.WeekendRef}}, so no list would be posted.</p>{{end}}</div></div>
<div class="panel"><h3>Queue</h3><table><tr><th>Due</th><th>Kind</th><th>Text</th><th></th></tr>{{range $d.Queue}}<tr><td class="mono">{{ts .DueAt}}</td><td><span class="pill">{{.Kind}}</span>{{if .Tries}}<div class="small mute">attempt {{.Tries}} failed: {{.Err}}</div>{{end}}</td><td><details><summary>{{short .Message 90}}{{if gt (len .Message) 90}}…{{end}}</summary><pre>{{.Message}}</pre>{{if .Link}}<div class="small mono">{{.Link}}</div>{{end}}</details></td><td class="row tight"><form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><button class="sm" name="action" value="fb-now">Post now</button> <button class="sm no" name="action" value="fb-cancel">Cancel</button></form></td></tr>{{else}}<tr><td class="mute" colspan="4">Nothing queued.</td></tr>{{end}}</table></div>
<div class="panel"><h3>History</h3><table><tr><th>When</th><th>Kind</th><th>Status</th><th>Text</th><th></th></tr>{{range $d.History}}<tr><td class="mono">{{ts (or .PostedAt .DueAt)}}</td><td><span class="pill">{{.Kind}}</span></td><td>{{if eq .Status "posted"}}<a class="pill ok" href="{{.Permalink}}" target="_blank" rel="noopener">posted ↗</a>{{else if eq .Status "failed"}}<span class="pill no">failed</span><div class="small mute">{{.Err}}</div>{{else}}<span class="pill">{{.Status}}</span>{{end}}</td><td><details><summary>{{short .Message 90}}{{if gt (len .Message) 90}}…{{end}}</summary><pre>{{.Message}}</pre></details></td><td>{{if ne .Status "posted"}}<form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><button class="sm" name="action" value="fb-retry">Post now</button></form>{{end}}</td></tr>{{else}}<tr><td class="mute" colspan="5">Nothing posted yet.</td></tr>{{end}}</table><p class="small mute">Posts already on the page are never taken down from here; do that on Facebook.</p></div>
{{end}}

{{/* ---------- facebook groups ---------- */}}
{{define "p_fb_groups"}}{{$d := .D}}{{$c := .CSRF}}{{template "fb_tabs" "/admin/facebook/groups"}}
<div class="cards"><div class="card{{if $d.DueTotal}} hot{{end}}"><b>{{$d.DueTotal}}</b><small>due now</small></div><div class="card"><b>{{$d.Enabled}}</b><small>groups on the rota</small></div><div class="card"><b>{{$d.Skipped}}</b><small>switched off</small></div><div class="card"><b>{{$d.Posted30}}</b><small>posted in the last 30 days</small></div><div class="card"><b>{{$d.Posted}}</b><small>posts recorded, all time</small></div></div>
<p class="small mute">The page is a member of these groups. Meta's API cannot post in groups and driving the browser by script is against Facebook's rules, so a person posts: open the group, switch the composer to post <b>as the page</b>, paste the text, then press <b>Mark posted</b> here and the group is booked again after its cadence (30 days by default). Reminder email {{if $d.RemindOn}}<span class="pill ok">on</span> at {{$d.Hour}}:00, {{$d.PerDay}} groups a day{{else}}<span class="pill">off</span>{{end}} · last sent {{or $d.LastRemind "never"}} · <a href="/admin/settings">change</a>.
<form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{$c}}"><button class="sm" name="action" value="grp-remind">Email me today's batch now</button></form></p>
{{if $d.Preview}}<div class="panel"><h3>Post for {{$d.PreviewFor.Name}}</h3><pre id="pv">{{$d.Preview}}</pre><p class="row"><a class="btn sm" href="{{$d.PreviewFor.URL}}" target="_blank" rel="noopener">Open the group ↗</a><form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{$d.PreviewFor.ID}}"><button class="ok sm" name="action" value="grp-posted">Mark posted</button></form><a class="btn sm" href="/admin/facebook/groups">Close</a></p></div>{{end}}
<div class="panel"><h3>Today's batch ({{len $d.Due}} of {{$d.DueTotal}} due)</h3>{{if $d.Due}}<table><tr><th>Group</th><th>Kind</th><th>State</th><th>Post text</th><th></th></tr>{{range $d.Due}}{{$g := .}}<tr><td><b><a href="{{.URL}}" target="_blank" rel="noopener">{{.Name}} ↗</a></b>{{if .Note}}<div class="small mute">{{.Note}}</div>{{end}}</td><td class="small">{{index $d.Kinds .Kind}}</td><td><span class="pill warn">{{.State $d.Today}}</span>{{if .LastPostedAt}}<div class="small mute">last {{ago .LastPostedAt}}, {{.Posts}} so far</div>{{end}}</td><td><details><summary>show</summary><pre>{{index $d.Texts .ID}}</pre></details></td>
<td class="row tight"><form method="post" action="/admin/do" class="row tight"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><button class="ok sm" name="action" value="grp-posted">Mark posted</button><button class="sm" name="action" value="grp-defer" title="Push out a week">Later</button></form></td></tr>{{end}}</table>{{else}}<p class="mute">Nothing due. {{if $d.Rest}}The next ones are at the top of the list below.{{end}}</p>{{end}}</div>
<div class="panel" style="padding:0 6px"><table><tr><th>Group</th><th>Kind</th><th>Every</th><th>Next due</th><th>Posted</th><th></th></tr>
{{range $d.Rest}}{{$g := .}}<tr {{if not .Enabled}}style="opacity:.55"{{end}}><td><b><a href="{{.URL}}" target="_blank" rel="noopener">{{.Name}} ↗</a></b>{{if .Note}}<div class="small mute">{{.Note}}</div>{{end}}{{if .SkipReason}}<div class="small mute">off: {{.SkipReason}}</div>{{end}}</td><td class="small">{{index $d.Kinds .Kind}}</td><td class="small">{{.Cadence}} days</td><td class="small">{{if .Enabled}}{{if .NextDue}}{{date .NextDue}}{{else}}now{{end}} <span class="pill {{if eq (.State $d.Today) "scheduled"}}ok{{else}}warn{{end}}">{{.State $d.Today}}</span>{{else}}<span class="pill">off</span>{{end}}</td><td class="small">{{.Posts}}{{if .LastPostedAt}}<div class="mute">last {{ago .LastPostedAt}}</div>{{end}}</td>
<td><form method="post" action="/admin/do" class="row tight"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><a class="btn sm" href="/admin/facebook/groups?preview={{.ID}}">Post text</a>{{if .Enabled}}<button class="ok sm" name="action" value="grp-posted">Mark posted</button><button class="sm" name="action" value="grp-skip">Switch off</button>{{else}}<button class="sm" name="action" value="grp-enable">Switch on</button>{{end}}<button class="no sm" name="action" value="grp-delete">Remove</button></form>
<details><summary>edit</summary><form method="post" action="/admin/do"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><label>Name</label><input type="text" name="name" value="{{.Name}}"><label>Facebook id or address</label><input type="text" name="fb_id" value="{{.FBID}}"><div class="grid3"><div><label>Kind</label><select name="kind">{{range $d.KindOrder}}<option value="{{.}}" {{if eq . $g.Kind}}selected{{end}}>{{index $d.Kinds .}}</option>{{end}}</select></div><div><label>Town (optional)</label><select name="town"><option value="">any</option>{{range $d.Towns}}<option value="{{.}}" {{if eq . $g.Town}}selected{{end}}>{{town .}}</option>{{end}}</select></div><div><label>Post every (days)</label><input type="number" name="cadence" value="{{.Cadence}}" min="{{$d.CadenceMin}}" max="{{$d.CadenceMax}}"></div></div><label>Note (group rules, what to lead with)</label><input type="text" name="note" value="{{.Note}}"><p><button class="sm pri" name="action" value="grp-save">Save</button></p></form></details></td></tr>
{{else}}<tr><td colspan="6" class="mute">No groups on the list.</td></tr>{{end}}</table></div>
<form method="post" action="/admin/do" class="panel"><input type="hidden" name="csrf" value="{{$c}}"><h3>Add a group</h3><p class="small mute">Join the group as the page on Facebook first, then add it here. The shipped list is <span class="mono">api/fb-groups.json</span>; groups added here survive deploys.</p>
<div class="grid2"><div><label>Name</label><input type="text" name="name" required placeholder="Helderberg Community"></div><div><label>Facebook id or address</label><input type="text" name="fb_id" required placeholder="https://www.facebook.com/groups/624783775519689/"></div></div>
<div class="grid3"><div><label>Kind</label><select name="kind">{{range $d.KindOrder}}<option value="{{.}}">{{index $d.Kinds .}}</option>{{end}}</select></div><div><label>Town (optional)</label><select name="town"><option value="">any</option>{{range $d.Towns}}<option value="{{.}}">{{town .}}</option>{{end}}</select></div><div><label>Post every (days)</label><input type="number" name="cadence" value="30" min="{{$d.CadenceMin}}" max="{{$d.CadenceMax}}"></div></div>
<label>Note (group rules, what to lead with)</label><input type="text" name="note" placeholder="no selling posts; lead with family events"><p><button class="pri" name="action" value="grp-save">Add the group</button></p></form>
{{end}}

{{/* ---------- sources ---------- */}}
{{define "p_sources"}}{{$d := .D}}{{$c := .CSRF}}
<div class="row" style="margin-bottom:12px"><span class="mute">Watching {{if $d.On}}<span class="pill ok">on</span>{{else}}<span class="pill no">off</span>{{end}} · every {{$d.Interval}} min · last check {{ago $d.LastWatch}}</span>
<form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/sources"><button name="action" value="watch">Check all now</button></form></div>
<div class="panel wide" style="padding:0 6px"><table class="compact"><tr><th>Source</th><th>Kind</th><th>Maps to</th><th>Last check</th><th>Status</th><th class="num">Events</th><th></th></tr>
{{range $d.Sources}}{{$s := .}}<tr {{if not .Enabled}}style="opacity:.55"{{end}}><td><b>{{.Label}}</b>{{if .Org}} <span class="pill ok" title="connected by a promoter">{{.Org}}</span>{{end}} <a class="small mute" href="{{.URL}}" rel="noopener noreferrer" target="_blank" title="{{.URL}}">{{short .URL 48}} ↗</a></td><td>{{.Kind}}</td><td class="small">{{town .Town}} · {{cat .Category}}{{if .Listing}} · <span class="mono">{{.Listing}}</span>{{end}}{{if .Match}} · <span class="mute" title="{{.Match}}">filter: <span class="mono">{{short .Match 28}}</span></span>{{end}}</td><td class="small">{{ago .Checked}}{{if .Changed}} <span class="mute">· changed {{ago .Changed}}</span>{{end}}</td><td class="small">{{if .Status}}<span class="pill {{if hasPrefix .Status "error"}}no{{else if eq .Status "changed"}}warn{{else if hasPrefix .Status "retired"}}{{else}}ok{{end}}" title="{{.Status}}">{{short .Status 48}}</span>{{else}}<span class="mute">not yet checked</span>{{end}}</td><td class="num">{{.Events}}</td>
<td><div class="row tight"><form method="post" action="/admin/do" class="row tight"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="return" value="/admin/sources"><button class="sm" name="action" value="watch-one">Check</button><button class="sm" name="action" value="source-toggle">{{if .Enabled}}Disable{{else}}Enable{{end}}</button><button class="sm" name="action" value="source-forget" title="Forget what was seen so everything is offered again">Forget</button><button class="no sm" name="action" value="source-delete">Delete</button></form>
<details><summary>edit</summary><form method="post" action="/admin/do"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="return" value="/admin/sources"><label>Label</label><input type="text" name="label" value="{{.Label}}"><label>URL</label><input type="url" name="url" value="{{.URL}}"><div class="grid2"><div><label>Kind</label><select name="kind">{{range $d.Kinds}}<option value="{{.}}" {{if eq . $s.Kind}}selected{{end}}>{{.}}</option>{{end}}</select></div><div><label>Listing id</label><input type="text" name="listing" value="{{.Listing}}"></div></div><div class="grid2"><div><label>Town</label><select name="town">{{range $d.Towns}}<option value="{{.}}" {{if eq . $s.Town}}selected{{end}}>{{town .}}</option>{{end}}</select></div><div><label>Category</label><select name="category">{{range $d.Cats}}<option value="{{.}}" {{if eq . $s.Category}}selected{{end}}>{{cat .}}</option>{{end}}</select></div></div><label>Only queue events matching (ics feeds; pattern, case-insensitive; blank = everything)</label><input type="text" name="match" value="{{.Match}}" placeholder="somerset|strand|gordon|helderberg"><p><button class="sm pri" name="action" value="source-save">Save</button></p></form></details></div></td></tr>
{{else}}<tr><td colspan="7" class="mute">No sources.</td></tr>{{end}}</table></div>
<form method="post" action="/admin/do" class="panel"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/sources"><h3>Add a source</h3><p class="small mute">An <b>ics</b> feed becomes queued events automatically (a recurring series is queued once, at its next date, with the repeat rule in the summary). An <b>html</b> page is watched for changes and you get an email when it changes. A <b>list</b> (an aggregator's RSS feed or a what's-on index that changes daily) is read for its links and only links you have never been shown are emailed, so a busy page does not alert on every check. A feed that covers the whole province can carry a filter so only events that mention the Helderberg are queued.</p>
<div class="grid2"><div><label>Label</label><input type="text" name="label" required placeholder="Lourensford events"></div><div><label>URL</label><input type="url" name="url" required placeholder="https://…"></div></div>
<div class="grid2"><div><label>Kind</label><select name="kind"><option value="html">html page (watch for changes)</option><option value="ics">ics calendar feed</option><option value="list">list (RSS or index page; only new links are reported)</option></select></div><div><label>Listing id (optional)</label><input type="text" name="listing" placeholder="lourensford-wine-estate"></div></div>
<div class="grid2"><div><label>Town</label><select name="town">{{range $d.Towns}}<option value="{{.}}">{{town .}}</option>{{end}}</select></div><div><label>Category</label><select name="category">{{range $d.Cats}}<option value="{{.}}" {{if eq . "community"}}selected{{end}}>{{cat .}}</option>{{end}}</select></div></div>
<label>Only queue events (or list links) matching (optional; pattern, case-insensitive)</label><input type="text" name="match" placeholder="somerset|strand|gordon|helderberg|sir lowry">
<p><button class="pri" name="action" value="source-save">Add source</button></p></form>
{{end}}

{{/* ---------- analytics ---------- */}}
{{define "p_analytics"}}{{$d := .D}}
<div class="tabs">{{range $n := (list "7" "30" "90")}}<a href="/admin/analytics?days={{$n}}" {{if eq $n (printf "%d" $d.Days)}}class="on"{{end}}>{{$n}} days</a>{{end}}</div>
<div class="cards"><div class="card"><b>{{$d.TotalPV}}</b><small>page views</small></div><div class="card"><b>{{$d.TotalUniq}}</b><small>daily visitors (sum)</small></div><div class="card"><b>{{$d.TotalReq}}</b><small>API requests</small></div><div class="card"><b>{{$d.TotalErr}}</b><small>API errors (4xx/5xx)</small></div></div>
<div class="panel"><h3>Page views per day <span class="mute">(orange = visitors)</span></h3><div class="bars">{{range $d.PV}}<div style="height:{{pct .N $d.MaxPV}}%" title="{{.Day}}: {{.N}} views, {{.N2}} visitors"></div><div class="u" style="height:{{pct .N2 $d.MaxPV}}%" title="{{.Day}}: {{.N2}} visitors"></div>{{end}}</div></div>
<div class="grid2"><div class="panel"><h3>Top pages</h3><table><tr><th>Path</th><th class="num">Views</th><th class="num">Visitors</th></tr>{{range $d.Top}}<tr><td class="mono">{{.K}}</td><td class="num">{{.N}}</td><td class="num">{{.N2}}</td></tr>{{else}}<tr><td class="mute" colspan="3">No page views recorded yet. The site sends a beacon on every page once the API is live.</td></tr>{{end}}</table></div>
<div class="panel"><h3>API routes</h3><table><tr><th>Route</th><th class="num">Requests</th><th class="num">Errors</th></tr>{{range $d.Routes}}<tr><td class="mono">{{.K}}</td><td class="num">{{.N}}</td><td class="num">{{.N2}}</td></tr>{{else}}<tr><td class="mute" colspan="3">Nothing yet.</td></tr>{{end}}</table></div></div>
<div class="panel"><h3>API requests per day <span class="mute">(orange = errors)</span></h3><div class="bars">{{range $d.Req}}<div style="height:{{pct .N $d.MaxReq}}%" title="{{.Day}}: {{.N}} requests, {{.N2}} errors"></div><div class="u" style="height:{{pct .N2 $d.MaxReq}}%"></div>{{end}}</div></div>
<div class="grid3"><div class="panel"><h3>New confirmed subscribers</h3><table>{{range $d.SubGrowth}}<tr><td>{{.Day}}</td><td class="num">{{.N}}</td></tr>{{else}}<tr><td class="mute">None in this period.</td></tr>{{end}}</table></div>
<div class="panel"><h3>Approved events by town</h3><table>{{range $d.Towns}}<tr><td>{{town .K}}</td><td class="num">{{.N}}</td></tr>{{end}}</table></div>
<div class="panel"><h3>Approved events by category</h3><table>{{range $d.Cats}}<tr><td>{{cat .K}}</td><td class="num">{{.N}}</td></tr>{{end}}</table></div></div>
{{end}}

{{/* ---------- logs ---------- */}}
{{define "p_logs"}}{{$d := .D}}
<div class="tabs">{{range $t := (list "requests" "mail" "audit" "app")}}<a href="/admin/logs?tab={{$t}}" {{if eq $t $d.Tab}}class="on"{{end}}>{{title $t}}</a>{{end}}</div>
{{if eq $d.Tab "requests"}}<p class="small mute">The last {{len $d.Reqs}} requests, newest first, in memory since start. Addresses are shown as hashes.</p><div class="panel log" style="padding:0 6px"><table><tr><th>When</th><th>Method</th><th>Path</th><th class="num">Status</th><th class="num">ms</th><th>Client</th></tr>{{range $d.Reqs}}<tr><td class="mono">{{.At.Format "15:04:05"}}</td><td>{{.Method}}</td><td class="mono">{{.Path}}</td><td class="num">{{if ge .Status 400}}<span class="pill no">{{.Status}}</span>{{else}}{{.Status}}{{end}}</td><td class="num">{{.Ms}}</td><td class="mono mute">{{.IP}}</td></tr>{{else}}<tr><td class="mute">Nothing yet.</td></tr>{{end}}</table></div>
{{else if eq $d.Tab "mail"}}<p class="row small"><a href="/admin/logs?tab=mail">All</a> · <a href="/admin/logs?tab=mail&fail=1">Failures only</a> <span class="mute">· recipients are stored as hashes only</span></p><div class="panel log" style="padding:0 6px"><table><tr><th>When</th><th>Kind</th><th>To (hash)</th><th>Result</th></tr>{{range $d.Mail}}<tr><td>{{ts .At}}</td><td>{{.Kind}}</td><td class="mono mute">{{.ToHash}}</td><td>{{if .OK}}<span class="pill ok">sent</span>{{else}}<span class="pill no">failed</span> <span class="small">{{.Err}}</span>{{end}}</td></tr>{{else}}<tr><td class="mute">Nothing yet.</td></tr>{{end}}</table></div>
{{else if eq $d.Tab "audit"}}<div class="panel log" style="padding:0 6px"><table><tr><th>When</th><th>Action</th><th>Target</th><th>Detail</th><th>Client</th></tr>{{range $d.Audit}}<tr><td>{{ts .At}}</td><td><b>{{.Action}}</b></td><td class="mono">{{.Target}}</td><td class="small">{{.Detail}}</td><td class="mono mute">{{.IP}}</td></tr>{{else}}<tr><td class="mute">Nothing yet.</td></tr>{{end}}</table></div>
{{else}}<p class="small mute">The last {{len $d.Lines}} log lines, newest first. The container's own log has the full history.</p><div class="panel log"><pre>{{range $d.Lines}}{{.}}
{{end}}</pre></div>{{end}}
{{end}}

{{/* ---------- security ---------- */}}
{{define "p_security"}}{{$d := .D}}{{$c := .CSRF}}
{{if $d.NewCodes}}<div class="panel danger"><h3>Your new backup codes</h3><p>Write them down now. This is the only time they are shown, and the old ones no longer work.</p><div class="codes">{{range $d.NewCodes}}{{.}}<br>{{end}}</div></div>{{end}}
<div class="grid2">
<div class="panel"><h3>How sign-in works</h3><table><tr><td>Factor 1</td><td>Single-use link emailed to <b>{{$d.AdminEmail}}</b>, 15 minutes</td></tr><tr><td>Factor 2</td><td>Time-based code from Google Authenticator (RFC 6238) or a backup code</td></tr><tr><td>Authenticator</td><td>{{if $d.Enrolled}}<span class="pill ok">enrolled</span> {{ts $d.EnrolledAt}}{{else}}<span class="pill no">not enrolled</span>{{end}}</td></tr><tr><td>Backup codes left</td><td>{{$d.BackupLeft}} of 10</td></tr><tr><td>Session</td><td>12 hours, 2 hours idle, cookie HttpOnly SameSite=Strict{{if $d.Secure}} Secure{{end}}</td></tr><tr><td>Wrong codes</td><td>5 per link, then the link is dead</td></tr><tr><td>Passwords</td><td>none exist</td></tr></table></div>
<div class="panel"><h3>Authenticator</h3>
<form method="post" action="/admin/do"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/security"><label>Current authenticator code</label><div class="row"><input type="text" name="code" inputmode="numeric" autocomplete="one-time-code" style="max-width:140px" required><button name="action" value="backup-codes">Regenerate backup codes</button><button class="no" name="action" value="totp-reset">Remove authenticator</button></div></form>
<p class="small mute">Removing the authenticator signs out every session; the next sign-in enrols a new one. If the phone is already lost, use a backup code to sign in first. If everything is lost, set <span class="mono">HS_TOTP_RESET=1</span> in the container's env for one restart.</p></div></div>
<div class="panel"><h3>Active sessions</h3><table><tr><th>Started</th><th>Last seen</th><th>Expires</th><th>Client</th><th>Browser</th><th></th></tr>{{range $d.Sessions}}<tr><td>{{ts .CreatedAt}}</td><td>{{ago .LastSeen}}</td><td>{{ts .ExpiresAt}}</td><td class="mono mute">{{.IPHash}}</td><td class="small">{{short .UA 80}}</td><td>{{if .Current}}<span class="pill ok">this one</span>{{else}}<form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/security"><input type="hidden" name="id" value="{{.Hash}}"><button class="sm" name="action" value="session-revoke">Revoke</button></form>{{end}}</td></tr>{{end}}</table>
<form method="post" action="/admin/do" style="margin-top:8px"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/security"><button class="sm" name="action" value="session-revoke-others">Sign out every other session</button></form></div>
<div class="panel"><h3>Sign-in history</h3><table>{{range $d.Logins}}<tr><td>{{ts .At}}</td><td><b>{{.Action}}</b></td><td class="small">{{.Detail}}</td><td class="mono mute">{{.IP}}</td></tr>{{else}}<tr><td class="mute">Nothing yet.</td></tr>{{end}}</table></div>
<div class="panel"><h3>Blocklist</h3><p class="small mute">Blocked addresses are dropped silently. Emails are stored as hashes; clients as the <span class="mono">ip:xxxxxxxx</span> tag from the logs.</p>
<table>{{range $d.Blocks}}<tr><td>{{.Kind}}</td><td class="mono">{{short .Value 16}}</td><td>{{.Note}}</td><td class="mute">{{ts .CreatedAt}}</td><td><form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/security"><input type="hidden" name="id" value="{{.ID}}"><button class="sm" name="action" value="block-remove">Unblock</button></form></td></tr>{{else}}<tr><td class="mute">Nothing blocked.</td></tr>{{end}}</table>
<form method="post" action="/admin/do" class="row" style="margin-top:8px"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/security"><select name="kind" style="width:auto"><option value="email">email</option><option value="ip">client tag</option></select><input type="text" name="value" placeholder="name@example.com or ip:1a2b3c4d" style="max-width:260px" required><input type="text" name="note" placeholder="why" style="max-width:200px"><button name="action" value="block-add">Block</button></form></div>
<div class="panel"><h3>Rate limits and hardening (fixed)</h3><table><tr><td>GET</td><td>60/min per client, burst 30</td></tr><tr><td>POST</td><td>6/min per client, burst 6 (public writes and the sign-in steps)</td></tr><tr><td>Console</td><td>120/min per client, burst 60</td></tr><tr><td>Body</td><td>32 KB</td></tr><tr><td>Mail per recipient</td><td>5/hour, 20/day</td></tr><tr><td>Headers</td><td>CSP (no scripts), HSTS, nosniff, referrer same-origin, frame DENY, no-store</td></tr><tr><td>CORS</td><td>site origin only (own origin for form posts)</td></tr><tr><td>Container</td><td>scratch image, non-root, read-only filesystem, no capabilities</td></tr></table></div>
{{end}}

{{/* ---------- settings ---------- */}}
{{define "p_settings"}}{{$d := .D}}{{$c := .CSRF}}
<form method="post" action="/admin/do" class="panel"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/settings">
<p class="small mute">These take effect immediately and survive restarts. Blank fields fall back to the environment default shown.</p>
{{range $d.Rows}}{{$row := .}}<label>{{.Label}}</label>
{{if eq .Kind "bool"}}<label class="inline" style="margin:0;font-weight:400;color:inherit"><input type="checkbox" name="{{.Key}}" value="1" {{if eq .Value "1"}}checked{{end}}> {{.Help}}</label>
{{else if eq .Kind "select-day"}}<select name="{{.Key}}" style="max-width:220px">{{range $i, $n := $d.Days}}<option value="{{$i}}" {{if eq (printf "%d" $i) $row.Value}}selected{{end}}>{{$n}}</option>{{end}}</select><div class="small mute">{{.Help}}</div>
{{else if eq .Kind "int"}}<input type="number" name="{{.Key}}" value="{{.Value}}" style="max-width:160px"><div class="small mute">{{.Help}} Default {{.Default}}.</div>
{{else}}<input type="text" name="{{.Key}}" value="{{.Value}}" maxlength="200"><div class="small mute">{{.Help}}</div>{{end}}
{{end}}
<p class="row" style="margin-top:14px"><button class="pri" name="action" value="settings-save">Save settings</button><button name="action" value="settings-reset">Reset all to defaults</button></p></form>
<div class="panel"><h3>Environment (read-only)</h3><table>{{range $d.Env}}<tr><td class="mono">{{index . 0}}</td><td class="mono">{{index . 1}}</td></tr>{{end}}</table><p class="small mute">Change these in api/.env on the host and restart the container.</p></div>
{{end}}

{{/* ---------- system ---------- */}}
{{define "p_system"}}{{$d := .D}}{{$c := .CSRF}}
{{if $d.Integrity}}<p class="msg">{{$d.Integrity}}</p>{{end}}
<div class="panel"><h3>Sign in with Google, Microsoft, Yahoo</h3>
<table>{{range $d.Logins}}<tr><td style="white-space:nowrap">{{.Name}} {{if .On}}<span class="pill ok">on</span>{{else}}<span class="pill">off</span>{{end}}</td><td>{{if .On}}client id <span class="mono">{{.ClientID}}</span> · redirect URI <span class="mono">{{.Redirect}}</span> · {{.Members}} member{{if ne .Members 1}}s{{end}} linked{{else}}<span class="mute">set <span class="mono">{{.Env}}</span></span>{{end}}<br><span class="small mute">{{.Note}}</span></td></tr>{{end}}</table>
<p class="small mute">OpenID Connect authorization-code flow (PKCE where the provider supports it). Google and Yahoo vouch for the address, so those members skip the confirmation mail; a Microsoft address counts as verified only for a personal account or when the app registration sends the <span class="mono">xms_edov</span> claim, otherwise the person still gets our confirmation mail and cannot attach to an existing account by address alone. The redirect URI must be listed on each provider's app exactly. Buttons appear only for providers that are on. Runbook: <span class="mono">docs/accounts.md</span>.</p></div>
<div class="panel"><h3>WhatsApp {{if $d.WAOn}}<span class="pill ok">on</span>{{else}}<span class="pill">off</span>{{end}}</h3>
{{if $d.WAOn}}<table><tr><td>Phone number id</td><td class="mono">{{$d.WAPhoneID}}</td></tr><tr><td>Graph API</td><td class="mono">{{$d.WAVersion}} · templates in {{$d.WALang}}</td></tr><tr><td>Webhook</td><td class="mono">{{$d.WAWebhook}}</td></tr>{{range $n, $st := $d.WATemplates}}<tr><td>Template <span class="mono">{{$n}}</span></td><td>{{$st}}</td></tr>{{end}}</table>{{if $d.WATemplateNote}}<p class="small mute">{{$d.WATemplateNote}}</p>{{end}}
<p class="small mute">Business-initiated messages are always one of the two templates above; free text goes out only as a reply within 24 h of a person writing to us (their Confirm tap or STOP). Setup runbook and template texts: <span class="mono">docs/whatsapp.md</span>.</p>
<form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/system"><button class="sm" name="action" value="test-whatsapp">Send the confirm template to {{if $d.WAAdminPhone}}the admin phone{{else}}HS_ADMIN_PHONE (not set){{end}}</button></form>
{{else}}<p class="small mute">Not configured. Set <span class="mono">HS_WA_PHONE_ID</span>, <span class="mono">HS_WA_TOKEN</span>, <span class="mono">HS_WA_APP_SECRET</span> and <span class="mono">HS_WA_VERIFY_TOKEN</span> (runbook in <span class="mono">docs/whatsapp.md</span>). The site hides the WhatsApp option while this is off.</p>{{end}}</div>
<div class="panel"><h3>Outbound mail {{if $d.MailOK}}<span class="pill ok">DNS complete</span>{{else}}<span class="pill warn">DNS incomplete</span>{{end}}</h3><table><tr><td>Mode</td><td>{{$d.MailMode}}</td></tr><tr><td>From</td><td class="mono">{{$d.MailFrom}}</td></tr><tr><td>HELO</td><td class="mono">{{$d.MailHelo}}</td></tr></table><p class="small mute">Each record is checked against live DNS on every load. Publish them with <span class="mono">docs/dns-setup.py --mail</span>; the values are also served at <span class="mono">/api/mail-dns</span>.</p><table style="margin-top:8px"><tr><th></th><th>Record</th><th>Should be</th><th>Is</th></tr>{{range $d.MailRecords}}<tr><td>{{if .OK}}<span class="pill ok">ok</span>{{else}}<span class="pill warn">missing</span>{{end}}</td><td class="mono">{{.Type}} {{.Name}}<div class="small mute" style="font-family:inherit">{{.Why}}</div></td><td class="mono" style="word-break:break-all;max-width:360px">{{.Want}}</td><td class="mono" style="word-break:break-all;max-width:360px">{{if .Have}}{{.Have}}{{else}}<span class="mute">nothing</span>{{end}}</td></tr>{{end}}</table><form method="post" action="/admin/do" class="inline" style="margin-top:8px"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/system"><button class="sm" name="action" value="test-mail">Send a test email to the admin address</button></form></div>
<div class="grid2"><div class="panel"><h3>Service</h3><table><tr><td>Version</td><td class="mono">{{$d.Version}}</td></tr><tr><td>Go</td><td class="mono">{{$d.Go}}</td></tr><tr><td>Started</td><td>{{ts $d.Started}} (up {{$d.Uptime}})</td></tr><tr><td>Goroutines</td><td>{{$d.Goroutines}}</td></tr><tr><td>Heap in use</td><td>{{$d.Mem}}</td></tr><tr><td>Last housekeeping</td><td>{{or $d.LastHousekeeping "never"}}</td></tr></table>
<div class="row" style="margin-top:10px"><form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/system"><button class="sm" name="action" value="housekeeping">Run housekeeping</button> <button class="sm" name="action" value="test-mail">Send test email</button> <button class="sm" name="action" value="flush-stats">Flush stats</button></form></div></div>
<div class="panel"><h3>Database</h3><table><tr><td>File</td><td class="mono">{{$d.DBPath}}</td></tr><tr><td>Size</td><td>{{$d.DBSize}}{{if $d.WALSize}} + WAL {{$d.WALSize}}{{end}}</td></tr><tr><td>Schema</td><td>v{{$d.Schema}}</td></tr></table>
<table style="margin-top:8px">{{range $d.Tables}}<tr><td class="mono">{{.K}}</td><td class="num">{{.N}}</td></tr>{{end}}</table>
<div class="row" style="margin-top:10px"><form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/system"><button class="sm" name="action" value="integrity">Integrity check</button> <button class="sm" name="action" value="checkpoint">Checkpoint WAL</button></form> <a class="btn sm" href="/admin/export/all.json">Export everything (JSON)</a></div></div></div>
<div class="panel"><h3>Backups</h3><p class="small mute">Snapshots are written inside the data volume (<span class="mono">backups/</span>); the newest 14 are kept. The host's nightly backup.sh copies the volume off the machine.</p>
<form method="post" action="/admin/do"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/system"><button class="pri sm" name="action" value="backup">Back up now</button></form>
<table style="margin-top:8px">{{range $d.Backups}}<tr><td class="mono">{{.Name}}</td><td>{{.Size}}</td><td class="mute">{{ts .At}}</td><td><a class="btn sm" href="/admin/backups/{{.Name}}">Download</a> <form method="post" action="/admin/do" class="inline"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="return" value="/admin/system"><input type="hidden" name="id" value="{{.Name}}"><button class="sm" name="action" value="backup-delete">Delete</button></form></td></tr>{{else}}<tr><td class="mute">No snapshots yet.</td></tr>{{end}}</table></div>
{{end}}
`
