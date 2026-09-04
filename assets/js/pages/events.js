/* Helderberg Social: events page. Loaded after data/data.js and assets/js/site.js. */
(function () {
  var D = HS.data, $ = function (id) { return document.getElementById(id); };
  var qs = HS.qs();
  var state = { town: qs.get("town") || "", cat: qs.get("cat") || "", when: qs.get("when") || "" };
  $("town").innerHTML += D.towns.map(function (t) { return '<option value="' + t.id + '">' + HS.esc(t.name) + '</option>'; }).join("");
  $("cat").innerHTML += D.categories.map(function (c) { return '<option value="' + c.id + '">' + c.icon + ' ' + HS.esc(c.name) + '</option>'; }).join("");

  function render() {
    $("town").value = state.town; $("cat").value = state.cat; $("when").value = state.when;
    var evs = HS.upcomingEvents(state.when ? Number(state.when) : undefined).filter(function (e) {
      return (!state.town || e.town === state.town) && (!state.cat || e.category === state.cat);
    });
    if (!evs.length) {
      $("dated").innerHTML = '<div class="empty">No dated events match. <a href="' + (HS.accountURL("/events/new") || "submit.html?kind=event") + '">Add one.</a></div>';
    } else {
      var out = "", month = "";
      evs.forEach(function (e) {
        var d = HS.parseDate(e.date), m = HS.MONTHS[d.getMonth()] + " " + d.getFullYear();
        if (m !== month) { if (month) out += "</div>"; month = m; out += '<h3 class="month-head">' + m + '</h3><div class="event-list">'; }
        out += HS.eventRow(e);
      });
      $("dated").innerHTML = out + "</div>";
    }

    var weekly = D.listings.filter(function (l) {
      return l.schedule && l.schedule.days && l.schedule.days.length && (!state.town || l.town === state.town) && (!state.cat || l.category === state.cat);
    });
    var byDay = [1, 2, 3, 4, 5, 6, 0].map(function (d) { return { d: d, items: weekly.filter(function (l) { return l.schedule.days.indexOf(d) !== -1; }) }; }).filter(function (x) { return x.items.length; });
    $("weekly").innerHTML = byDay.length ? byDay.map(function (x) {
      return '<h3 class="month-head">' + HS.DAYS[x.d] + '</h3><div class="grid" style="margin-bottom:1rem">' + x.items.map(HS.card).join("") + '</div>';
    }).join("") : '<p class="muted">No weekly fixtures match those filters.</p>';
    HS.setQs(state);
    HS.focusEvent();
  }
  if (HS.api) { $("post-link").href = HS.accountURL("/events/new"); $("post-cta").hidden = false; }
  ["town", "cat", "when"].forEach(function (id) { $(id).addEventListener("change", function () { state[id] = this.value; render(); }); });
  document.getElementById("evfilters").addEventListener("submit", function (e) { e.preventDefault(); });
  HS.onEvents(render);
})();
