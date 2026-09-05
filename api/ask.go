package main

// Ask: answers a question about what is on from the site's own content.
//
// "today", "week", "weekend in Strand", "free markets", "running clubs in
// Gordon's Bay", "is there archery?" all work without any AI: the question is
// read for a period, a town, a category and a few keywords, and the answer is
// built from the events the API holds plus the listings and events in the
// site's data/data.js, which the API fetches from the site and refreshes
// hourly. The same engine answers WhatsApp messages (waInbound) and
// GET /api/ask?q=….
//
// When HS_AI_URL is set, a question that is not a plain command is also put to
// an Ollama-compatible model together with the matching items, and its answer
// is used if it comes back in time; otherwise the plain list is sent. The
// model only ever sees site content and the question, never who asked.
//
// Nothing here is trusted from the asker: the text is length-capped, the
// output is built from our own data, and the WhatsApp reply goes through the
// same per-number budget as everything else the service sends.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	askMaxQuestion = 300              // characters of question we read
	askMaxReply    = 1500             // characters of a WhatsApp answer (limit is 4096; short is kinder)
	askMaxItems    = 8                // events or listings in one answer
	askRefresh     = time.Hour        // how often data.js is re-fetched
	askAITimeout   = 12 * time.Second // the model must answer within this or the plain list goes
	askPerHour     = 30               // answers one number may get per hour
)

// Listing is the subset of a data.js listing the answerer needs.
type Listing struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Town     string   `json:"town"`
	Summary  string   `json:"summary"`
	Cost     string   `json:"cost"`
	Audience []string `json:"audience"`
	Tags     []string `json:"tags"`
	Website  string   `json:"website"`
	Verified bool     `json:"verified"`
	Schedule struct {
		Days []int  `json:"days"`
		Text string `json:"text"`
	} `json:"schedule"`
	Status struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	} `json:"status"`
}

// siteData is what the API knows about the static site's content.
type siteData struct {
	Listings []Listing `json:"listings"`
	Events   []Event   `json:"events"`
	fetched  time.Time
}

var (
	askMu   sync.Mutex
	askData *siteData
	// askFetch is swapped by tests so no network is touched.
	askFetch func(a *App) ([]byte, error) = func(a *App) ([]byte, error) { return a.fetch(a.cfg.SiteURL + "/data/data.js") }
)

// site returns the parsed data.js, fetching it on first use and after
// askRefresh. A failed refresh keeps the previous copy; with no copy at all
// the answerer works from the database alone.
func (a *App) site() *siteData {
	askMu.Lock()
	defer askMu.Unlock()
	if askData != nil && time.Since(askData.fetched) < askRefresh {
		return askData
	}
	body, err := askFetch(a)
	if err == nil {
		if d, perr := parseDataJS(body); perr == nil {
			d.fetched = time.Now()
			askData = d
			return askData
		} else {
			err = perr
		}
	}
	a.logf("ask: data.js not refreshed: %v", err)
	if askData == nil {
		askData = &siteData{fetched: time.Now().Add(-askRefresh + 5*time.Minute)} // retry in 5 min
	} else {
		askData.fetched = time.Now().Add(-askRefresh + 5*time.Minute)
	}
	return askData
}

// parseDataJS turns `window.HS_DATA = {...};` into JSON and decodes it. The
// file is a JavaScript object literal written by hand: comments, unquoted
// keys, single-quoted strings and trailing commas are allowed; anything
// beyond that (expressions, functions) is not, and never has been used.
func parseDataJS(src []byte) (*siteData, error) {
	// The header comment mentions "Sunday=0", so look for the assignment
	// itself, not the first "=" in the file.
	i := bytes.Index(src, []byte("HS_DATA"))
	if i < 0 {
		return nil, fmt.Errorf("no HS_DATA assignment")
	}
	j := bytes.Index(src[i:], []byte("="))
	if j < 0 {
		return nil, fmt.Errorf("no assignment")
	}
	js := jsToJSON(src[i+j+1:])
	js = bytes.TrimSpace(js)
	js = bytes.TrimSuffix(js, []byte(";"))
	var d siteData
	if err := json.Unmarshal(js, &d); err != nil {
		return nil, fmt.Errorf("data.js: %w", err)
	}
	return &d, nil
}

