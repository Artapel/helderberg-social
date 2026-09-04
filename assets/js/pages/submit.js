/* Helderberg Social: submit page. Posts to the API (double opt-in by email); falls back to a copy-out block. */
(function () {
  var D = HS.data, $ = function (id) { return document.getElementById(id); }, qs = HS.qs();
  var byName = D.listings.slice().sort(function (a, b) { return a.name.localeCompare(b.name); });
  var listingOpts = byName.map(function (l) { return '<option value="' + HS.esc(l.id) + '">' + HS.esc(l.name) + ' (' + HS.esc(HS.townName(l.town)) + ')</option>'; }).join("");
  $("category").innerHTML = D.categories.map(function (c) { return '<option value="' + c.id + '">' + c.icon + ' ' + HS.esc(c.name) + '</option>'; }).join("");
  $("town").innerHTML = D.towns.map(function (t) { return '<option value="' + t.id + '">' + HS.esc(t.name) + '</option>'; }).join("");
  $("existing").innerHTML += listingOpts;
  $("evlisting").innerHTML += listingOpts;
  $("aud").innerHTML = D.audiences.map(function (a) { return '<button type="button" class="chip" data-a="' + a.id + '">' + HS.esc(a.name) + '</button>'; }).join("");
  $("aud").addEventListener("click", function (e) { var b = e.target.closest(".chip"); if (b) b.classList.toggle("on"); });
  $("how").textContent = HS.api ? "You'll get a confirmation email first." : D.site.submitEmail ? "Opens your email app with the details filled in." : "Sending is not wired up on this copy of the site.";

  function isEvent() { return $("kind").value === "event"; }
  function syncKind() {
    var k = $("kind").value;
    $("f-existing").hidden = k !== "update";
    $("f-event").hidden = k !== "event";
    $("f-schedule").hidden = k === "event";
    $("f-aud").hidden = k === "event";
    $("name-label").textContent = k === "event" ? "Event title" : "Name";
    $("h").textContent = k === "update" ? "Report a change" : k === "event" ? "Add an event" : "Add a listing";
    if (HS.api) { $("account-new").href = HS.accountURL("/events/new"); $("account-in").href = HS.accountURL(); $("account-cta").hidden = k !== "event"; }
    if (k === "update" && $("existing").value) {
      var l = HS.listings[$("existing").value];
      if (l) { $("name").value = l.name; $("category").value = l.category; $("town").value = l.town; $("schedule").value = (l.schedule && l.schedule.text) || ""; $("summary").value = l.summary; $("cost").value = l.cost || "free"; $("website").value = l.website || ""; }
    }
  }
  if (qs.get("kind")) $("kind").value = qs.get("kind");
  if (qs.get("id")) { $("kind").value = "update"; $("existing").value = qs.get("id"); }
  if (qs.get("listing") && HS.listings[qs.get("listing")]) { $("kind").value = "event"; $("evlisting").value = qs.get("listing"); var src = HS.listings[qs.get("listing")]; $("town").value = src.town; $("category").value = src.category; }
  $("kind").addEventListener("change", syncKind); $("existing").addEventListener("change", syncKind); syncKind();

  function setErr(id, msg) { var f = $(id).closest(".field"); f.classList.toggle("error", !!msg); f.querySelector(".err").textContent = msg || ""; return !msg; }
  function audience() { return [].map.call(document.querySelectorAll("#aud .chip.on"), function (b) { return b.getAttribute("data-a"); }); }
  function payload() {
    var base = { town: $("town").value, category: $("category").value, summary: $("summary").value.trim(), cost: $("cost").value, website: $("website").value.trim(), email: $("email").value.trim(), company: $("company").value };
    if (isEvent()) {
      return Object.assign(base, { title: $("name").value.trim(), date: $("date").value, endDate: $("endDate").value, time: $("time").value, endTime: $("endTime").value, listing: $("evlisting").value, name: $("yourname").value.trim() });
    }
    return Object.assign(base, { kind: $("kind").value, existing: $("existing").value, name: $("name").value.trim(), schedule: $("schedule").value.trim(), audience: audience(), yourName: $("yourname").value.trim() });
  }
  function asText(p) {
    var lines = ["Helderberg Social submission", "Kind: " + (isEvent() ? "event" : p.kind) + (p.existing ? " (" + p.existing + ")" : ""), "Name: " + (p.title || p.name), "Category: " + p.category, "Town: " + p.town];
    if (isEvent()) lines.push("Date: " + p.date + (p.endDate ? " to " + p.endDate : "") + (p.time ? " " + p.time : "") + (p.endTime ? "-" + p.endTime : ""), p.listing ? "Listing: " + p.listing : "");
    else lines.push("When: " + p.schedule, "Audience: " + p.audience.join(", "));
    lines.push("Cost: " + p.cost, "Website: " + p.website, "", p.summary, "", "From: " + (p.yourName || p.name) + " <" + p.email + ">");
    return lines.filter(function (x) { return x !== ""; }).join("\n");
  }
  function show(msg, ok) { var d = $("done"); d.hidden = false; d.className = ok ? "notice ok" : "notice"; d.textContent = msg; d.scrollIntoView({ block: "nearest" }); }
  function offerFallback(text) {
    $("raw").value = text; $("fallback").hidden = false; $("fallback").open = true;
    $("fallback-to").textContent = D.site.submitEmail ? "Send it to " + D.site.submitEmail + "." : "";
  }

  $("f").addEventListener("submit", function (e) {
    e.preventDefault();
    var ok = true, ev = isEvent();
    ok = setErr("name", $("name").value.trim() ? "" : (ev ? "Give the event a title." : "Give it a name.")) && ok;
    ok = setErr("summary", $("summary").value.trim().length >= 20 ? "" : "A sentence or two, please (20+ characters).") && ok;
    ok = setErr("website", !$("website").value || /^https?:\/\/\S+\.\S+/.test($("website").value) ? "" : "Needs to start with http:// or https://") && ok;
    ok = setErr("yourname", $("yourname").value.trim() ? "" : "We need a name.") && ok;
    ok = setErr("email", /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test($("email").value) ? "" : "A valid email address.") && ok;
    ok = setErr("consent", $("consent").checked ? "" : "Please confirm.") && ok;
    if (ev) {
      var today = HS.today(), d = $("date").value ? HS.parseDate($("date").value) : null;
      ok = setErr("date", !d ? "Events need a date." : d < today ? "That date has passed." : "") && ok;
      ok = setErr("endDate", $("endDate").value && $("endDate").value < $("date").value ? "The last day can't be before the first." : "") && ok;
      ok = setErr("endTime", $("endTime").value && $("time").value && $("endTime").value < $("time").value ? "Ends before it starts." : "") && ok;
    }
    if (!ok) return;
    var p = payload(), text = asText(p), btn = $("send");
    if (!HS.api) {
      if (D.site.submitEmail) {
        location.href = "mailto:" + D.site.submitEmail + "?subject=" + encodeURIComponent("[Helderberg Social] " + (ev ? "event" : p.kind) + ": " + (p.title || p.name)) + "&body=" + encodeURIComponent(text);
        show("Your email app should have opened with the details filled in. If not, copy the text below.", true);
      } else show("Sending is not configured on this copy of the site. Copy the text below and email it to us.", false);
      offerFallback(text);
      return;
    }
    btn.disabled = true;
    HS.post(ev ? "/api/submit/event" : "/api/submit/listing", p).then(function (j) {
      btn.disabled = false;
      if (j.ok) {
        $("f").hidden = true;
        show("Thanks. We've emailed a confirmation link to " + p.email + ". Click it and your submission goes to us for a quick check before it appears on the site.", true);
      } else {
        show(j.error || "That didn't go through. Please check the form and try again.", false);
      }
    }).catch(function () {
      btn.disabled = false;
      show("We couldn't reach the server. Please try again in a minute, or copy the text below and email it to us.", false);
      offerFallback(text);
    });
  });
})();
