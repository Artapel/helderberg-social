/* Helderberg Social: landing page after an emailed link (subscription confirm, unsubscribe, submission verify). */
(function () {
  var m = HS.qs().get("m") || "";
  var copy = {
    subscribed: ["You're subscribed", "Your address is confirmed. Your first update arrives on the next scheduled morning. Every email carries a one-click unsubscribe link.", [["events.html", "See what's on now"], ["subscribe.html", "Change your choices"]]],
    unsubscribed: ["You're unsubscribed", "Your address has been removed from our records. You won't hear from us again unless you subscribe afresh.", [["index.html", "Back to the site"]]],
    verified: ["Submission received", "Thanks, your email address is confirmed and your submission is with us. We check each one by hand before it appears, usually within a few days.", [["directory.html", "Browse the directory"], ["submit.html", "Add another"]]],
    invalid: ["That link didn't work", "It may have expired, or it was already used. Links in our emails are single-use. Subscribe or submit again and we'll send a fresh one.", [["subscribe.html", "Subscribe"], ["submit.html", "Add a listing"]]]
  };
  var c = copy[m] || ["Thanks", "You can go back to the site from here.", [["index.html", "Home"]]];
  document.title = c[0] + " — " + (HS.data.site.name || "Helderberg Social");
  document.getElementById("h").textContent = c[0];
  document.getElementById("p").textContent = c[1];
  document.getElementById("links").innerHTML = c[2].map(function (l, i) { return '<a class="btn' + (i ? " ghost" : "") + '" href="' + l[0] + '">' + HS.esc(l[1]) + '</a>'; }).join(" ");
})();
