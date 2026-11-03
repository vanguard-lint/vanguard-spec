/* vanguard-spec site behaviors (theme toggle + copy buttons).
 * The osv.dev JS bundle is replaced with this minimal script — the page
 * keeps the original markup and styling. */

(function () {
  "use strict";

  /* ---- theme (same storage key + default as osv.dev) ---- */

  var root = document.documentElement;

  document.getElementById("theme-toggle").addEventListener("click", function () {
    var next = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
    root.setAttribute("data-theme", next);
    try { localStorage.setItem("theme", next); } catch (e) {}
  });

  /* ---- copy buttons (replaces the clipboard-copy web component) ---- */

  document.querySelectorAll(".copy-btn").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var pre = document.getElementById(btn.getAttribute("for"));
      if (!pre) return;
      var icon = btn.querySelector(".material-icons");
      function done() {
        if (!icon) return;
        icon.textContent = "done";
        setTimeout(function () { icon.textContent = "content_copy"; }, 1500);
      }
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(pre.innerText).then(done);
      } else {
        var ta = document.createElement("textarea");
        ta.value = pre.innerText;
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        document.body.removeChild(ta);
        done();
      }
    });
  });
})();
