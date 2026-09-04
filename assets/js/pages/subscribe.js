/* Helderberg Social: subscribe page. Double opt-in via the API. */
(function () {
  var D = HS.data, $ = function (id) { return document.getElementById(id); }, qs = HS.qs();
  function boxes(el, items, name, preset) {
    el.innerHTML = items.map(function (it) {
      return '<label><input type="checkbox" name="' + name + '" value="' + HS.esc(it.id) + '"' + (preset.indexOf(it.id) >= 0 ? " checked" : "") + '>' + (it.icon ? it.icon + " " : "") + HS.esc(it.name) + '</label>';
    }).join("");
  }
  boxes($("towns"), D.towns, "towns", (qs.get("town") || "").split(","));
  boxes($("cats"), D.categories, "categories", (qs.get("category") || "").split(","));
  if (qs.get("frequency") === "daily") $("f").frequency.value = "daily";
  if (["7", "14", "30"].indexOf(qs.get("horizon")) >= 0) $("f").horizon.value = qs.get("horizon");
  try { var pre = sessionStorage.getItem("hs-sub-email"); if (pre) { $("email").value = pre; sessionStorage.removeItem("hs-sub-email"); } } catch (e) {}

  function vals(name) { return [].map.call(document.querySelectorAll('input[name="' + name + '"]:checked'), function (i) { return i.value; }); }
  function setErr(id, msg) { var f = $(id).closest(".field"); f.classList.toggle("error", !!msg); f.querySelector(".err").textContent = msg || ""; return !msg; }
  function show(msg, ok) { var d = $("done"); d.hidden = false; d.className = ok ? "notice ok" : "notice"; d.textContent = msg; d.scrollIntoView({ block: "nearest" }); }

  $("f").addEventListener("submit", function (e) {
    e.preventDefault();
    var email = $("email").value.trim();
    if (!setErr("email", /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email) ? "" : "A valid email address, please.")) return;
    if (!HS.api) { show("Email updates are not available on this copy of the site.", false); return; }
    var body = { email: email, frequency: $("f").frequency.value, horizon: parseInt($("f").horizon.value, 10), towns: vals("towns"), categories: vals("categories"), website: $("website").value };
    var btn = $("send"); btn.disabled = true;
    HS.post("/api/subscribe", body).then(function (j) {
      btn.disabled = false;
      if (j.ok) { $("f").hidden = true; show("Nearly there. We've sent a confirmation link to " + email + ". Nothing arrives until you click it. If it's not there in a few minutes, check your spam folder.", true); }
      else show(j.error || "That didn't work. Please try again.", false);
    }).catch(function () { btn.disabled = false; show("We couldn't reach the server. Please try again in a minute.", false); });
  });
})();