// jsToJSON strips comments, quotes bare keys, converts single-quoted
// strings and drops trailing commas. It walks the text once, tracking
// whether it is inside a string.
func jsToJSON(src []byte) []byte {
	var out bytes.Buffer
	n := len(src)
	for i := 0; i < n; {
		c := src[i]
		switch {
		case c == '/' && i+1 < n && src[i+1] == '*':
			j := bytes.Index(src[i+2:], []byte("*/"))
			if j < 0 {
				return out.Bytes()
			}
			i += j + 4
		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '"' || c == '\'':
			q := c
			out.WriteByte('"')
			i++
			for i < n && src[i] != q {
				if src[i] == '\\' && i+1 < n {
					if src[i+1] == '\'' {
						out.WriteByte('\'') // \' is not valid JSON
						i += 2
						continue
					}
					out.WriteByte(src[i])
					out.WriteByte(src[i+1])
					i += 2
					continue
				}
				if src[i] == '"' && q == '\'' {
					out.WriteString(`\"`)
					i++
					continue
				}
				out.WriteByte(src[i])
				i++
			}
			out.WriteByte('"')
			i++
		case c == ',':
			// drop the comma if the next non-space, non-comment char closes a container
			j := i + 1
			for j < n {
				if src[j] == ' ' || src[j] == '\n' || src[j] == '\r' || src[j] == '\t' {
					j++
					continue
				}
				if src[j] == '/' && j+1 < n && src[j+1] == '/' {
					for j < n && src[j] != '\n' {
						j++
					}
					continue
				}
				if src[j] == '/' && j+1 < n && src[j+1] == '*' {
					k := bytes.Index(src[j+2:], []byte("*/"))
					if k < 0 {
						j = n
						break
					}
					j += k + 4
					continue
				}
				break
			}
			if j < n && (src[j] == '}' || src[j] == ']') {
				i++
				continue
			}
			out.WriteByte(c)
			i++
		case c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			// a bare word: a key if followed by ':', else a literal (true/false/null)
			j := i
			for j < n && (src[j] == '_' || src[j] == '$' || (src[j] >= 'a' && src[j] <= 'z') || (src[j] >= 'A' && src[j] <= 'Z') || (src[j] >= '0' && src[j] <= '9')) {
				j++
			}
			word := src[i:j]
			k := j
			for k < n && (src[k] == ' ' || src[k] == '\t') {
				k++
			}
			if k < n && src[k] == ':' {
				out.WriteByte('"')
				out.Write(word)
				out.WriteByte('"')
			} else {
				out.Write(word)
			}
			i = j
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.Bytes()
}

/* ---------- reading the question ---------- */

type askQuery struct {
	raw      string
	words    []string // meaningful tokens for keyword matching
	from, to string   // ISO date window for events, "" = none asked for
	period   string   // today | tomorrow | weekend | week | month | ""
	town     string
	category string
	free     bool
	wants    string // events | listings | "" (both)
	help     bool
	bare     bool // only command words (today, Strand, markets, clubs): a list, never the model
}

var askPrefix = regexp.MustCompile(`(?i)^\s*@?\s*helderberg(\s*social)?\s*[:,!.\-–]?\s*`)
var askSplit = regexp.MustCompile(`[^\p{L}\p{N}']+`)

var askStop = set("the", "a", "an", "is", "are", "there", "any", "what", "whats", "what's", "on", "in", "at", "for", "to", "of", "and", "or", "me", "i", "we", "you", "can", "do", "does", "please", "pls", "asb", "show", "tell", "find", "list", "give", "hi", "hello", "hey", "good", "morning", "afternoon", "evening", "want", "looking", "something", "anything", "things", "thing", "stuff", "with", "near", "around", "this", "that", "next", "going", "happening", "up", "about", "know", "like", "some", "my", "our", "us", "it", "its", "be", "from", "by", "wat", "waar", "wanneer", "daar", "die", "en", "of", "vir", "ek", "ons", "hulle", "sal", "kan")

var askTownWords = []struct {
	re *regexp.Regexp
	id string
}{
	{regexp.MustCompile(`(?i)\bsomerset(\s*-?\s*west)?\b|\bsw\b`), "somerset-west"},
	{regexp.MustCompile(`(?i)\bstrand\b`), "strand"},
	{regexp.MustCompile(`(?i)\bgordon'?s?\s*-?\s*bay\b|\bgordons\b|\bgb\b`), "gordons-bay"},
	{regexp.MustCompile(`(?i)\bsir\s*lowry'?s?(\s*pass)?\b|\bslp\b`), "sir-lowrys-pass"},
}

var askCatWords = []struct {
	re *regexp.Regexp
	id string
}{
	{regexp.MustCompile(`(?i)\b(run|runs|running|runner|runners|walk|walks|walking|walker|walkers|parkrun|jog|jogging|hardloop|stap)\b`), "running"},
	{regexp.MustCompile(`(?i)\b(cycle|cycling|cyclist|cyclists|mtb|bike|bikes|biking|mountain\s*bik\w*|fietsry|fiets)\b`), "cycling"},
	{regexp.MustCompile(`(?i)\b(hike|hikes|hiking|hiker|hikers|trail|trails|stap\w*roete)\b`), "hiking"},
	{regexp.MustCompile(`(?i)\b(beach|beaches|sea|ocean|swim|swimming|surf|surfing|dive|diving|scuba|sail|sailing|kayak|kayaking|paddle|sup|fish|fishing|angling|strand\s*aktiwiteit)\b`), "water"},
	{regexp.MustCompile(`(?i)\b(market|markets|food|foodie|eat|eating|coffee|breakfast|brunch|lunch|dinner|mark)\b`), "markets"},
	{regexp.MustCompile(`(?i)\b(wine|wines|winery|wineries|estate|estates|tasting|tastings|vineyard|vineyards|wyn)\b`), "wine"},
	{regexp.MustCompile(`(?i)\b(community|volunteer|volunteers|volunteering|charity|clean-?up|cleanup|npo|ngo|help\s*out|gemeenskap)\b`), "community"},
	{regexp.MustCompile(`(?i)\b(kid|kids|child|children|family|families|toddler|toddlers|teen|teens|kinders|gesin)\b`), "family"},
	{regexp.MustCompile(`(?i)\b(art|arts|artist|artists|culture|cultural|craft|crafts|theatre|theater|drama|paint|painting|pottery|photography|exhibition|gallery|kuns)\b`), "arts"},
	{regexp.MustCompile(`(?i)\b(online|facebook|whatsapp\s*group|forum)\b`), "online"},
	{regexp.MustCompile(`(?i)\b(park|parks|nature|garden|gardens|bird|birds|birding|reserve|picnic|natuur)\b`), "nature"},
	{regexp.MustCompile(`(?i)\b(sport|sports|gym|fitness|yoga|pilates|archery|tennis|padel|squash|golf|cricket|rugby|soccer|football|hockey|netball|bowls|karate|boxing|crossfit|climbing|sokker)\b`), "sport"},
	{regexp.MustCompile(`(?i)\b(music|musical|show|shows|concert|concerts|gig|gigs|band|bands|live|jazz|choir|comedy|musiek)\b`), "music"},
	{regexp.MustCompile(`(?i)\b(church|churches|faith|worship|service|services|kerk|gemeente|prayer|bible|mosque|temple)\b`), "faith"},
	{regexp.MustCompile(`(?i)\b(camp|camping|campsite|caravan|outdoor|outdoors|braai|kampeer)\b`), "camping"},
	{regexp.MustCompile(`(?i)\b(game|games|board\s*game|boardgames|chess|cards|hobby|hobbies|tabletop|pokemon|magic|warhammer|miniatures|puzzle|quiz)\b`), "games"},
}

// readQuestion turns free text into a query. now is local midnight today.
func readQuestion(text string, now time.Time) askQuery {
	q := askQuery{}
	text = strings.TrimSpace(text)
	if len(text) > askMaxQuestion {
		text = text[:askMaxQuestion]
	}
	text = askPrefix.ReplaceAllString(text, "")
	q.raw = text
	low := strings.ToLower(text)
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	switch {
	case low == "" || low == "help" || low == "menu" || low == "?" || low == "hulp":
		q.help = true
	}
	// rest is the question with every recognised command word removed, so
	// the keywords left are the ones that need matching against content;
	// "today" alone leaves nothing and is a bare command.
	rest := low
	has := func(re string) bool {
		r := regexp.MustCompile(`(?i)\b(` + re + `)\b`)
		if !r.MatchString(low) {
			return false
		}
		rest = r.ReplaceAllString(rest, " ")
		return true
	}
	switch {
	case has(`today|tonight|vandag|vanaand|now|nou`):
		q.period, q.from, q.to = "today", day(now), day(now)
	case has(`tomorrow|tomorow|môre|more|morge`):
		t := now.AddDate(0, 0, 1)
		q.period, q.from, q.to = "tomorrow", day(t), day(t)
	case has(`weekend|naweek|saturday|sunday|saterdag|sondag`):
		sat := now
		for sat.Weekday() != time.Saturday {
			sat = sat.AddDate(0, 0, 1)
		}
		if now.Weekday() == time.Sunday {
			sat = now.AddDate(0, 0, -1)
		}
		q.period, q.from, q.to = "weekend", day(sat), day(sat.AddDate(0, 0, 1))
		if now.Weekday() == time.Sunday {
			q.from = day(now)
		}
	case has(`month|maand|30 days`):
		q.period, q.from, q.to = "month", day(now), day(now.AddDate(0, 0, 30))
	case has(`week|weekly|7 days|days|dae`):
		q.period, q.from, q.to = "week", day(now), day(now.AddDate(0, 0, 7))
	}
	for _, t := range askTownWords {
		if t.re.MatchString(low) {
			q.town = t.id
			rest = t.re.ReplaceAllString(rest, " ")
			break
		}
	}
	// Category words stay in the keywords: "archery" narrows Sport & fitness
	// to archery. They are dropped only when deciding whether the question
	// was a bare command.
	cmdRest := rest
	for _, c := range askCatWords {
		if c.re.MatchString(low) {
			q.category = c.id
			cmdRest = c.re.ReplaceAllString(cmdRest, " ")
			break
		}
	}
	q.free = has(`free|gratis|verniet|no cost`)
	wantsRe := func(re string) bool {
		r := regexp.MustCompile(`(?i)\b(` + re + `)\b`)
		if !r.MatchString(low) {
			return false
		}
		rest = r.ReplaceAllString(rest, " ")
		cmdRest = r.ReplaceAllString(cmdRest, " ")
		return true
	}
	switch {
	case wantsRe(`event|events|happening|whats on|what's on|on this|geleentheid|geleenthede|gebeur`):
		q.wants = "events"
	case wantsRe(`club|clubs|group|groups|join|place|places|venue|venues|listing|listings|regular|weekly|classes|class|lessons|klub|groep`):
		q.wants = "listings"
	}
	if q.period != "" && q.wants == "" {
		q.wants = "events"
	}
	for _, w := range askSplit.Split(rest, -1) {
		w = strings.Trim(w, "'")
		if len(w) < 3 || askStop[w] {
			continue
		}
		q.words = append(q.words, w)
	}
	q.bare = true
	for _, w := range askSplit.Split(cmdRest, -1) {
		w = strings.Trim(w, "'")
		if len(w) >= 3 && !askStop[w] {
			q.bare = false
			break
		}
	}
	return q
}

/* ---------- finding the matches ---------- */

type askHit struct {
	score int
	event *Event
	list  *Listing
}

// stem is a crude normaliser so "markets" finds "market" and "hikes" "hiking".
func stem(w string) string {
	w = strings.ToLower(w)
	for _, suf := range []string{"ing", "ers", "er", "ies", "es", "s"} {
		if strings.HasSuffix(w, suf) && len(w)-len(suf) >= 3 {
			w = strings.TrimSuffix(w, suf)
			break
		}
	}
	return w
}

func askScore(words []string, fields ...string) int {
	if len(words) == 0 {
		return 1
	}
	hay := strings.ToLower(strings.Join(fields, " "))
	score := 0
	for _, w := range words {
		s := stem(w)
		if strings.Contains(hay, s) {
			score++
		}
	}
	return score
}

// answerData collects the events and listings a question should see.
func (a *App) answerData(q askQuery, today string) (events []Event, lists []Listing) {
	d := a.site()
	// Events: the database's approved ones over the site's own list, by id.
	seen := map[string]bool{}
	from, to := q.from, q.to
	if from == "" {
		from, to = today, mustDate(today).AddDate(0, 0, 30).Format("2006-01-02")
	}
	dbEvents, _ := a.queryEvents(`status = 'approved' AND `+liveEventsWhere+` AND (CASE WHEN end_date = '' THEN date ELSE end_date END) >= ? AND date <= ?`, today, from, to)
	all := append([]Event{}, dbEvents...)
	for _, e := range dbEvents {
		seen[e.ID] = true
	}
	for _, e := range d.Events {
		if seen[e.ID] {
			continue
		}
		end := e.EndDate
		if end == "" {
			end = e.Date
		}
		if end < from || e.Date > to {
			continue
		}
		all = append(all, e)
	}
	var hits []askHit
	for i := range all {
		e := &all[i]
		if q.town != "" && e.Town != q.town {
			continue
		}
		if q.category != "" && e.Category != q.category {
			continue
		}
		if q.free && e.Cost != "free" {
			continue
		}
		s := askScore(q.words, e.Title, e.Summary, townName(e.Town), catName(e.Category))
		if q.period != "" || q.town != "" || q.category != "" {
			s++ // a filter matched; keywords are a bonus, not a requirement
		}
		if s > 0 {
			hits = append(hits, askHit{score: s, event: e})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].event.Date+hits[i].event.Time < hits[j].event.Date+hits[j].event.Time
	})
	for _, h := range hits {
		events = append(events, *h.event)
	}
	// Listings: only when the question is not purely about a period.
	if q.wants == "events" && len(q.words) == 0 {
		return events, nil
	}
	var lhits []askHit
	for i := range d.Listings {
		l := &d.Listings[i]
		if q.town != "" && l.Town != q.town {
			continue
		}
		if q.category != "" && l.Category != q.category {
			continue
		}
		if q.free && l.Cost != "free" {
			continue
		}
		s := askScore(q.words, l.Name, l.Summary, strings.Join(l.Tags, " "), townName(l.Town), catName(l.Category), l.Type)
		if q.town != "" || q.category != "" {
			s++
		}
		if len(q.words) > 0 && askScore(q.words, l.Name, strings.Join(l.Tags, " ")) > 0 {
			s += 2 // a hit in the name or tags outranks one buried in the summary
		}
		if s > 0 {
			lhits = append(lhits, askHit{score: s, list: l})
		}
	}
	sort.SliceStable(lhits, func(i, j int) bool { return lhits[i].score > lhits[j].score })
	for _, h := range lhits {
		lists = append(lists, *h.list)
	}
	return events, lists
}

/* ---------- writing the answer ---------- */

func (a *App) askHelp() string {
	return "Hi, this is Helderberg Social. Ask me what's on:\n" +
		"• *today* or *tomorrow*\n• *weekend* or *week*\n• a town: *Strand this weekend*\n• a kind of thing: *free markets*, *running clubs in Gordon's Bay*, *is there archery?*\n\n" +
		"I answer from " + strings.TrimPrefix(a.cfg.SiteURL, "https://") + ". Reply STOP to leave the updates list."
}

func askPeriodTitle(q askQuery, now time.Time) string {
	switch q.period {
	case "today":
		return "Today, " + now.Format("Mon 2 Jan")
	case "tomorrow":
		return "Tomorrow, " + now.AddDate(0, 0, 1).Format("Mon 2 Jan")
	case "weekend":
		return "This weekend, " + fmtDate(q.from) + " to " + fmtDate(q.to)
	case "week":
		return "The next 7 days"
	case "month":
		return "The next 30 days"
	}
	return "Coming up"
}

func askEventLine(e Event) string {
	s := "• "
	if e.Time != "" {
		s += e.Time + " "
	}
	s += e.Title + " · " + townName(e.Town)
	if e.EndDate != "" && e.EndDate != e.Date {
		s += " (until " + fmtDate(e.EndDate) + ")"
	}
	if e.Cost == "free" {
		s += " · free"
	}
	return s
}

func askListingLine(l Listing) string {
	s := "• *" + l.Name + "* · " + townName(l.Town)
	if l.Status.Kind == "paused" {
		s += " · paused"
	}
	if l.Schedule.Text != "" {
		s += " · " + clip(l.Schedule.Text, 70)
	} else if l.Summary != "" {
		s += " · " + clip(l.Summary, 90)
	}
	return s
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	if i := strings.LastIndexFunc(cut, unicode.IsSpace); i > n/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:") + "…"
}

// plainAnswer is the deterministic answer: a titled list of events, then
// listings, then where to see more. Always short enough for one WhatsApp
// message.
func (a *App) plainAnswer(q askQuery, now time.Time, events []Event, lists []Listing) string {
	site := a.cfg.SiteURL
	var b strings.Builder
	filter := ""
	if q.town != "" {
		filter += " in " + townName(q.town)
	}
	if q.category != "" {
		filter += " · " + catName(q.category)
	}
	if q.free {
		filter += " · free"
	}
	link := func(page string) string {
		u := site + "/" + page
		sep := "?"
		if q.town != "" {
			u += sep + "town=" + q.town
			sep = "&"
		}
		if q.category != "" {
			u += sep + "cat=" + q.category
		}
		return u
	}
	if q.wants != "listings" && (q.period != "" || len(events) > 0) {
		b.WriteString("*" + askPeriodTitle(q, now) + filter + "*\n")
		if len(events) == 0 {
			b.WriteString("Nothing listed" + filter + " for then yet. The full list is at " + link("events.html") + "\n")
		} else {
			lastDay := ""
			n := 0
			for _, e := range events {
				if n == askMaxItems {
					b.WriteString(fmt.Sprintf("…and %d more at %s\n", len(events)-n, link("events.html")))
					break
				}
				if q.period != "today" && q.period != "tomorrow" && e.Date != lastDay {
					b.WriteString("_" + fmtDate(e.Date) + "_\n")
					lastDay = e.Date
				}
				b.WriteString(askEventLine(e) + "\n")
				n++
			}
			if n == len(events) {
				b.WriteString("Details and directions: " + link("events.html") + "\n")
			}
		}
	}
	if q.wants != "events" && len(lists) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("*Groups, activities and places" + filter + "*\n")
		n := 0
		for _, l := range lists {
			if n == askMaxItems {
				b.WriteString(fmt.Sprintf("…and %d more at %s\n", len(lists)-n, link("directory.html")))
				break
			}
			b.WriteString(askListingLine(l) + "\n")
			n++
		}
		if n == len(lists) {
			b.WriteString("More on each at " + link("directory.html") + "\n")
		}
	}
	if b.Len() == 0 {
		b.WriteString("I could not find anything for \"" + clip(q.raw, 60) + "\". Try a town (Strand, Somerset West, Gordon's Bay, Sir Lowry's Pass), a kind of thing (markets, running, kids) or a time (today, weekend, week). Everything is at " + site + "\n")
	}
	out := strings.TrimSpace(b.String())
	if len(out) > askMaxReply {
		out = clip(out, askMaxReply-len(site)-12) + "\nMore: " + site
	}
	return out
}

