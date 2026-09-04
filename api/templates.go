package main

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"
)

// Email and admin-page rendering. HTML goes through html/template, so
// user-supplied text is escaped by construction; plain-text parts are built
// by hand and carry no markup.

var townNames = map[string]string{"somerset-west": "Somerset West", "strand": "Strand", "gordons-bay": "Gordon's Bay", "sir-lowrys-pass": "Sir Lowry's Pass"}
var catNames = map[string]string{"running": "Running & walking", "cycling": "Cycling & MTB", "hiking": "Hiking & trails", "water": "Beach & water", "markets": "Markets & food", "wine": "Wine & estates", "community": "Community & service", "family": "Family & kids", "arts": "Arts & culture", "online": "Online groups", "nature": "Parks & nature"}

func townName(id string) string {
	if n, ok := townNames[id]; ok {
		return n
	}
	return id
}
func catName(id string) string {
	if n, ok := catNames[id]; ok {
		return n
	}
	return id
}

func fmtDate(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("Mon 2 Jan")
}

func (e Event) When() string {
	s := fmtDate(e.Date)
	if e.EndDate != "" && e.EndDate != e.Date {
		s += " – " + fmtDate(e.EndDate)
	}
	if e.Time != "" {
		s += " · " + e.Time
		if e.EndTime != "" {
			s += "–" + e.EndTime
		}
	}
	return s
}
func (e Event) TownName() string { return townName(e.Town) }
func (e Event) CatName() string  { return catName(e.Category) }

func (a *App) eventURL(e Event) string {
	if e.Listing != "" {
		return a.cfg.SiteURL + "/listing.html?id=" + e.Listing
	}
	if e.Website != "" {
		return e.Website
	}
	return a.cfg.SiteURL + "/events.html"
}

/* ---------- plain text ---------- */

func (a *App) textConfirm(freq string, horizon int, link string) string {
	return fmt.Sprintf("Hi,\n\nSomeone (hopefully you) asked for %s updates from Helderberg Social covering the next %d days.\n\nConfirm by opening this link within 72 hours:\n\n%s\n\nIf it was not you, ignore this email and nothing will be sent.\n\nHelderberg Social\n%s\n", freq, horizon, link, a.cfg.SiteURL)
}

func (a *App) textAlready(s Subscriber) string {
	unsub := a.cfg.APIURL + "/api/unsubscribe?t=" + a.sign("unsub", fmt.Sprint(s.ID), 365*24*time.Hour)
	return fmt.Sprintf("Hi,\n\nThis address already receives %s updates (next %d days). To change your choices, unsubscribe with the link below and sign up again with the new settings.\n\nUnsubscribe: %s\n\nHelderberg Social\n%s\n", s.Frequency, s.Horizon, unsub, a.cfg.SiteURL)
}

func (a *App) textVerify(kind, name, link string) string {
	return fmt.Sprintf("Hi,\n\nThanks for sending us the %s \"%s\".\n\nPlease confirm it came from you by opening this link within 72 hours:\n\n%s\n\nAfter that a person checks it before it appears on the site. We never publish your name or email address.\n\nHelderberg Social\n%s\n", kind, name, link, a.cfg.SiteURL)
}

func (a *App) textEvent(e Event) string {
	return fmt.Sprintf("Event %q\nWhen: %s\nWhere: %s · %s\nCost: %s\nWebsite: %s\nListing: %s\nSubmitted by: %s (%s)\n\n%s", e.ID, e.When(), e.TownName(), e.CatName(), e.Cost, e.Website, e.Listing, e.SubmitterName, e.Origin, e.Summary)
}

func (a *App) textDigest(evs []Event, s Subscriber, unsub string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "What's on in the Helderberg over the next %d days\n\n", s.Horizon)
	last := ""
	for _, e := range evs {
		if e.Date != last {
			last = e.Date
			fmt.Fprintf(&b, "%s\n", strings.ToUpper(fmtDate(e.Date)))
		}
		fmt.Fprintf(&b, "  %s\n    %s · %s", e.Title, e.When(), e.TownName())
		if e.Cost == "free" {
			b.WriteString(" · Free")
		}
		if !e.Verified {
			b.WriteString(" · unverified")
		}
		b.WriteString("\n")
		if e.Summary != "" {
			fmt.Fprintf(&b, "    %s\n", strings.ReplaceAll(e.Summary, "\n", " "))
		}
		fmt.Fprintf(&b, "    %s\n\n", a.eventURL(e))
	}
	fmt.Fprintf(&b, "All events: %s/events.html\nAdd an event: %s/submit.html?kind=event\n\nYou get this because you signed up at %s and confirmed by email.\nUnsubscribe (instant, no questions): %s\n", a.cfg.SiteURL, a.cfg.SiteURL, a.cfg.SiteURL, unsub)
	return b.String()
}

/* ---------- HTML ---------- */

