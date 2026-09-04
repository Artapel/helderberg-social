/* Helderberg Social — shared runtime. Depends on data/data.js having set window.HS_DATA. */
(function () {
  "use strict";
  var D = window.HS_DATA || { site: {}, towns: [], categories: [], audiences: [], listings: [], events: [] };
  var HS = window.HS = { data: D };

  var index = function (arr) { var m = {}; (arr || []).forEach(function (x) { m[x.id] = x; }); return m; };
  HS.towns = index(D.towns);
  HS.categories = index(D.categories);
  HS.audiences = index(D.audiences);
  HS.listings = index(D.listings);
  HS.events = index(D.events);

  HS.DAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
  HS.DAYS_SHORT = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  HS.MONTHS = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];

  HS.esc = function (s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  };
  HS.townName = function (id) { return (HS.towns[id] || {}).name || id || ""; };
  HS.catName = function (id) { return (HS.categories[id] || {}).name || id || ""; };
  HS.catIcon = function (id) { return (HS.categories[id] || {}).icon || "📍"; };
  HS.catHue = function (id) { var c = HS.categories[id]; return c ? c.hue : 200; };
  HS.typeLabel = function (t) { return { group: "Group", activity: "Activity", place: "Place" }[t] || t; };
  HS.costLabel = function (c) { return { free: "Free", paid: "Paid", membership: "Membership", donation: "Donation" }[c] || c || ""; };

  HS.qs = function () { return new URLSearchParams(location.search); };
  HS.setQs = function (params) {
    var p = new URLSearchParams();
    Object.keys(params).forEach(function (k) { if (params[k] !== "" && params[k] != null) p.set(k, params[k]); });
    var s = p.toString();
    history.replaceState(null, "", location.pathname + (s ? "?" + s : ""));
  };

  HS.parseDate = function (iso) { var p = (iso || "").split("-").map(Number); return new Date(p[0], (p[1] || 1) - 1, p[2] || 1); };
  HS.today = function () { var d = new Date(); return new Date(d.getFullYear(), d.getMonth(), d.getDate()); };
  HS.fmtDate = function (iso) {
    var d = HS.parseDate(iso);
    return HS.DAYS_SHORT[d.getDay()] + " " + d.getDate() + " " + HS.MONTHS[d.getMonth()].slice(0, 3) + " " + d.getFullYear();
  };
  HS.fmtRange = function (ev) {
    var s = HS.fmtDate(ev.date);
    if (ev.endDate && ev.endDate !== ev.date) s += " – " + HS.fmtDate(ev.endDate);
    if (ev.time) s += " · " + ev.time + (ev.endTime ? "–" + ev.endTime : "");
    return s;
  };

  /* ---------- Search ---------- */
  HS.score = function (item, q) {
    if (!q) return 1;
    var hay = [item.name || item.title, item.summary, (item.tags || []).join(" "), HS.townName(item.town), HS.catName(item.category)].join(" ").toLowerCase();
    var terms = q.toLowerCase().split(/\s+/).filter(Boolean);
    var score = 0;
    for (var i = 0; i < terms.length; i++) {
      if (hay.indexOf(terms[i]) === -1) return 0;
      score += ((item.name || item.title || "").toLowerCase().indexOf(terms[i]) !== -1) ? 3 : 1;
    }
    return score;
  };
  HS.search = function (q, items) {
    return (items || D.listings).map(function (it) { return { it: it, s: HS.score(it, q) }; })
      .filter(function (x) { return x.s > 0; })
      .sort(function (a, b) { return b.s - a.s || a.it.name.localeCompare(b.it.name); })
      .map(function (x) { return x.it; });
  };

  /* ---------- Event helpers ---------- */
  HS.upcomingEvents = function (days) {
    var t0 = HS.today(), t1 = new Date(t0.getTime() + (days || 3650) * 864e5);
    return D.events.filter(function (e) {
      var end = HS.parseDate(e.endDate || e.date);
      return end >= t0 && HS.parseDate(e.date) <= t1;
    }).sort(function (a, b) { return a.date < b.date ? -1 : a.date > b.date ? 1 : 0; });
  };
  HS.weekendListings = function () {
    return D.listings.filter(function (l) {
      var d = (l.schedule && l.schedule.days) || [];
      return d.indexOf(6) !== -1 || d.indexOf(0) !== -1 || d.indexOf(5) !== -1;
    });
  };
  HS.icsFor = function (ev) {
    var dt = function (iso, t) { var d = iso.replace(/-/g, ""); return t ? d + "T" + t.replace(":", "") + "00" : d; };
    var end = ev.endDate || ev.date;
    var lines = ["BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Helderberg Social//EN", "BEGIN:VEVENT",
      "UID:" + ev.id + "@" + (D.site.domain || "helderbergsocial.local"),
      "DTSTAMP:" + dt(new Date().toISOString().slice(0, 10)) + "T000000Z",
      ev.time ? "DTSTART:" + dt(ev.date, ev.time) : "DTSTART;VALUE=DATE:" + dt(ev.date),
      ev.time ? "DTEND:" + dt(end, ev.endTime || ev.time) : "DTEND;VALUE=DATE:" + dt(HS.plusDay(end)),
      "SUMMARY:" + ev.title.replace(/,/g, "\\,"),
      "DESCRIPTION:" + (ev.summary || "").replace(/,/g, "\\,") + (ev.website ? "\\n" + ev.website : ""),
      "LOCATION:" + HS.townName(ev.town) + "\\, Helderberg",
      "END:VEVENT", "END:VCALENDAR"];
    return "data:text/calendar;charset=utf-8," + encodeURIComponent(lines.join("\r\n"));
  };
  HS.plusDay = function (iso) { var d = HS.parseDate(iso); d.setDate(d.getDate() + 1); return d.toISOString().slice(0, 10); };

  /* ---------- Rendering ---------- */
  HS.art = function (cat) {
    var h = HS.catHue(cat);
    return "background: linear-gradient(135deg, hsl(" + h + " 55% 38%), hsl(" + ((h + 40) % 360) + " 70% 52%));";
  };
  HS.card = function (l) {
    var sched = l.schedule && l.schedule.text ? l.schedule.text : "";
    return '<a class="card" href="listing.html?id=' + encodeURIComponent(l.id) + '">' +
      '<div class="card-art" style="' + HS.art(l.category) + '"><span>' + HS.catIcon(l.category) + '</span><span class="type">' + HS.typeLabel(l.type) + '</span></div>' +
      '<div class="card-body"><h3>' + HS.esc(l.name) + '</h3>' +
      '<div class="meta"><span>📍 ' + HS.esc(HS.townName(l.town)) + '</span><span>' + HS.esc(HS.catName(l.category)) + '</span></div>' +
      '<p class="summary">' + HS.esc(l.summary) + '</p>' +
      (sched ? '<div class="small muted">🕒 ' + HS.esc(sched) + '</div>' : "") +
      '<div class="foot">' + (l.cost === "free" ? '<span class="pill free">Free</span>' : '<span class="pill">' + HS.esc(HS.costLabel(l.cost)) + '</span>') +
      (l.verified ? '<span class="pill verified">Verified</span>' : '<span class="pill unverified">Unverified</span>') +
      (l.tags || []).slice(0, 2).map(function (t) { return '<span class="pill">' + HS.esc(t) + '</span>'; }).join("") +
      '</div></div></a>';
  };
  HS.eventRow = function (e) {
    var d = HS.parseDate(e.date);
    var link = e.listing && HS.listings[e.listing] ? 'listing.html?id=' + encodeURIComponent(e.listing) : (e.website || "#");
    return '<div class="event">' +
      '<div class="date"><span>' + HS.MONTHS[d.getMonth()].slice(0, 3) + '</span><b>' + d.getDate() + '</b><span>' + HS.DAYS_SHORT[d.getDay()] + '</span></div>' +
      '<div><h3><a href="' + HS.esc(link) + '"' + (/^https?:/.test(link) ? ' target="_blank" rel="noopener"' : "") + '>' + HS.esc(e.title) + '</a></h3>' +
      '<div class="meta">' + HS.esc(HS.fmtRange(e)) + ' · 📍 ' + HS.esc(HS.townName(e.town)) + ' · ' + HS.esc(HS.catName(e.category)) +
      (e.cost === "free" ? ' · <span class="pill free">Free</span>' : "") + (e.verified ? "" : ' · <span class="pill unverified">Unverified</span>') + '</div>' +
      (e.summary ? '<p class="small" style="margin:.3rem 0 0">' + HS.esc(e.summary) + '</p>' : "") + '</div>' +
      '<div class="actions">' + (e.website ? '<a class="btn ghost sm" target="_blank" rel="noopener" href="' + HS.esc(e.website) + '">Details</a>' : "") +
      '<a class="btn ghost sm" download="' + HS.esc(e.id) + '.ics" href="' + HS.icsFor(e) + '">Add to calendar</a></div></div>';
  };

  HS.hasCoords = function (l) { return !!l && Array.isArray(l.coords) && l.coords.length === 2 && typeof l.coords[0] === "number" && typeof l.coords[1] === "number"; };
  HS.map = function (el, items, opts) {
    if (!window.L || !el) return null;
    opts = opts || {};
    var m = L.map(el, { scrollWheelZoom: false }).setView(opts.center || D.site.center || [-34.12, 18.86], opts.zoom || D.site.zoom || 12);
    L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", { maxZoom: 18, attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>' }).addTo(m);
    var pts = [];
    (items || []).forEach(function (l) {
      if (!l.coords) return;
      var mk = L.marker(l.coords).addTo(m);
      mk.bindPopup('<b>' + HS.catIcon(l.category) + ' <a href="listing.html?id=' + encodeURIComponent(l.id) + '">' + HS.esc(l.name) + '</a></b><br>' + HS.esc(HS.townName(l.town)) + (l.coordsApprox ? ' <span style="opacity:.7">(approx. position)</span>' : ""));
      pts.push(l.coords);
    });
    if (pts.length > 1 && opts.fit !== false) m.fitBounds(pts, { padding: [24, 24] });
    return m;
  };

  /* ---------- Chrome ---------- */
  var NAV = [["index.html", "Home"], ["directory.html", "Directory"], ["events.html", "Events"], ["places.html", "Places"], ["towns.html", "Towns"], ["subscribe.html", "Get updates"], ["about.html", "About"], ["submit.html", "Add a listing", "cta"]];
  HS.wordmark = function () {
    var n = String(D.site.name || "Helderberg Social"), i = n.lastIndexOf(" ");
    return i > 0 ? HS.esc(n.slice(0, i)) + " <em>" + HS.esc(n.slice(i + 1)) + "</em>" : HS.esc(n);
  };
  HS.renderChrome = function () {
    var here = (location.pathname.split("/").pop() || "index.html").toLowerCase();
    var h = document.querySelector("[data-hs-header]");
    if (h) {
      h.className = "site-header";
      h.innerHTML = '<div class="wrap"><a class="logo" href="index.html" aria-label="' + HS.esc(D.site.name || "Helderberg Social") + ' home"><img class="logo-mark" src="assets/img/logo-mark.svg" alt="" width="36" height="36"><span class="wordmark">' + HS.wordmark() + '</span></a>' +
        '<button class="nav-toggle" aria-label="Menu" aria-expanded="false">☰</button><nav class="nav" aria-label="Main">' +
        NAV.map(function (n) { return '<a href="' + n[0] + '" class="' + (n[2] || "") + (here === n[0] ? " active" : "") + '">' + n[1] + '</a>'; }).join("") + '</nav></div>';
      var btn = h.querySelector(".nav-toggle"), nav = h.querySelector(".nav");
      btn.addEventListener("click", function () { var o = nav.classList.toggle("open"); btn.setAttribute("aria-expanded", o); });
    }
    var f = document.querySelector("[data-hs-footer]");
    if (f) {
      f.className = "site-footer";
      f.innerHTML = '<div class="wrap"><div><div class="logo" style="margin-bottom:.5rem"><img class="logo-mark" src="assets/img/logo-mark.svg" alt="" width="30" height="30"><span class="wordmark">' + HS.wordmark() + '</span></div>' +
        '<p>' + HS.esc(D.site.tagline || "") + '</p><p class="small">Listings are community-submitted. Anything marked <em>Unverified</em> has not yet been checked by us: confirm times and prices with the organiser before you go.</p>' +
        '<p class="small">© ' + new Date().getFullYear() + ' ' + HS.esc(D.site.name || "") + ' · ' + HS.esc(D.site.region || "") + '</p></div>' +
        '<div><h4>Explore</h4><ul><li><a href="directory.html">Groups &amp; activities</a></li><li><a href="events.html">Events</a></li><li><a href="places.html">Places</a></li><li><a href="towns.html">Towns</a></li></ul></div>' +
        '<div><h4>Get involved</h4><ul><li><a href="submit.html">Add a listing</a></li><li><a href="submit.html?kind=update">Report a change</a></li><li><a href="subscribe.html">Email updates</a></li><li><a href="about.html">About this site</a></li><li><a href="privacy.html">Privacy</a></li></ul></div></div>';
    }
    HS.updateCounts();
    document.querySelectorAll("form[data-hs-subscribe]").forEach(function (f) {
      f.addEventListener("submit", function (e) {
        e.preventDefault();
        try { sessionStorage.setItem("hs-sub-email", (f.querySelector("input[type=email]") || {}).value || ""); } catch (err) {}
        location.href = "subscribe.html";
      });
    });
  };
  HS.updateCounts = function () {
    document.querySelectorAll("[data-hs-count]").forEach(function (el) {
      var k = el.getAttribute("data-hs-count");
      var n = k === "events" ? HS.upcomingEvents().length : k === "towns" ? D.towns.length : D.listings.filter(function (l) { return k === "all" || l.type === k; }).length;
      el.textContent = n;
    });
  };

  /* ---------- API (optional: the site works fully static without it) ----------
     Approved events live in the API's database. data.js only carries a small
     offline fallback. Pages register a render function with HS.onEvents(fn):
     it runs once immediately with the static data and again with `true` if
     the API returns something different. */
  HS.api = String(D.site.apiBase || "").replace(/\/+$/, "");
  HS.post = function (path, body) {
    return fetch(HS.api + path, {
      method: "POST", credentials: "omit", mode: "cors",
      headers: { "Content-Type": "application/json", "Accept": "application/json" },
      body: JSON.stringify(body)
    }).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (j) {
        if (typeof j !== "object" || j === null) j = {};
        if (!r.ok && !j.error) j.error = r.status === 429 ? "Too many requests from your connection. Please wait a minute and try again." : "The server could not process that (" + r.status + ").";
        if (!r.ok) j.ok = false;
        return j;
      });
    });
  };
  var hooks = [], loaded = false, changed = false;
  HS.onEvents = function (fn) {
    fn(false);
    if (loaded) { if (changed) fn(true); } else hooks.push(fn);
  };
  /* Site-wide switches the admin sets in the console: announcement banner,
     maintenance mode, paused forms. They ride along on /api/events. */
  HS.site = null;
  var siteHooks = [];
  HS.onSite = function (fn) { if (HS.site) fn(HS.site); else siteHooks.push(fn); };
  function applySite(site) {
    if (!site || typeof site !== "object") return;
    HS.site = site;
    var old = document.getElementById("hs-announce");
    if (old) old.remove();
    var a = site.announcement;
    if (a && a.text) {
      var bar = document.createElement("div");
      bar.id = "hs-announce"; bar.className = "announce";
      bar.innerHTML = '<div class="wrap">' + HS.esc(a.text) + (a.link && /^https?:\/\//.test(a.link) ? ' <a href="' + HS.esc(a.link) + '" target="_blank" rel="noopener">More</a>' : "") + "</div>";
      var h = document.querySelector("[data-hs-header]");
      if (h && h.parentNode) h.parentNode.insertBefore(bar, h.nextSibling); else document.body.insertBefore(bar, document.body.firstChild);
    }
    document.querySelectorAll("form[data-gate]").forEach(function (f) {
      var gate = f.getAttribute("data-gate"), open = site[gate] !== false;
      var n = f.querySelector(".gate-notice");
      if (!open) {
        if (!n) { n = document.createElement("p"); n.className = "notice gate-notice"; f.insertBefore(n, f.firstChild); }
        n.textContent = site.maintenance ? (site.maintenanceText || "We are doing a little maintenance. Please try again in a few minutes.") : "This form is paused for the moment. Please try again later.";
      } else if (n) n.remove();
      f.querySelectorAll("button[type=submit],button:not([type])").forEach(function (b) { b.disabled = !open; });
    });
    var sh = siteHooks; siteHooks = [];
    sh.forEach(function (fn) { fn(site); });
  }
  /* One tiny page-view beacon per page: the path only. No cookie, no id,
     no referrer; the server hashes the connection with a daily salt. */
  HS.ping = function () {
    if (!HS.api || !window.fetch) return;
    var p = location.pathname.replace(/\/index\.html$/, "/") || "/";
    if (p.length > 80) p = p.slice(0, 80);
    try {
      fetch(HS.api + "/api/ping", { method: "POST", credentials: "omit", mode: "cors", keepalive: true,
        headers: { "Content-Type": "application/json" }, body: JSON.stringify({ p: p }) }).catch(function () {});
    } catch (e) {}
  };
  function mergeEvents(list) {
    var diff = false;
    (list || []).forEach(function (e) {
      if (!e || !e.id || !e.date || !e.title) return;
      var cur = HS.events[e.id];
      if (!cur) { D.events.push(e); diff = true; return; }
      var merged = Object.assign({}, cur, e);
      if (JSON.stringify(merged) !== JSON.stringify(cur)) { Object.assign(cur, e); diff = true; }
    });
    if (diff) HS.events = index(D.events);
    return diff;
  }
  HS.loadEvents = function () {
    if (!HS.api || !window.fetch) { loaded = true; return; }
    var key = "hs-events-v1", cached = null;
    try { cached = JSON.parse(sessionStorage.getItem(key) || "null"); } catch (e) {}
    var apply = function (list, site) {
      applySite(site);
      changed = mergeEvents(list) || changed;
      loaded = true;
      var h = hooks; hooks = [];
      if (changed) { h.forEach(function (fn) { fn(true); }); HS.updateCounts(); }
    };
    if (cached && cached.at && Date.now() - cached.at < 5 * 60 * 1000 && Array.isArray(cached.events)) { apply(cached.events, cached.site); return; }
    var ctrl = window.AbortController ? new AbortController() : null;
    if (ctrl) setTimeout(function () { ctrl.abort(); }, 6000);
    fetch(HS.api + "/api/events", { credentials: "omit", mode: "cors", signal: ctrl && ctrl.signal, headers: { "Accept": "application/json" } })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (j) {
        var list = j && Array.isArray(j.events) ? j.events : [];
        var site = j && j.site && typeof j.site === "object" ? j.site : null;
        try { sessionStorage.setItem(key, JSON.stringify({ at: Date.now(), events: list, site: site })); } catch (e) {}
        apply(list, site);
      })
      .catch(function () { apply([]); });
  };

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", HS.renderChrome); else HS.renderChrome();
  HS.loadEvents();
  HS.ping();
})();
