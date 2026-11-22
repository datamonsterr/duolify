/* global self, caches, URL, fetch, Response */
// Public shell only. Private APIs, documents and asset payloads stay in the host's
// account-scoped cache and can never be served by a service-worker URL.
//
// What is cached, and when (see build/offlineManifest.ts):
//   install        the shell: /index.html and what the first screen loads.
//   lesson opened  the page posts "warm-lesson-routes" (also on a course download): the player,
//   or downloaded  lesson-test and course-list chunks are cached, and from then on every new
//                  service-worker version caches them on install too. An opened or downloaded
//                  lesson therefore plays offline although those chunks are not in the shell.
//   first use      any other hashed /assets/ file is cached when fetched (cache-first).
// The registration URL carries a build id, so each deploy installs a fresh worker and cache;
// activate drops the caches of older builds.
const version = new URL(self.location.href).searchParams.get("v") || "dev";
const shell = `duolify-public-shell-${version}`;
const meta = "duolify-public-shell-meta";
const warmedKey = "/__lesson-routes-warmed";
const prefix = "duolify-public-shell";
const safePath = (path) =>
  path === "/index.html" || /^\/assets\/[A-Za-z0-9_.-]+$/.test(path);

async function manifest() {
  const response = await fetch("/lesson-shell-assets.json", {
    cache: "no-store",
  });
  const { shell: shellFiles = [], offline = [] } = await response.json();
  return {
    shell: Array.from(new Set(shellFiles.filter(safePath))),
    offline: Array.from(new Set(offline.filter(safePath))),
  };
}
async function addMissing(cache, paths) {
  const missing = [];
  for (const path of paths) {
    if (!(await cache.match(path, { ignoreSearch: true }))) missing.push(path);
  }
  if (missing.length) await cache.addAll(missing);
}
async function wasWarmed() {
  const cache = await caches.open(meta);
  return Boolean(await cache.match(warmedKey));
}

// The sandboxed slide host is an iframe with an opaque origin, which this worker never sees. It is served
// immutable under a per-build URL, so fetching it here puts it in the HTTP cache for offline playback.
const primeLessonFrame = () =>
  fetch(`/lesson-frame.html?v=${encodeURIComponent(version)}`).catch(() => {});

self.addEventListener("install", (event) => {
  event.waitUntil(
    (async () => {
      await primeLessonFrame();
      const files = await manifest();
      const cache = await caches.open(shell);
      await addMissing(
        cache,
        (await wasWarmed()) ? [...files.shell, ...files.offline] : files.shell,
      );
    })(),
  );
  self.skipWaiting();
});
self.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      for (const name of await caches.keys()) {
        if (name.startsWith(prefix) && name !== shell && name !== meta)
          await caches.delete(name);
      }
      await self.clients.claim();
    })(),
  );
});
self.addEventListener("message", (event) => {
  if (event.data?.type !== "warm-lesson-routes") return;
  const reply = (value) => event.ports[0]?.postMessage(value);
  event.waitUntil(
    (async () => {
      try {
        await primeLessonFrame();
        const files = await manifest();
        await addMissing(await caches.open(shell), files.offline);
        await (await caches.open(meta)).put(warmedKey, new Response("1"));
        reply("warmed");
      } catch {
        reply("failed");
      }
    })(),
  );
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
        try {
          const response = await fetch(request);
          // A missing hashed file is answered with index.html (200); never cache that.
          const type = response.headers.get("content-type") || "";
          if (response.ok && !type.includes("text/html"))
            event.waitUntil(cache.put(request, response.clone()));
          return response;
        } catch {
          return new Response(null, { status: 404 });
        }
      }),
    );
  }
});
