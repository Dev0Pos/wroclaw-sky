package server

import (
	"net/http"
)

const webManifest = `{
  "name": "wroclaw-sky",
  "short_name": "wroclaw-sky",
  "description": "Live ADS-B map around the focus airport",
  "start_url": "/",
  "display": "standalone",
  "background_color": "#020617",
  "theme_color": "#0ea5e9",
  "icons": [
    {
      "src": "/static/icon.svg",
      "sizes": "any",
      "type": "image/svg+xml",
      "purpose": "any"
    }
  ]
}`

const serviceWorkerJS = `const CACHE = 'wroclaw-sky-v1';
const SHELL = ['/', '/static/htmx.min.js', '/static/leaflet.js', '/static/leaflet.css', '/manifest.webmanifest'];

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);
  if (event.request.method !== 'GET') return;
  if (url.pathname === '/api/aircraft') {
    event.respondWith((async () => {
      try {
        const res = await fetch(event.request);
        const cache = await caches.open(CACHE);
        cache.put(event.request, res.clone());
        return res;
      } catch (e) {
        const cached = await caches.match(event.request);
        if (cached) return cached;
        throw e;
      }
    })());
    return;
  }
  if (SHELL.includes(url.pathname) || url.pathname.startsWith('/static/')) {
    event.respondWith(
      caches.match(event.request).then((cached) => cached || fetch(event.request).then((res) => {
        const copy = res.clone();
        caches.open(CACHE).then((c) => c.put(event.request, copy));
        return res;
      }))
    );
  }
});
`

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	_, _ = w.Write([]byte(webManifest))
}

func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Service-Worker-Allowed", "/")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(serviceWorkerJS))
}
