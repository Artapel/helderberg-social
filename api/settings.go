package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Runtime settings live in the meta table under set:* and override the
// environment defaults without a restart. The console edits them; the
// scheduler and the public API read them through these helpers.

type settingDef struct {
	Key, Label, Help, Kind string // kind: text, int, bool, select-day, textarea
	Options                []string
}

var settingDefs = []settingDef{
	{Key: "digest_hour", Label: "Digest hour (local)", Help: "Hour of the day, 0-23, when daily and weekly digests go out.", Kind: "int"},
	{Key: "weekly_day", Label: "Weekly digest day", Help: "Which day the weekly digest is sent.", Kind: "select-day"},
	{Key: "digests_on", Label: "Digests enabled", Help: "Switch off to pause all scheduled digests (previews still work).", Kind: "bool"},
	{Key: "watch_minutes", Label: "Source check interval (minutes)", Help: "How often watched sources are fetched. Minimum 15.", Kind: "int"},
	{Key: "watch_on", Label: "Source watching enabled", Help: "Switch off to stop scheduled source checks.", Kind: "bool"},
	{Key: "notify_extra", Label: "Extra notification addresses", Help: "Comma-separated. They receive copies of moderation emails but cannot sign in.", Kind: "text"},
	{Key: "announcement_on", Label: "Show announcement banner", Help: "Shows the banner below on every page of the site within 5 minutes.", Kind: "bool"},
	{Key: "announcement_text", Label: "Announcement text", Help: "Plain text, up to 200 characters.", Kind: "text"},
	{Key: "announcement_link", Label: "Announcement link", Help: "Optional https:// address the banner points to.", Kind: "text"},
	{Key: "maintenance", Label: "Maintenance mode", Help: "Public submissions and subscriptions answer 503 with a friendly message. Reading still works.", Kind: "bool"},
	{Key: "maintenance_text", Label: "Maintenance message", Help: "Shown to visitors while maintenance mode is on.", Kind: "text"},
	{Key: "submissions_on", Label: "Accept public submissions", Help: "Switch off to refuse new event and listing submissions (subscriptions unaffected).", Kind: "bool"},
	{Key: "registrations_on", Label: "Accept new member accounts", Help: "Switch off to stop people creating accounts. Existing members can still sign in and post.", Kind: "bool"},
	{Key: "subscriptions_on", Label: "Accept new subscribers", Help: "Switch off to refuse new subscriptions (existing ones keep receiving digests).", Kind: "bool"},
	{Key: "events_window_days", Label: "Public events window (days)", Help: "How far ahead /api/events publishes. 30-400.", Kind: "int"},
	{Key: "fb_events_on", Label: "Facebook: post approved events", Help: "Each event you approve is posted to the Facebook page after the delay below. Needs HS_FB_PAGE_ID and HS_FB_PAGE_TOKEN.", Kind: "bool"},
	{Key: "fb_events_delay", Label: "Facebook: delay before an event is posted (minutes)", Help: "Time to spot a typo and cancel it from the Facebook page in the console. 0-1440.", Kind: "int"},
	{Key: "fb_weekend_on", Label: "Facebook: weekly \"this weekend\" post", Help: "Once a week, a list of the approved events on the coming Saturday and Sunday. Skipped when there is nothing on.", Kind: "bool"},
	{Key: "fb_weekend_day", Label: "Facebook: weekend post day", Help: "Which day the weekend list is posted (Thursday or Friday works best).", Kind: "select-day"},
	{Key: "fb_weekend_hour", Label: "Facebook: weekend post hour (local)", Help: "Hour of the day, 0-23.", Kind: "int"},
}

func (a *App) settingDefault(key string) string {
	switch key {
	case "digest_hour":
		return fmt.Sprint(a.cfg.DigestHour)
	case "weekly_day":
		return fmt.Sprint(int(a.cfg.WeeklyDay))
	case "watch_minutes":
		return fmt.Sprint(int(a.cfg.WatchInterval.Minutes()))
	case "digests_on", "watch_on", "submissions_on", "subscriptions_on", "registrations_on":
		return "1"
	case "maintenance_text":
		return "We are doing a little maintenance. Please try again in a few minutes."
	case "events_window_days":
		return "400"
	case "fb_events_delay":
		return "30"
	case "fb_weekend_day":
		return "4"
	case "fb_weekend_hour":
		return "17"
	}
	return ""
}

func (a *App) setting(key string) string {
	if v := a.metaGet("set:" + key); v != "" {
		return v
	}
	return a.settingDefault(key)
}

func (a *App) settingBool(key string) bool { return a.setting(key) == "1" }

func (a *App) settingInt(key string) int {
	n, _ := strconv.Atoi(a.setting(key))
	return n
}

