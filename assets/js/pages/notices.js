/* Helderberg Social: noticeboard. Loaded after data/data.js and assets/js/site.js. */
(function () {
  var D = HS.data, $ = function (id) { return document.getElementById(id); };
  var qs = HS.qs();
  var state = { town: qs.get("town") || "", cat: qs.get("cat") || "" }, all = [];
  $("town").innerHTML += D.towns.map(function (t) { return '<option value="' + t.id + '">' + HS.esc(t.name) + '</option>'; }).join("");
  $("cat").innerHTML += D.categories.map(function (c) { return '<option value="' + c.id + '">' + c.icon + ' ' + HS.esc(c.name) + '</option>'; }).join("");
  function render() {
    $("town").value = state.town; $("cat").value = state.cat;
    var posts = all.filter(function (p) { return (!state.town || p.town === state.town) && (!state.cat || p.category === state.cat); });
    $("posts").innerHTML = posts.length ? posts.map(HS.postCard).join("") :
      '<div class="empty">' + (all.length ? "No notices match those filters." : (HS.api ? "Nothing on the board right now." : "The noticeboard is offline at the moment.")) + ' Run something? <a href="promote.html">Promote with us</a>.</div>';
    HS.setQs(state);
    var want = qs.get("post");
    if (want) { var el = document.getElementById("post-" + want); if (el) { el.scrollIntoView({ block: "center" }); el.classList.add("hit"); } }
  }
  ["town", "cat"].forEach(function (id) { $(id).addEventListener("change", function () { state[id] = this.value; render(); }); });
  $("pfilters").addEventListener("submit", function (e) { e.preventDefault(); });
  HS.onPosts(function (posts) { all = posts; render(); });
})();
