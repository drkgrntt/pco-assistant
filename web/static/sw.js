// Minimal service worker, registered only so the app satisfies PWA
// installability criteria (Add to Home Screen / install prompt). It
// deliberately does no caching: this app is live PCO and database data, so
// there's nothing meaningful to serve while offline yet - every request
// just passes straight through to the network.
self.addEventListener("fetch", function (event) {
  event.respondWith(fetch(event.request));
});
