// Click-to-load YouTube trailer facade. Kept tiny and dependency-free; the
// CSP forbids inline scripts, so this file is the only JS needed.
(function () {
	"use strict";

	document.addEventListener("click", function (e) {
		var trigger = e.target.closest("[data-youtube-play]");
		if (!trigger) return;
		var box = trigger.closest("[data-youtube-id]");
		if (!box) return;
		var id = box.getAttribute("data-youtube-id") || "";
		// YouTube IDs are [A-Za-z0-9_-]; validate before it reaches a URL.
		if (!/^[A-Za-z0-9_-]{6,20}$/.test(id)) return;

		var iframe = document.createElement("iframe");
		iframe.src = "https://www.youtube-nocookie.com/embed/" + id + "?autoplay=1&rel=0";
		iframe.title = box.getAttribute("data-youtube-title") || "Trailer";
		iframe.allow = "accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share";
		iframe.allowFullscreen = true;
		iframe.setAttribute("frameborder", "0");
		iframe.className = "h-full w-full";

		box.textContent = "";
		box.appendChild(iframe);
	});
})();
