/**
 * The landing page's install box has a copy button; this makes it copy. It
 * subscribes to document$ (fires on load and on instant navigation) the same
 * way nav-specs.js does, so the handler is attached whichever way the page
 * was reached.
 */
document$.subscribe(function () {
  document.querySelectorAll(".install-box").forEach(function (box) {
    var button = box.querySelector(".install-copy");
    var command = box.querySelector(".install-command");
    if (!button || !command || button.dataset.bound) {
      return;
    }

    button.dataset.bound = "1";
    button.addEventListener("click", function () {
      var flash = function (glyph) {
        button.textContent = glyph;
        button.classList.add("copied");
        setTimeout(function () {
          button.textContent = "📋";
          button.classList.remove("copied");
        }, 1500);
      };

      navigator.clipboard.writeText(command.textContent.trim()).then(
        function () { flash("✓"); },
        function () {
          // No clipboard access: leave the command selected for a manual copy.
          window.getSelection().selectAllChildren(command);
          flash("⌘C");
        }
      );
    });
  });
});
