package main

// Account pages for promoters. Appended to accountSrc; same layout, CSS and
// funcs as the rest of /account. No scripts (CSP), so every control is a
// plain form.

const promoterSrc = `
{{define "acc_promoter_apply"}}{{$d := .D}}{{$c := .CSRF}}
{{if $d.P}}{{if eq $d.P.Status "pending"}}<div class="panel"><p><span class="pill warn">Waiting for a look</span> &nbsp;Your application for <b>{{$d.P.Org}}</b> ({{$d.P.KindName}}) went in {{ago $d.P.AppliedAt}}. A person reads every one, usually within a day or two, and you get an email either way.</p><p class="hint">Meanwhile you can still post individual events under <a href="/account">My events</a>.</p></div>
{{else}}<div class="panel"><p><span class="pill no">Not approved</span> &nbsp;We looked at the application for <b>{{$d.P.Org}}</b> and could not approve it{{if $d.P.Note}}: <i>{{$d.P.Note}}</i>{{else}}.{{end}}</p><p class="hint">If things have changed you can apply again below. Individual events still work under <a href="/account">My events</a>.</p></div>{{end}}{{end}}
<div class="panel"><h2 style="margin-top:0">What a promoter account gives you</h2>
<p>Helderberg Social lists what is on in Somerset West, Strand, Gordon's Bay and Sir Lowry's Pass. If you put on events, run a venue, club, school or church, market things for others, or reach people as a creator, a promoter account lets you post on your own behalf, as yourself, not as an anonymous submission:</p>
<ul class="steps"><li><b>Events</b>: add as many as you like, <b>schedule</b> them (choose the day they first show), <b>hide</b> and show them again, edit, remove.</li><li><b>Posts</b>: short notices for the noticeboard (specials, sign-ups, calls for volunteers, new season dates) that run between two dates you set.</li><li><b>Import</b> a whole season from an .ics or .csv file, or <b>connect a calendar</b> we check every few hours.</li><li><b>Listings</b>: add your group, activity or place to the directory without the email round trip.</li></ul>
<p>Everything still gets a quick look by a person before it shows, unless we mark you trusted after a while. What we publish: things residents can go to, join or use, in the Helderberg. What we don't: adverts with nothing in them for the community, things outside the area, and anything we would not want our own families to see.</p></div>
{{if or (not $d.P) (ne $d.P.Status "pending")}}<form method="post" action="/account/promoter/apply" class="panel"><input type="hidden" name="csrf" value="{{$c}}"><h2 style="margin-top:0">Apply</h2>
<label for="org">Name you post under</label><input type="text" id="org" name="org" required maxlength="80" value="{{if $d.P}}{{$d.P.Org}}{{end}}" placeholder="e.g. Strand Surf Club, Helderberg Markets, your own name">
<div class="grid2"><div><label for="kind">What best describes you</label><select id="kind" name="kind">{{range $d.Kinds}}<option value="{{.}}" {{if and $d.P (eq . $d.P.Kind)}}selected{{end}}>{{index $d.KindNames .}}</option>{{end}}</select></div><div><label for="website">Website</label><input type="url" id="website" name="website" value="{{if $d.P}}{{$d.P.Website}}{{end}}" placeholder="https://"></div>
<div><label for="facebook">Facebook page</label><input type="text" id="facebook" name="facebook" value="{{if $d.P}}{{$d.P.Facebook}}{{end}}" placeholder="facebook.com/yourpage"></div><div><label for="instagram">Instagram</label><input type="text" id="instagram" name="instagram" value="{{if $d.P}}{{$d.P.Instagram}}{{end}}" placeholder="@yourname"></div></div>
<p class="hint">At least one of website, Facebook or Instagram, so we can see who you are.</p>
<label>Towns you post about</label><div class="row" style="margin-top:4px">{{range $d.Towns}}<label class="inline" style="margin:0"><input type="checkbox" name="towns" value="{{.}}" {{if and $d.P (has $d.P.Towns .)}}checked{{end}}> {{town .}}</label>{{end}}</div>
<label for="blurb">What would you post?</label><textarea id="blurb" name="blurb" required minlength="30" maxlength="600">{{if $d.P}}{{$d.P.Blurb}}{{end}}</textarea><p class="hint">A few sentences: the kind of events or notices, how often, who they are for.</p>
<div class="row"><button class="pri">Send application</button><a class="btn" href="/account">Cancel</a></div></form>{{end}}
{{end}}

{{define "acc_promoter"}}{{$d := .D}}{{$c := .CSRF}}{{$p := $d.P}}
<div class="panel"><div class="ev" style="border-top:0;padding-top:0"><div><span class="pill ok">Approved promoter</span>{{if .V.Member.Trusted}} <span class="pill ok">trusted: publishes at once</span>{{else}} <span class="pill">each item gets a quick check</span>{{end}}<div class="meta">{{$p.KindName}} · {{join (townList $p.Towns) ", "}}{{if $p.Website}} · <a href="{{$p.Website}}">{{$p.Website}}</a>{{end}}</div></div>
<div class="acts"><span class="pill">{{$d.Live}} live</span>{{if $d.Waiting}}<span class="pill warn">{{$d.Waiting}} waiting</span>{{end}}</div></div>
<div class="row"><a class="btn pri" href="/account/events/new">Add an event</a><a class="btn pri" href="/account/promoter/posts/new">Add a post</a><a class="btn" href="/account/promoter/import">Import / connect a calendar</a><a class="btn" href="/account/promoter/listing">Add a listing</a></div></div>

<h2>Posts <span class="hint" style="display:inline">notices that run between two dates</span></h2>
<div class="panel">{{range $d.Posts}}<div class="ev"><div><b>{{.Title}}</b> <span class="pill {{statusCls .Status}}">{{.StatusText}}</span><div class="meta">{{.When}} · {{.TownName}} · {{.CatName}}{{if .Live}} · <a href="{{.Live}}">see it on the site</a>{{end}}</div></div>
<div class="acts">{{if .Editable}}<a class="btn sm" href="/account/promoter/posts/edit?id={{.ID}}">Edit</a>
<form method="post" action="/account/promoter/posts/toggle"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="to" value="{{if .Hidden}}show{{else}}hide{{end}}"><button class="sm">{{if .Hidden}}Show{{else}}Hide{{end}}</button></form>{{end}}
<form method="post" action="/account/promoter/posts/delete"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><label class="inline" style="margin:0;font-size:13px"><input type="checkbox" name="confirm" value="yes"> delete</label> <button class="no sm">Delete</button></form></div></div>
{{else}}<p>No posts yet. A post is a short notice, a special, a call for volunteers, new term dates, that shows on the <a href="{{$d.Site}}/notices.html">noticeboard</a> for the days you choose.</p>{{end}}</div>

<h2>Events</h2>
<div class="panel">{{range $d.Events}}<div class="ev"><div><b>{{.Title}}</b> <span class="pill {{statusCls .Status}}">{{.StatusText}}</span><div class="meta">{{.When}} · {{.TownName}} · {{.CatName}}{{if .Live}} · <a href="{{.Live}}">see it on the site</a>{{end}}</div></div>
<div class="acts">{{if .Editable}}<a class="btn sm" href="/account/events/edit?id={{.ID}}">Edit</a>
<form method="post" action="/account/promoter/events/toggle"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="return" value="/account/promoter"><input type="hidden" name="to" value="{{if .Hidden}}show{{else}}hide{{end}}"><button class="sm">{{if .Hidden}}Show{{else}}Hide{{end}}</button></form>{{end}}
<form method="post" action="/account/events/withdraw"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><label class="inline" style="margin:0;font-size:13px"><input type="checkbox" name="confirm" value="yes"> remove</label> <button class="no sm">Remove</button></form></div></div>
{{else}}<p>No events yet. <a href="/account/events/new">Add one</a>, or <a href="/account/promoter/import">import a file or connect a calendar</a>.</p>{{end}}</div>

<h2>Connected calendars</h2>
<div class="panel">{{range $d.Calendars}}<div class="ev"><div><b>{{.Label}}</b> {{if .Enabled}}<span class="pill ok">watching</span>{{else}}<span class="pill no">off</span>{{end}}<div class="meta">{{.URL}}{{if .Checked}} · checked {{ago .Checked}}: {{.Status}}{{end}}</div></div>
<div class="acts"><form method="post" action="/account/promoter/calendar/check"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><button class="sm">Check now</button></form><form method="post" action="/account/promoter/calendar/remove"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><button class="no sm">Remove</button></form></div></div>
{{else}}<p>None. <a href="/account/promoter/import">Connect a public .ics calendar</a> (Google Calendar, Outlook, a website's calendar feed) and new events appear here on their own.</p>{{end}}</div>
{{end}}

{{define "acc_post_form"}}{{$f := .D}}{{$p := $f.P}}
<form method="post" action="/account/promoter/posts/save" class="panel"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="id" value="{{$p.ID}}">
<p class="hint" style="margin:0 0 6px">A post is a short notice on the <b>noticeboard</b>, not an event: a special, a sign-up window, a call for volunteers, new term dates, an opening. It shows from its start date to its end date, then drops off on its own.{{if not $f.Trusted}} A person checks it first, usually within a day.{{end}}</p>
<label for="title">Title</label><input type="text" id="title" name="title" value="{{$p.Title}}" required maxlength="120" placeholder="e.g. Junior lifesaving sign-ups open">
<div class="grid2"><div><label for="starts">Show from</label><input type="date" id="starts" name="starts" value="{{$p.Starts}}" required></div><div><label for="ends">Until (last day it shows)</label><input type="date" id="ends" name="ends" value="{{$p.Ends}}" required><p class="hint">At most 90 days.</p></div>
<div><label for="town">Town</label><select id="town" name="town">{{range $f.Towns}}<option value="{{.}}" {{if eq . $p.Town}}selected{{end}}>{{town .}}</option>{{end}}</select></div><div><label for="category">Category</label><select id="category" name="category">{{range $f.Cats}}<option value="{{.}}" {{if eq . $p.Category}}selected{{end}}>{{cat .}}</option>{{end}}</select></div></div>
<label for="link">Link (optional: where to sign up, read more, book)</label><input type="url" id="link" name="link" value="{{$p.Link}}" placeholder="https://">
<label for="body">The notice</label><textarea id="body" name="body" required minlength="20" maxlength="600">{{$p.Body}}</textarea><p class="hint">Plain text, up to 600 characters. Say what, where, who it is for and what to do.</p>
<div class="row"><button class="pri">{{if $f.New}}{{if $f.Trusted}}Publish{{else}}Send for checking{{end}}{{else}}Save changes{{end}}</button><a class="btn" href="/account/promoter">Cancel</a></div></form>
{{end}}

{{define "acc_import"}}{{$d := .D}}{{$c := .CSRF}}
<form method="post" action="/account/promoter/import" enctype="multipart/form-data" class="panel"><input type="hidden" name="csrf" value="{{$c}}"><h2 style="margin-top:0">Import from a file</h2>
<p class="hint" style="margin:0 0 6px">Upload an <b>.ics</b> calendar export (Google Calendar, Outlook, Apple Calendar, most ticketing sites) or a <b>.csv</b> spreadsheet. You see every row and what we made of it before anything is added. Recurring entries come in as their next date with the pattern in the summary. Up to 200 rows, 2 MB.</p>
<label for="file">File</label><input type="file" id="file" name="file" accept=".ics,.csv,text/calendar,text/csv" required>
<div class="grid2"><div><label for="town">Town for rows without one</label><select id="town" name="town">{{range $d.Towns}}<option value="{{.}}">{{town .}}</option>{{end}}</select></div><div><label for="category">Category for rows without one</label><select id="category" name="category">{{range $d.Cats}}<option value="{{.}}" {{if eq . "community"}}selected{{end}}>{{cat .}}</option>{{end}}</select></div></div>
<p class="hint">CSV columns (header row, any order, only the first three required): <span style="font-family:ui-monospace,monospace">title, date, summary, end_date, time, end_time, town, category, cost, website</span>. Dates as 2026-10-31 or 31/10/2026; times as 18:30 or 6:30pm.</p>
<div class="row"><button class="pri">Preview</button><a class="btn" href="/account/promoter">Back</a></div></form>

<form method="post" action="/account/promoter/calendar" class="panel"><input type="hidden" name="csrf" value="{{$c}}"><h2 style="margin-top:0">Connect a calendar</h2>
<p class="hint" style="margin:0 0 6px">Paste the public <b>.ics address</b> of a calendar and we check it every few hours: new entries become events under your name, changes to entries we already have are left alone. In Google Calendar: Settings → the calendar → <i>Public address in iCal format</i>. In Outlook: Settings → Calendar → Shared calendars → Publish → the ICS link. {{if gt $d.CalendarsLeft 0}}You can connect {{$d.CalendarsLeft}} more.{{else}}You have connected the maximum; remove one to add another.{{end}}</p>
{{if gt $d.CalendarsLeft 0}}<label for="url">Calendar address (.ics)</label><input type="url" id="url" name="url" required placeholder="https://calendar.google.com/calendar/ical/…/public/basic.ics">
<div class="grid2"><div><label for="label">Name for it (optional)</label><input type="text" id="label" name="label" maxlength="80" placeholder="e.g. Club fixtures"></div><div></div><div><label for="ctown">Town for its events</label><select id="ctown" name="town">{{range $d.Towns}}<option value="{{.}}">{{town .}}</option>{{end}}</select></div><div><label for="ccategory">Category for its events</label><select id="ccategory" name="category">{{range $d.Cats}}<option value="{{.}}" {{if eq . "community"}}selected{{end}}>{{cat .}}</option>{{end}}</select></div></div>
<div class="row"><button class="pri">Connect and check now</button></div>{{end}}</form>
{{if $d.Calendars}}<div class="panel">{{range $d.Calendars}}<div class="ev"><div><b>{{.Label}}</b> {{if .Enabled}}<span class="pill ok">watching</span>{{else}}<span class="pill no">off</span>{{end}}<div class="meta">{{.URL}}{{if .Checked}} · checked {{ago .Checked}}: {{.Status}}{{end}}</div></div>
<div class="acts"><form method="post" action="/account/promoter/calendar/check"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><button class="sm">Check now</button></form><form method="post" action="/account/promoter/calendar/remove"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="id" value="{{.ID}}"><button class="no sm">Remove</button></form></div></div>{{end}}</div>{{end}}
{{end}}

{{define "acc_import_preview"}}{{$d := .D}}{{$c := .CSRF}}
<div class="panel"><p style="margin:0"><b>{{$d.FileName}}</b>: <span class="pill ok">{{$d.Good}} to add</span> {{if $d.Dups}}<span class="pill">{{$d.Dups}} already yours</span>{{end}} {{if $d.Bad}}<span class="pill no">{{$d.Bad}} with problems</span>{{end}}</p>
<p class="hint">Rows with a problem are skipped; fix them in the file and upload again, or add them one at a time afterwards. {{if $d.Trusted}}Added events publish at once.{{else}}Added events go in the queue for a check.{{end}}</p>
{{if $d.Token}}<form method="post" action="/account/promoter/import/confirm"><input type="hidden" name="csrf" value="{{$c}}"><input type="hidden" name="token" value="{{$d.Token}}"><div class="row"><button class="pri">Add {{$d.Good}} event{{if ne $d.Good 1}}s{{end}}</button><a class="btn" href="/account/promoter/import">Start over</a></div></form>{{else}}<p class="row"><a class="btn" href="/account/promoter/import">Back</a></p>{{end}}</div>
<div class="panel">{{range $d.Rows}}<div class="ev"><div><b>{{.E.Title}}</b> {{if .Problem}}<span class="pill no">skipped</span>{{else if .Dup}}<span class="pill">already yours</span>{{else}}<span class="pill ok">add</span>{{end}}<div class="meta">{{.E.When}} · {{town .E.Town}} · {{cat .E.Category}}{{if .Problem}} · <span style="color:var(--no)">{{.Problem}}</span>{{end}}</div>{{if .E.Summary}}<div class="meta">{{short .E.Summary 160}}</div>{{end}}</div></div>{{end}}</div>
{{end}}

{{define "acc_listing_form"}}{{$f := .D}}
<form method="post" action="/account/promoter/listing" class="panel"><input type="hidden" name="csrf" value="{{.CSRF}}">
<p class="hint" style="margin:0 0 6px">A listing is a standing entry in the <b>directory</b>: a group people can join, a regular activity (a weekly run, a class, a market), or a place. It is not an event. Listings are added to the site by hand, so this can take a few days.</p>
<label for="kind">What are you adding?</label><select id="kind" name="kind"><option value="group">A group or club</option><option value="activity">A regular activity</option><option value="place">A place</option></select>
<label for="name">Name</label><input type="text" id="name" name="name" required maxlength="120">
<div class="grid2"><div><label for="category">Category</label><select id="category" name="category">{{range $f.Cats}}<option value="{{.}}">{{cat .}}</option>{{end}}</select></div><div><label for="town">Town</label><select id="town" name="town">{{range $f.Towns}}<option value="{{.}}">{{town .}}</option>{{end}}</select></div>
<div><label for="cost">Cost</label><select id="cost" name="cost">{{range $f.Costs}}<option value="{{.}}">{{title .}}</option>{{end}}</select></div><div><label for="website">Website or Facebook page</label><input type="url" id="website" name="website" placeholder="https://"></div></div>
<label for="schedule">When (for a regular activity)</label><input type="text" id="schedule" name="schedule" maxlength="160" placeholder="e.g. Saturdays 08:00, Strand beach">
<label>Who it is for</label><div class="row" style="margin-top:4px">{{range $f.Audiences}}<label class="inline" style="margin:0"><input type="checkbox" name="audience" value="{{.}}"> {{title .}}</label>{{end}}</div>
<label for="summary">About it</label><textarea id="summary" name="summary" required minlength="20" maxlength="800"></textarea><p class="hint">Where exactly, what happens, how to join. Up to 800 characters.</p>
<div class="row"><button class="pri">Send for checking</button><a class="btn" href="/account/promoter">Cancel</a></div></form>
{{end}}
`
