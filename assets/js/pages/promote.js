/* Helderberg Social: promote page. Points the buttons at the account area when the API is on. */
(function () {
  if (!HS.api) return;
  var u = HS.accountURL("/promoter");
  document.getElementById("promote-link").href = HS.accountURL("/register?next=" + encodeURIComponent("/account/promoter"));
  document.getElementById("promote-cta").href = u;
})();
