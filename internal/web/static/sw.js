// HomeView service worker. It exists so the dashboard can be installed as an app
// and opens quickly; it never stores pages or API answers, because they are
// private to the signed-in user. Only the bundled CSS, scripts and icons are
// kept, and a small notice is shown when the server cannot be reached.
'use strict';

var CACHE = 'hv-static-1';

self.addEventListener('install', function () { self.skipWaiting(); });

self.addEventListener('activate', function (e) {
  e.waitUntil(
    caches.keys()
      .then(function (keys) {
        return Promise.all(keys.filter(function (k) { return k.indexOf('hv-') === 0 && k !== CACHE; })
          .map(function (k) { return caches.delete(k); }));
      })
      .then(function () { return self.clients.claim(); })
  );
});

// Bundled assets only. Wallpapers are large and are left to the HTTP cache.
function cacheable(url) {
  return url.origin === self.location.origin &&
    /^\/static\/(?!backgrounds\/).+\.(css|js|svg|png)$/.test(url.pathname);
}

function offlinePage() {
  var pt = (self.navigator.language || '').toLowerCase().indexOf('pt') === 0;
  var t = pt ? ['Sem conexão', 'Não foi possível falar com o HomeView. Verifique a rede e tente de novo.', 'Tentar de novo']
             : ['You are offline', 'HomeView could not be reached. Check the network and try again.', 'Try again'];
  var html = '<!doctype html><html lang="' + (pt ? 'pt-BR' : 'en') + '"><head><meta charset="utf-8">' +
    '<meta name="viewport" content="width=device-width, initial-scale=1"><title>' + t[0] + ' · HomeView</title>' +
    '<style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0f1218;color:#e8ebf1;' +
    'font:16px/1.5 system-ui,sans-serif;text-align:center;padding:1.5rem}h1{font-size:1.4rem;margin:0 0 .5rem}' +
    'p{color:#9aa3b2;margin:0 0 1.2rem}a{display:inline-block;padding:.6rem 1.2rem;border-radius:10px;' +
    'background:#4f8bff;color:#0b1020;font-weight:600;text-decoration:none}</style></head><body><main>' +
    '<h1>' + t[0] + '</h1><p>' + t[1] + '</p><a href="/">' + t[2] + '</a></main></body></html>';
  return new Response(html, { status: 503, headers: { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' } });
}

self.addEventListener('fetch', function (e) {
  var req = e.request;
  if (req.method !== 'GET') return;
  var url = new URL(req.url);

  if (req.mode === 'navigate') {
    e.respondWith(fetch(req).catch(offlinePage));
    return;
  }
  if (!cacheable(url)) return; // everything else goes straight to the network

  // Stale-while-revalidate: answer from the cache and refresh it in the background.
  e.respondWith(caches.open(CACHE).then(function (cache) {
    return cache.match(req).then(function (hit) {
      var fresh = fetch(req).then(function (res) {
        if (res.ok) {
          cache.put(req, res.clone());
          // Drop older versions of the same file (app.css?v=old).
          cache.keys().then(function (keys) {
            keys.forEach(function (k) {
              var ku = new URL(k.url);
              if (ku.pathname === url.pathname && ku.search !== url.search) cache.delete(k);
            });
          });
        }
        return res;
      });
      return hit || fresh;
    });
  }));
});
