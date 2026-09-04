/* Helderberg Social: towns page. Loaded after data/data.js and assets/js/site.js. */
(function () {
  var D = HS.data;
  document.getElementById("towns").innerHTML = D.towns.map(function (t) {
    var items = D.listings.filter(function (l) { return l.town === t.id; });
    var groups = items.filter(function (l) { return l.type === "group"; }).length, places = items.filter(function (l) { return l.type === "place"; }).length;
    return '<article class="town" id="' + t.id + '"><div class="town-art" aria-hidden="true"></div><div>' +
      '<h2>' + HS.esc(t.name) + '</h2><p>' + HS.esc(t.blurb) + '</p>' +
      '<ul>' + t.highlights.map(function (h) { return '<li>' + HS.esc(h) + '</li>'; }).join("") + '</ul>' +
      '<div class="row"><a class="btn" href="directory.html?town=' + t.id + '">' + items.length + ' listings</a>' +
      '<a class="btn ghost" href="directory.html?town=' + t.id + '&type=group">' + groups + ' groups</a>' +
      '<a class="btn ghost" href="directory.html?town=' + t.id + '&type=place">' + places + ' places</a>' +
      '<a class="btn ghost" href="events.html?town=' + t.id + '">Events</a></div></div></article>';
  }).join("");
})();