// answer is the whole thing: read, find, write, and optionally ask the model.
func (a *App) answer(text string) string {
	nowLocal := time.Now().In(a.cfg.TZ)
	today := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, a.cfg.TZ)
	q := readQuestion(text, today)
	if q.help {
		return a.askHelp()
	}
	events, lists := a.answerData(q, today.Format("2006-01-02"))
	plain := a.plainAnswer(q, today, events, lists)
	// A bare command (today, week, Strand weekend) is answered as a list;
	// a question in words goes to the model when one is configured.
	if a.cfg.AIURL == "" || q.bare {
		return plain
	}
	if ai, err := a.aiAnswer(q, today, events, lists); err == nil && ai != "" {
		return ai
	} else if err != nil {
		a.logf("ask: model unavailable, plain answer sent: %v", err)
	}
	return plain
}

/* ---------- the optional model ---------- */

// aiAnswer asks an Ollama-compatible /api/chat for a short reply grounded in
// the matches. The prompt carries only site content and the question.
func (a *App) aiAnswer(q askQuery, now time.Time, events []Event, lists []Listing) (string, error) {
	var ctx strings.Builder
	ctx.WriteString("Today is " + now.Format("Monday 2 January 2006") + ".\n")
	if len(events) > 0 {
		ctx.WriteString("EVENTS (date, time, title, town, cost, summary, link):\n")
		for i, e := range events {
			if i == 12 {
				break
			}
			ctx.WriteString(fmt.Sprintf("- %s %s | %s | %s | %s | %s | %s/events.html?ev=%s\n", e.Date, e.Time, e.Title, townName(e.Town), e.Cost, clip(e.Summary, 160), a.cfg.SiteURL, e.ID))
		}
	}
	if len(lists) > 0 {
		ctx.WriteString("LISTINGS (name, type, town, category, cost, when, summary, link):\n")
		for i, l := range lists {
			if i == 12 {
				break
			}
			ctx.WriteString(fmt.Sprintf("- %s | %s | %s | %s | %s | %s | %s | %s/listing.html?id=%s\n", l.Name, l.Type, townName(l.Town), catName(l.Category), l.Cost, l.Schedule.Text, clip(l.Summary, 160), a.cfg.SiteURL, l.ID))
		}
	}
	if len(events) == 0 && len(lists) == 0 {
		ctx.WriteString("No matching events or listings were found on the site.\n")
	}
	system := "You are Helderberg Social's WhatsApp helper for Somerset West, Strand, Gordon's Bay and Sir Lowry's Pass. Answer the question using ONLY the site content given. If the content does not answer it, say so and point to " + a.cfg.SiteURL + ". Never invent an event, place, time, price or phone number. Plain text for WhatsApp: short lines, • bullets, *bold* for names, no markdown headings, under 900 characters, include the link for anything you name. Answer in the language of the question (English or Afrikaans)."
	body := map[string]any{
		"model":   a.cfg.AIModel,
		"stream":  false,
		"options": map[string]any{"temperature": 0.2, "num_ctx": 8192},
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": "SITE CONTENT:\n" + ctx.String() + "\nQUESTION: " + q.raw},
		},
	}
	buf, _ := json.Marshal(body)
	c, cancel := context.WithTimeout(context.Background(), askAITimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(c, "POST", strings.TrimRight(a.cfg.AIURL, "/")+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.cfg.AIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.AIKey)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("model HTTP %d", res.StatusCode)
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, res.Body, 64<<10)).Decode(&out); err != nil {
		return "", err
	}
	text := strings.TrimSpace(out.Message.Content)
	if text == "" || len(text) > 2000 {
		return "", fmt.Errorf("model answer unusable (%d chars)", len(text))
	}
	return text, nil
}

/* ---------- the two doors ---------- */

// askAPI is GET /api/ask?q=…: the same answer as WhatsApp, for the site and
// for trying questions out. Read bucket rate limit, no caching.
func (a *App) askAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		q = "help"
	}
	a.json(w, 200, map[string]any{"ok": true, "answer": a.answer(q), "ai": a.cfg.AIURL != ""})
}

// waAnswer replies to a WhatsApp message with an answer, within a per-number
// budget of its own (askPerHour) so a chatty number cannot run up the bill,
// and logs the reply like any other send.
func (a *App) waAnswer(from, text string) {
	h := emailHash("tel:" + from)
	var n int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM mail_log WHERE to_hash = ? AND kind = 'wa-answer' AND sent_at > ?`, h, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)).Scan(&n)
	if n >= askPerHour {
		a.logMail(h, "wa-answer", fmt.Errorf("answer budget exceeded"))
		return
	}
	reply := a.answer(text)
	err := a.wa.sendText(from, reply)
	a.logMail(h, "wa-answer", err)
	if err != nil {
		a.logf("whatsapp answer failed: %v", err)
	}
}
