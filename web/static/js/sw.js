// Minimal service worker, registered only so the app satisfies PWA
// installability criteria (Add to Home Screen / install prompt) - that just
// needs a registered fetch listener to exist, not for it to actually do
// anything. This deliberately does no caching (this app is live PCO and
// database data, so there's nothing meaningful to serve while offline yet)
// and deliberately doesn't call event.respondWith(): every request should
// just pass straight through to the network exactly as if this worker
// weren't here at all.
//
// It used to call event.respondWith(fetch(event.request)) to say that
// explicitly - but manually re-fetching and relaying the response is its
// own source of bugs for no benefit when there's no caching to justify it:
// it doubled every request in DevTools' Network panel (the original
// request plus the worker's own re-fetch of it) and, worse, could hand the
// page an empty response even though the real network call the worker
// made succeeded - see the empty-pagination-response bug this replaced.
// Not calling respondWith() is the actually-correct way to "pass straight
// through": the browser handles the fetch normally, with nothing in the
// middle to get wrong.
self.addEventListener("fetch", function () {});

// Without these, a new worker sits "waiting" until every open tab of the
// app is closed, so an updated sw.js (like this fix) wouldn't actually
// take over an already-open tab on a plain reload - only a full close and
// reopen. skipWaiting activates a new worker as soon as it finishes
// installing; clients.claim() then hands it control of already-open tabs
// immediately too, instead of only new navigations.
self.addEventListener("install", function (event) {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener("activate", function (event) {
  event.waitUntil(self.clients.claim());
});
