import { AuthError, session } from "../accounts/session";
import { isManifest, type Manifest, type ManifestSlide } from "./manifest";
import { clearDownloadIndex, isLessonSavedOffline } from "./library";
const cacheName = "duolify-player-v1";
const maxBundle = 8 * 1024 * 1024;
const DEFAULT_MAX_ACCOUNT = 16 * 1024 * 1024;
const DEFAULT_MAX_LESSONS = 5;

let configuredMaxLessons = DEFAULT_MAX_LESSONS;
let configuredMaxAccount = DEFAULT_MAX_ACCOUNT;

export function configureCacheBounds(options: {
  maxLessons?: number;
  maxAccount?: number;
}) {
  if (options.maxLessons !== undefined)
    configuredMaxLessons = options.maxLessons;
  if (options.maxAccount !== undefined)
    configuredMaxAccount = options.maxAccount;
}

export function resetCacheBounds() {
  configuredMaxLessons = DEFAULT_MAX_LESSONS;
  configuredMaxAccount = DEFAULT_MAX_ACCOUNT;
}
type Asset = { url: string; mime: string; data: string };
type Document = { html: string; assets: Asset[] };
export type LessonBundle = {
  manifest: Manifest;
  documents: Record<string, Document>;
};
let epoch = 0;
let mutations: Promise<unknown> = Promise.resolve();
const prepared = new Map<string, string>();
const saved = new Set<string>();
class CacheWriteError extends Error {
  constructor(
    public bundle: LessonBundle,
    options?: ErrorOptions,
  ) {
    super("cache_unavailable", options);
  }
}
function serialize<T>(operation: () => Promise<T>): Promise<T> {
  const result = mutations.catch(() => {}).then(operation);
  mutations = result;
  return result;
}
function key(owner: string, lesson: string) {
  return `${location.origin}/__lesson_cache/${encodeURIComponent(owner)}/${encodeURIComponent(lesson)}`;
}
function documentKey(slide: ManifestSlide) {
  return `${session.current()?.id}:${slide.html_url}:${slide.html_sha256}`;
}
export function preparedDocument(slide: ManifestSlide) {
  if (!session.current()?.id) return undefined;
  return prepared.get(documentKey(slide));
}
export function availableOffline(lesson: string) {
  const owner = session.current()?.id;
  if (!owner) return false;
  if (saved.has(`${owner}:${lesson}`)) return true;
  try {
    if (
      typeof localStorage !== "undefined" &&
      localStorage.getItem(`duolify:lesson-cached:${lesson}`) === "true"
    ) {
      return true;
    }
  } catch {
    // ignore
  }
  return isLessonSavedOffline(owner, lesson);
}
export async function digest(value: string | Uint8Array): Promise<string> {
  const bytes =
    typeof value === "string" ? new TextEncoder().encode(value) : value;
  return Array.from(
    new Uint8Array(
      await crypto.subtle.digest("SHA-256", new Uint8Array(bytes)),
    ),
    (b) => b.toString(16).padStart(2, "0"),
  ).join("");
}
function bytes(data: string) {
  return Uint8Array.from(atob(data), (c) => c.charCodeAt(0));
}
async function verify(bundle: LessonBundle) {
  if (
    !isManifest(bundle.manifest) ||
    !bundle.documents ||
    Object.keys(bundle.documents).length !== bundle.manifest.slides.length
  )
    throw new Error("invalid_manifest");
  for (const slide of bundle.manifest.slides) {
    const doc = bundle.documents[slide.slide_id];
    if (
      !doc ||
      typeof doc.html !== "string" ||
      doc.html.length > 1048576 ||
      (await digest(doc.html)) !== slide.html_sha256
    )
      throw new Error("checksum_mismatch");
    if (!Array.isArray(doc.assets) || doc.assets.length !== slide.assets.length)
      throw new Error("checksum_mismatch");
    const seen = new Set<string>();
    for (const asset of doc.assets) {
      if (
        !slide.assets.includes(asset.url) ||
        seen.has(asset.url) ||
        typeof asset.data !== "string" ||
        !/^(image\/(png|jpeg|gif|webp|svg\+xml)|audio\/[a-z0-9.+-]+|video\/[a-z0-9.+-]+|font\/[a-z0-9.+-]+|application\/(octet-stream|font-woff)|text\/(css|javascript))$/.test(
          asset.mime,
        ) ||
        (await digest(bytes(asset.data))) !== slide.asset_sha256[asset.url]
      )
        throw new Error("checksum_mismatch");
      seen.add(asset.url);
    }
  }
}
export function materialize(
  bundle: LessonBundle,
  slide: ManifestSlide,
): string {
  const doc = bundle.documents[slide.slide_id];
  // Assets must be self-contained, just like the authored HTML. A network-denying
  // CSP blocks undeclared nested references even in CSS, SVG or script bodies.
  let html = doc.html;
  for (const asset of doc.assets)
    html = html
      .split(asset.url)
      .join(`data:${asset.mime};base64,${asset.data}`);
  return html;
}
function activate(bundle: LessonBundle, owner: string, durable: boolean) {
  if (session.current()?.id !== owner) throw new Error("session_changed");
  prepared.clear();
  for (const slide of bundle.manifest.slides)
    prepared.set(documentKey(slide), materialize(bundle, slide));
  if (durable) {
    saved.add(`${owner}:${bundle.manifest.lesson_id}`);
    try {
      if (typeof localStorage !== "undefined") {
        localStorage.setItem(
          `duolify:lesson-cached:${bundle.manifest.lesson_id}`,
          "true",
        );
      }
    } catch {
      // ignore
    }
  }
  return bundle.manifest;
}
export async function downloadLesson(
  manifest: Manifest,
  owner: string,
): Promise<LessonBundle> {
  const expected = epoch;
  const documents: Record<string, Document> = {};
  // Fetch in order: the immediate next slide is ready before distant slides.
  for (const slide of manifest.slides) {
    if (expected !== epoch || session.current()?.id !== owner)
      throw new Error("session_changed");
    const value = await session.request<Document>(slide.html_url);
    documents[slide.slide_id] = {
      html: value.html,
      assets: value.assets ?? [],
    };
  }
  const bundle = { manifest, documents };
  await verify(bundle);
  const raw = JSON.stringify(bundle);
  if (new TextEncoder().encode(raw).length > maxBundle)
    throw new Error("cache_unavailable");
  try {
    await serialize(async () => {
      if (expected !== epoch || session.current()?.id !== owner)
        throw new Error("session_changed");
      const cache = await caches.open(cacheName);
      const target = key(owner, manifest.lesson_id);
      await cache.put(
        target,
        new Response(raw, { headers: { "Content-Type": "application/json" } }),
      );
      if (expected !== epoch || session.current()?.id !== owner) {
        await cache.delete(target);
        throw new Error("session_changed");
      }
      // Evict only after publication succeeds. Quota failures preserve valid data.
      const keys = (await cache.keys()).filter(
        (k) => k.url.startsWith(key(owner, "")) && k.url !== target,
      );
      let size = new TextEncoder().encode(raw).length;
      let count = 1;
      for (const request of keys.reverse()) {
        const response = await cache.match(request);
        const length = response ? (await response.arrayBuffer()).byteLength : 0;
        if (
          ++count > configuredMaxLessons ||
          (size += length) > configuredMaxAccount
        ) {
          await cache.delete(request);
          const match = request.url.match(/__lesson_cache\/[^/]+\/([^/?#]+)/);
          if (match && match[1]) {
            const evictedLesson = decodeURIComponent(match[1]);
            saved.delete(`${owner}:${evictedLesson}`);
            try {
              if (typeof localStorage !== "undefined") {
                localStorage.removeItem(
                  `duolify:lesson-cached:${evictedLesson}`,
                );
              }
            } catch {
              // ignore
            }
          }
        }
      }
      saved.add(`${owner}:${manifest.lesson_id}`);
      try {
        if (typeof localStorage !== "undefined") {
          localStorage.setItem(
            `duolify:lesson-cached:${manifest.lesson_id}`,
            "true",
          );
        }
      } catch {
        // ignore
      }
    });
  } catch (error) {
    if (expected !== epoch || session.current()?.id !== owner)
      throw new Error("session_changed", { cause: error });
    if (error instanceof Error && error.message === "session_changed")
      throw error;
    throw new CacheWriteError(bundle, { cause: error });
  }
  return bundle;
}
export async function readCachedLesson(
  owner: string,
  lesson: string,
): Promise<LessonBundle | null> {
  const expected = epoch;
  if (session.current()?.id !== owner) return null;
  try {
    const response = await (
      await caches.open(cacheName)
    ).match(key(owner, lesson));
    if (!response) return null;
    const raw = await response.text();
    if (new TextEncoder().encode(raw).length > maxBundle) return null;
    const bundle = JSON.parse(raw) as LessonBundle;
    await verify(bundle);
    if (
      bundle.manifest.lesson_id !== lesson ||
      expected !== epoch ||
      session.current()?.id !== owner
    )
      return null;
    return bundle;
  } catch {
    return null;
  }
}
export async function openLesson(lesson: string): Promise<Manifest> {
  const owner = session.current()?.id;
  const expected = epoch;
  if (!owner) throw new Error("offline_uncached");
  let value: unknown;
  try {
    value = await session.request(
      `/v1/lessons/${encodeURIComponent(lesson)}/manifest`,
    );
  } catch (error) {
    if (error instanceof AuthError && error.status === 404) {
      try {
        const dyn = await session.request<{
          id: string;
          course_id?: string;
          title?: string;
          slides?: {
            slide_id: string;
            html: string;
            duration_ms: number;
            narration: string;
          }[];
        }>(`/v1/lessons/${encodeURIComponent(lesson)}`);
        if (dyn?.slides && dyn.slides.length > 0) {
          const slides: ManifestSlide[] = await Promise.all(
            dyn.slides.map(async (s) => {
              const html_sha256 = await digest(s.html);
              return {
                slide_id: s.slide_id,
                html_sha256,
                asset_sha256: {},
                duration_ms: s.duration_ms || 3000,
                html_url: `/v1/lessons/${encodeURIComponent(lesson)}/slides/${encodeURIComponent(s.slide_id)}`,
                assets: [],
                checkpoints: [],
                narration_status: "disabled" as const,
                narration_text: s.narration || "",
                explanation: s.narration || "",
              };
            }),
          );
          const synthesized: Manifest = {
            lesson_id: dyn.id || lesson,
            course_id: dyn.course_id || "course",
            title: dyn.title || "Lesson",
            version: 1,
            asset_origin: "",
            slides,
          };
          if (expected !== epoch || session.current()?.id !== owner)
            throw new Error("session_changed", { cause: error });
          for (const s of slides) {
            const raw = dyn.slides.find((x) => x.slide_id === s.slide_id);
            const html = raw?.html ?? "";
            prepared.set(documentKey(s), html);
          }
          return synthesized;
        }
      } catch {
        // Fall through to error
      }
      throw error;
    }
    if (error instanceof AuthError && [401, 403].includes(error.status))
      throw error;
    const cached = await readCachedLesson(owner, lesson);
    if (cached) return activate(cached, owner, true);
    throw new Error(
      navigator.onLine ? "manifest_unavailable" : "offline_uncached",
      { cause: error },
    );
  }
  if (!isManifest(value) || value.lesson_id !== lesson)
    throw new Error("invalid_manifest");
  try {
    const bundle = await downloadLesson(value, owner);
    return activate(bundle, owner, true);
  } catch (error) {
    if (session.current()?.id !== owner) throw error;
    const cached = await readCachedLesson(owner, lesson);
    if (cached) return activate(cached, owner, true);
    if (error instanceof CacheWriteError)
      return activate(error.bundle, owner, false);
    if (navigator.onLine) return value;
    throw error;
  }
}
export function clearLessonCache(): Promise<void> {
  epoch++;
  prepared.clear();
  saved.clear();
  try {
    if (typeof localStorage !== "undefined") {
      for (const k of Object.keys(localStorage)) {
        if (k.startsWith("duolify:lesson-cached:")) {
          localStorage.removeItem(k);
        }
      }
    }
  } catch {
    // ignore
  }
  resetCacheBounds();
  clearDownloadIndex();
  return serialize(async () => {
    if (typeof caches !== "undefined") await caches.delete(cacheName);
  });
}

export function removeCachedLesson(
  owner: string,
  lesson: string,
): Promise<void> {
  saved.delete(`${owner}:${lesson}`);
  try {
    if (typeof localStorage !== "undefined") {
      localStorage.removeItem(`duolify:lesson-cached:${lesson}`);
    }
  } catch {
    // ignore
  }
  return serialize(async () => {
    if (typeof caches !== "undefined") {
      const cache = await caches.open(cacheName);
      await cache.delete(key(owner, lesson));
    }
  });
}
