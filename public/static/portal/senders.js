// senders.js narrows the register-a-sender form to the channel that was
// picked: one line of guidance under Secret and one under Settings instead of
// eight, and that channel's own example as the Settings placeholder.
//
// The page is written so that this file is an improvement and never a
// requirement. Every line is rendered by the server and visible; all this does
// is hide the seven that do not apply. Guidance a customer cannot read because
// a script did not load would be worse than a longer page.
//
// The first script on this surface. It reads the data attributes the template
// already writes and holds no copy of them -- what each channel wants is in
// internal/adapter/api/web/portal_channels.go and nowhere else.
(function () {
  "use strict";

  var pick = document.getElementById("channel");
  var settings = document.getElementById("config");
  if (!pick || !settings) {
    return;
  }

  var lines = document.querySelectorAll("[data-channel]");

  function narrow() {
    var chosen = pick.value;
    var example = "";

    Array.prototype.forEach.call(lines, function (line) {
      var mine = line.getAttribute("data-channel") === chosen;

      // Nothing picked yet is the same as no script: show all of them.
      line.hidden = chosen !== "" && !mine;

      if (mine && line.hasAttribute("data-settings")) {
        example = line.getAttribute("data-settings");
      }
    });

    // Empty for the channels that read no settings, which is the honest
    // placeholder: there is nothing to put there.
    settings.placeholder = example;
  }

  pick.addEventListener("change", narrow);

  // Run once at load, because a browser restoring a form after a back button
  // brings the old choice with it and fires no event.
  narrow();
})();