// saveSettings validates the whole form and writes only what passed. It
// returns the first problem as a sentence.
func (a *App) saveSettings(form map[string]string) (string, error) {
	vals := map[string]string{}
	for _, d := range settingDefs {
		v := strings.TrimSpace(form[d.Key])
		if v == "" && (d.Kind == "int" || d.Kind == "select-day") {
			v = a.settingDefault(d.Key) // the page promises blank means default
		}
		switch d.Kind {
		case "bool":
			if v == "1" || v == "on" {
				v = "1"
			} else {
				v = "0"
			}
		case "int":
			n, err := strconv.Atoi(v)
			if err != nil {
				return "", fmt.Errorf("%s must be a number", d.Label)
			}
			switch d.Key {
			case "digest_hour":
				if n < 0 || n > 23 {
					return "", fmt.Errorf("digest hour must be 0-23")
				}
			case "watch_minutes":
				if n < 15 || n > 24*60 {
					return "", fmt.Errorf("check interval must be 15-1440 minutes")
				}
			case "events_window_days":
				if n < 30 || n > 400 {
					return "", fmt.Errorf("events window must be 30-400 days")
				}
			case "fb_events_delay":
				if n < 0 || n > 1440 {
					return "", fmt.Errorf("Facebook delay must be 0-1440 minutes")
				}
			case "fb_weekend_hour":
				if n < 0 || n > 23 {
					return "", fmt.Errorf("Facebook weekend post hour must be 0-23")
				}
			}
			v = fmt.Sprint(n)
		case "select-day":
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > 6 {
				return "", fmt.Errorf("%s must be 0-6", d.Label)
			}
			v = fmt.Sprint(n)
		default:
			v = clean(v, 200)
			switch d.Key {
			case "announcement_link":
				if v != "" {
					u, ok := validURL(v)
					if !ok {
						return "", fmt.Errorf("announcement link must be a full http(s) address")
					}
					v = u
				}
			case "notify_extra":
				var keep []string
				for _, e := range strings.Split(v, ",") {
					e = normEmail(e)
					if e == "" {
						continue
					}
					if !validEmail(e) {
						return "", fmt.Errorf("%q is not a valid email address", e)
					}
					keep = append(keep, e)
				}
				v = strings.Join(keep, ",")
			}
		}
		vals[d.Key] = v
	}
	for k, v := range vals {
		if err := a.metaSet("set:"+k, v); err != nil {
			return "", err
		}
	}
	return "Settings saved.", nil
}

func (a *App) digestHour() int         { return a.settingInt("digest_hour") }
func (a *App) weeklyDay() time.Weekday { return time.Weekday(a.settingInt("weekly_day")) }
func (a *App) watchInterval() time.Duration {
	return time.Duration(a.settingInt("watch_minutes")) * time.Minute
}

// notifyList is who gets moderation mail: the admin plus any extras.
func (a *App) notifyList() []string {
	out := []string{a.cfg.AdminEmail}
	for _, e := range strings.Split(a.setting("notify_extra"), ",") {
		if e = normEmail(e); e != "" && e != a.cfg.AdminEmail {
			out = append(out, e)
		}
	}
	return out
}

// siteInfo is the block /api/events carries so the static site can show an
// announcement or a maintenance notice without a deploy.
func (a *App) siteInfo() map[string]any {
	m := map[string]any{"maintenance": a.settingBool("maintenance"), "submissions": a.settingBool("submissions_on") && !a.settingBool("maintenance"), "subscriptions": a.settingBool("subscriptions_on") && !a.settingBool("maintenance")}
	if a.settingBool("announcement_on") && a.setting("announcement_text") != "" {
		m["announcement"] = map[string]string{"text": a.setting("announcement_text"), "link": a.setting("announcement_link")}
	}
	if a.settingBool("maintenance") {
		m["maintenanceText"] = a.setting("maintenance_text")
	}
	return m
}

/* ---------- blocklist ---------- */

type blocklist struct {
	mu sync.RWMutex
	m  map[string]bool
}

func (a *App) loadBlocklist() {
	m := map[string]bool{}
	rows, err := a.db.Query(`SELECT kind, value FROM blocklist`)
	if err == nil {
		for rows.Next() {
			var k, v string
			if rows.Scan(&k, &v) == nil {
				m[k+":"+v] = true
			}
		}
		rows.Close()
	}
	a.block.mu.Lock()
	a.block.m = m
	a.block.mu.Unlock()
}

func (a *App) isBlocked(kind, value string) bool {
	a.block.mu.RLock()
	defer a.block.mu.RUnlock()
	return a.block.m[kind+":"+value]
}

func (a *App) addBlock(kind, value, note string) error {
	if kind != "ip" && kind != "email" {
		return fmt.Errorf("unknown block kind")
	}
	_, err := a.db.Exec(`INSERT OR IGNORE INTO blocklist(kind, value, note, created_at) VALUES(?,?,?,?)`, kind, value, clean(note, 120), now())
	a.loadBlocklist()
	return err
}

func (a *App) removeBlock(id string) {
	_, _ = a.db.Exec(`DELETE FROM blocklist WHERE id = ?`, id)
	a.loadBlocklist()
}
