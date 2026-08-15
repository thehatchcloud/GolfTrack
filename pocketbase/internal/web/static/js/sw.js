// GolfTrack service worker — caches static assets for offline play.
//
// Scope is '/' (granted by the Service-Worker-Allowed response header the
// /sw.js route sets), so it covers the entire app even though the file lives
// under /static/.
//
// Caching strategies:
//   /static/  — cache-first; assets are fingerprinted so stale is fine.
//   navigate  — network-first, fall back to the cached version of the page.
//   /api/     — always network; the page JS queues mutations when offline.

var CACHE = 'golftrack-v2';

// Assets pre-loaded on install so the round-play page renders immediately
// after the golfer's first online visit, even when signal disappears later.
var PRECACHE = [
  '/static/css/app.css',
  '/static/js/golftrack.js',
  '/static/js/alpine.min.js',
  '/static/js/round-play.js',
  '/static/manifest.webmanifest',
  '/static/icons/icon-192.png',
  '/static/icons/icon-512.png',
  '/static/icons/icon-512-maskable.png',
];

self.addEventListener('install', function (e) {
  e.waitUntil(
    caches.open(CACHE).then(function (cache) {
      return cache.addAll(PRECACHE);
    })
  );
  // Activate immediately rather than waiting for old tabs to close.
  self.skipWaiting();
});

self.addEventListener('activate', function (e) {
  // Delete caches from previous versions of the service worker.
  e.waitUntil(
    caches.keys().then(function (keys) {
      return Promise.all(
        keys.filter(function (k) { return k !== CACHE; }).map(function (k) {
          return caches.delete(k);
        })
      );
    })
  );
  self.clients.claim();
});

// Sign-out purges the cache: it holds authenticated, personalized HTML
// (including the round JSON embedded in the play page), and leaving it in
// place would let a subsequent signed-out or different user on this device
// open a previous golfer's cached round while offline.
self.addEventListener('message', function (e) {
  if (e.data && e.data.type === 'clear-cache') {
    e.waitUntil(caches.delete(CACHE));
  }
});

self.addEventListener('fetch', function (e) {
  var req = e.request;
  var url = new URL(req.url);

  // Only intercept same-origin requests.
  if (url.origin !== self.location.origin) return;

  // API calls always go to the network; round-play.js handles failures.
  if (url.pathname.startsWith('/api/')) return;

  // Static assets: serve from cache, populate cache on first fetch.
  if (url.pathname.startsWith('/static/') || url.pathname === '/sw.js') {
    e.respondWith(
      caches.match(req).then(function (cached) {
        if (cached) return cached;
        return fetch(req).then(function (res) {
          var clone = res.clone();
          caches.open(CACHE).then(function (c) { c.put(req, clone); });
          return res;
        });
      })
    );
    return;
  }

  // HTML pages: try network, fall back to cached version so the play page
  // renders even without a connection.
  if (req.mode === 'navigate') {
    e.respondWith(
      fetch(req)
        .then(function (res) {
          if (res.ok) {
            var clone = res.clone();
            caches.open(CACHE).then(function (c) { c.put(req, clone); });
          }
          return res;
        })
        .catch(function () {
          return caches.match(req);
        })
    );
  }
});
