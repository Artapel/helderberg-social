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
    var p = new URLSearchParams(), keep = HS.qs().get("ev");
    if (keep) p.set("ev", keep);
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
  /* A listing with a status (paused, closed) stays findable, because people
     search for it, but is marked on every card and never offered as
     something to do this weekend. */
  HS.statusPill = function (l) {
    if (!l.status || !l.status.kind) return "";
    var label = l.status.kind === "closed" ? "Closed" : "Paused";
    return '<span class="pill status-' + HS.esc(l.status.kind) + '" title="' + HS.esc(l.status.text || "") + '">' + label + '</span>';
  };
  HS.weekendListings = function () {
    return D.listings.filter(function (l) {
      if (l.status && l.status.kind) return false;
      var d = (l.schedule && l.schedule.days) || [];
      return d.indexOf(6) !== -1 || d.indexOf(0) !== -1 || d.indexOf(5) !== -1;
    });
  };
  /* ---------- Calendar ----------
     Times in data are local Helderberg times with no zone; both the Google
     link (ctz=) and the .ics (TZID=) say so explicitly, so a phone set to
     another zone still files the event at the right local hour. */
  HS.TZ = "Africa/Johannesburg";
  HS.calTimes = function (ev) {
    var dt = function (iso, t) { var d = iso.replace(/-/g, ""); return t ? d + "T" + t.replace(":", "") + "00" : d; };
    var end = ev.endDate || ev.date;
    if (ev.time) {
      var endT = ev.endTime || (end === ev.date ? HS.plusHours(ev.time, 2) : ev.time);
      return { timed: true, start: dt(ev.date, ev.time), end: dt(end, endT) };
    }
    return { timed: false, start: dt(ev.date), end: dt(HS.plusDay(end)) };
  };
  HS.plusHours = function (hhmm, n) {
    var p = hhmm.split(":").map(Number), h = Math.min(23, (p[0] || 0) + n), m = p[1] || 0;
    return (h < 10 ? "0" : "") + h + ":" + (m < 10 ? "0" : "") + m;
  };
  HS.plusDay = function (iso) { var d = HS.parseDate(iso); d.setDate(d.getDate() + 1); return d.getFullYear() + "-" + (d.getMonth() < 9 ? "0" : "") + (d.getMonth() + 1) + "-" + (d.getDate() < 10 ? "0" : "") + d.getDate(); };
  HS.calDetails = function (ev) {
    return (ev.summary || "") + (ev.website ? "\n" + ev.website : "") + "\nFrom Helderberg Social: " + HS.eventURL(ev);
  };
  HS.googleCalURL = function (ev) {
    var t = HS.calTimes(ev);
    var p = new URLSearchParams({ action: "TEMPLATE", text: ev.title, dates: t.start + "/" + t.end, details: HS.calDetails(ev),
      location: HS.townName(ev.town) + ", Helderberg, South Africa" });
    if (t.timed) p.set("ctz", HS.TZ);
    return "https://calendar.google.com/calendar/render?" + p.toString();
  };
  HS.icsText = function (ev) {
    var t = HS.calTimes(ev);
    var esc = function (s) { return String(s || "").replace(/\\/g, "\\\\").replace(/;/g, "\\;").replace(/,/g, "\\,").replace(/\r?\n/g, "\\n"); };
    var stamp = new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
    var lines = ["BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Helderberg Social//EN", "CALSCALE:GREGORIAN", "METHOD:PUBLISH", "BEGIN:VEVENT",
      "UID:" + ev.id + "@" + (D.site.domain || "helderbergsocial.local"),
      "DTSTAMP:" + stamp,
      t.timed ? "DTSTART;TZID=" + HS.TZ + ":" + t.start : "DTSTART;VALUE=DATE:" + t.start,
      t.timed ? "DTEND;TZID=" + HS.TZ + ":" + t.end : "DTEND;VALUE=DATE:" + t.end,
      "SUMMARY:" + esc(ev.title),
      "DESCRIPTION:" + esc(HS.calDetails(ev)),
      "LOCATION:" + esc(HS.townName(ev.town) + ", Helderberg, South Africa"),
      "URL:" + HS.eventURL(ev),
      "END:VEVENT", "END:VCALENDAR"];
    return lines.join("\r\n") + "\r\n";
  };
  /* Hands the browser a real file (a data: link is refused by some browsers
     and mail apps). Falls back to opening the text where blobs are missing. */
  HS.downloadICS = function (ev) {
    var text = HS.icsText(ev), name = (ev.id || "event").replace(/[^\w.-]+/g, "-") + ".ics";
    try {
      var blob = new Blob([text], { type: "text/calendar;charset=utf-8" });
      if (window.navigator.msSaveOrOpenBlob) { window.navigator.msSaveOrOpenBlob(blob, name); return; }
      var url = URL.createObjectURL(blob), a = document.createElement("a");
      a.href = url; a.download = name; a.rel = "noopener"; a.style.display = "none";
      document.body.appendChild(a); a.click();
      setTimeout(function () { document.body.removeChild(a); URL.revokeObjectURL(url); }, 4000);
    } catch (e) {
      location.href = "data:text/calendar;charset=utf-8," + encodeURIComponent(text);
    }
  };

  /* ---------- Sharing ----------
     Every event has a permanent address on the events page (?ev=id) that
     scrolls to and highlights it, so a shared link lands on the right thing
     even though the site is static. */
  HS.eventURL = function (ev) { return HS.siteURL() + "events.html?ev=" + encodeURIComponent(ev.id); };
  HS.shareText = function (ev) {
    return ev.title + " · " + HS.fmtRange(ev) + " · " + HS.townName(ev.town) + (ev.cost === "free" ? " · Free" : "");
  };
  HS.shareLinks = function (ev) {
    var url = HS.eventURL(ev), text = HS.shareText(ev);
    return {
      whatsapp: "https://wa.me/?text=" + encodeURIComponent(text + "\n" + url),
      facebook: "https://www.facebook.com/sharer/sharer.php?u=" + encodeURIComponent(url),
      email: "mailto:?subject=" + encodeURIComponent(ev.title) + "&body=" + encodeURIComponent(text + "\n\n" + url),
      url: url, text: text
    };
  };
  HS.copyText = function (s) {
    if (navigator.clipboard && navigator.clipboard.writeText) return navigator.clipboard.writeText(s);
    return new Promise(function (res, rej) {
      var ta = document.createElement("textarea"); ta.value = s; ta.setAttribute("readonly", ""); ta.style.position = "fixed"; ta.style.opacity = "0";
      document.body.appendChild(ta); ta.select();
      try { document.execCommand("copy") ? res() : rej(new Error("copy failed")); } catch (e) { rej(e); }
      document.body.removeChild(ta);
    });
  };
  /* One listener for every event card on every page. Buttons carry
     data-act and the card carries data-ev; nothing inline, so the CSP holds. */
  HS.closeMenus = function (except) {
    document.querySelectorAll(".share-menu.open").forEach(function (m) { if (m !== except) { m.classList.remove("open"); var b = m.parentNode.querySelector("[data-act=share]"); if (b) b.setAttribute("aria-expanded", "false"); } });
  };
  document.addEventListener("click", function (e) {
    var btn = e.target.closest ? e.target.closest("[data-act]") : null;
    if (!btn) { if (!(e.target.closest && e.target.closest(".share-wrap"))) HS.closeMenus(); return; }
    var card = btn.closest("[data-ev]"), ev = card && HS.events[card.getAttribute("data-ev")];
    if (!ev) return;
    var act = btn.getAttribute("data-act");
    if (act === "ics") { e.preventDefault(); HS.downloadICS(ev); HS.closeMenus(); return; }
    if (act === "share") {
      e.preventDefault();
      var links = HS.shareLinks(ev);
      /* The system share sheet only on touch devices: on a desktop it hides
         the WhatsApp/Facebook/copy choices behind an OS dialog. */
      var touch = window.matchMedia && matchMedia("(pointer: coarse)").matches;
      if (touch && navigator.share && (!navigator.canShare || navigator.canShare({ url: links.url }))) {
        navigator.share({ title: ev.title, text: links.text, url: links.url }).catch(function () {});
        return;
      }
      var menu = btn.parentNode.querySelector(".share-menu");
      if (!menu) return;
      var open = !menu.classList.contains("open");
      HS.closeMenus(menu);
      menu.classList.toggle("open", open); btn.setAttribute("aria-expanded", open ? "true" : "false");
      return;
    }
    if (act === "copy") {
      e.preventDefault();
      HS.copyText(HS.eventURL(ev)).then(function () { btn.textContent = "Link copied"; }, function () { btn.textContent = "Copy failed"; });
      setTimeout(function () { btn.textContent = "Copy link"; HS.closeMenus(); }, 1600);
    }
  });
  document.addEventListener("keydown", function (e) { if (e.key === "Escape") HS.closeMenus(); });
  /* ?ev=<id>: scroll to that event and light it up once the list has rendered. */
  HS.focusEvent = function () {
    var id = HS.qs().get("ev");
    if (!id) return;
    var el = document.querySelector('[data-ev="' + (window.CSS && CSS.escape ? CSS.escape(id) : id.replace(/["\\]/g, "")) + '"]');
    if (!el || el.classList.contains("hl")) return;
    el.classList.add("hl"); el.setAttribute("tabindex", "-1");
    setTimeout(function () { el.scrollIntoView({ behavior: "smooth", block: "center" }); try { el.focus({ preventScroll: true }); } catch (err) {} }, 60);
  };

  /* ---------- Rendering ---------- */
  HS.ICON = {
    cal: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 2v2H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V6a2 2 0 0 0-2-2h-3V2h-2v2H9V2H7zm-3 8h16v10H4V10zm7 2v3H8v2h3v3h2v-3h3v-2h-3v-3h-2z"/></svg>',
    share: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18 16a3 3 0 0 0-2.1.9L8.9 13a3 3 0 0 0 0-2l7-3.9A3 3 0 1 0 15 5a3 3 0 0 0 .1.7L8 9.7a3 3 0 1 0 0 4.6l7.1 4A3 3 0 1 0 18 16z"/></svg>',
    wa: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2a10 10 0 0 0-8.6 15.1L2 22l5-1.3A10 10 0 1 0 12 2zm0 18.2a8.2 8.2 0 0 1-4.2-1.2l-.3-.2-3 .8.8-2.9-.2-.3A8.2 8.2 0 1 1 12 20.2zm4.5-6.1c-.2-.1-1.5-.7-1.7-.8s-.4-.1-.6.1-.6.8-.8 1-.3.2-.5.1a6.7 6.7 0 0 1-3.3-2.9c-.3-.4.2-.4.7-1.3a.5.5 0 0 0 0-.4l-.8-1.8c-.2-.5-.4-.4-.6-.4h-.5a1 1 0 0 0-.7.3 2.9 2.9 0 0 0-.9 2.2 5 5 0 0 0 1 2.7 11.5 11.5 0 0 0 4.4 3.9c1.6.7 2.3.8 3.1.6a2.6 2.6 0 0 0 1.7-1.2 2 2 0 0 0 .2-1.2c-.1-.1-.3-.2-.5-.3z"/></svg>',
    fb: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M13.5 22v-8h2.7l.4-3.2h-3.1V8.8c0-.9.3-1.6 1.6-1.6h1.7V4.4c-.3 0-1.3-.1-2.5-.1-2.5 0-4.1 1.5-4.1 4.2v2.3H7.4V14h2.8v8h3.3z"/></svg>',
    mail: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 4h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2zm0 2v.5l8 5 8-5V6H4zm0 3v9h16V9l-8 5-8-5z"/></svg>',
    link: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M10.6 13.4a1 1 0 0 1 0 1.4 4 4 0 0 1-5.7-5.6l2.9-2.9a4 4 0 0 1 5.6 0 1 1 0 1 1-1.4 1.4 2 2 0 0 0-2.8 0L6.3 10.6a2 2 0 0 0 2.9 2.8 1 1 0 0 1 1.4 0zm2.8-2.8a1 1 0 0 1 0-1.4 4 4 0 0 1 5.7 5.6l-2.9 2.9a4 4 0 0 1-5.6 0 1 1 0 1 1 1.4-1.4 2 2 0 0 0 2.8 0l2.9-2.9a2 2 0 0 0-2.9-2.8 1 1 0 0 1-1.4 0z"/></svg>'
  };
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
      '<div class="foot">' + HS.statusPill(l) + (l.cost === "free" ? '<span class="pill free">Free</span>' : '<span class="pill">' + HS.esc(HS.costLabel(l.cost)) + '</span>') +
      (l.verified ? '<span class="pill verified">Verified</span>' : '<span class="pill unverified">Unverified</span>') +
      (l.tags || []).slice(0, 2).map(function (t) { return '<span class="pill">' + HS.esc(t) + '</span>'; }).join("") +
      '</div></div></a>';
  };
  HS.eventRow = function (e) {
    var d = HS.parseDate(e.date);
    var link = e.listing && HS.listings[e.listing] ? 'listing.html?id=' + encodeURIComponent(e.listing) : (e.website || "#");
    var sh = HS.shareLinks(e);
    return '<div class="event" data-ev="' + HS.esc(e.id) + '" id="ev-' + HS.esc(e.id) + '">' +
      '<div class="date"><span>' + HS.MONTHS[d.getMonth()].slice(0, 3) + '</span><b>' + d.getDate() + '</b><span>' + HS.DAYS_SHORT[d.getDay()] + '</span></div>' +
      '<div><h3><a href="' + HS.esc(link) + '"' + (/^https?:/.test(link) ? ' target="_blank" rel="noopener"' : "") + '>' + HS.esc(e.title) + '</a></h3>' +
      '<div class="meta">' + HS.esc(HS.fmtRange(e)) + ' · 📍 ' + HS.esc(HS.townName(e.town)) + ' · ' + HS.esc(HS.catName(e.category)) +
      (e.cost === "free" ? ' · <span class="pill free">Free</span>' : "") + (e.verified ? "" : ' · <span class="pill unverified">Unverified</span>') + '</div>' +
      (e.summary ? '<p class="small" style="margin:.3rem 0 0">' + HS.esc(e.summary) + '</p>' : "") + '</div>' +
      '<div class="actions">' + (e.website ? '<a class="btn ghost sm" target="_blank" rel="noopener" href="' + HS.esc(e.website) + '">Details</a>' : "") +
      '<span class="cal-wrap"><a class="btn ghost sm" target="_blank" rel="noopener" href="' + HS.esc(HS.googleCalURL(e)) + '" title="Opens Google Calendar">' + HS.ICON.cal + 'Add to calendar</a>' +
      '<button type="button" class="btn ghost sm ics" data-act="ics" title="Download an .ics file for Outlook, Apple Calendar or any other calendar app" aria-label="Download .ics calendar file">.ics</button></span>' +
      '<span class="share-wrap"><button type="button" class="btn ghost sm" data-act="share" aria-haspopup="true" aria-expanded="false">' + HS.ICON.share + 'Share</button>' +
      '<span class="share-menu" role="menu">' +
      '<a role="menuitem" href="' + HS.esc(sh.whatsapp) + '" target="_blank" rel="noopener">' + HS.ICON.wa + 'WhatsApp</a>' +
      '<a role="menuitem" href="' + HS.esc(sh.facebook) + '" target="_blank" rel="noopener">' + HS.ICON.fb + 'Facebook</a>' +
      '<a role="menuitem" href="' + HS.esc(sh.email) + '">' + HS.ICON.mail + 'Email</a>' +
      '<button type="button" role="menuitem" data-act="copy">' + HS.ICON.link + 'Copy link</button>' +
      '</span></span></div></div>';
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
  /* Member accounts live on the API host (server-rendered, no JS needed there).
     These are the only absolute links in the chrome; they are omitted on a copy
     of the site that has no API. */
  HS.accountURL = function (path) { return HS.api ? HS.api + "/account" + (path || "") : ""; };
  HS.wordmark = function () {
    var n = String(D.site.name || "Helderberg Social"), i = n.lastIndexOf(" ");
    return i > 0 ? HS.esc(n.slice(0, i)) + " <em>" + HS.esc(n.slice(i + 1)) + "</em>" : HS.esc(n);
  };
  /* Footer "Follow" column, built only from links set in data.js site.social. */
  HS.followBlock = function () {
    var s = D.site.social || {}, items = [];
    if (s.facebook && /^https:\/\/(www\.)?facebook\.com\//.test(s.facebook)) {
      items.push('<li><a href="' + HS.esc(s.facebook) + '" target="_blank" rel="noopener">' + '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M13.5 22v-8h2.7l.4-3.2h-3.1V8.8c0-.9.3-1.6 1.6-1.6h1.7V4.4c-.3 0-1.3-.1-2.5-.1-2.5 0-4.1 1.5-4.1 4.2v2.3H7.4V14h2.8v8h3.3z"/></svg>' + 'Facebook</a></li>');
    }
    items.push('<li><a href="subscribe.html">Email digest</a></li>');
    return '<div class="follow"><h4>Follow</h4><ul>' + items.join("") + '</ul><p class="small">Share the site: <a href="https://www.facebook.com/sharer/sharer.php?u=' + encodeURIComponent(HS.siteURL()) + '" target="_blank" rel="noopener">post it on Facebook</a></p></div>';
  };
  HS.siteURL = function () { return "https://" + (D.site.domain || location.host) + "/"; };
  HS.renderChrome = function () {
    var here = (location.pathname.split("/").pop() || "index.html").toLowerCase();
    var h = document.querySelector("[data-hs-header]");
    if (h) {
      h.className = "site-header";
      h.innerHTML = '<div class="wrap"><a class="logo" href="index.html" aria-label="' + HS.esc(D.site.name || "Helderberg Social") + ' home"><img class="logo-mark" src="assets/img/logo-mark.svg" alt="" width="36" height="36"><span class="wordmark">' + HS.wordmark() + '</span></a>' +
        '<button class="nav-toggle" aria-label="Menu" aria-expanded="false">☰</button><nav class="nav" aria-label="Main">' +
        NAV.map(function (n) { return '<a href="' + n[0] + '" class="' + (n[2] || "") + (here === n[0] ? " active" : "") + '">' + n[1] + '</a>'; }).join("") +
        (HS.api ? '<a href="' + HS.accountURL("/events/new") + '" class="cta alt">Post an event</a>' : "") + '</nav></div>';
      var btn = h.querySelector(".nav-toggle"), nav = h.querySelector(".nav");
      btn.addEventListener("click", function () { var o = nav.classList.toggle("open"); btn.setAttribute("aria-expanded", o); });
    }
    var f = document.querySelector("[data-hs-footer]");
    if (f) {
      f.className = "site-footer";
      f.innerHTML = '<div class="wrap"><div><div class="logo" style="margin-bottom:.5rem"><img class="logo-mark" src="assets/img/logo-mark.svg" alt="" width="30" height="30"><span class="wordmark">' + HS.wordmark() + '</span></div>' +
        '<p>' + HS.esc(D.site.tagline || "") + '</p><p class="small">Listings are community-submitted. Anything marked <em>Unverified</em> has not yet been checked by us: confirm times and prices with the organiser before you go.</p>' +
        '<p class="small">© ' + new Date().getFullYear() + ' ' + HS.esc(D.site.name || "") + ' · ' + HS.esc(D.site.region || "") +
        /* Admin console: a near-invisible dot after the copyright line, for the
           people who run the site. Not a secret (the console has its own sign-in
           and second factor); just kept out of visitors' way. */
        (HS.api ? ' <a class="console-link" href="' + HS.api + '/admin/login" rel="nofollow" aria-label="Admin console" title="Admin console">·</a>' : "") + '</p></div>' +
        '<div><h4>Explore</h4><ul><li><a href="directory.html">Groups &amp; activities</a></li><li><a href="events.html">Events</a></li><li><a href="places.html">Places</a></li><li><a href="towns.html">Towns</a></li></ul></div>' +
        '<div><h4>Get involved</h4><ul>' + (HS.api ? '<li><a href="' + HS.accountURL("/events/new") + '">Post an event</a></li>' : "") + '<li><a href="submit.html">Add a listing</a></li><li><a href="submit.html?kind=update">Report a change</a></li><li><a href="subscribe.html">Email or WhatsApp updates</a></li><li><a href="about.html">About this site</a></li><li><a href="privacy.html">Privacy</a></li>' + (HS.api ? '<li><a href="' + HS.accountURL() + '">Sign in / my account</a></li>' : "") + '</ul></div>' +
        HS.followBlock() + '</div>';
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
