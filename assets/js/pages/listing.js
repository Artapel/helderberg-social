/* Helderberg Social: listing page. Loaded after data/data.js and assets/js/site.js. */
(function () {
  var D = HS.data, id = HS.qs().get("id"), l = HS.listings[id], page = document.getElementById("page");
  if (!l) {
    page.innerHTML = '<section><div class="wrap"><h1>Listing not found</h1><p class="muted">It may have been removed or the link is wrong.</p><a class="btn" href="directory.html">Back to the directory</a></div></section>';
    document.title = "Not found — Helderberg Social";
    return;
  }
  document.title = l.name + " — " + HS.townName(l.town) + " — Helderberg Social";
  var meta = document.querySelector('meta[name="description"]'); if (meta) meta.setAttribute("content", l.summary);
  var days = (l.schedule && l.schedule.days || []).map(function (d) { return HS.DAYS[d]; }).join(", ");
  var events = D.events.filter(function (e) { return e.listing === l.id; }).filter(function (e) { return HS.parseDate(e.endDate || e.date) >= HS.today(); });
  var related = D.listings.filter(function (x) { return x.id !== l.id && (x.category === l.category || x.town === l.town); })
    .sort(function (a, b) { return ((b.category === l.category) + (b.town === l.town)) - ((a.category === l.category) + (a.town === l.town)); }).slice(0, 3);
  var host = function (u) { try { return new URL(u).host.replace(/^www\./, ""); } catch (e) { return u; } };

  page.innerHTML =
    '<section class="detail-hero" style="' + HS.art(l.category) + ' color:#fff"><div class="wrap">' +
    '<div class="kicker" style="color:rgba(255,255,255,.85)"><a href="directory.html" style="color:#fff">Directory</a> › <a href="directory.html?cat=' + l.category + '" style="color:#fff">' + HS.esc(HS.catName(l.category)) + '</a> › ' + HS.esc(HS.typeLabel(l.type)) + '</div>' +
    '<h1 style="color:#fff">' + HS.catIcon(l.category) + ' ' + HS.esc(l.name) + '</h1>' +
    '<div class="row"><span class="pill" style="background:rgba(255,255,255,.18);color:#fff;border-color:transparent">📍 ' + HS.esc(HS.townName(l.town)) + '</span>' +
    (l.cost === "free" ? '<span class="pill free">Free</span>' : '<span class="pill" style="background:rgba(255,255,255,.18);color:#fff;border-color:transparent">' + HS.esc(HS.costLabel(l.cost)) + '</span>') +
    HS.statusPill(l) + (l.verified ? '<span class="pill verified">Verified</span>' : '<span class="pill unverified">Unverified</span>') + '</div></div></section>' +
    '<section><div class="wrap detail-grid"><div class="stack">' +
    (l.status && l.status.kind ? '<div class="notice"><b>' + (l.status.kind === "closed" ? "Closed." : "Paused.") + '</b> ' + HS.esc(l.status.text || "") +
      (l.status.link ? ' <a href="' + HS.esc(l.status.link) + '" target="_blank" rel="noopener">Organiser\'s notice ↗</a>' : "") +
      ' <a href="submit.html?kind=update&id=' + encodeURIComponent(l.id) + '">Has it restarted? Tell us.</a></div>' : "") +
    '<div class="panel"><h3>About</h3><p>' + HS.esc(l.summary) + '</p>' +
    (l.tags && l.tags.length ? '<div class="row">' + l.tags.map(function (t) { return '<a class="pill" href="directory.html?q=' + encodeURIComponent(t) + '">' + HS.esc(t) + '</a>'; }).join("") + '</div>' : "") + '</div>' +
    '<div class="panel" id="lst-upcoming"' + (events.length ? "" : " hidden") + '><h3>Upcoming</h3><div class="event-list">' + events.map(HS.eventRow).join("") + '</div></div>' +
    '<div class="panel"><h3>Nearby &amp; similar</h3><div class="grid">' + related.map(HS.card).join("") + '</div></div>' +
    (l.verified ? '' : '<div class="notice"><b>Not yet verified.</b> This listing was added from public sources and has not been confirmed with the organiser. Check times, prices and whether it is still running before you go. <a href="submit.html?kind=update&id=' + encodeURIComponent(l.id) + '">Know better? Tell us.</a></div>') +
    '</div><aside class="stack">' +
    '<div class="panel"><h3>Details</h3><dl class="kv">' +
    '<dt>Type</dt><dd>' + HS.esc(HS.typeLabel(l.type)) + '</dd>' +
    '<dt>Category</dt><dd><a href="directory.html?cat=' + l.category + '">' + HS.esc(HS.catName(l.category)) + '</a></dd>' +
    '<dt>Town</dt><dd><a href="towns.html#' + l.town + '">' + HS.esc(HS.townName(l.town)) + '</a></dd>' +
    (l.address ? '<dt>Where</dt><dd>' + HS.esc(l.address) + '</dd>' : "") +
    '<dt>When</dt><dd>' + HS.esc((l.schedule && l.schedule.text) || "Varies") + (days ? '<br><span class="small muted">' + HS.esc(days) + '</span>' : "") + '</dd>' +
    '<dt>Cost</dt><dd>' + HS.esc(HS.costLabel(l.cost) || "Unknown") + '</dd>' +
    '<dt>Who for</dt><dd>' + (l.audience || []).map(function (a) { return HS.esc((HS.audiences[a] || {}).name || a); }).join(", ") + '</dd>' +
    (l.website ? '<dt>Website</dt><dd><a href="' + HS.esc(l.website) + '" target="_blank" rel="noopener">' + HS.esc(host(l.website)) + ' ↗</a></dd>' : "") +
    (l.contact ? '<dt>Contact</dt><dd>' + HS.esc(l.contact) + '</dd>' : "") +
    (l.source && l.source !== l.website ? '<dt>Source</dt><dd><a href="' + HS.esc(l.source) + '" target="_blank" rel="noopener">' + HS.esc(host(l.source)) + ' ↗</a></dd>' : "") +
    '</dl><div class="row" style="margin-top:.9rem">' + (l.website ? '<a class="btn" target="_blank" rel="noopener" href="' + HS.esc(l.website) + '">Visit website</a>' : "") +
    '<a class="btn ghost" href="submit.html?kind=update&id=' + encodeURIComponent(l.id) + '">Suggest an edit</a></div></div>' +
    '<div class="panel"><h3>Where</h3><div id="map" class="map small"></div>' + (l.coordsApprox ? '<p class="small muted" style="margin:.5rem 0 0">Approximate position. Use the organiser\'s directions.</p>' : "") + '</div>' +
    '</aside></div></section>';

  if (l.coords) HS.map(document.getElementById("map"), [l], { center: l.coords, zoom: 13, fit: false });

  HS.onEvents(function (changed) {
    if (!changed) return;
    var evs = D.events.filter(function (e) { return e.listing === l.id && HS.parseDate(e.endDate || e.date) >= HS.today(); });
    var box = document.getElementById("lst-upcoming"); if (!box) return;
    box.hidden = !evs.length; box.querySelector(".event-list").innerHTML = evs.map(HS.eventRow).join("");
  });
})();
