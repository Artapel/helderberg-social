package main

import (
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Vocabulary the site understands. Anything outside it is rejected, never
// coerced, so a bad payload cannot smuggle a new category or town into the
// data. Keep in step with data/data.js.
var (
	towns      = set("somerset-west", "strand", "gordons-bay", "sir-lowrys-pass")
	categories = set("running", "cycling", "hiking", "water", "markets", "wine", "community", "family", "arts", "online", "nature", "sport", "music", "faith", "camping", "games")
	audiences  = set("everyone", "families", "kids", "seniors", "beginners", "dogs")
	costs      = set("free", "paid", "membership", "donation", "varies")
	kinds      = set("group", "activity", "place", "update")
	slugRe     = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	timeRe     = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
)

func set(v ...string) map[string]bool {
	m := make(map[string]bool, len(v))
	for _, s := range v {
		m[s] = true
	}
	return m
}

func validEmail(s string) bool {
	if len(s) > 254 || strings.ContainsAny(s, " \t\r\n<>,") {
		return false
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || !strings.Contains(s, ".") {
		return false
	}
	return true
}

func normEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// clean trims, collapses whitespace, strips control characters and caps
// length. Everything user-typed passes through here before storage.
func clean(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || r == '\uFEFF' {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	return cut(s, max)
}

// cut truncates to at most max bytes without splitting a multi-byte rune.
func cut(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// cleanMulti keeps paragraph breaks for free-text fields.
func cleanMulti(s string, max int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	parts := strings.Split(s, "\n")
	for i := range parts {
		parts[i] = clean(parts[i], max)
	}
	s = strings.Trim(strings.Join(parts, "\n"), "\n")
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return cut(s, max)
}

// validURL accepts only absolute http(s) URLs with a host, and rejects
// anything with credentials or a javascript:/data: scheme by construction.
func validURL(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}
	if len(s) > 500 {
		return "", false
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", false
	}
	return u.String(), true
}

func validDate(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 60 {
		out = strings.Trim(out[:60], "-")
	}
	return out
}

func filterSet(vals []string, allowed map[string]bool, max int) ([]string, bool) {
	out := make([]string, 0, len(vals))
	seen := map[string]bool{}
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if !allowed[v] {
			return nil, false
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	if len(out) > max {
		return nil, false
	}
	return out, true
}
