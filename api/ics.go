package main

import (
	"bufio"
	"fmt"
	"strings"
	"time"
)

// icsEvent is the subset of an iCalendar VEVENT the watcher needs.
type icsEvent struct {
	UID, Summary, Description, URL, Location string
	Start, End                               time.Time
	AllDay                                   bool
	RRule                                    string      // the raw RRULE, "" for a one-off
	ExDates                                  []time.Time // EXDATE instances (day precision)
	RecurrenceID                             bool        // an override of one instance of a series
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
			case "RRULE":
				cur.RRule = strings.ToUpper(strings.TrimSpace(val))
			case "EXDATE":
				for _, v := range strings.Split(val, ",") {
					if t, _ := icsTime(v, params, loc); !t.IsZero() {
						cur.ExDates = append(cur.ExDates, t)
					}
				}
			case "RECURRENCE-ID":
				cur.RecurrenceID = true
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

/* ---------- recurrence ---------- */

// rrule is the part of RFC 5545 recurrence the watcher understands: the
// four frequencies, INTERVAL, COUNT, UNTIL, BYDAY (with ordinals for monthly
// rules, "1SU" = first Sunday) and BYMONTHDAY. Anything else in the rule is
// ignored, which can only ever produce fewer occurrences, never invented ones.
type rrule struct {
	Freq     string
	Interval int
	Count    int
	Until    time.Time
	ByDay    []byDay
	ByMonthD []int
}

type byDay struct {
	Weekday time.Weekday
	Ord     int // 0 = every, 1 = first, -1 = last
}

var weekdayCodes = map[string]time.Weekday{"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday, "TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday}

func parseRRule(raw string, loc *time.Location) (rrule, bool) {
	r := rrule{Interval: 1}
	for _, part := range strings.Split(strings.ToUpper(raw), ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch k {
		case "FREQ":
			r.Freq = v
		case "INTERVAL":
			if n := atoi(v); n > 0 {
				r.Interval = n
			}
		case "COUNT":
			r.Count = atoi(v)
		case "UNTIL":
			r.Until, _ = icsTime(v, "", loc)
			if len(v) == 8 { // a date: the whole of that day counts
				r.Until = r.Until.Add(24*time.Hour - time.Second)
			}
		case "BYDAY":
			for _, d := range strings.Split(v, ",") {
				code := d
				ord := 0
				if len(d) > 2 {
					ord = atoi(d[:len(d)-2])
					code = d[len(d)-2:]
				}
				if wd, ok := weekdayCodes[code]; ok {
					r.ByDay = append(r.ByDay, byDay{wd, ord})
				}
			}
		case "BYMONTHDAY":
			for _, d := range strings.Split(v, ",") {
				if n := atoi(d); n != 0 {
					r.ByMonthD = append(r.ByMonthD, n)
				}
			}
		}
	}
	switch r.Freq {
	case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
		return r, true
	}
	return r, false
}

func atoi(s string) int {
	n, neg := 0, false
	for i, c := range s {
		if i == 0 && c == '-' {
			neg = true
			continue
		}
		if i == 0 && c == '+' {
			continue
		}
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		return -n
	}
	return n
}

// occurrences lists the instances of a recurring event that start in
// [from, to], at most max of them, walking the rule from DTSTART so COUNT
// is honoured. Instances on an EXDATE day are left out. A one-off event
// yields itself if it is in the window.
func (ev icsEvent) occurrences(from, to time.Time, max int) []time.Time {
	in := func(t time.Time) bool { return !t.Before(from) && !t.After(to) }
	if ev.RRule == "" {
		if in(ev.Start) {
			return []time.Time{ev.Start}
		}
		return nil
	}
	r, ok := parseRRule(ev.RRule, ev.Start.Location())
	if !ok {
		return nil
	}
	excluded := func(t time.Time) bool {
		for _, x := range ev.ExDates {
			if x.Year() == t.Year() && x.YearDay() == t.YearDay() {
				return true
			}
		}
		return false
	}
	var out []time.Time
	made := 0 // instances generated by the rule, for COUNT
	emit := func(t time.Time) bool {
		if !r.Until.IsZero() && t.After(r.Until) {
			return false
		}
		made++
		if r.Count > 0 && made > r.Count {
			return false
		}
		if t.After(to) {
			return false
		}
		if in(t) && !excluded(t) {
			out = append(out, t)
		}
		return len(out) < max
	}
	start := ev.Start
	// Safety valve: a rule is walked for at most this many candidate
	// periods, so a malformed feed cannot spin.
	const maxPeriods = 5000
	switch r.Freq {
	case "DAILY":
		for i := 0; i < maxPeriods; i++ {
			if !emit(start.AddDate(0, 0, i*r.Interval)) {
				break
			}
		}
	case "WEEKLY":
		days := r.ByDay
		if len(days) == 0 {
			days = []byDay{{start.Weekday(), 0}}
		}
		// Walk week by week from the Monday-based week of DTSTART; within a
		// week, instances before DTSTART itself do not exist.
		weekStart := start.AddDate(0, 0, -int((start.Weekday()+6)%7))
		for w := 0; w < maxPeriods; w++ {
			base := weekStart.AddDate(0, 0, 7*w*r.Interval)
			stop := false
			for d := 0; d < 7; d++ {
				t := base.AddDate(0, 0, d)
				if t.Before(start) {
					continue
				}
				for _, bd := range days {
					if bd.Weekday == t.Weekday() {
						if !emit(t) {
							stop = true
						}
						break
					}
				}
				if stop {
					break
				}
			}
			if stop {
				break
			}
		}
	case "MONTHLY":
		for m := 0; m < maxPeriods; m++ {
			first := time.Date(start.Year(), start.Month()+time.Month(m*r.Interval), 1, start.Hour(), start.Minute(), start.Second(), 0, start.Location())
			last := first.AddDate(0, 1, -1)
			var cands []time.Time
			switch {
			case len(r.ByDay) > 0:
				for _, bd := range r.ByDay {
					var ds []time.Time
					for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
						if d.Weekday() == bd.Weekday {
							ds = append(ds, d)
						}
					}
					switch {
					case bd.Ord == 0:
						cands = append(cands, ds...)
					case bd.Ord > 0 && bd.Ord <= len(ds):
						cands = append(cands, ds[bd.Ord-1])
					case bd.Ord < 0 && -bd.Ord <= len(ds):
						cands = append(cands, ds[len(ds)+bd.Ord])
					}
				}
			case len(r.ByMonthD) > 0:
				for _, md := range r.ByMonthD {
					day := md
					if md < 0 {
						day = last.Day() + 1 + md
					}
					if day >= 1 && day <= last.Day() {
						cands = append(cands, time.Date(first.Year(), first.Month(), day, first.Hour(), first.Minute(), first.Second(), 0, first.Location()))
					}
				}
			default:
				if start.Day() <= last.Day() {
					cands = append(cands, time.Date(first.Year(), first.Month(), start.Day(), first.Hour(), first.Minute(), first.Second(), 0, first.Location()))
				}
			}
			sortTimes(cands)
			stop := false
			for _, t := range cands {
				if t.Before(start) {
					continue
				}
				if !emit(t) {
					stop = true
					break
				}
			}
			if stop {
				break
			}
		}
	case "YEARLY":
		for y := 0; y < maxPeriods; y++ {
			if !emit(start.AddDate(y*r.Interval, 0, 0)) {
				break
			}
		}
	}
	return out
}

