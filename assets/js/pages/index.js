/* Helderberg Social: index page. Loaded after data/data.js and assets/js/site.js. */
(function () {
  var D = HS.data;
  var counts = {};
  D.listings.forEach(function (l) { counts[l.category] = (counts[l.category] || 0) + 1; });
  document.getElementById("tiles").innerHTML = D.categories.map(function (c) {
    return '<a class="tile" href="directory.html?cat=' + c.id + '"><span class="ic">' + c.icon + '</span><b>' + HS.esc(c.name) + '</b><span>' + (counts[c.id] || 0) + ' listing' + ((counts[c.id] || 0) === 1 ? "" : "s") + '</span></a>';
  }).join("");

  HS.onEvents(function () {
    var soon = HS.upcomingEvents(7);
    var evEl = document.getElementById("weekend-events");
    evEl.innerHTML = soon.length ? soon.map(HS.eventRow).join("") : '<p class="muted small">No dated events in the next seven days. Regular weekend fixtures are below.</p>';
  });
  document.getElementById("weekend-regular").innerHTML = HS.weekendListings().slice(0, 6).map(HS.card).join("");

  document.getElementById("featured-groups").innerHTML = D.listings.filter(function (l) { return l.type === "group" && l.category !== "online"; }).slice(0, 6).map(HS.card).join("");

  var tc = {};
  D.listings.forEach(function (l) { tc[l.town] = (tc[l.town] || 0) + 1; });
  document.getElementById("town-tiles").innerHTML = D.towns.map(function (t) {
    return '<a class="tile" href="directory.html?town=' + t.id + '"><span class="ic">🏘️</span><b>' + HS.esc(t.name) + '</b><span>' + (tc[t.id] || 0) + ' listings</span></a>';
  }).join("");
})();
