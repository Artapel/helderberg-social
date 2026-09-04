/* Helderberg Social: moderator sign-in (magic link by email, no password). */
(function () {
  var $ = function (id) { return document.getElementById(id); };
  function show(msg, ok) { var d = $("done"); d.hidden = false; d.className = ok ? "notice ok" : "notice"; d.textContent = msg; }
  $("f").addEventListener("submit", function (e) {
    e.preventDefault();
    var email = $("email").value.trim(), f = $("email").closest(".field");
    var bad = !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email);
    f.classList.toggle("error", bad); f.querySelector(".err").textContent = bad ? "A valid email address, please." : "";
    if (bad) return;
    if (!HS.api) { show("Moderation is not available on this copy of the site.", false); return; }
    var btn = $("send"); btn.disabled = true;
    HS.post("/api/admin/login", { email: email }).then(function (j) {
      btn.disabled = false;
      if (j.ok) { $("f").hidden = true; show("If that is the moderator address, a sign-in link is on its way.", true); }
      else show(j.error || "That didn't work. Please try again.", false);
    }).catch(function () { btn.disabled = false; show("We couldn't reach the server. Please try again in a minute.", false); });
  });
})();