var tmplSrc = `
{{define "shell"}}<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}}</title></head>
<body style="margin:0;background:#f4f6f8;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#1c2431">
<div style="max-width:600px;margin:0 auto;padding:24px 16px">
<div style="background:#0f4c5c;color:#fff;padding:16px 20px;border-radius:12px 12px 0 0;font-weight:700;font-size:18px">Helderberg Social</div>
<div style="background:#fff;padding:20px;border-radius:0 0 12px 12px;line-height:1.5">{{template "body" .}}</div>
<p style="color:#6b7280;font-size:12px;margin-top:16px">{{.Foot}}{{if .Unsub}} <a href="{{.Unsub}}" style="color:#6b7280">Unsubscribe instantly</a>.{{end}}</p>
</div></body></html>{{end}}

{{define "confirm"}}{{template "shell" .}}{{end}}
{{define "body"}}{{if eq .Kind "confirm"}}
<p>Someone (hopefully you) asked for <b>{{.Freq}}</b> updates covering the next <b>{{.Horizon}} days</b>.</p>
<p><a href="{{.Link}}" style="display:inline-block;background:#f26b3a;color:#fff;text-decoration:none;padding:10px 18px;border-radius:8px;font-weight:600">Confirm subscription</a></p>
<p style="font-size:13px;color:#6b7280">The link works for 72 hours. If it was not you, ignore this email and nothing will be sent.</p>
{{else if eq .Kind "verify"}}
<p>Thanks for sending us the {{.What}} <b>{{.Name}}</b>.</p>
<p><a href="{{.Link}}" style="display:inline-block;background:#f26b3a;color:#fff;text-decoration:none;padding:10px 18px;border-radius:8px;font-weight:600">Yes, that was me</a></p>
<p style="font-size:13px;color:#6b7280">After that a person checks it before it appears on the site. We never publish your name or email address.</p>
{{else if eq .Kind "member"}}
<h2 style="margin:0 0 12px;font-size:20px">{{.Title}}</h2>
<p>{{.What}}</p>
<p><a href="{{.Link}}" style="display:inline-block;background:#f26b3a;color:#fff;text-decoration:none;padding:10px 18px;border-radius:8px;font-weight:600">{{.Name}}</a></p>
<p style="font-size:13px;color:#6b7280">If the button does not work, copy this address into your browser:<br>{{.Link}}</p>
{{else if eq .Kind "digest"}}
<h2 style="margin:0 0 12px;font-size:20px">What's on over the next {{.Horizon}} days</h2>
{{range .Days}}<h3 style="margin:18px 0 6px;font-size:13px;letter-spacing:.06em;text-transform:uppercase;color:#0f4c5c">{{.Label}}</h3>
{{range .Events}}<div style="padding:8px 0;border-top:1px solid #eef1f4">
<a href="{{.URL}}" style="color:#1c2431;font-weight:600;text-decoration:none">{{.E.Title}}</a><br>
<span style="font-size:13px;color:#6b7280">{{.E.When}} · {{.E.TownName}} · {{.E.CatName}}{{if eq .E.Cost "free"}} · <b style="color:#1f7a4d">Free</b>{{end}}{{if not .E.Verified}} · unverified{{end}}</span>
{{if .E.Summary}}<div style="font-size:14px;margin-top:3px">{{.E.Summary}}</div>{{end}}
</div>{{end}}{{end}}
<p style="margin-top:18px"><a href="{{.Site}}/events.html" style="color:#0f4c5c">All events</a> · <a href="{{.Site}}/submit.html?kind=event" style="color:#0f4c5c">Add an event</a></p>
{{end}}{{end}}

{{define "page"}}<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}} · Helderberg Social</title></head>
<body style="margin:0;background:#f4f6f8;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#1c2431"><div style="max-width:560px;margin:48px auto;padding:24px;background:#fff;border-radius:12px">
<h1 style="font-size:22px;margin:0 0 12px">{{.Title}}</h1><p>{{.Body}}</p>
{{if .Queue}}<p><a href="{{.Queue}}">Open the console</a></p>{{end}}
<p><a href="{{.Site}}">Back to Helderberg Social</a></p></div></body></html>{{end}}

`

func parseTemplates() *template.Template {
	return template.Must(template.New("hs").Parse(tmplSrc))
}

type mailView struct {
	Kind, Title, Foot, Freq, What, Name, Link, Site, Unsub string
	Horizon                                                int
	Days                                                   []digestDay
}

type digestDay struct {
	Label  string
	Events []digestItem
}

type digestItem struct {
	E   Event
	URL string
}

func (a *App) render(v mailView) string {
	var b bytes.Buffer
	if err := a.tmpl.ExecuteTemplate(&b, "shell", v); err != nil {
		a.logf("render %s: %v", v.Kind, err)
		return ""
	}
	return b.String()
}

func (a *App) htmlConfirm(freq string, horizon int, link string) string {
	return a.render(mailView{Kind: "confirm", Title: "Confirm your updates", Freq: freq, Horizon: horizon, Link: link, Site: a.cfg.SiteURL,
		Foot: "Sent by Helderberg Social because this address was entered on " + a.cfg.SiteURL + "."})
}

func (a *App) htmlVerify(what, name, link string) string {
	return a.render(mailView{Kind: "verify", Title: "Confirm your " + what, What: what, Name: name, Link: link, Site: a.cfg.SiteURL,
		Foot: "Sent by Helderberg Social because this address was entered on " + a.cfg.SiteURL + "."})
}

func (a *App) htmlDigest(evs []Event, s Subscriber, unsub string) string {
	v := mailView{Kind: "digest", Title: "What's on in the Helderberg", Horizon: s.Horizon, Site: a.cfg.SiteURL, Unsub: unsub,
		Foot: "You get this because you signed up at " + a.cfg.SiteURL + " and confirmed by email."}
	var day *digestDay
	for _, e := range evs {
		if day == nil || day.Label != fmtDate(e.Date) {
			v.Days = append(v.Days, digestDay{Label: fmtDate(e.Date)})
			day = &v.Days[len(v.Days)-1]
		}
		day.Events = append(day.Events, digestItem{E: e, URL: a.eventURL(e)})
	}
	return a.render(v)
}
