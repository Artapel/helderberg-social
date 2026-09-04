/* Helderberg Social: directory page. Loaded after data/data.js and assets/js/site.js. */
(function () {
  var D = HS.data, $ = function (id) { return document.getElementById(id); };
  var state = { q: "", town: "", aud: "", cost: "", cat: "", type: "", sort: "name", view: "list" };
  var qs = HS.qs();
  Object.keys(state).forEach(function (k) { if (qs.has(k)) state[k] = qs.get(k); });

  $("town").innerHTML += D.towns.map(function (t) { return '<option value="' + t.id + '">' + HS.esc(t.name) + '</option>'; }).join("");
  $("aud").innerHTML += D.audiences.map(function (a) { return '<option value="' + a.id + '">' + HS.esc(a.name) + '</option>'; }).join("");
  $("cats").innerHTML = '<button type="button" class="chip" data-cat="">All categories</button>' + D.categories.map(function (c) { return '<button type="button" class="chip" data-cat="' + c.id + '">' + c.icon + ' ' + HS.esc(c.name) + '</button>'; }).join("");
  $("types").innerHTML = [["", "Everything"], ["group", "Groups & clubs"], ["activity", "Regular activities"], ["place", "Places"]].map(function (t) { return '<button type="button" class="chip" data-type="' + t[0] + '">' + t[1] + '</button>'; }).join("");

  var map = null, layer = null;

  function filtered() {
    var items = D.listings.filter(function (l) {
      if (state.town && l.town !== state.town) return false;
      if (state.cat && l.category !== state.cat) return false;
      if (state.type && l.type !== state.type) return false;
      if (state.cost && l.cost !== state.cost) return false;
      if (state.aud && (l.audience || []).indexOf(state.aud) === -1) return false;
      return true;
    });
    items = state.q ? HS.search(state.q, items) : items.slice();
    if (!state.q || state.sort !== "name") {
      items.sort(function (a, b) {
        if (state.sort === "town") return HS.townName(a.town).localeCompare(HS.townName(b.town)) || a.name.localeCompare(b.name);
        if (state.sort === "category") return HS.catName(a.category).localeCompare(HS.catName(b.category)) || a.name.localeCompare(b.name);
        return a.name.localeCompare(b.name);
      });
    }
    return items;
  }

  function render() {
    $("q").value = state.q; $("town").value = state.town; $("aud").value = state.aud; $("cost").value = state.cost; $("sort").value = state.sort;
    document.querySelectorAll("#cats .chip").forEach(function (b) { b.classList.toggle("on", b.getAttribute("data-cat") === state.cat); });
    document.querySelectorAll("#types .chip").forEach(function (b) { b.classList.toggle("on", b.getAttribute("data-type") === state.type); });
    var items = filtered();
    var title = state.cat ? HS.catName(state.cat) : state.type ? { group: "Groups & clubs", activity: "Regular activities", place: "Places" }[state.type] : "Directory";
    if (state.town) title += " in " + HS.townName(state.town);
    $("title").textContent = title;
    document.title = title + " — Helderberg Social";
    $("count").textContent = items.length + " listing" + (items.length === 1 ? "" : "s") + (state.q ? ' for “' + state.q + '”' : "");
    $("results").innerHTML = items.length ? items.map(HS.card).join("") : '<div class="empty" style="grid-column:1/-1">Nothing matches those filters yet.<br><a href="submit.html">Know something that belongs here? Add it.</a></div>';
    $("results").hidden = state.view === "map"; $("map").hidden = state.view !== "map";
    $("v-list").classList.toggle("on", state.view !== "map"); $("v-map").classList.toggle("on", state.view === "map");
    if (state.view === "map" && window.L) {
      if (!map) { map = HS.map($("map"), [], { fit: false }); }
      if (layer) map.removeLayer(layer);
      layer = L.layerGroup().addTo(map);
      var pts = [];
      items.forEach(function (l) {
        if (!l.coords) return;
        L.marker(l.coords).addTo(layer).bindPopup('<b>' + HS.catIcon(l.category) + ' <a href="listing.html?id=' + encodeURIComponent(l.id) + '">' + HS.esc(l.name) + '</a></b><br>' + HS.esc(HS.townName(l.town)) + (l.coordsApprox ? ' <span style="opacity:.7">(approx.)</span>' : ""));
        pts.push(l.coords);
      });
      setTimeout(function () { map.invalidateSize(); if (pts.length > 1) map.fitBounds(pts, { padding: [24, 24] }); else if (pts.length === 1) map.setView(pts[0], 14); }, 30);
    }
    HS.setQs(state);
  }

  $("q").addEventListener("input", function () { state.q = this.value.trim(); render(); });
  ["town", "aud", "cost", "sort"].forEach(function (id) { $(id).addEventListener("change", function () { state[id] = this.value; render(); }); });
  $("cats").addEventListener("click", function (e) { var b = e.target.closest("[data-cat]"); if (b) { state.cat = b.getAttribute("data-cat"); render(); } });
  $("types").addEventListener("click", function (e) { var b = e.target.closest("[data-type]"); if (b) { state.type = b.getAttribute("data-type"); render(); } });
  $("v-list").addEventListener("click", function () { state.view = "list"; render(); });
  $("v-map").addEventListener("click", function () { state.view = "map"; render(); });
  $("clear").addEventListener("click", function () { state = { q: "", town: "", aud: "", cost: "", cat: "", type: "", sort: "name", view: state.view }; render(); });
  render();
  /* evfilters-guard */ document.getElementById("filters").addEventListener("submit", function (e) { e.preventDefault(); });
})();
