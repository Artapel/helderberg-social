package main

import (
	"bufio"
	"strings"
	"time"
)

// icsEvent is the subset of an iCalendar VEVENT the watcher needs.
type icsEvent struct {
	UID, Summary, Description, URL, Location string
	Start, End                               time.Time
	AllDay                                   bool
}

// parseICS handles folded lines, VALUE=DATE and date-time forms (floating,
// UTC "Z" and TZID). Anything it cannot read it skips rather than guesses.
func parseICS(raw string, loc *time.Location) []icsEvent {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(raw))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		l := strings.TrimRight(sc.Text(), "\r")
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += l[1:]
			continue
		}
		lines = append(lines, l)
	}
	var out []icsEvent
	var cur *icsEvent
	for _, l := range lines {
		switch {
		case l == "BEGIN:VEVENT":
			cur = &icsEvent{}
		case l == "END:VEVENT":
			if cur != nil && cur.UID != "" && cur.Summary != "" && !cur.Start.IsZero() {
				out = append(out, *cur)
			}
			cur = nil
		case cur == nil:
			continue
		default:
			i := strings.Index(l, ":")
			if i < 1 {
				continue
			}
			key, val := l[:i], l[i+1:]
			name, params := key, ""
			if j := strings.Index(key, ";"); j >= 0 {
				name, params = key[:j], key[j+1:]
			}
			switch strings.ToUpper(name) {
			case "UID":
				cur.UID = strings.TrimSpace(val)
			case "SUMMARY":
				cur.Summary = icsUnescape(val)
			case "DESCRIPTION":
				cur.Description = icsUnescape(val)
			case "URL":
				cur.URL = strings.TrimSpace(val)
			case "LOCATION":
				cur.Location = icsUnescape(val)
			case "DTSTART":
				cur.Start, cur.AllDay = icsTime(val, params, loc)
			case "DTEND":
				cur.End, _ = icsTime(val, params, loc)
			}
		}
	}
	return out
}

func icsTime(val, params string, loc *time.Location) (time.Time, bool) {
	val = strings.TrimSpace(val)
	if strings.Contains(strings.ToUpper(params), "VALUE=DATE") || len(val) == 8 {
		t, err := time.ParseInLocation("20060102", val, loc)
		if err != nil {
			return time.Time{}, false
		}
		return t, true
	}
	if strings.HasSuffix(val, "Z") {
		t, err := time.Parse("20060102T150405Z", val)
		if err != nil {
			return time.Time{}, false
		}
		return t.In(loc), false
	}
	l := loc
	for _, p := range strings.Split(params, ";") {
		if strings.HasPrefix(strings.ToUpper(p), "TZID=") {
			if z, err := time.LoadLocation(strings.Trim(p[5:], `"`)); err == nil {
				l = z
			}
		}
	}
	t, err := time.ParseInLocation("20060102T150405", val, l)
	if err != nil {
		return time.Time{}, false
	}
	return t.In(loc), false
}

func icsUnescape(s string) string {
	return strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(s)
}
