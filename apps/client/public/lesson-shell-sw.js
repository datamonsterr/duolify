/* global self, caches, URL, fetch, Response */
// Public shell only. Private APIs, documents and asset payloads stay in the host's
// account-scoped cache and can never be served by a service-worker URL.
const shell = "duolify-public-shell-v1";
self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(shell).then(async (cache) => {
      const response = await fetch("/lesson-shell-assets.json", {
        cache: "no-store",
      });
      const files = await response.json();
      const assets = Array.from(
        new Set([
          "/index.html",
          ...files.filter(
            (path) =>
              path === "/index.html" ||
              /^\/assets\/[A-Za-z0-9_.-]+$/.test(path),
          ),
        ]),
      );
      await cache.addAll(assets);
    }),
  );
  self.skipWaiting();
});
self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});
self.addEventListener("fetch", (event) => {
  const request = event.request;
  const url = new URL(request.url);
  if (request.method !== "GET" || url.origin !== self.location.origin) return;
  if (request.mode === "navigate") {
    event.respondWith(
      fetch(request).catch(() =>
        caches.open(shell).then((cache) =>
          cache.match("/index.html", {
            ignoreVary: true,
            ignoreSearch: true,
          }),
        ),
      ),
    );
  } else if (url.pathname.startsWith("/assets/")) {
    event.respondWith(
      caches.open(shell).then(async (cache) => {
        const hit =
          (await cache.match(request, {
            ignoreVary: true,
            ignoreSearch: true,
          })) ||
          (await cache.match(url.pathname, {
            ignoreVary: true,
            ignoreSearch: true,
          }));
        if (hit) return hit;
        return fetch(request).catch(() => new Response(null, { status: 404 }));
      }),
    );
  }
});
