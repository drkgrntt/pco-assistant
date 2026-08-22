// Registers the service worker needed for PWA installability (see sw.js).
// Served from "/sw.js" at the app's root, not "/static/sw.js" - a service
// worker's default max scope is the directory its script lives in, so it
// has to be root-served to be allowed to control the whole app instead of
// just /static/*.
if ("serviceWorker" in navigator) {
  window.addEventListener("load", function () {
    navigator.serviceWorker.register("/sw.js").catch(function (err) {
      console.error("Service worker registration failed:", err);
    });
  });
}
