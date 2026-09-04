/* Helderberg Social: places page. Loaded after data/data.js and assets/js/site.js. */
(function () {
  var D = HS.data, $ = function (id) { return document.getElementById(id); };
  var places = D.listings.filter(function (l) { return l.type === "place"; });
  var cats = {};
  places.forEach(function (p) { cats[p.category] = 1; });
  var state = { cat: HS.qs().get("cat") || "" };
  $("cats").innerHTML = '<button type="button" class="chip" data-cat="">All</button>' + D.categories.filter(function (c) { return cats[c.id]; }).map(function (c) { return '<button type="button" class="chip" data-cat="' + c.id + '">' + c.icon + ' ' + HS.esc(c.name) + '</button>'; }).join("");
  var map = HS.map($("map"), places), layer = null;
  function render() {
    var items = places.filter(function (p) { return !state.cat || p.category === state.cat; }).sort(function (a, b) { return a.name.localeCompare(b.name); });
    document.querySelectorAll("#cats .chip").forEach(function (b) { b.classList.toggle("on", b.getAttribute("data-cat") === state.cat); });
    $("results").innerHTML = items.map(HS.card).join("");
    if (map) {
      if (layer) map.removeLayer(layer);
      layer = L.layerGroup().addTo(map);
      items.forEach(function (l) { if (!HS.hasCoords(l)) return; L.marker(l.coords).addTo(layer).bindPopup('<b>' + HS.catIcon(l.category) + ' <a href="listing.html?id=' + encodeURIComponent(l.id) + '">' + HS.esc(l.name) + '</a></b><br>' + HS.esc(HS.townName(l.town))); });
    }
    HS.setQs(state);
  }
  $("cats").addEventListener("click", function (e) { var b = e.target.closest("[data-cat]"); if (b) { state.cat = b.getAttribute("data-cat"); render(); } });
  render();
})();
