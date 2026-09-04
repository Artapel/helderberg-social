/* Helderberg Social: privacy page. Fills in the contact address when one is configured. */
(function () {
  var c = HS.data.site.contactEmail;
  if (c) document.getElementById("contact").innerHTML = '<a href="mailto:' + HS.esc(c) + '">' + HS.esc(c) + '</a>';
})();
