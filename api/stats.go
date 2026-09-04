package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Usage statistics for the console. Everything is counted in memory and
// flushed to SQLite once a minute, so a busy day costs a handful of writes,
// and nothing here identifies a person: page-view uniqueness uses a hash
// salted with the day, which cannot be joined across days or back to an
// address.

const (
	reqRingSize = 300
	logRingSize = 500
)

type reqEntry struct {
	At     time.Time
	Method string
	Path   string
	Status int
	Ms     int64
	IP     string
}

type stats struct {
	mu     sync.Mutex
	start  time.Time
	counts map[string]int      // day|route|status -> n
	pv     map[string]int      // day|path -> views
	pvu    map[string]struct{} // day|path|hash
	reqs   []reqEntry
	logs   []string
}

func newStats() *stats {
	return &stats{start: time.Now(), counts: map[string]int{}, pv: map[string]int{}, pvu: map[string]struct{}{}}
}

func (s *stats) request(day, route string, status int, e reqEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[fmt.Sprintf("%s|%s|%d", day, route, status)]++
	s.reqs = append(s.reqs, e)
	if len(s.reqs) > reqRingSize {
		s.reqs = s.reqs[len(s.reqs)-reqRingSize:]
	}
}

func (s *stats) pageview(day, path, hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pv[day+"|"+path]++
	s.pvu[day+"|"+path+"|"+hash] = struct{}{}
	if len(s.pvu) > 50000 { // a flood must not grow memory without bound
		s.pvu = map[string]struct{}{}
	}
}

func (s *stats) log(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, line)
	if len(s.logs) > logRingSize {
		s.logs = s.logs[len(s.logs)-logRingSize:]
	}
}

func (s *stats) recent() ([]reqEntry, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := make([]reqEntry, len(s.reqs))
	copy(r, s.reqs)
	l := make([]string, len(s.logs))
	copy(l, s.logs)
	return r, l
}

// flush moves the counters into the database. Called every minute by the
// scheduler and once at shutdown.
func (a *App) flushStats() {
	s := a.stats
	s.mu.Lock()
	counts, pv, pvu := s.counts, s.pv, s.pvu
	s.counts, s.pv, s.pvu = map[string]int{}, map[string]int{}, map[string]struct{}{}
	s.mu.Unlock()
	for k, n := range counts {
		p := strings.SplitN(k, "|", 3)
		_, _ = a.db.Exec(`INSERT INTO req_stats(day, route, status, n) VALUES(?,?,?,?) ON CONFLICT(day, route, status) DO UPDATE SET n = n + excluded.n`, p[0], p[1], p[2], n)
	}
	for k, n := range pv {
		p := strings.SplitN(k, "|", 2)
		_, _ = a.db.Exec(`INSERT INTO pageviews(day, path, views) VALUES(?,?,?) ON CONFLICT(day, path) DO UPDATE SET views = views + excluded.views`, p[0], p[1], n)
	}
	for k := range pvu {
		p := strings.SplitN(k, "|", 3)
		_, _ = a.db.Exec(`INSERT OR IGNORE INTO pv_uniques(day, path, iph) VALUES(?,?,?)`, p[0], p[1], p[2])
	}
}

func (a *App) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	log.Print(line)
	if a.stats != nil {
		a.stats.log(time.Now().UTC().Format("2006-01-02 15:04:05") + " " + line)
	}
}

func (a *App) localDay(t time.Time) string { return t.In(a.cfg.TZ).Format("2006-01-02") }

// dailyHash is the per-day visitor token: HMAC(secret, day) salts the IP so
// the same address hashes differently tomorrow.
func (a *App) dailyHash(day, ip string) string {
	m := hmac.New(sha256.New, a.cfg.Secret)
	m.Write([]byte("pv:" + day))
	m.Write([]byte(ip))
	return hex.EncodeToString(m.Sum(nil)[:12])
}

var pathRe = regexp.MustCompile(`^/[A-Za-z0-9._/-]{0,79}$`)

