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

  // WhatsApp is offered only when the API says it is switched on.
  function channel() { return $("f").channel.value; }
  function applyChannel() {
    var wa = channel() === "whatsapp";
    $("email-field").hidden = wa; $("phone-field").hidden = !wa;
    $("send").textContent = wa ? "Send me the WhatsApp confirmation" : "Send me the confirmation link";
    $("send-note").textContent = wa ? "You'll get one WhatsApp message with a Confirm button. Nothing else until you tap it." : "You'll get one email to confirm. Nothing else until you click it.";
  }
  [].forEach.call(document.querySelectorAll('input[name="channel"]'), function (r) { r.addEventListener("change", applyChannel); });
  if (HS.api && window.fetch) {
    fetch(HS.api + "/api/health", { credentials: "omit", mode: "cors" }).then(function (r) { return r.json(); }).then(function (j) {
      if (j && j.whatsapp) {
        $("channel-field").hidden = false;
        if (qs.get("channel") === "whatsapp") { $("f").channel.value = "whatsapp"; applyChannel(); }
      }
    }).catch(function () {});
  }

  function vals(name) { return [].map.call(document.querySelectorAll('input[name="' + name + '"]:checked'), function (i) { return i.value; }); }
  function setErr(id, msg) { var f = $(id).closest(".field"); f.classList.toggle("error", !!msg); f.querySelector(".err").textContent = msg || ""; return !msg; }
  function show(msg, ok) { var d = $("done"); d.hidden = false; d.className = ok ? "notice ok" : "notice"; d.textContent = msg; d.scrollIntoView({ block: "nearest" }); }

  $("f").addEventListener("submit", function (e) {
    e.preventDefault();
    var wa = channel() === "whatsapp", email = $("email").value.trim(), phone = $("phone").value.trim();
    if (!wa && !setErr("email", /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email) ? "" : "A valid email address, please.")) return;
    if (wa && !setErr("phone", /^\+?[0-9 ()./-]{8,20}$/.test(phone) ? "" : "The number your WhatsApp is on, please.")) return;
    if (!HS.api) { show("Updates are not available on this copy of the site.", false); return; }
    var body = { channel: wa ? "whatsapp" : "email", email: wa ? "" : email, phone: wa ? phone : "", frequency: $("f").frequency.value, horizon: parseInt($("f").horizon.value, 10), towns: vals("towns"), categories: vals("categories"), website: $("website").value };
    var btn = $("send"); btn.disabled = true;
    HS.post("/api/subscribe", body).then(function (j) {
      btn.disabled = false;
      if (j.ok && wa) { $("f").hidden = true; show("Nearly there. We've sent a WhatsApp message to " + phone + " with a Confirm button. Nothing arrives until you tap it.", true); }
      else if (j.ok) { $("f").hidden = true; show("Nearly there. We've sent a confirmation link to " + email + ". Nothing arrives until you click it. If it's not there in a few minutes, check your spam folder.", true); }
      else show(j.error || "That didn't work. Please try again.", false);
    }).catch(function () { btn.disabled = false; show("We couldn't reach the server. Please try again in a minute.", false); });
  });
})();