func sortTimes(ts []time.Time) {
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && ts[j].Before(ts[j-1]); j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}

// repeatText says in plain words how a series repeats, for the queued
// event's summary: "Repeats every week on Sunday."
func repeatText(raw string, loc *time.Location) string {
	r, ok := parseRRule(raw, loc)
	if !ok {
		return ""
	}
	unit := map[string]string{"DAILY": "day", "WEEKLY": "week", "MONTHLY": "month", "YEARLY": "year"}[r.Freq]
	every := "every " + unit
	if r.Interval > 1 {
		every = fmt.Sprintf("every %d %ss", r.Interval, unit)
	}
	ords := map[int]string{1: "first", 2: "second", 3: "third", 4: "fourth", 5: "fifth", -1: "last"}
	var days []string
	for _, bd := range r.ByDay {
		name := bd.Weekday.String()
		if o, ok := ords[bd.Ord]; ok && r.Freq == "MONTHLY" {
			name = o + " " + name
		}
		days = append(days, name)
	}
	s := "Repeats " + every
	if len(days) > 0 {
		s += " on " + strings.Join(days, ", ")
	} else if len(r.ByMonthD) > 0 && r.Freq == "MONTHLY" {
		s += fmt.Sprintf(" on day %d", r.ByMonthD[0])
	}
	if !r.Until.IsZero() {
		s += " until " + r.Until.In(loc).Format("2 Jan 2006")
	} else if r.Count > 0 {
		s += fmt.Sprintf(" (%d times)", r.Count)
	}
	return s + "."
}