// ping is the page-view beacon the site sends: {"p":"/events.html"}. It is
// deliberately dumb: no cookies, no ids, no referrers, no user agents.
func (a *App) ping(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Path string `json:"p"`
	}
	if err := readJSON(r, &q); err != nil || !pathRe.MatchString(q.Path) {
		a.fail(w, 400, "bad ping")
		return
	}
	day := a.localDay(time.Now())
	a.stats.pageview(day, q.Path, a.dailyHash(day, ipOf(r)))
	w.WriteHeader(204)
}

/* ---------- queries for the console ---------- */

type dayCount struct {
	Day string
	N   int
	N2  int
}

func (a *App) pageviewSeries(days int) []dayCount {
	from := a.localDay(time.Now().AddDate(0, 0, -days+1))
	m := map[string]*dayCount{}
	for i := 0; i < days; i++ {
		d := a.localDay(time.Now().AddDate(0, 0, -days+1+i))
		m[d] = &dayCount{Day: d}
	}
	rows, err := a.db.Query(`SELECT day, SUM(views) FROM pageviews WHERE day >= ? GROUP BY day`, from)
	if err == nil {
		for rows.Next() {
			var d string
			var n int
			_ = rows.Scan(&d, &n)
			if c, ok := m[d]; ok {
				c.N = n
			}
		}
		rows.Close()
	}
	rows, err = a.db.Query(`SELECT day, COUNT(DISTINCT iph) FROM pv_uniques WHERE day >= ? GROUP BY day`, from)
	if err == nil {
		for rows.Next() {
			var d string
			var n int
			_ = rows.Scan(&d, &n)
			if c, ok := m[d]; ok {
				c.N2 = n
			}
		}
		rows.Close()
	}
	out := make([]dayCount, 0, days)
	for i := 0; i < days; i++ {
		out = append(out, *m[a.localDay(time.Now().AddDate(0, 0, -days+1+i))])
	}
	return out
}

type kv struct {
	K  string
	N  int
	N2 int
}

func (a *App) topPages(days, limit int) []kv {
	from := a.localDay(time.Now().AddDate(0, 0, -days+1))
	rows, err := a.db.Query(`SELECT p.path, SUM(p.views), (SELECT COUNT(*) FROM pv_uniques u WHERE u.path = p.path AND u.day >= ?) FROM pageviews p WHERE p.day >= ? GROUP BY p.path ORDER BY 2 DESC LIMIT ?`, from, from, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []kv
	for rows.Next() {
		var k kv
		if rows.Scan(&k.K, &k.N, &k.N2) == nil {
			out = append(out, k)
		}
	}
	return out
}

func (a *App) routeStats(days int) []kv {
	from := a.localDay(time.Now().AddDate(0, 0, -days+1))
	rows, err := a.db.Query(`SELECT route, SUM(n), SUM(CASE WHEN status >= 400 THEN n ELSE 0 END) FROM req_stats WHERE day >= ? GROUP BY route ORDER BY 2 DESC`, from)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []kv
	for rows.Next() {
		var k kv
		if rows.Scan(&k.K, &k.N, &k.N2) == nil {
			out = append(out, k)
		}
	}
	return out
}

func (a *App) requestSeries(days int) []dayCount {
	from := a.localDay(time.Now().AddDate(0, 0, -days+1))
	m := map[string]*dayCount{}
	out := make([]dayCount, days)
	for i := 0; i < days; i++ {
		d := a.localDay(time.Now().AddDate(0, 0, -days+1+i))
		out[i] = dayCount{Day: d}
		m[d] = &out[i]
	}
	rows, err := a.db.Query(`SELECT day, SUM(n), SUM(CASE WHEN status >= 400 THEN n ELSE 0 END) FROM req_stats WHERE day >= ? GROUP BY day`, from)
	if err == nil {
		for rows.Next() {
			var d string
			var n, e int
			_ = rows.Scan(&d, &n, &e)
			if c, ok := m[d]; ok {
				c.N, c.N2 = n, e
			}
		}
		rows.Close()
	}
	return out
}

func (a *App) count(q string, args ...any) int {
	var n int
	_ = a.db.QueryRow(q, args...).Scan(&n)
	return n
}
